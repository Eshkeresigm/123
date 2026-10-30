package engine

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectWindowsAssets(t *testing.T) {
	js := `{"tag_name":"sf_18","assets":[
	 {"name":"stockfish-ubuntu-x86-64-avx2.tar","browser_download_url":"u1"},
	 {"name":"stockfish-windows-x86-64-bmi2.zip","browser_download_url":"u2"},
	 {"name":"stockfish-windows-x86-64-sse41-popcnt.zip","browser_download_url":"u3"},
	 {"name":"stockfish-windows-x86-64-avx2.zip","browser_download_url":"u4","digest":"sha256:abc"},
	 {"name":"stockfish-windows-x86-64.zip","browser_download_url":"u5"},
	 {"name":"stockfish-windows-x86-64-avx512.zip","browser_download_url":"u6"}]}`
	a, tag, err := SelectWindowsAssets([]byte(js))
	if err != nil || tag != "sf_18" {
		t.Fatal(err, tag)
	}
	want := []string{"u4", "u3", "u5"}
	if len(a) != len(want) {
		t.Fatalf("got %d assets: %+v", len(a), a)
	}
	for i, w := range want {
		if a[i].URL != w {
			t.Errorf("asset %d = %s want %s", i, a[i].URL, w)
		}
	}
	if _, _, err := SelectWindowsAssets([]byte(`{"assets":[{"name":"x.tar"}]}`)); err == nil {
		t.Error("expected error when no windows asset")
	}
}

func TestExtractEngine(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, size := range map[string]int{"stockfish/AUTHORS": 10, "stockfish/stockfish-windows-x86-64-avx2.exe": 5000, "stockfish/src/readme.exe": 10} {
		w, _ := zw.Create(name)
		w.Write(bytes.Repeat([]byte{'x'}, size))
	}
	zw.Close()
	dst := filepath.Join(t.TempDir(), "sub", "stockfish.exe")
	if err := ExtractEngine(buf.Bytes(), dst); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dst)
	if err != nil || st.Size() != 5000 {
		t.Fatalf("bad extraction: %v %v", st, err)
	}
}
