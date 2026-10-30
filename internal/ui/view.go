package ui

import (
	"fmt"
	"math"
	"strings"

	"chessvision/internal/app"
	"chessvision/internal/chess"
	"chessvision/internal/gfx"
	"chessvision/internal/vision"
)

var moveTimes = []int{300, 500, 800, 1000, 1500, 2000, 3000, 5000, 8000, 0}

func moveTimeLabel(ms int) string {
	if ms == 0 {
		return "до смены позиции"
	}
	return fmt.Sprintf("%.1f с", float64(ms)/1000)
}

// cardSpec describes the static panels of the window.
var cardSpecs = []struct {
	x, y, w, h float64
	title      string
}{
	{24, 68, 588, 640, "Позиция"},
	{636, 68, 460, 240, "Управление"},
	{636, 320, 460, 280, "Настройки"},
	{636, 612, 460, 96, "Ходы партии"},
}

// paintChrome draws the unchanging backdrop (gradient, glow, panels with their
// shadows and titles). It is rendered once per scale and blitted every frame,
// which is what keeps the interface smooth.
func (u *UI) paintChrome(p P) {
	w, h := float64(LogicalW), float64(LogicalH)
	p.C.Clear(colBg0)
	p.Round(0, 0, w, h, 0, p.Lin(0, 0, w*0.4, h, gfx.Stop{Pos: 0, Color: colBg0}, gfx.Stop{Pos: 1, Color: colBg1}))
	p.Circle(150, 40, 520, p.Rad(150, 40, 520, gfx.Stop{Pos: 0, Color: colViolet.Alpha(0.16)}, gfx.Stop{Pos: 1, Color: colViolet.Alpha(0)}))
	p.Circle(w-120, h-60, 560, p.Rad(w-120, h-60, 560, gfx.Stop{Pos: 0, Color: colCyan.Alpha(0.16)}, gfx.Stop{Pos: 1, Color: colCyan.Alpha(0)}))
	u.building = true
	for _, c := range cardSpecs {
		u.card(p, c.x, c.y, c.w, c.h, c.title)
	}
	u.boardBase(p, 92, 124, boardSize)
	u.building = false
}

func (u *UI) draw(p P) {
	w, h := float64(LogicalW), float64(LogicalH)

	if u.chrome == nil || u.chrome.Rect != p.C.Img.Rect || u.chromeScale != u.S {
		cc := gfx.NewCanvas(p.C.Img.Rect.Dx(), p.C.Img.Rect.Dy())
		u.paintChrome(P{C: cc, S: p.S})
		u.chrome, u.chromeScale = cc.Img, u.S
	}
	copy(p.C.Img.Pix, u.chrome.Pix)

	u.drawTitle(p)
	u.drawBoardCard(p, 24, 68, 588, 640)
	u.drawControlCard(p, 636, 68, 460, 240)
	u.drawSettingsCard(p, 636, 320, 460, 280)
	u.drawMovesCard(p, 636, 612, 460, 96)
	u.drawStatusBar(p)

	// window frame
	p.Stroke(0, 0, w, h, 0, 1, colLineHi)
}

// ---- title bar ---------------------------------------------------------------

