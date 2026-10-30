package app

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"time"

	"chessvision/internal/chess"
	"chessvision/internal/vision"
)

// Optional platform abilities.
type ownRecter interface{ OwnRects() []image.Rectangle }
type screener interface{ ScreenBounds() image.Rectangle }
type selfHider interface{ SetSelfVisible(bool) }
type windowMover interface {
	MoveAway(board image.Rectangle) bool
}

// hideSelf makes our own window transparent so that a capture shows the real
// board even where the window lies over it. The returned func restores it.
// ok is false when the platform cannot do this (then blankOwn is the fallback).
func (c *Controller) hideSelf() (restore func(), ok bool) {
	h, can := c.plat.(selfHider)
	if !can {
		return func() {}, false
	}
	h.SetSelfVisible(false)
	time.Sleep(140 * time.Millisecond) // let the compositor repaint the screen
	return func() { h.SetSelfVisible(true) }, true
}

// minBoardPx is the smallest board (in pixels) the shape recognition can use.
const minBoardPx = 8 * 38

func (c *Controller) screenArea() image.Rectangle {
	if s, ok := c.plat.(screener); ok {
		if r := s.ScreenBounds(); !r.Empty() {
			return r
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m.Cfg.HasRegion {
		r := c.m.Cfg.Region
		return image.Rect(r[0], r[1], r[2], r[3]).Inset(-200)
	}
	return image.Rect(0, 0, 1920, 1080)
}

// blankOwn paints our own window grey so the program never mistakes its
// own interface (which contains a board) for the game.
func (c *Controller) blankOwn(img *image.RGBA, origin image.Point) {
	o, ok := c.plat.(ownRecter)
	if !ok {
		return
	}
	grey := color.RGBA{120, 120, 120, 255}
	for _, r := range o.OwnRects() {
		r = r.Sub(origin).Intersect(img.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, grey)
			}
		}
	}
}

// locateBoard captures the screen (or the user's region when it holds a
// board), finds the chessboard and stores its position. The returned image
// is the capture and rect is the board inside it.
func (c *Controller) locateBoard() (*image.RGBA, image.Rectangle, error) {
	c.hideOverlayNow()
	time.Sleep(90 * time.Millisecond)
	restore, hidden := c.hideSelf()
	defer restore()

	c.mu.Lock()
	has := c.m.Cfg.HasRegion
	reg := c.m.Cfg.Region
	wide := c.wideSearch
	c.wideSearch = false
	c.mu.Unlock()

	var areas []image.Rectangle
	if has {
		// Search around the remembered region, but generously: the stored
		// rectangle may itself be off by a square or two, and a tight crop
		// would then hide the real board and lock the mistake in.
		r := image.Rect(reg[0], reg[1], reg[2], reg[3])
		margin := 24
		if wide {
			margin = max(24, r.Dx()/8*5/2)
		}
		areas = append(areas, r.Inset(-margin).Intersect(c.screenArea()))
	}
	areas = append(areas, c.screenArea())

	var lastErr error = errors.New("шахматная доска на экране не найдена: откройте доску целиком и повторите")
	for _, area := range areas {
		if area.Empty() {
			continue
		}
		img, err := c.plat.Capture(area)
		if err != nil {
			lastErr = fmt.Errorf("не удалось снять экран: %w", err)
			continue
		}
		if !hidden {
			c.blankOwn(img, area.Min)
		}
		rect, conf := vision.DetectBoard(img)
		if conf < 0.62 {
			continue
		}
		if rect.Dx() < minBoardPx {
			lastErr = fmt.Errorf("доска слишком мала на экране (%d px): увеличьте её (клетка не меньше ~40 px)", rect.Dx())
			continue
		}
		// finishCalibration turns (Region origin + rect) into the board rectangle
		c.mu.Lock()
		c.m.Cfg.HasRegion = true
		c.m.Cfg.Region = [4]int{area.Min.X, area.Min.Y, area.Max.X, area.Max.Y}
		c.mu.Unlock()
		return img, rect, nil
	}
	return nil, image.Rectangle{}, lastErr
}

