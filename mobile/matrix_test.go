package cvmobile

import (
	"encoding/json"
	"image"
	"image/draw"
	"testing"
	"time"

	"chessvision/internal/boardimg"
	"chessvision/internal/chess"
)

// phone-like frames: 720 px wide, board edge to edge, UI above and below
func phone720(st boardimg.Style, b [64]chess.Piece, white bool, last chess.Move, dots []int) (w, h, stride int, pix []byte, board image.Rectangle) {
	img, rect := boardimg.Render(st, b, boardimg.Options{Square: 90, Margin: 0, WhiteAtBottom: white, LastMove: last, Dots: dots})
	w, h = 720, 1560
	scr := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(scr.Pix); i += 4 {
		scr.Pix[i], scr.Pix[i+1], scr.Pix[i+2], scr.Pix[i+3] = 38, 36, 34, 255
	}
	off := image.Pt(0, 330)
	draw.Draw(scr, img.Rect.Add(off), img, image.Point{}, draw.Src)
	stride = w * 4
	return w, h, stride, scr.Pix, rect.Add(off)
}

func TestPhoneMatrix(t *testing.T) {
	styles := map[string]boardimg.Style{"green": boardimg.Green, "brown": boardimg.Brown}
	for name, st := range styles {
		for _, white := range []bool{true, false} {
			host := &fakeHost{}
			s := NewSession(host)
			pos := chess.Start()
			var last chess.Move
			for _, mv := range []string{"e2e4", "c7c5", "g1f3", "d7d6", "d2d4", "c5d4", "f3d4", "g8f6"} {
				m, _ := pos.ParseUCI(mv)
				pos, _ = pos.Make(m)
				last = m
			}
			e5, _ := chess.ParseSquare("e5")
			d3, _ := chess.ParseSquare("d3")
			dots := []int{e5, d3}
			w, h, stride, pix, board := phone720(st, pos.Board, white, last, dots)
			s.PushFrame(w, h, stride, pix)
			s.Toggle()
			var stt state
			deadline := time.Now().Add(40 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(200 * time.Millisecond)
				_ = json.Unmarshal([]byte(s.State()), &stt)
				if stt.Running && (stt.Best != "" || !white && stt.Analyzing == false && stt.Scans > 3) {
					break
				}
			}
			got := image.Rect(stt.Board[0], stt.Board[1], stt.Board[2], stt.Board[3])
			_, perr := pos.ParseUCI(stt.Best)
			t.Logf("%s white=%v: running=%v best=%s legal=%v white=%v board=%v want=%v status=%q", name, white, stt.Running, stt.Best, perr == nil, stt.White, got, board, stt.Status)
			if (white && perr != nil) || stt.White != white {
				t.Errorf("%s white=%v failed", name, white)
			}
			s.Close()
		}
	}
}