func (u *UI) drawTitle(p P) {
	m := &u.m
	// logo
	p.Shadow(24, 12, 34, 34, 11, 18, 0, 5, colViolet.Alpha(0.5))
	p.Round(24, 12, 34, 34, 11, p.Lin(24, 12, 58, 46, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: colCyan}))
	path := u.sym.GlyphPath(0x265E, 25*p.S, 0, 0)
	minX, minY, maxX, maxY := path.Bounds()
	ox, oy := 41*p.S-(minX+maxX)/2, 29.5*p.S-(minY+maxY)/2
	for _, c := range path.C {
		for i := range c {
			c[i].X += ox
			c[i].Y += oy
		}
	}
	p.C.Fill(path, gfx.Solid(gfx.Hex(0xFFFFFF)))
	p.Text(u.bold, 19, 72, 22, "ChessVision", colText)
	p.Text(u.reg, 12.5, 72, 41, "помощник по ходам", colMuted)

	// status chip
	var label string
	var dot gfx.Color
	switch {
	case m.Running && m.HavePos:
		label, dot = "Анализ идёт", colGreen
	case m.Running:
		label, dot = "Ищу позицию…", colAmber
	case m.Calibrated:
		label, dot = "Пауза", colAmber
	default:
		label, dot = "Не настроено", colFaint
	}
	cw := p.W(u.reg, 13.5, label) + 44
	cx := float64(LogicalW) - 24 - 3*40 - 18 - cw
	p.Shadow(cx, 14, cw, 30, 15, 12, 0, 3, glass(0.10))
	p.RoundC(cx, 14, cw, 30, 15, paper(0.92))
	p.Stroke(cx, 14, cw, 30, 15, 1, colLine)
	pulse := 0.0
	if m.Running {
		pulse = 0.5 + 0.5*math.Sin(u.now()*3.2)
		u.animating = true
		p.CircleC(cx+19, 29, 5+4*pulse, dot.Alpha(0.22*(1-pulse)+0.05))
	}
	p.CircleC(cx+19, 29, 4.2, dot)
	p.Text(u.reg, 13.5, cx+32, 29, label, colText)

	// window buttons
	bx := float64(LogicalW) - 24 - 3*40
	u.titleButton(p, "pin", bx, func(p P, cx, cy float64, col gfx.Color) { p.iconPin(cx, cy, 15, m.Cfg.AlwaysOnTop, col) }, m.Cfg.AlwaysOnTop, false, func() {
		u.Ctl.SetAlwaysOnTop(!m.Cfg.AlwaysOnTop)
		if u.OnPin != nil {
			u.OnPin(!m.Cfg.AlwaysOnTop)
		}
	})
	u.titleButton(p, "min", bx+40, func(p P, cx, cy float64, col gfx.Color) { p.iconMin(cx, cy, 15, col) }, false, false, func() {
		if u.OnMinimize != nil {
			u.OnMinimize()
		}
	})
	u.titleButton(p, "close", bx+80, func(p P, cx, cy float64, col gfx.Color) { p.iconClose(cx, cy, 15, col) }, false, true, func() {
		if u.OnClose != nil {
			u.OnClose()
		}
	})
}

func (u *UI) titleButton(p P, id string, x float64, icon func(P, float64, float64, gfx.Color), on, danger bool, act func()) {
	hv := u.ease(id+"/h", b2f(u.isHot(id)), 16)
	bg := glass(0.09 * hv)
	if danger {
		bg = colRed.Alpha(0.85 * hv)
	}
	p.RoundC(x+2, 12, 36, 34, 10, bg)
	col := gfx.Mix(colMuted, colText, hv)
	if on {
		col = colViolet
	}
	if danger && hv > 0.5 {
		col = gfx.Hex(0xFFFFFF)
	}
	icon(p, x+20, 29, col)
	u.hit(id, x+2, 12, 36, 34)
	u.actions[id] = act
}

// ---- board card ----------------------------------------------------------------

const boardSize = 464.0

func (u *UI) drawBoardCard(p P, x, y, w, h float64) {
	m := &u.m
	u.card(p, x, y, w, h, "Позиция")

	// turn chip + spinner
	turn := "Ход не определён"
	var tcol gfx.Color = colFaint
	if m.HavePos {
		if m.Result != "" {
			turn, tcol = m.Result, colAmber
		} else if m.Pos.Side == chess.White {
			turn, tcol = "Ход белых", gfx.Hex(0xFFFFFF)
		} else {
			turn, tcol = "Ход чёрных", gfx.Hex(0x2A2F45)
		}
	}
	chipW := p.W(u.reg, 13.5, turn) + 40
	cx := x + w - 24 - chipW
	p.Shadow(cx, y+14, chipW, 28, 14, 12, 0, 3, glass(0.10))
	p.RoundC(cx, y+14, chipW, 28, 14, paper(0.95))
	p.Stroke(cx, y+14, chipW, 28, 14, 1, colLine)
	if m.HavePos && m.Result == "" {
		p.CircleC(cx+17, y+28, 6.5, tcol)
		p.Ring(cx+17, y+28, 6.5, 1.2, glass(0.45))
	} else {
		p.CircleC(cx+17, y+28, 4.5, tcol)
	}
	p.Text(u.reg, 13.5, cx+31, y+28, turn, colText)
	if m.Analyzing {
		p.iconSpinner(cx-22, y+28, 8, u.now(), colCyan)
		u.animating = true
	}

	bx, by := x+68.0, y+56
	u.drawEvalBar(p, x+36, by, 18, boardSize)
	u.drawBoard(p, bx, by, boardSize)
	u.drawBestPanel(p, x+36, by+boardSize+14, w-36-24, 92)
}