// AutoSetup finds the board and learns the pieces from the current position,
// whatever it is. It returns true when recognition is ready.
func (c *Controller) AutoSetup() bool {
	c.mu.Lock()
	if c.setupBusy {
		c.mu.Unlock()
		return false
	}
	c.setupBusy = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.setupBusy = false
		c.mu.Unlock()
	}()
	c.setStatus(StatusInfo, "Ищу шахматную доску на экране…")
	img, rect, err := c.locateBoard()
	if err != nil {
		c.setStatus(StatusWarn, err.Error())
		return false
	}
	c.setStatus(StatusInfo, "Доска найдена, определяю фигуры…")

	// 1. the initial position: labels are certain
	start := vision.StartPlacement()
	if m := vision.OccupancyMatch(img, rect, true, start); m >= 0.99 {
		white := vision.WhiteAtBottom(img, rect)
		prof := &vision.Profile{}
		if err := prof.Learn(img, rect, start, white); err == nil {
			if brighter, sure := prof.WhiteBrighter(); sure && !brighter {
				white = !white
				prof = &vision.Profile{}
				if err := prof.Learn(img, rect, start, white); err != nil {
					c.setStatus(StatusError, "Калибровка: "+err.Error())
					return false
				}
			}
			c.finishCalibration(prof, img, rect, white, chess.Start(), false, "Начальная позиция распознана, фигуры выучены. Можно играть")
			return true
		}
	}

	// 2. any other position: name the pieces by their shape
	res, err := vision.AutoLearn(img, rect, nil)
	if err != nil {
		c.dumpDebug(img, "calibration_debug.png")
		c.setStatus(StatusWarn, "Не удалось определить фигуры: "+err.Error()+". Проверьте, что доска видна целиком и фигуры не закрыты")
		return false
	}
	// Sanity: white pawns sit lower than black ones. If the opposite is
	// overwhelmingly true the colours were read upside down, so turn the
	// reading around (the cells stay the same, only the labelling changes).
	if sk, ok := pawnSkew(res.Placement); ok && sk > 2.5 {
		res.Placement = rotateBoard(res.Placement)
		res.WhiteAtBottom = !res.WhiteAtBottom
		res.SideKnown = false
	}
	side := chess.White
	known := res.SideKnown
	if known {
		side = res.Side
	} else if !res.WhiteAtBottom {
		side = chess.Black // assume it is the user's move
	}
	pos, err2 := chess.FromBoard(res.Placement, side, 15, -1)
	if err2 != nil {
		side = side.Opp()
		pos, err2 = chess.FromBoard(res.Placement, side, 15, -1)
		known = err2 == nil // only one side can be on move: a king is in check
	}
	if err2 != nil {
		c.setStatus(StatusWarn, "Распознанная расстановка недопустима: "+err2.Error())
		return false
	}
	prof := &vision.Profile{}
	if err := prof.Learn(img, rect, res.Placement, res.WhiteAtBottom); err != nil {
		c.setStatus(StatusError, "Калибровка: "+err.Error())
		return false
	}
	synth := prof.FillMissing()
	if !prof.Ready() {
		c.setStatus(StatusWarn, "Не удалось выучить все типы фигур по этой позиции — оставляю прежнюю калибровку")
		return false
	}
	msg := "Фигуры определены автоматически по форме"
	if !known {
		msg += "; чей ход — не определён, предполагаю ваш"
	}
	if synth > 0 {
		msg += fmt.Sprintf("; %d типов фигур на доске нет, достроены по форме", synth)
	}
	if res.Conf < 0.2 {
		msg += ". Сверьтесь с панелью «Позиция»: если расстановка неверна, начните партию с начальной позиции и нажмите «Калибровка»"
	}
	c.finishCalibration(prof, img, rect, res.WhiteAtBottom, pos, !known, msg)
	return true
}

// maybeRelocate looks for the board again when it stops being recognised
// (window moved, page scrolled, board resized).
func (c *Controller) maybeRelocate() {
	c.mu.Lock()
	if c.relocating || time.Since(c.lastReloc) < 4*time.Second || !c.m.Running {
		c.mu.Unlock()
		return
	}
	c.relocating = true
	old := c.boardScreenRect()
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.relocating = false
		c.lastReloc = time.Now()
		c.mu.Unlock()
	}()

	area := c.screenArea()
	img, err := c.plat.Capture(area)
	if err != nil {
		return
	}
	c.blankOwn(img, area.Min)
	rect, conf := vision.DetectBoard(img)
	if conf < 0.62 || rect.Dx() < minBoardPx {
		return
	}
	abs := rect.Add(area.Min)
	if old.Dx() > 0 {
		if d := abs.Dx() - old.Dx(); d > old.Dx()/3 || -d > old.Dx()/3 {
			return // a different board
		}
		dx, dy, dw := abs.Min.X-old.Min.X, abs.Min.Y-old.Min.Y, abs.Dx()-old.Dx()
		if abs2(dx) < 3 && abs2(dy) < 3 && abs2(dw) < 4 {
			return // same place
		}
		// The checkerboard repeats every square, so a grid shifted by whole
		// squares looks just as good. Never accept such a jump while the
		// current board rectangle still looks like a board.
		sq := old.Dx() / 8
		whole := func(v int) bool { return v == 0 || abs2(abs2(v)%sq-0) < 5 || abs2(abs2(v)%sq-sq) < 5 }
		if whole(dx) && whole(dy) && abs2(dw) < sq/2 {
			return
		}
		if o := old.Sub(area.Min); o.In(img.Bounds()) && vision.GridScore(img, o) >= 0.62 {
			return
		}
	}
	c.mu.Lock()
	c.m.Cfg.Region = [4]int{abs.Min.X, abs.Min.Y, abs.Max.X, abs.Max.Y}
	c.m.Cfg.BoardRect = c.m.Cfg.Region
	c.m.Cfg.HasRegion, c.m.Cfg.HasBoard = true, true
	c.badFrames, c.lowQ = 0, 0
	c.setStatusLocked(StatusInfo, "Доска сдвинулась — нашёл её заново")
	c.mu.Unlock()
	c.saveConfig()
	c.notify()
}

func abs2(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
