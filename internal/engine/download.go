package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StockfishLatestAPI is the official release feed of Stockfish.
const StockfishLatestAPI = "https://api.github.com/repos/official-stockfish/Stockfish/releases/latest"

// ReleaseAsset is one downloadable file of a GitHub release.
type ReleaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:<hex>" on current GitHub releases
}

type release struct {
	Tag    string         `json:"tag_name"`
	Assets []ReleaseAsset `json:"assets"`
}

// SelectWindowsAssets orders the Windows x64 builds from the release JSON by
// preference: AVX2 first (fast and widely supported), then SSE4.1+POPCNT, then
// the generic build. BMI2 builds are skipped because they are slow on some AMD CPUs.
func SelectWindowsAssets(releaseJSON []byte) ([]ReleaseAsset, string, error) {
	var rel release
	if err := json.Unmarshal(releaseJSON, &rel); err != nil {
		return nil, "", err
	}
	prefs := []string{"windows-x86-64-avx2", "windows-x86-64-sse41-popcnt", "windows-x86-64-modern", "windows-x86-64.zip", "windows-x86-64.", "windows-x86-64"}
	var out []ReleaseAsset
	used := map[string]bool{}
	for _, pref := range prefs {
		for _, a := range rel.Assets {
			n := strings.ToLower(a.Name)
			if used[n] || !strings.HasSuffix(n, ".zip") || !strings.Contains(n, pref) || strings.Contains(n, "bmi2") || strings.Contains(n, "avx512") || strings.Contains(n, "vnni") || strings.Contains(n, "avxvnni") {
				continue
			}
			used[n] = true
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, rel.Tag, errors.New("в релизе Stockfish не найден архив для Windows x64")
	}
	return out, rel.Tag, nil
}

// ExtractEngine pulls the biggest stockfish*.exe out of a release archive.
func ExtractEngine(zipData []byte, destExe string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}
	var best *zip.File
	for _, f := range zr.File {
		n := strings.ToLower(filepath.Base(f.Name))
		if f.FileInfo().IsDir() || !strings.HasSuffix(n, ".exe") || !strings.Contains(n, "stockfish") {
			continue
		}
		if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
			best = f
		}
	}
	if best == nil {
		return errors.New("в архиве нет stockfish.exe")
	}
	rc, err := best.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(destExe), 0o755); err != nil {
		return err
	}
	tmp := destExe + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, destExe)
}

// DownloadStockfish fetches the newest official Stockfish for Windows into
// destDir and returns the path of the working executable. It tries several
// CPU variants until one starts and answers the UCI handshake.
func DownloadStockfish(ctx context.Context, destDir string, progress func(frac float64, msg string)) (string, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	client := &http.Client{Timeout: 0}
	get := func(url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ChessVision/1.0")
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("сервер ответил %s", resp.Status)
		}
		return resp, nil
	}
	progress(0.02, "Запрашиваю последний релиз Stockfish…")
	resp, err := get(StockfishLatestAPI)
	if err != nil {
		return "", fmt.Errorf("не удалось получить список релизов: %w", err)
	}
	meta, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	assets, tag, err := SelectWindowsAssets(meta)
	if err != nil {
		return "", err
	}
	exe := filepath.Join(destDir, "stockfish.exe")
	var lastErr error
	for i, a := range assets {
		progress(0.05, fmt.Sprintf("Скачиваю %s (%s)…", a.Name, tag))
		resp, err := get(a.URL)
		if err != nil {
			lastErr = err
			continue
		}
		var buf bytes.Buffer
		total := a.Size
		if total <= 0 {
			total = resp.ContentLength
		}
		chunk := make([]byte, 64*1024)
		for {
			n, rerr := resp.Body.Read(chunk)
			buf.Write(chunk[:n])
			if total > 0 {
				progress(0.05+0.85*float64(buf.Len())/float64(total), fmt.Sprintf("Скачано %.1f МБ", float64(buf.Len())/1e6))
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				err = rerr
				break
			}
		}
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if strings.HasPrefix(a.Digest, "sha256:") {
			sum := sha256.Sum256(buf.Bytes())
			if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimPrefix(a.Digest, "sha256:")) {
				lastErr = errors.New("контрольная сумма архива не совпала")
				continue
			}
		}
		progress(0.92, "Распаковываю…")
		if err := ExtractEngine(buf.Bytes(), exe); err != nil {
			lastErr = err
			continue
		}
		progress(0.96, "Проверяю запуск движка…")
		u, err := StartUCI(exe, UCIOptions{})
		if err != nil {
			lastErr = fmt.Errorf("вариант %d не запустился: %w", i+1, err)
			_ = os.Remove(exe)
			continue
		}
		_ = u.Close()
		progress(1, "Stockfish установлен")
		return exe, nil
	}
	if lastErr == nil {
		lastErr = errors.New("неизвестная ошибка")
	}
	return "", lastErr
}

// SortedByName is a small helper for deterministic engine lists.
func SortedByName(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}

var _ = time.Second