func (u *UI) drawEvalBar(p P, x, y, w, h float64) {
	m := &u.m
	target := 0.5
	if m.HaveInfo {
		target = 1 / (1 + math.Exp(-0.55*m.EvalWhite))
	}
	share := u.ease("evalbar", target, 6)
	p.RoundC(x, y, w, h, 9, gfx.Hex(0x232A45))
	p.Stroke(x, y, w, h, 9, 1, colLine)
	// white share sits at the bottom when White plays from the bottom
	wh := (h - 4) * share
	p.Clip(x, y, w, h)
	if m.Cfg.WhiteAtBottom {
		p.Round(x+2, y+h-2-wh, w-4, wh+8, 7, p.Lin(0, y+h-wh, 0, y+h, gfx.Stop{Pos: 0, Color: gfx.Hex(0xF4F6FC)}, gfx.Stop{Pos: 1, Color: gfx.Hex(0xBFC8E2)}))
	} else {
		p.Round(x+2, y+2-8, w-4, wh+8, 7, p.Lin(0, y, 0, y+wh, gfx.Stop{Pos: 0, Color: gfx.Hex(0xBFC8E2)}, gfx.Stop{Pos: 1, Color: gfx.Hex(0xF4F6FC)}))
	}
	p.Unclip()
	p.Rect(x+3, y+h/2-0.5, w-6, 1, gfx.RGBA(124, 92, 255, 160))
}

func (u *UI) squareRect(bx, by, size float64, sq int) (float64, float64) {
	s := size / 8
	f, r := float64(chess.File(sq)), float64(chess.Rank(sq))
	if u.m.Cfg.WhiteAtBottom {
		return bx + f*s, by + (7-r)*s
	}
	return bx + (7-f)*s, by + r*s
}

// boardBase paints the board squares, shadow and gloss. The pattern is
// identical for both orientations, so it lives in the cached backdrop.
func (u *UI) boardBase(p P, bx, by, size float64) {
	s := size / 8
	p.Shadow(bx, by, size, size, 12, 40, 0, 16, gfx.RGBA(40, 55, 120, 120))
	p.Clip(bx, by, size, size)
	for sq := 0; sq < 64; sq++ {
		x, y := bx+float64(chess.File(sq))*s, by+float64(7-chess.Rank(sq))*s
		col := colBoardDk
		if (chess.File(sq)+chess.Rank(sq))%2 == 1 {
			col = colBoardLt
		}
		p.Rect(x, y, s, s, col)
	}
	// subtle gloss
	p.Round(bx, by, size, size, 0, p.Lin(bx, by, bx+size, by+size, gfx.Stop{Pos: 0, Color: gfx.RGBA(255, 255, 255, 22)}, gfx.Stop{Pos: 0.5, Color: gfx.RGBA(255, 255, 255, 0)}, gfx.Stop{Pos: 1, Color: gfx.RGBA(0, 0, 20, 36)}))
	p.Unclip()
}

