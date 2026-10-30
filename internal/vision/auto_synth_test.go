package vision

import (
	"image"
	"image/draw"
	"math/rand"
	"testing"

	"chessvision/internal/boardimg"
	"chessvision/internal/chess"
)

func randomGame(r *rand.Rand, plies int) chess.Position {
	p := chess.Start()
	for i := 0; i < plies; i++ {
		ms := p.LegalMoves()
		if len(ms) == 0 {
			break
		}
		m := ms[r.Intn(len(ms))]
		q, ok := p.Make(m)
		if !ok {
			break
		}
		p = q
	}
	return p
}

func TestAutoLearnSynthetic(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	fails := 0
	total := 0
	for n := 0; n < 24; n++ {
		pos := randomGame(r, 12+r.Intn(70))
		cnt := 0
		for _, pc := range pos.Board {
			if pc != chess.NoPiece {
				cnt++
			}
		}
		if cnt < 6 {
			continue
		}
		style := boardimg.Green
		if n%2 == 1 {
			style = boardimg.Brown
		}
		white := n%3 != 0
		sq := []int{44, 56, 70, 90}[n%4]
		img, rect := boardimg.Render(style, pos.Board, boardimg.Options{Square: sq, Margin: 14, WhiteAtBottom: white})
		scr := image.NewRGBA(image.Rect(0, 0, img.Rect.Dx()+260, img.Rect.Dy()+120))
		for i := 0; i < len(scr.Pix); i += 4 {
			scr.Pix[i], scr.Pix[i+1], scr.Pix[i+2], scr.Pix[i+3] = 38, 36, 33, 255
		}
		draw.Draw(scr, img.Rect.Add(image.Pt(110, 60)), img, image.Point{}, draw.Src)
		det, conf := DetectBoard(scr)
		want := rect.Add(image.Pt(110, 60))
		res, err := AutoLearn(scr, det, nil)
		total++
		bad := err != nil || res.Placement != pos.Board || res.WhiteAtBottom != white
		if bad {
			fails++
			if err == nil {
				for sq := 0; sq < 64; sq++ {
					if res.Placement[sq] != pos.Board[sq] {
						t.Logf("  %s got %d want %d", chess.SqName(sq), res.Placement[sq], pos.Board[sq])
					}
				}
			}
			t.Errorf("case %d (%v sq=%d white=%v pieces=%d): det=%v want=%v conf=%.2f err=%v res=%v\nFEN %s", n, style, sq, white, cnt, det, want, conf, err, res, pos.FEN())
		}
	}
	t.Logf("%d/%d failed", fails, total)
}
