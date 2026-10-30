// mkandroidicon writes the launcher icons of the Android app.
package main

import (
	"image/png"
	"os"
	"path/filepath"

	"chessvision/internal/ui"
)

func main() {
	res := filepath.Join("android", "app", "src", "main", "res")
	for dir, size := range map[string]int{"mipmap-mdpi": 48, "mipmap-hdpi": 72, "mipmap-xhdpi": 96, "mipmap-xxhdpi": 144, "mipmap-xxxhdpi": 192} {
		_ = os.MkdirAll(filepath.Join(res, dir), 0o755)
		f, err := os.Create(filepath.Join(res, dir, "ic_launcher.png"))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, ui.RenderIcon(size)); err != nil {
			panic(err)
		}
		f.Close()
	}
}