func (u *UI) drawBoard(p P, bx, by, size float64) {
	m := &u.m
	s := size / 8
	p.Clip(bx, by, size, size)
	// last move
	if m.LastMove != chess.NoMove {
		for _, sq := range []int{m.LastMove.From(), m.LastMove.To()} {
			x, y := u.squareRect(bx, by, size, sq)
			p.Rect(x, y, s, s, colAmber.Alpha(0.34))
		}
	}
	// best move squares (fade with the arrow)
	for _, st := range []struct {
		mv chess.Move
		k  float64
	}{{u.arrow.old, u.arrow.oldA}, {u.arrow.cur, u.arrowVis()}} {
		if st.mv == chess.NoMove || st.k <= 0.005 {
			continue
		}
		for i, sq := range []int{st.mv.From(), st.mv.To()} {
			x, y := u.squareRect(bx, by, size, sq)
			a := 0.30
			if i == 1 {
				a = 0.46
			}
			p.Rect(x, y, s, s, colViolet.Alpha(a*st.k))
		}
	}
	// coordinates
	for i := 0; i < 8; i++ {
		file, rank := i, i
		fx, fy := u.squareRect(bx, by, size, chess.Rank(0)*8+file) // bottom edge squares via rank 0 or 7
		_ = fx
		_ = fy
		var sqF, sqR int
		if m.Cfg.WhiteAtBottom {
			sqF, sqR = file, rank*8
		} else {
			sqF, sqR = 7*8+file, rank*8+7
		}
		x, y := u.squareRect(bx, by, size, sqF)
		light := (chess.File(sqF)+chess.Rank(sqF))%2 == 1
		cc := colBoardLt
		if light {
			cc = colBoardDk
		}
		p.Text(u.bold, 10.5, x+s-12, y+s-9, string(rune('a'+chess.File(sqF))), cc.Alpha(0.9))
		x, y = u.squareRect(bx, by, size, sqR)
		light = (chess.File(sqR)+chess.Rank(sqR))%2 == 1
		cc = colBoardLt
		if light {
			cc = colBoardDk
		}
		p.Text(u.bold, 10.5, x+5, y+11, string(rune('1'+chess.Rank(sqR))), cc.Alpha(0.9))
	}
	// pieces
	board := vision.StartPlacement()
	faded := true
	switch {
	case m.HavePos:
		board, faded = m.Pos.Board, false
	case m.HaveBoard:
		board, faded = m.Board, false
	}
	px := int(math.Round(s * p.S))
	for sq := 0; sq < 64; sq++ {
		pc := board[sq]
		if pc == chess.NoPiece {
			continue
		}
		x, y := u.squareRect(bx, by, size, sq)
		sprite := u.sprite(pc, px)
		op := 1.0
		if faded {
			op = 0.28
		}
		p.C.DrawRGBA(sprite, imagePt(x*p.S, y*p.S), op)
	}
	// best move arrow (animated)
	u.drawArrow(p, bx, by, size, u.arrow.old, 1, u.arrow.oldA)
	u.drawArrow(p, bx, by, size, u.arrow.cur, easeOut(u.arrow.t), u.arrowVis())
	p.Unclip()
	p.Stroke(bx, by, size, size, 0, 1, colLineHi)

	// overlays on the board
	switch {
	case !m.HavePos && !m.HaveBoard:
		msg := "Откройте шахматную доску и нажмите «Начать анализ»"
		sub := "Доска и фигуры определяются автоматически, в любой позиции"
		if m.Cfg.HasRegion && m.Calibrated {
			msg, sub = "Нажмите «Начать анализ» и откройте партию", "Позиция появится здесь"
		} else if m.Cfg.HasRegion {
			msg, sub = "Нажмите «Начать анализ»", "Программа сама найдёт доску и определит фигуры"
		}
		bw := math.Max(p.W(u.bold, 17, msg), p.W(u.reg, 12.5, sub)) + 48
		p.Shadow(bx+size/2-bw/2, by+size/2-40, bw, 80, 18, 30, 0, 10, glass(0.28))
		p.RoundC(bx+size/2-bw/2, by+size/2-40, bw, 80, 18, paper(0.96))
		p.Stroke(bx+size/2-bw/2, by+size/2-40, bw, 80, 18, 1, colLineHi)
		p.TextC(u.bold, 17, bx+size/2, by+size/2-10, msg, colText)
		p.TextC(u.reg, 12.5, bx+size/2, by+size/2+16, sub, colMuted)
	case m.Result != "":
		bw := p.W(u.bold, 20, m.Result) + 56
		p.Shadow(bx+size/2-bw/2, by+size/2-26, bw, 52, 26, 30, 0, 10, glass(0.28))
		p.RoundC(bx+size/2-bw/2, by+size/2-26, bw, 52, 26, paper(0.96))
		p.Stroke(bx+size/2-bw/2, by+size/2-26, bw, 52, 26, 1, colAmber.Alpha(0.7))
		p.TextC(u.bold, 20, bx+size/2, by+size/2, m.Result, colAmber)
	case m.Running && m.HavePos && !m.Cfg.HintBoth && ((m.Pos.Side == chess.White) != m.Cfg.WhiteAtBottom):
		label := "Ход соперника"
		bw := p.W(u.reg, 13.5, label) + 44
		p.RoundC(bx+size/2-bw/2, by+10, bw, 30, 15, paper(0.96))
		p.Stroke(bx+size/2-bw/2, by+10, bw, 30, 15, 1, colLineHi)
		p.iconSpinner(bx+size/2-bw/2+18, by+25, 7, u.now()*0.7, colAmber)
		u.animating = true
		p.Text(u.reg, 13.5, bx+size/2-bw/2+32, by+25, label, colText)
	}
}

