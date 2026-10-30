# Downloads sing-box (latest stable of the pinned series, windows-amd64) and wintun.dll into ..\core
# Used by CI and build.cmd. Needs internet. Prints the resolved version.
param([string]$Series = '1.11')
$ErrorActionPreference = 'Stop'

$core = Join-Path (Split-Path $PSScriptRoot -Parent) 'core'
New-Item -ItemType Directory -Force $core | Out-Null

$headers = @{ 'User-Agent' = 'provpn-build' }
if ($env:GITHUB_TOKEN) { $headers.Authorization = "Bearer $env:GITHUB_TOKEN" }

$rx = '^v' + [regex]::Escape($Series) + '\.(\d+)$'
$release = $null
for ($page = 1; $page -le 8 -and -not $release; $page++) {
  $list = Invoke-RestMethod -Headers $headers "https://api.github.com/repos/SagerNet/sing-box/releases?per_page=100&page=$page"
  if (-not $list -or $list.Count -eq 0) { break }
  $cands = @($list | Where-Object { -not $_.prerelease -and -not $_.draft -and $_.tag_name -match $rx })
  if ($cands.Count -gt 0) {
    $release = $cands | Sort-Object { [int]($_.tag_name -replace '^v\d+\.\d+\.', '') } -Descending | Select-Object -First 1
  }
}
if (-not $release) { throw "No stable sing-box $Series.x release found" }

$asset = $release.assets | Where-Object { $_.name -match '^sing-box-[\d.]+-windows-amd64\.zip$' } | Select-Object -First 1
if (-not $asset) { throw "windows-amd64 asset missing in $($release.tag_name)" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("provpn-core-" + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
Invoke-WebRequest $asset.browser_download_url -OutFile (Join-Path $tmp 'sb.zip') -Headers @{ 'User-Agent' = 'provpn-build' }
Expand-Archive (Join-Path $tmp 'sb.zip') -DestinationPath (Join-Path $tmp 'sb')
$exe = Get-ChildItem (Join-Path $tmp 'sb') -Recurse -Filter sing-box.exe | Select-Object -First 1
if (-not $exe) { throw 'sing-box.exe not found in the archive' }
Copy-Item $exe.FullName (Join-Path $core 'sing-box.exe') -Force

# wintun.dll: some sing-box builds need it next to the exe; harmless otherwise. Non-fatal if unavailable.
try {
  Invoke-WebRequest 'https://www.wintun.net/builds/wintun-0.14.1.zip' -OutFile (Join-Path $tmp 'wintun.zip') -Headers @{ 'User-Agent' = 'provpn-build' }
  Expand-Archive (Join-Path $tmp 'wintun.zip') -DestinationPath (Join-Path $tmp 'wintun')
  Copy-Item (Join-Path $tmp 'wintun\wintun\bin\amd64\wintun.dll') (Join-Path $core 'wintun.dll') -Force
} catch {
  Write-Warning "wintun.dll not bundled: $($_.Exception.Message)"
}

Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
$ver = & (Join-Path $core 'sing-box.exe') version | Select-Object -First 1
Write-Host "core: $($release.tag_name) | $ver | files: $((Get-ChildItem $core).Name -join ', ')"
