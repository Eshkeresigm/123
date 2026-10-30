# CI smoke test for the packaged desktop app (Windows runner).
# Diagnostics are emitted as workflow annotations because step logs are not
# retrievable by the author. Exit code 0 only if the PORTABLE exe works.
$ErrorActionPreference = 'Continue'

function Annotate($level, $text) { Write-Host ("::$level::" + ($text -replace "`r?`n", '%0A')) }
function Tail($f, $n = 700) {
  if (Test-Path $f) { $t = Get-Content $f -Raw; if ($t) { return $t.Substring([Math]::Max(0, $t.Length - $n)) } }
  return ''
}
function Stop-App { Get-Process | Where-Object { $_.ProcessName -like 'ProVpn*' } | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep 2 }

function Try-Launch($label, $path) {
  Stop-App
  $env:PROVPN_PORT = '17980'
  $env:ELECTRON_ENABLE_LOGGING = '1'
  $r = [ordered]@{ label = $label; ok = $false; note = '' }
  if (-not (Test-Path $path)) { $r.note = "missing $path"; return $r }
  $p = Start-Process -FilePath $path -PassThru -RedirectStandardError "err-$label.txt" -RedirectStandardOutput "out-$label.txt"
  $state = $null
  for ($i = 0; $i -lt 25 -and -not $state; $i++) {
    Start-Sleep -Seconds 2
    try { $state = Invoke-RestMethod http://127.0.0.1:17980/api/state -TimeoutSec 2 } catch {}
    if ($p.HasExited -and $i -ge 4) { break }
  }
  $r.exited = $p.HasExited
  if ($p.HasExited) { $r.exitCode = $p.ExitCode }
  if (-not $state) {
    $r.note = 'not serving'
  } else {
    $r.platform = $state.platform
    $page = Invoke-WebRequest http://127.0.0.1:17980/ -UseBasicParsing
    $r.ui = ($page.Content -match 'ProVpn')
    Start-Sleep -Seconds 4
    $wins = Get-Process | Where-Object { $_.ProcessName -like 'ProVpn*' -and $_.MainWindowTitle } | ForEach-Object { "$($_.ProcessName)=[$($_.MainWindowTitle)]" }
    $r.windows = ($wins -join ';')
    $r.ok = ($state.platform -eq 'win32') -and $r.ui -and [bool]($wins | Where-Object { $_ -like '*=[[]ProVpn]' })
  }
  $r.err = Tail "err-$label.txt"
  $r.out = Tail "out-$label.txt"
  return $r
}

$results = @()
$results += , (Try-Launch 'portable' 'dist\ProVpn.exe')
if (-not $results[0].ok) { $results += , (Try-Launch 'unpacked' 'dist\win-unpacked\ProVpn.exe') }
Stop-App

$summary = ($results | ForEach-Object { ($_.GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" }) -join ' ' }) -join ' || '
if ($results[0].ok) { Annotate 'notice' "SMOKE OK | $summary"; exit 0 }
Annotate 'error' "SMOKE FAILED | $summary"
exit 1