// ---- best move panel ------------------------------------------------------------

func squareLabel(sq int) string { return chess.SqName(sq) }

func (u *UI) drawBestPanel(p P, x, y, w, h float64) {
	m := &u.m
	p.RoundC(x, y, w, h, 16, gfx.Hex(0xF0F3FB))
	p.Stroke(x, y, w, h, 16, 1, colLine)
	p.Text(u.bold, 11, x+20, y+17, "ЛУЧШИЙ ХОД", colMuted)

	switch {
	case m.Best != chess.NoMove:
		from, to := squareLabel(m.Best.From()), squareLabel(m.Best.To())
		fw := p.W(u.bold, 36, from)
		p.Text(u.bold, 36, x+20, y+50, from, colText)
		p.iconArrowRight(x+20+fw+26, y+50, 22, colGreen)
		p.Text(u.bold, 36, x+20+fw+52, y+50, to, colText)
		tw := p.W(u.bold, 36, to)
		if pr := m.Best.Promo(); pr != chess.NoType {
			p.Text(u.bold, 20, x+20+fw+52+tw+6, y+54, "="+string("NBRQ"[int(pr)-1]), colAmber)
			tw += 40
		}
		// SAN pill
		sx := x + 20 + fw + 52 + tw + 18
		sw := p.W(u.bold, 17, m.BestSAN) + 26
		p.RoundC(sx, y+34, sw, 32, 16, colGreen.Alpha(0.15))
		p.Stroke(sx, y+34, sw, 32, 16, 1, colGreen.Alpha(0.5))
		p.Text(u.bold, 17, sx+13, y+50, m.BestSAN, colGreen)
		// evaluation on the right
		u.drawEval(p, x+w-20, y+30, 24)
		info := ""
		if m.HaveInfo {
			info = fmt.Sprintf("глубина %d · %s · %s", m.Info.Depth, nodesStr(m.Info.Nodes), m.EngineName)
		}
		p.Text(u.reg, 12, x+20, y+h-16, p.Fit(u.reg, 12, w-40, firstNonEmpty(m.PVText, info)), colMuted)
		if m.PVText != "" && info != "" {
			// show engine info on the right of the label row
			p.TextR(u.reg, 11.5, x+w-20, y+17, p.Fit(u.reg, 11.5, w-180, info), colFaint)
		}
	case m.Result != "":
		p.Text(u.bold, 26, x+20, y+50, m.Result, colAmber)
	case m.Running && m.HavePos && m.Analyzing:
		p.Text(u.bold, 26, x+20, y+48, "Анализирую позицию…", colText)
		p.iconSpinner(x+w-40, y+46, 11, u.now(), colCyan)
		u.animating = true
	case m.Running && m.HavePos:
		p.Text(u.bold, 24, x+20, y+48, "Жду ход соперника", colText)
		p.Text(u.reg, 12.5, x+20, y+h-16, "Подсказка появится, когда придёт ваша очередь (или включите «Все ходы»)", colMuted)
	case m.Running:
		p.Text(u.bold, 24, x+20, y+48, "Ищу позицию на экране…", colText)
		p.Text(u.reg, 12.5, x+20, y+h-16, "Убедитесь, что доска видна целиком и не закрыта окнами", colMuted)
	default:
		p.Text(u.bold, 24, x+20, y+48, "Нажмите «Начать анализ»", colText)
		p.Text(u.reg, 12.5, x+20, y+h-16, "Подсказки появятся здесь и стрелкой на доске", colMuted)
	}
}

