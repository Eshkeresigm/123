// mkref records piece silhouettes from a screenshot of a known position into
// internal/vision/refsets.gob (shape priors for recognising a new screen).
package main

import (
	"flag"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"

	"chessvision/internal/chess"
	"chessvision/internal/vision"
)

func main() {
	in := flag.String("in", "", "screenshot PNG")
	fen := flag.String("fen", "", "FEN of the position on the screenshot")
	name := flag.String("name", "recorded", "set name")
	out := flag.String("o", "internal/vision/refsets.gob", "output")
	crop := flag.String("crop", "", "x0,y0,x1,y1 crop (optional)")
	flag.Parse()
	f, err := os.Open(*in)
	if err != nil {
		log.Fatal(err)
	}
	im, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	img := image.NewRGBA(im.Bounds())
	draw.Draw(img, img.Rect, im, im.Bounds().Min, draw.Src)
	if *crop != "" {
		var x0, y0, x1, y1 int
		if _, err := fmtSscan(*crop, &x0, &y0, &x1, &y1); err != nil {
			log.Fatal(err)
		}
		c := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
		draw.Draw(c, c.Rect, img, image.Pt(x0, y0), draw.Src)
		img = c
	}
	pos, err := chess.ParseFEN(*fen)
	if err != nil {
		log.Fatal(err)
	}
	rect, conf := vision.DetectBoard(img)
	log.Printf("board %v conf %.2f", rect, conf)
	res, err := vision.AutoLearn(img, rect, nil)
	if err != nil {
		log.Fatal(err)
	}
	if res.Placement != pos.Board {
		log.Fatalf("auto recognition differs from the given FEN: %v", res)
	}
	rs, err := vision.BuildRefSet(*name, img, rect, pos.Board, res.WhiteAtBottom)
	if err != nil {
		log.Fatal(err)
	}
	data, err := vision.EncodeRefSets([]vision.RefSet{rs})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d bytes)", *out, len(data))
}