func (u *UI) drawEval(p P, rx, cy, size float64) {
	m := &u.m
	if !m.HaveInfo {
		return
	}
	v := m.Info.ScoreString()
	// from the user's point of view
	my := m.EvalWhite
	if !m.Cfg.WhiteAtBottom {
		my = -my
	}
	col := colMuted
	switch {
	case m.Info.IsMate:
		col = colAmber
	case my > 0.35:
		col = colGreen
	case my < -0.35:
		col = colRed
	}
	if m.InfoSTM != chess.White && m.Cfg.WhiteAtBottom || m.InfoSTM == chess.White && !m.Cfg.WhiteAtBottom {
		// info is from the side to move; show it from the user's side
		if !m.Info.IsMate {
			v = fmt.Sprintf("%+.2f", -float64(m.Info.ScoreCP)/100)
		} else if m.Info.Mate > 0 {
			v = fmt.Sprintf("#-%d", m.Info.Mate)
		} else {
			v = fmt.Sprintf("#%d", -m.Info.Mate)
		}
	}
	w := p.W(u.bold, size, v) + 28
	p.RoundC(rx-w, cy-4, w, 40, 14, col.Alpha(0.13))
	p.Stroke(rx-w, cy-4, w, 40, 14, 1, col.Alpha(0.45))
	p.TextC(u.bold, size, rx-w/2, cy+16, v, col)
}

func nodesStr(n uint64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1f M узлов", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%d K узлов", n/1000)
	}
	return fmt.Sprintf("%d узлов", n)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- control card ----------------------------------------------------------------

func (u *UI) drawControlCard(p P, x, y, w, h float64) {
	m := &u.m
	u.card(p, x, y, w, h, "Управление")

	// steps
	steps := []struct {
		label string
		done  bool
	}{{"Доска", m.Cfg.HasBoard}, {"Фигуры", m.Calibrated}, {"Анализ", m.Running}}
	cur := 2
	for i, s := range steps {
		if !s.done {
			cur = i
			break
		}
	}
	sx := x + 24
	sy := y + 62
	colw := (w - 48) / 3
	for i, s := range steps {
		cx := sx + colw*float64(i)
		col := colFaint
		switch {
		case s.done:
			col = colGreen
		case i == cur:
			col = colCyan
		}
		if i < 2 {
			lx := cx + 30 + p.W(u.reg, 13, s.label) + 8
			p.Line(lx, sy, cx+colw-12, sy, 2, gfx.Mix(glass(0.14), colGreen, b2f(s.done)*0.6))
		}
		if i == cur && !s.done {
			p.CircleC(cx+10, sy, 15, col.Alpha(0.16+0.08*math.Sin(u.now()*3)))
			u.animating = true
		}
		p.CircleC(cx+10, sy, 10.5, col.Alpha(0.18))
		p.Ring(cx+10, sy, 10.5, 1.5, col)
		if s.done {
			p.iconCheck(cx+10, sy, 10, col)
		} else {
			p.TextC(u.bold, 12, cx+10, sy, fmt.Sprintf("%d", i+1), col)
		}
		p.Text(u.reg, 13, cx+28, sy, s.label, gfx.Mix(colMuted, colText, b2f(s.done || i == cur)))
	}

	// primary action
	label := "Начать анализ"
	icon := func(p P, cx, cy, s float64, c gfx.Color) { p.iconPlay(cx, cy, s, c) }
	if m.Running {
		label = "Пауза"
		icon = func(p P, cx, cy, s float64, c gfx.Color) { p.iconPause(cx, cy, s, c) }
	}
	u.button(p, "run", x+24, y+90, w-48, 54, label, icon, btnPrimary, true, func() { go u.Ctl.ToggleRun() })

	bw := (w - 48 - 12) / 2
	u.button(p, "region", x+24, y+156, bw, 42, "Указать область", func(p P, cx, cy, s float64, c gfx.Color) { p.iconRegion(cx, cy, s, c) }, btnSecondary, true, func() { go u.Ctl.SelectRegion() })
	u.button(p, "calib", x+24+bw+12, y+156, bw, 42, "Найти заново", func(p P, cx, cy, s float64, c gfx.Color) { p.iconTarget(cx, cy, s, c) }, btnSecondary, true, func() { go u.Ctl.Calibrate() })

	tw := (w - 48 - 16) / 3
	u.button(p, "fenin", x+24, y+208-8, tw, 30, "FEN из буфера", func(p P, cx, cy, s float64, c gfx.Color) { p.iconPaste(cx, cy, s, c) }, btnGhost, true, func() { go u.Ctl.CalibrateFEN() })
	u.button(p, "fenout", x+24+tw+8, y+208-8, tw, 30, "Копировать FEN", func(p P, cx, cy, s float64, c gfx.Color) { p.iconCopy(cx, cy, s, c) }, btnGhost, m.HavePos, func() { go u.Ctl.CopyFEN() })
	u.button(p, "reset", x+24+2*(tw+8), y+208-8, tw, 30, "Сброс партии", func(p P, cx, cy, s float64, c gfx.Color) { p.iconReset(cx, cy, s, c) }, btnGhost, true, func() { go u.Ctl.ResetGame() })
}

// ---- settings card -----------------------------------------------------------------

func (u *UI) drawSettingsCard(p P, x, y, w, h float64) {
	m := &u.m
	u.card(p, x, y, w, h, "Настройки")
	lx := x + 24
	cw := w - 48
	segW := 236.0
	sx := x + w - 24 - segW

	p.Text(u.reg, 14, lx, y+73, "Я играю", colMuted)
	// the side is read from the screen (board orientation / piece colours)
	p.RoundC(sx, y+58, segW, 30, 15, gfx.Hex(0xE8ECF7))
	p.Stroke(sx, y+58, segW, 30, 15, 1, colLine)
	p.Round(sx+3, y+61, segW-6, 24, 12, p.Lin(sx, 0, sx+segW, 0, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: gfx.Mix(colViolet, colCyan, 0.75)}))
	p.TextC(u.bold, 13.5, sx+segW/2, y+73, "Авто", colWhite)

	p.Text(u.reg, 14, lx, y+115, "Подсказки", colMuted)
	sel := 0
	if m.Cfg.HintBoth {
		sel = 1
	}
	u.segmented(p, "hint", sx, y+100, segW, 30, []string{"Мои ходы", "Все ходы"}, sel, nil, func(i int) { u.Ctl.SetHintBoth(i == 1) })

	p.Text(u.reg, 14, lx, y+157, "Движок", colMuted)
	sel = 0
	if m.Cfg.UseExternal {
		sel = 1
	}
	en := []bool{true, m.ExternalFound}
	u.segmented(p, "engine", sx, y+142, segW, 30, []string{"Встроенный", "Stockfish"}, sel, en, func(i int) { u.Ctl.SetUseExternal(i == 1) })

	hy := y + 191
	switch {
	case m.DL.Active:
		p.RoundC(lx, hy-4, cw, 8, 4, glass(0.10))
		p.Round(lx, hy-4, math.Max(8, cw*m.DL.Frac), 8, 4, p.Lin(lx, 0, lx+cw, 0, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: colCyan}))
		p.Text(u.reg, 11.5, lx, hy+14, p.Fit(u.reg, 11.5, cw, m.DL.Msg), colMuted)
	case !m.ExternalFound:
		p.Text(u.reg, 12, lx, hy, p.Fit(u.reg, 12, cw-176, "Stockfish заметно сильнее"), colFaint)
		u.button(p, "dl", x+w-24-164, hy-13, 164, 26, "Скачать Stockfish", func(p P, cx, cy, s float64, c gfx.Color) { p.iconDownload(cx, cy, s, c) }, btnGhost, true, func() { go u.Ctl.DownloadEngine() })
	default:
		name := m.EngineName
		if !m.Cfg.UseExternal {
			name = "Найден: " + shortName(m.Cfg.ExternalPath)
		}
		p.Text(u.reg, 12, lx, hy, p.Fit(u.reg, 12, cw, name), colFaint)
	}

	// think time slider
	sy := y + 226
	p.Text(u.reg, 14, lx, sy, "Время на ход", colMuted)
	idx := 0
	for i, v := range moveTimes {
		if v == m.Cfg.MoveTimeMs {
			idx = i
		}
	}
	u.slider(p, "think", sx, sx+segW-64, sy, idx, len(moveTimes), func(i int) { u.Ctl.SetMoveTime(moveTimes[i]) })
	p.TextR(u.bold, 14, x+w-24, sy, moveTimeLabelShort(m.Cfg.MoveTimeMs), colText)

	// toggles
	ty := y + 244
	half := cw / 2
	u.toggle(p, "t_overlay", lx, ty, half-8, "Стрелка на доске", m.Cfg.Overlay, func(v bool) { u.Ctl.SetOverlay(v) })
	u.toggle(p, "t_ru", lx+half, ty, half, "Русская нотация", m.Cfg.Russian, func(v bool) { u.Ctl.SetRussian(v) })
}

func moveTimeLabelShort(ms int) string {
	if ms == 0 {
		return "∞"
	}
	return fmt.Sprintf("%.1f с", float64(ms)/1000)
}

func shortName(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	return path[i+1:]
}

// slider draws a stepped slider; value is an index in [0,n).
func (u *UI) slider(p P, id string, x0, x1, cy float64, idx, n int, set func(i int)) {
	frac := float64(idx) / float64(n-1)
	pos := u.ease(id+"/pos", frac, 20)
	hv := u.ease(id+"/h", b2f(u.isHot(id) || u.isActive(id)), 14)
	p.RoundC(x0, cy-3, x1-x0, 6, 3, glass(0.12))
	for i := 0; i < n; i++ {
		tx := x0 + (x1-x0)*float64(i)/float64(n-1)
		p.CircleC(tx, cy, 1.6, glass(0.28))
	}
	fx := x0 + (x1-x0)*pos
	p.Round(x0, cy-3, math.Max(fx-x0, 6), 6, 3, p.Lin(x0, 0, x1, 0, gfx.Stop{Pos: 0, Color: colViolet}, gfx.Stop{Pos: 1, Color: colCyan}))
	p.Circle(fx, cy, 15, p.Rad(fx, cy, 15, gfx.Stop{Pos: 0, Color: colCyan.Alpha(0.3 * (0.4 + hv))}, gfx.Stop{Pos: 1, Color: colCyan.Alpha(0)}))
	p.CircleC(fx+0, cy+1, 9.5, glass(0.16))
	p.CircleC(fx, cy, 8.5, gfx.Hex(0xFFFFFF))
	p.Ring(fx, cy, 8.2, 1, colViolet.Alpha(0.55))
	p.CircleC(fx, cy, 4, colViolet)
	u.hit(id, x0-12, cy-16, x1-x0+24, 32)
	u.drags[id] = func(mx float64) {
		f := (mx - x0) / (x1 - x0)
		i := int(math.Round(f * float64(n-1)))
		if i < 0 {
			i = 0
		}
		if i > n-1 {
			i = n - 1
		}
		if i != idx {
			idx = i
			set(i)
		}
	}
}

// ---- moves card -----------------------------------------------------------------------

func (u *UI) drawMovesCard(p P, x, y, w, h float64) {
	m := &u.m
	u.card(p, x, y, w, h, "Ходы партии")
	hist := m.History
	if len(hist) == 0 {
		p.Text(u.reg, 13, x+24, y+64, "Ходы появятся по мере игры", colFaint)
		return
	}
	// show the last lines in two columns, newest at the end
	const perCol = 2
	maxLines := perCol * 3
	start := 0
	if len(hist) > maxLines {
		start = len(hist) - maxLines
	}
	lines := hist[start:]
	colW := (w - 48) / 3
	for i, hl := range lines {
		c, r := i/perCol, i%perCol
		cx := x + 24 + float64(c)*colW
		cy := y + 54 + float64(r)*22
		p.Text(u.reg, 13, cx, cy, fmt.Sprintf("%d.", hl.Num), colFaint)
		wh := hl.White
		if wh == "" {
			wh = "…"
		}
		p.Text(u.bold, 13.5, cx+26, cy, wh, colText)
		p.Text(u.bold, 13.5, cx+26+p.W(u.bold, 13.5, wh)+10, cy, hl.Black, colMuted)
	}
}

// ---- status bar -------------------------------------------------------------------------

func (u *UI) drawStatusBar(p P) {
	m := &u.m
	y := float64(LogicalH) - 36
	col := colMuted
	dot := colCyan
	switch m.StatusKind {
	case app.StatusOK:
		dot = colGreen
	case app.StatusWarn:
		dot = colAmber
	case app.StatusError:
		dot = colRed
	}
	_ = col
	p.CircleC(34, y+18, 4, dot)
	hint := "F8 — старт/пауза   ·   F9 — найти доску заново   ·   Esc — отмена выбора"
	hw := p.W(u.reg, 12, hint)
	p.Text(u.reg, 13, 48, y+18, p.Fit(u.reg, 13, float64(LogicalW)-48-hw-70, m.Status), colText)
	p.TextR(u.reg, 12, float64(LogicalW)-24, y+18, hint, colFaint)
}
