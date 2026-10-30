package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"chessvision/internal/chess"
	"chessvision/internal/engine"
	"chessvision/internal/track"
	"chessvision/internal/vision"
)

// Version is shown in the interface.
const Version = "1.0"

// Controller owns the whole pipeline.
type Controller struct {
	mu   sync.Mutex
	m    Model
	plat Platform
	tr   *track.Tracker
	prof *vision.Profile

	cfgDir   string
	cacheDir string
	exeDir   string

	builtin  *engine.Builtin
	ext      *engine.UCI
	anCancel context.CancelFunc
	anGen    int

	overlayImg *image.RGBA
	overlayFor chess.Move
	overlayOn  bool
	ovGen      int        // generation of the running overlay animation
	ovAnim     bool       // an overlay animation is in progress
	ovShown    chess.Move // move currently drawn on the overlay window
	ovLevel    float64    // its opacity factor (0..1)

	badFrames  int
	flipVotes  int
	alignTick  int
	alignVotes int
	alignCand  image.Rectangle
	coverNote  bool
	lastMove   time.Time
	lowQ       int
	relocating bool
	setupBusy  bool
	wideSearch bool // next locateBoard looks around a stale region more widely
	lastReloc  time.Time
	stop       chan struct{}
	wg         sync.WaitGroup
	scanEvery  time.Duration
	changed    chan struct{}
}

// New builds a controller. Directories may be empty (then nothing is persisted).
func New(plat Platform, cfgDir, cacheDir, exeDir string) *Controller {
	c := &Controller{plat: plat, cfgDir: cfgDir, cacheDir: cacheDir, exeDir: exeDir,
		tr: track.New(), builtin: engine.NewBuiltin(64), stop: make(chan struct{}),
		scanEvery: 220 * time.Millisecond, changed: make(chan struct{}, 1)}
	c.m.Cfg = DefaultConfig()
	c.m.Version = Version
	c.loadConfig()
	if c.cfgDir != "" {
		if p, err := vision.LoadProfile(filepath.Join(c.cfgDir, "profile.bin")); err == nil && p.Ready() {
			c.prof = p
		}
	}
	c.refreshExternal()
	c.tr.SetAssumedSide(c.myColor())
	c.refreshCalibState()
	c.m.EngineName = c.builtin.Name()
	switch {
	case !c.m.Calibrated:
		c.setStatusLocked(StatusInfo, "Откройте шахматную доску и нажмите «Начать анализ»: программа сама найдёт доску и фигуры")
	default:
		c.setStatusLocked(StatusOK, "Готово. Нажмите «Старт», когда начнёте партию")
	}
	return c
}

// Changed returns a channel that receives a signal whenever the model changed.
func (c *Controller) Changed() <-chan struct{} { return c.changed }

func (c *Controller) notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

// Start launches the background scanner.
func (c *Controller) Start() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(c.scanEvery)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
				c.mu.Lock()
				run := c.m.Running
				c.mu.Unlock()
				if run {
					c.scanOnce()
				}
			}
		}
	}()
}

// Close stops everything.
func (c *Controller) Close() {
	close(c.stop)
	c.wg.Wait()
	c.mu.Lock()
	if c.anCancel != nil {
		c.anCancel()
	}
	ext := c.ext
	c.ext = nil
	c.mu.Unlock()
	c.hideOverlayNow()
	if ext != nil {
		ext.Close()
	}
	c.saveConfig()
}

// Snapshot returns a copy of the model.
func (c *Controller) Snapshot() Model {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m
}

// ---- persistence ---------------------------------------------------------

func (c *Controller) loadConfig() {
	if c.cfgDir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(c.cfgDir, "config.json"))
	if err != nil {
		return
	}
	cfg := DefaultConfig()
	if json.Unmarshal(data, &cfg) == nil {
		c.m.Cfg = cfg
	}
}

func (c *Controller) saveConfig() {
	if c.cfgDir == "" {
		return
	}
	c.mu.Lock()
	cfg := c.m.Cfg
	c.mu.Unlock()
	data, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.MkdirAll(c.cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(c.cfgDir, "config.json"), data, 0o644)
}

func (c *Controller) saveProfile() {
	if c.cfgDir == "" || c.prof == nil {
		return
	}
	_ = c.prof.Save(filepath.Join(c.cfgDir, "profile.bin"))
}

// SaveWindowPos stores the window position.
func (c *Controller) SaveWindowPos(x, y int) {
	c.mu.Lock()
	c.m.Cfg.WinX, c.m.Cfg.WinY, c.m.Cfg.HasWinPos = x, y, true
	c.mu.Unlock()
	c.saveConfig()
}

// ---- helpers --------------------------------------------------------------

func (c *Controller) myColor() chess.Color {
	if c.m.Cfg.WhiteAtBottom {
		return chess.White
	}
	return chess.Black
}

func (c *Controller) setStatusLocked(k StatusKind, s string) {
	c.m.Status, c.m.StatusKind = s, k
}

func (c *Controller) setStatus(k StatusKind, s string) {
	c.mu.Lock()
	c.setStatusLocked(k, s)
	c.mu.Unlock()
	c.notify()
}

func (c *Controller) refreshCalibState() {
	c.m.Calibrated = c.prof.Ready() && c.m.Cfg.HasBoard
	n := 0
	if c.prof != nil {
		for i := 0; i < 12; i++ {
			if len(c.prof.Samples[i]) > 0 {
				n++
			}
		}
	}
	c.m.Learned = n
}

func (c *Controller) refreshExternal() {
	var dirs []string
	if c.exeDir != "" {
		dirs = append(dirs, c.exeDir, filepath.Join(c.exeDir, "engines"))
	}
	if c.cacheDir != "" {
		dirs = append(dirs, filepath.Join(c.cacheDir, "engines"))
	}
	found := engine.FindExternalEngines(dirs...)
	if c.m.Cfg.ExternalPath != "" {
		if _, err := os.Stat(c.m.Cfg.ExternalPath); err != nil {
			c.m.Cfg.ExternalPath = ""
		}
	}
	if c.m.Cfg.ExternalPath == "" && len(found) > 0 {
		c.m.Cfg.ExternalPath = found[len(found)-1]
	}
	c.m.ExternalFound = c.m.Cfg.ExternalPath != ""
	if !c.m.ExternalFound {
		c.m.Cfg.UseExternal = false
	}
}

func (c *Controller) boardScreenRect() image.Rectangle {
	b := c.m.Cfg.BoardRect
	return image.Rect(b[0], b[1], b[2], b[3])
}

// activeEngine returns the engine to use, starting Stockfish lazily.
func (c *Controller) activeEngine() engine.Engine {
	c.mu.Lock()
	useExt := c.m.Cfg.UseExternal && c.m.Cfg.ExternalPath != ""
	path := c.m.Cfg.ExternalPath
	ext := c.ext
	c.mu.Unlock()
	if !useExt {
		return c.builtin
	}
	if ext != nil && ext.Path() == path {
		return ext
	}
	if ext != nil {
		ext.Close()
	}
	threads := runtime.NumCPU() - 1
	if threads < 1 {
		threads = 1
	}
	if threads > 8 {
		threads = 8
	}
	u, err := engine.StartUCI(path, engine.UCIOptions{Threads: threads, HashMB: 256})
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.m.Cfg.UseExternal = false
		c.m.EngineName = c.builtin.Name()
		c.setStatusLocked(StatusWarn, "Stockfish не запустился ("+err.Error()+"), использую встроенный движок")
		c.ext = nil
		return c.builtin
	}
	c.ext = u
	c.m.EngineName = u.Name()
	return u
}

// ---- commands -------------------------------------------------------------

// ToggleRun starts or pauses scanning.
func (c *Controller) ToggleRun() {
	c.mu.Lock()
	needSetup := !c.m.Calibrated && !c.m.Running
	c.mu.Unlock()
	if needSetup && !c.AutoSetup() {
		return
	}
	c.mu.Lock()
	switch {
	case !c.m.Calibrated:
		c.setStatusLocked(StatusWarn, "Не удалось подготовить распознавание")
	case c.m.Running:
		c.m.Running = false
		if c.anCancel != nil {
			c.anCancel()
		}
		c.m.Analyzing = false
		c.setStatusLocked(StatusInfo, "Пауза")
		c.mu.Unlock()
		c.hideOverlayNow()
		c.notify()
		return
	default:
		c.m.Running = true
		c.badFrames = 0
		c.tr.SetAssumedSide(c.myColor())
		c.setStatusLocked(StatusInfo, "Читаю доску…")
		known := c.tr.Known()
		c.mu.Unlock()
		if known {
			c.requestAnalysis()
		}
		c.notify()
		return
	}
	c.mu.Unlock()
	c.notify()
}

// SelectRegion lets the user pick the board area on screen.
func (c *Controller) SelectRegion() {
	c.mu.Lock()
	wasRunning := c.m.Running
	c.m.Running = false
	c.mu.Unlock()
	c.hideOverlayNow()
	r, ok, err := c.plat.PickRegion()
	c.mu.Lock()
	c.m.Running = wasRunning
	c.mu.Unlock()
	if err != nil {
		c.setStatus(StatusError, "Не удалось выбрать область: "+err.Error())
		return
	}
	if !ok {
		c.setStatus(StatusInfo, "Выбор области отменён")
		return
	}
	img, err := c.plat.Capture(r)
	if err != nil {
		c.setStatus(StatusError, "Не удалось снять экран: "+err.Error())
		return
	}
	rect, conf := vision.DetectBoard(img)
	found := conf >= 0.6
	if !found {
		rect, found = vision.FindBoard(img)
	}
	c.mu.Lock()
	c.m.Cfg.HasRegion = true
	c.m.Cfg.Region = [4]int{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}
	if found {
		c.m.Cfg.BoardRect = [4]int{r.Min.X + rect.Min.X, r.Min.Y + rect.Min.Y, r.Min.X + rect.Max.X, r.Min.Y + rect.Max.Y}
		c.m.Cfg.HasBoard = true
	} else {
		c.m.Cfg.BoardRect = [4]int{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}
		c.m.Cfg.HasBoard = true
	}
	c.tr.Reset()
	c.refreshCalibState()
	switch {
	case !found && c.m.Calibrated:
		c.setStatusLocked(StatusWarn, "Границы доски определены грубо: выделите доску точнее или повторите калибровку")
	case !found:
		c.setStatusLocked(StatusWarn, "Доска найдена не точно. Выделите её ровнее и нажмите «Калибровка»")
	case c.m.Calibrated:
		c.setStatusLocked(StatusOK, "Область выбрана, доска найдена. Профиль фигур сохранён — можно нажимать «Старт»")
	default:
		c.setStatusLocked(StatusOK, "Доска найдена. Теперь начните новую партию и нажмите «Калибровка»")
	}
	c.mu.Unlock()
	c.saveConfig()
	c.notify()
}

func (c *Controller) captureBoardArea() (*image.RGBA, image.Rectangle, error) {
	c.mu.Lock()
	has := c.m.Cfg.HasRegion
	reg := c.m.Cfg.Region
	c.mu.Unlock()
	if !has {
		return nil, image.Rectangle{}, errors.New("сначала выберите область с доской")
	}
	c.hideOverlayNow()
	time.Sleep(90 * time.Millisecond)
	restore, _ := c.hideSelf()
	defer restore()
	img, err := c.plat.Capture(image.Rect(reg[0], reg[1], reg[2], reg[3]))
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	rect, conf := vision.DetectBoard(img)
	if conf < 0.6 {
		var ok bool
		if rect, ok = vision.FindBoard(img); !ok {
			rect = img.Bounds()
		}
	}
	return img, rect, nil
}

// Calibrate finds the board on the screen and learns the piece set from
// whatever position is shown (no initial position or FEN needed).
func (c *Controller) Calibrate() { c.AutoSetup() }

// CalibrateFEN learns from the position given by a FEN in the clipboard.
func (c *Controller) CalibrateFEN() {
	c.setStatus(StatusInfo, "Калибровка по FEN: ищу доску и фигуры…")
	text, err := c.plat.GetClipboard()
	if err != nil {
		c.setStatus(StatusError, "Не удалось прочитать буфер обмена: "+err.Error())
		return
	}
	fen := strings.TrimSpace(strings.Split(strings.TrimSpace(text), "\n")[0])
	pos, err := chess.ParseFEN(fen)
	if err != nil {
		c.setStatus(StatusWarn, "В буфере обмена нет корректного FEN (скопируйте FEN текущей позиции с сайта)")
		return
	}
	img, rect, err := c.locateBoard()
	if err != nil {
		c.setStatus(StatusWarn, err.Error())
		return
	}
	c.mu.Lock()
	white := c.m.Cfg.WhiteAtBottom
	c.mu.Unlock()
	m1 := vision.OccupancyMatch(img, rect, white, pos.Board)
	if m2 := vision.OccupancyMatch(img, rect, !white, pos.Board); m2 > m1 {
		m1, white = m2, !white
	}
	if m1 < 0.9 {
		c.dumpDebug(img, "calibration_debug.png")
		c.setStatus(StatusWarn, fmt.Sprintf("FEN из буфера не совпадает с доской на экране (совпадение %.0f%%)", m1*100))
		return
	}
	prof := &vision.Profile{}
	if err := prof.Learn(img, rect, pos.Board, white); err != nil {
		c.setStatus(StatusError, "Калибровка: "+err.Error())
		return
	}
	msg := "Калибровка по FEN выполнена"
	if n := prof.FillMissing(); n > 0 {
		msg += fmt.Sprintf(" (недостающие типы фигур, %d, достроены по форме)", n)
	}
	c.finishCalibration(prof, img, rect, white, pos, false, msg)
}

// dumpDebug stores a screenshot next to the settings for diagnosis.
func (c *Controller) dumpDebug(img *image.RGBA, name string) string {
	if c.cfgDir == "" {
		return ""
	}
	_ = os.MkdirAll(c.cfgDir, 0o755)
	p := filepath.Join(c.cfgDir, name)
	f, err := os.Create(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	if png.Encode(f, img) != nil {
		return ""
	}
	return p
}

func countLearned(p *vision.Profile) int {
	n := 0
	for i := 0; i < 12; i++ {
		if len(p.Samples[i]) > 0 {
			n++
		}
	}
	return n
}

func (c *Controller) finishCalibration(prof *vision.Profile, img *image.RGBA, rect image.Rectangle, white bool, pos chess.Position, sideUnsure bool, msg string) {
	c.mu.Lock()
	reg := c.m.Cfg.Region
	c.prof = prof
	c.m.Cfg.WhiteAtBottom = white
	c.m.Cfg.BoardRect = [4]int{reg[0] + rect.Min.X, reg[1] + rect.Min.Y, reg[0] + rect.Max.X, reg[1] + rect.Max.Y}
	c.m.Cfg.HasBoard = true
	c.m.Cfg.Region = c.m.Cfg.BoardRect // from now on the region is the board itself
	c.tr.SetAssumedSide(c.myColor())
	c.tr.SetPosition(pos, sideUnsure)
	c.m.Pos, c.m.HavePos = pos, true
	c.m.History = nil
	c.m.Best, c.m.BestSAN, c.m.HaveInfo, c.m.PVText, c.m.Result = chess.NoMove, "", false, "", ""
	c.refreshCalibState()
	kind := StatusOK
	if !c.m.Calibrated {
		kind = StatusWarn
	}
	c.setStatusLocked(kind, msg)
	c.mu.Unlock()
	c.saveProfile()
	c.saveConfig()
	c.builtin.NewGame()
	c.notify()
}

// CopyFEN puts the tracked position's FEN on the clipboard.
func (c *Controller) CopyFEN() {
	c.mu.Lock()
	pos, have := c.tr.Position(), c.tr.Known()
	c.mu.Unlock()
	if !have {
		c.setStatus(StatusWarn, "Позиция ещё не распознана")
		return
	}
	if err := c.plat.SetClipboard(pos.FEN()); err != nil {
		c.setStatus(StatusError, "Не удалось скопировать: "+err.Error())
		return
	}
	c.setStatus(StatusOK, "FEN скопирован в буфер обмена")
}

// ResetGame forgets the tracked game; the next stable board is adopted.
func (c *Controller) ResetGame() {
	c.mu.Lock()
	if c.anCancel != nil {
		c.anCancel()
	}
	c.tr.Reset()
	c.tr.SetAssumedSide(c.myColor())
	c.m.HavePos, c.m.History, c.m.Best, c.m.BestSAN, c.m.HaveInfo, c.m.PVText, c.m.Result, c.m.Analyzing = false, nil, chess.NoMove, "", false, "", "", false
	c.m.LastMove = chess.NoMove
	c.setStatusLocked(StatusInfo, "Партия сброшена, жду позицию на экране")
	c.mu.Unlock()
	c.hideOverlayNow()
	c.builtin.NewGame()
	c.notify()
}

// SetWhiteAtBottom sets which colour the user plays (bottom of the board).
func (c *Controller) SetWhiteAtBottom(white bool) {
	c.mu.Lock()
	changed := c.m.Cfg.WhiteAtBottom != white
	c.m.Cfg.WhiteAtBottom = white
	c.mu.Unlock()
	if changed {
		c.ResetGame()
	}
	c.saveConfig()
}

// SetHintBoth chooses between "only my moves" and "both sides".
func (c *Controller) SetHintBoth(both bool) {
	c.mu.Lock()
	c.m.Cfg.HintBoth = both
	have := c.tr.Known()
	c.mu.Unlock()
	c.saveConfig()
	if have {
		c.requestAnalysis()
	}
	c.notify()
}

// SetMoveTime sets the thinking time in milliseconds (0 = until the position changes).
func (c *Controller) SetMoveTime(ms int) {
	c.mu.Lock()
	c.m.Cfg.MoveTimeMs = ms
	c.mu.Unlock()
	c.saveConfig()
	c.notify()
}

// SetUseExternal switches between the built-in engine and Stockfish.
func (c *Controller) SetUseExternal(use bool) {
	c.mu.Lock()
	if use && c.m.Cfg.ExternalPath == "" {
		c.setStatusLocked(StatusWarn, "Stockfish не найден: нажмите «Скачать» или положите stockfish.exe рядом с программой")
		c.mu.Unlock()
		c.notify()
		return
	}
	c.m.Cfg.UseExternal = use
	if !use {
		c.m.EngineName = c.builtin.Name()
	}
	have := c.tr.Known()
	c.mu.Unlock()
	c.saveConfig()
	if have {
		c.requestAnalysis()
	}
	c.notify()
}

// SetOverlay toggles the arrow drawn over the real board.
func (c *Controller) SetOverlay(on bool) {
	c.mu.Lock()
	c.m.Cfg.Overlay = on
	c.mu.Unlock()
	c.saveConfig()
	if !on {
		c.mu.Lock()
		c.ovGen++
		c.overlayOn, c.overlayImg, c.overlayFor = false, nil, chess.NoMove
		c.ovAnim, c.ovShown, c.ovLevel = false, chess.NoMove, 0
		c.plat.HideOverlay()
		c.mu.Unlock()
	} else {
		c.mu.Lock()
		c.updateOverlayLocked(true)
		c.mu.Unlock()
	}
	c.notify()
}

// SetAlwaysOnTop stores the preference (the window layer applies it).
func (c *Controller) SetAlwaysOnTop(on bool) {
	c.mu.Lock()
	c.m.Cfg.AlwaysOnTop = on
	c.mu.Unlock()
	c.saveConfig()
	c.notify()
}

// SetRussian toggles Russian piece letters.
func (c *Controller) SetRussian(ru bool) {
	c.mu.Lock()
	c.m.Cfg.Russian = ru
	c.rebuildHistoryLocked()
	if c.m.HavePos && c.m.HaveInfo {
		c.m.PVText = c.pvTextLocked(c.m.Pos, c.m.Info.PV)
		if c.m.Best != chess.NoMove {
			c.m.BestSAN = c.m.Pos.SAN(c.m.Best, ru)
		}
	}
	c.mu.Unlock()
	c.saveConfig()
	c.notify()
}

// DownloadEngine fetches Stockfish in the background.
func (c *Controller) DownloadEngine() {
	c.mu.Lock()
	if c.m.DL.Active {
		c.mu.Unlock()
		return
	}
	c.m.DL.Active, c.m.DL.Frac, c.m.DL.Msg = true, 0, "Подключаюсь…"
	dir := filepath.Join(c.cacheDir, "engines")
	c.mu.Unlock()
	c.notify()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		path, err := engine.DownloadStockfish(ctx, dir, func(f float64, msg string) {
			c.mu.Lock()
			c.m.DL.Frac, c.m.DL.Msg = f, msg
			c.mu.Unlock()
			c.notify()
		})
		c.mu.Lock()
		c.m.DL.Active = false
		if err != nil {
			c.setStatusLocked(StatusError, "Не удалось скачать Stockfish: "+err.Error())
			c.mu.Unlock()
			c.notify()
			return
		}
		c.m.Cfg.ExternalPath = path
		c.m.ExternalFound = true
		c.m.Cfg.UseExternal = true
		c.setStatusLocked(StatusOK, "Stockfish установлен и включён")
		c.mu.Unlock()
		c.saveConfig()
		c.requestAnalysis()
		c.notify()
	}()
}

// ---- scanning ---------------------------------------------------------------

func boardOK(b [64]chess.Piece) bool {
	var wk, bk int
	for _, pc := range b {
		switch pc {
		case chess.MakePiece(chess.White, chess.King):
			wk++
		case chess.MakePiece(chess.Black, chess.King):
			bk++
		}
	}
	return wk == 1 && bk == 1
}

func (c *Controller) scanOnce() {
	c.mu.Lock()
	if !c.m.Running || !c.m.Cfg.HasBoard || c.prof == nil {
		c.mu.Unlock()
		return
	}
	if c.ovAnim {
		// the arrow window is mid-fade: its exact opacity is unknown, so a
		// capture could not be cleaned reliably. Try again on the next tick.
		c.mu.Unlock()
		return
	}
	rect := c.boardScreenRect()
	white := c.m.Cfg.WhiteAtBottom
	prof := c.prof
	ov := c.overlayImg
	c.alignTick++
	verify := c.alignTick%30 == 5 && !c.setupBusy
	c.mu.Unlock()
	if verify {
		go c.verifyAlignment()
	}

	img, err := c.plat.Capture(rect)
	if err != nil {
		c.setStatus(StatusError, "Не удалось снять экран: "+err.Error())
		return
	}
	if ov != nil {
		unblend(img, ov)
	}
	rec, err := prof.Recognize(img, image.Rect(0, 0, img.Rect.Dx(), img.Rect.Dy()), white)
	if err != nil {
		c.setStatus(StatusError, err.Error())
		return
	}

	c.mu.Lock()
	c.m.Scans++
	c.m.Quality = rec.Quality()
	if n := c.coveredLocked(&rec.Board, rect); n > 0 && !c.tr.Known() {
		c.mu.Unlock()
		return
	}
	c.m.Board, c.m.HaveBoard = rec.Board, true
	if !boardOK(rec.Board) {
		c.badFrames++
		if c.badFrames == 8 {
			go c.maybeRelocate()
		}
		if c.badFrames == 12 {
			c.setStatusLocked(StatusWarn, "Доска не распознаётся: проверьте область и калибровку (кнопки «Область», «Калибровка»)")
		}
		c.mu.Unlock()
		c.notify()
		return
	}
	c.badFrames = 0
	if rec.Quality() < 0.6 {
		c.lowQ++
		if c.lowQ == 10 {
			go c.maybeRelocate()
		}
		if c.lowQ == 15 {
			c.setStatusLocked(StatusWarn, "Распознавание нестабильно: выполните калибровку заново или уберите стрелки/подсказки с доски")
		}
	} else {
		c.lowQ = 0
	}
	if st := c.detectFlipLocked(rec.Board); st != flipNone {
		c.mu.Unlock()
		if st == flipDone {
			c.saveConfig()
			c.requestAnalysis()
			c.notify()
		}
		return
	}
	ev := c.tr.Feed(rec.Board)
	changed := ev.Kind != track.None
	if changed {
		c.applyEventLocked(ev)
	}
	c.mu.Unlock()
	if changed {
		if ev.Kind != track.Invalid {
			c.requestAnalysis()
		}
		c.notify()
	}
}

func (c *Controller) applyEventLocked(ev track.Event) {
	pos := c.tr.Position()
	c.m.Pos, c.m.HavePos, c.m.SideUnsure = pos, true, c.tr.SideUnsure()
	switch ev.Kind {
	case track.NewGame:
		c.m.History = nil
		c.m.LastMove = chess.NoMove
		go c.builtin.NewGame()
		c.setStatusLocked(StatusOK, "Новая партия")
	case track.Move, track.Moves, track.Flipped:
		c.m.LastMove = ev.Moves[len(ev.Moves)-1]
	case track.Takeback:
		h := c.tr.History()
		if len(h) > 0 {
			c.m.LastMove = h[len(h)-1].Move
		} else {
			c.m.LastMove = chess.NoMove
		}
		c.setStatusLocked(StatusInfo, "Ход отменён — позиция возвращена")
	case track.Resync, track.Adopted:
		c.m.LastMove = chess.NoMove
		side := "белых"
		if pos.Side == chess.Black {
			side = "чёрных"
		}
		c.setStatusLocked(StatusInfo, "Позиция взята с экрана. Предполагаю ход "+side+" (определится по следующему ходу)")
	case track.Invalid:
		c.setStatusLocked(StatusWarn, "Позиция на экране невозможна (проверьте, что видна вся доска)")
		return
	}
	c.rebuildHistoryLocked()
}

func (c *Controller) rebuildHistoryLocked() {
	h := c.tr.History()
	var lines []HistLine
	ru := c.m.Cfg.Russian
	for _, e := range h {
		san := e.Before.SAN(e.Move, ru)
		if e.Before.Side == chess.White {
			lines = append(lines, HistLine{Num: int(e.Before.Full), White: san})
		} else {
			if n := len(lines); n > 0 && lines[n-1].Black == "" && lines[n-1].Num == int(e.Before.Full) {
				lines[n-1].Black = san
			} else {
				lines = append(lines, HistLine{Num: int(e.Before.Full), Black: san})
			}
		}
	}
	c.m.History = lines
}

func (c *Controller) pvTextLocked(pos chess.Position, pv []chess.Move) string {
	return pos.SANLine(pv, c.m.Cfg.Russian, 7)
}

// ---- analysis -------------------------------------------------------------------

func resultText(r chess.Result, side chess.Color) string {
	switch r {
	case chess.Checkmate:
		if side == chess.White {
			return "Мат белым"
		}
		return "Мат чёрным"
	case chess.Stalemate:
		return "Пат — ничья"
	case chess.DrawMaterial:
		return "Ничья: мало материала"
	case chess.DrawFifty:
		return "Ничья: правило 50 ходов"
	}
	return ""
}

func evalToPawns(info engine.Info, stm chess.Color) float64 {
	v := float64(info.ScoreCP) / 100
	if info.IsMate {
		if info.Mate > 0 {
			v = 12
		} else {
			v = -12
		}
	}
	if stm == chess.Black {
		v = -v
	}
	if v > 12 {
		v = 12
	}
	if v < -12 {
		v = -12
	}
	return v
}

// requestAnalysis (re)starts the engine on the current position.
func (c *Controller) requestAnalysis() {
	c.mu.Lock()
	if c.anCancel != nil {
		c.anCancel()
		c.anCancel = nil
	}
	if !c.tr.Known() {
		c.mu.Unlock()
		return
	}
	pos := c.tr.Position()
	hist := c.tr.Hashes()
	c.m.Pos, c.m.HavePos = pos, true
	c.m.Best, c.m.BestSAN, c.m.HaveInfo, c.m.PVText = chess.NoMove, "", false, ""
	c.m.Analyzing = false
	c.m.Result = ""
	if st := pos.Status(); st != chess.Ongoing {
		c.m.Result = resultText(st, pos.Side)
		c.setStatusLocked(StatusInfo, "Партия окончена: "+c.m.Result)
		c.updateOverlayLocked(true)
		c.mu.Unlock()
		c.notify()
		return
	}
	mine := (pos.Side == chess.White) == c.m.Cfg.WhiteAtBottom
	if !mine && !c.m.Cfg.HintBoth {
		c.setStatusLocked(StatusInfo, "Ход соперника — жду…")
		c.updateOverlayLocked(true)
		c.mu.Unlock()
		c.notify()
		return
	}
	c.anGen++
	gen := c.anGen
	ctx, cancel := context.WithCancel(context.Background())
	c.anCancel = cancel
	c.m.Analyzing = true
	lim := engine.Limits{MoveTime: time.Duration(c.m.Cfg.MoveTimeMs) * time.Millisecond}
	side := "белых"
	if pos.Side == chess.Black {
		side = "чёрных"
	}
	c.setStatusLocked(StatusInfo, "Думаю над ходом "+side+"…")
	c.mu.Unlock()
	c.notify()

	go func() {
		eng := c.activeEngine()
		var pend chess.Move
		streak := 0
		report := func(info engine.Info) {
			if len(info.PV) == 0 {
				return
			}
			// A suggestion that flips while the engine is still thinking is worse
			// than none. With a time limit the answer is published once, when the
			// search ends. Without a limit (search runs until the position
			// changes) an interim move is shown only when it is deep enough and
			// has stayed the same for a few iterations; a different move replaces
			// it only after it proves just as stable.
			if lim.MoveTime > 0 {
				return
			}
			if info.PV[0] == pend {
				streak++
			} else {
				pend, streak = info.PV[0], 1
			}
			c.mu.Lock()
			shown := c.m.Best
			if info.Depth < 10 || streak < 3 || (shown != chess.NoMove && shown != pend && streak < 5) {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
			c.mu.Lock()
			if gen != c.anGen {
				c.mu.Unlock()
				return
			}
			c.publishLocked(pos, info)
			c.mu.Unlock()
			c.notify()
		}
		res, err := eng.Analyze(ctx, pos, hist, lim, report)
		c.mu.Lock()
		if gen != c.anGen {
			c.mu.Unlock()
			return
		}
		c.m.Analyzing = false
		if err != nil {
			if eng != engine.Engine(c.builtin) {
				// Stockfish died: fall back to the built-in engine and retry once
				c.m.Cfg.UseExternal = false
				if c.ext != nil {
					c.ext.Close()
					c.ext = nil
				}
				c.m.EngineName = c.builtin.Name()
				c.setStatusLocked(StatusWarn, "Stockfish перестал отвечать — переключился на встроенный движок")
				c.mu.Unlock()
				c.notify()
				c.requestAnalysis()
				return
			}
			c.setStatusLocked(StatusError, "Ошибка движка: "+err.Error())
			c.mu.Unlock()
			c.notify()
			return
		}
		if res.Best != chess.NoMove {
			info := res.Info
			if len(info.PV) == 0 || info.PV[0] != res.Best {
				info.PV = []chess.Move{res.Best}
			}
			c.publishLocked(pos, info)
			c.setStatusLocked(StatusOK, fmt.Sprintf("Лучший ход найден · глубина %d", info.Depth))
		}
		c.mu.Unlock()
		c.notify()
	}()
}

func (c *Controller) publishLocked(pos chess.Position, info engine.Info) {
	c.m.Info, c.m.InfoSTM, c.m.HaveInfo = info, pos.Side, true
	best := info.PV[0]
	c.m.Best = best
	c.m.BestSAN = pos.SAN(best, c.m.Cfg.Russian)
	c.m.PVText = c.pvTextLocked(pos, info.PV)
	c.m.EvalWhite = evalToPawns(info, pos.Side)
	c.updateOverlayLocked(false)
}

// updateOverlayLocked shows/hides the arrow on the real board. Changes are
// animated: the previous arrow fades out, the new one grows out of its source
// square (see runOverlayAnim).
func (c *Controller) updateOverlayLocked(force bool) {
	if !c.m.Cfg.Overlay || !c.m.Cfg.HasBoard || !c.m.Running || c.m.Best == chess.NoMove {
		if c.overlayOn {
			c.ovGen++
			gen, from, level := c.ovGen, c.ovShown, c.ovLevel
			c.overlayOn, c.overlayFor = false, chess.NoMove
			if from == chess.NoMove || level <= 0 {
				c.overlayImg, c.ovAnim, c.ovShown, c.ovLevel = nil, false, chess.NoMove, 0
				go c.plat.HideOverlay()
				return
			}
			c.ovAnim = true
			go c.runOverlayAnim(gen, c.boardScreenRect(), c.m.Cfg.WhiteAtBottom, from, level, chess.NoMove)
		}
		return
	}
	if c.overlayOn && c.overlayFor == c.m.Best {
		if !force || c.ovAnim {
			return
		}
		// same move, maybe a moved board: redraw at rest without animation
		r := c.boardScreenRect()
		img := renderOverlay(r.Dx(), r.Dy(), c.m.Cfg.WhiteAtBottom, c.m.Best)
		c.overlayImg = img
		c.ovShown, c.ovLevel = c.m.Best, 1
		go c.plat.ShowOverlay(r, img)
		return
	}
	c.ovGen++
	gen, from, level := c.ovGen, c.ovShown, c.ovLevel
	c.overlayFor, c.overlayOn, c.ovAnim = c.m.Best, true, true
	go c.runOverlayAnim(gen, c.boardScreenRect(), c.m.Cfg.WhiteAtBottom, from, level, c.m.Best)
}

const (
	ovOutDur = 140 * time.Millisecond
	ovInDur  = 340 * time.Millisecond
	ovFrame  = 16 * time.Millisecond
)

// runOverlayAnim cross-fades the arrow window: the previously shown move
// (from, at opacity level) fades out, then `to` grows in. to == NoMove hides
// the window at the end. Every frame is stored in c.overlayImg so that a
// capture is always un-blended with exactly what is on screen; scans are
// paused while ovAnim is set anyway.
func (c *Controller) runOverlayAnim(gen int, r image.Rectangle, white bool, from chess.Move, level float64, to chess.Move) {
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 {
		c.mu.Lock()
		if c.ovGen == gen {
			c.ovAnim = false
		}
		c.mu.Unlock()
		return
	}
	show := func(mv chess.Move, grow, k float64) bool {
		img := renderOverlayT(w, h, white, mv, grow, k)
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.ovGen != gen {
			return false
		}
		c.overlayImg, c.ovShown, c.ovLevel = img, mv, k
		c.plat.ShowOverlay(r, img)
		return true
	}
	easeOut := func(t float64) float64 { t = 1 - t; return 1 - t*t*t }
	run := func(d time.Duration, f func(t float64) bool) bool {
		start := time.Now()
		for {
			t := float64(time.Since(start)) / float64(d)
			if t >= 1 {
				return true
			}
			if !f(t) {
				return false
			}
			time.Sleep(ovFrame)
		}
	}
	if from != chess.NoMove && level > 0 && from != to {
		d := time.Duration(float64(ovOutDur) * level)
		if !run(d, func(t float64) bool { return show(from, 1, level*(1-t)) }) {
			return
		}
	}
	if to == chess.NoMove {
		c.mu.Lock()
		if c.ovGen == gen {
			c.overlayImg, c.ovAnim, c.ovShown, c.ovLevel = nil, false, chess.NoMove, 0
			c.plat.HideOverlay()
		}
		c.mu.Unlock()
		return
	}
	if !run(ovInDur, func(t float64) bool {
		e := easeOut(t)
		return show(to, e, math.Min(1, e*2.2))
	}) {
		return
	}
	if show(to, 1, 1) {
		c.mu.Lock()
		if c.ovGen == gen {
			c.ovAnim = false
		}
		c.mu.Unlock()
	}
}

// ---- automatic side detection ---------------------------------------------------

func rotateBoard(b [64]chess.Piece) (out [64]chess.Piece) {
	for i, pc := range b {
		out[63-i] = pc
	}
	return
}

func boardDiff(a, b [64]chess.Piece) (n int) {
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return
}

// pawnSkew is the mean rank of white pawns minus that of black pawns. In an
// upright reading it is (almost always) negative: white pawns sit lower.
func pawnSkew(b [64]chess.Piece) (skew float64, ok bool) {
	var ws, bs, wn, bn float64
	for sq, pc := range b {
		if pc == chess.MakePiece(chess.White, chess.Pawn) {
			ws += float64(chess.Rank(sq))
			wn++
		} else if pc == chess.MakePiece(chess.Black, chess.Pawn) {
			bs += float64(chess.Rank(sq))
			bn++
		}
	}
	if wn < 3 || bn < 3 {
		return 0, false
	}
	return ws/wn - bs/bn, true
}

const (
	flipNone = iota
	flipHold
	flipDone
)

// detectFlipLocked notices that the site turned the board around (a new game
// as the other colour) and flips the assumed orientation, so "my side" is
// always determined from the screen. It reports whether the frame was held or the orientation changed.
func (c *Controller) detectFlipLocked(cur [64]chess.Piece) int {
	rot := rotateBoard(cur)
	evidence := false
	if c.tr.Known() {
		ref := c.tr.Position().Board
		d0, d1 := boardDiff(cur, ref), boardDiff(rot, ref)
		evidence = d1 <= 6 && d0 >= d1+10
	} else {
		start := chess.Start().Board
		d0, d1 := boardDiff(cur, start), boardDiff(rot, start)
		if d1 <= 4 && d0 >= 12 {
			evidence = true
		} else if sk, ok := pawnSkew(cur); ok && sk > 1.5 && d0 > 4 {
			evidence = true
		}
	}
	if !evidence {
		c.flipVotes = 0
		return flipNone
	}
	c.flipVotes++
	if c.flipVotes < 3 {
		return flipHold // do not feed a mirrored board to the tracker
	}
	c.flipVotes = 0
	c.m.Cfg.WhiteAtBottom = !c.m.Cfg.WhiteAtBottom
	c.tr.SetAssumedSide(c.myColor())
	side := "чёрными"
	if c.m.Cfg.WhiteAtBottom {
		side = "белыми"
	}
	c.m.Board = rot
	c.m.Best, c.m.BestSAN, c.m.HaveInfo, c.m.PVText = chess.NoMove, "", false, ""
	c.setStatusLocked(StatusInfo, "Доска перевёрнута — вы играете "+side)
	c.updateOverlayLocked(true)
	return flipDone
}

// hideOverlayNow removes the arrow window at once and forgets everything about
// it, cancelling any fade that is still running (so it cannot reappear on top
// of a capture, e.g. during calibration).
func (c *Controller) hideOverlayNow() {
	c.mu.Lock()
	c.ovGen++
	c.overlayOn, c.overlayImg, c.overlayFor = false, nil, chess.NoMove
	c.ovAnim, c.ovShown, c.ovLevel = false, chess.NoMove, 0
	c.plat.HideOverlay()
	c.mu.Unlock()
}

// coveredLocked finds the board squares that our own window hides. Their
// content cannot be read, so the tracker's last known pieces are used for
// them, and the window is moved aside when there is room (otherwise the
// person is asked to do it). Returns the number of covered squares.
func (c *Controller) coveredLocked(board *[64]chess.Piece, rect image.Rectangle) int {
	o, ok := c.plat.(ownRecter)
	if !ok || rect.Dx() < 8 {
		return 0
	}
	own := o.OwnRects()
	if len(own) == 0 {
		c.coverNote = false
		return 0
	}
	sq := float64(rect.Dx()) / 8
	n := 0
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			r := image.Rect(rect.Min.X+int(float64(col)*sq), rect.Min.Y+int(float64(row)*sq),
				rect.Min.X+int(float64(col+1)*sq), rect.Min.Y+int(float64(row+1)*sq))
			hit := 0
			for _, w := range own {
				hit += w.Intersect(r).Dx() * w.Intersect(r).Dy()
			}
			if float64(hit) < 0.2*float64(r.Dx()*r.Dy()) {
				continue
			}
			n++
			if c.tr.Known() {
				idx := coverIndex(col, row, c.m.Cfg.WhiteAtBottom)
				board[idx] = c.tr.Position().Board[idx]
			}
		}
	}
	if n == 0 {
		c.coverNote = false
		return 0
	}
	if mv, ok := c.plat.(windowMover); ok && time.Since(c.lastMove) > 3*time.Second {
		c.lastMove = time.Now()
		if mv.MoveAway(rect) {
			return n
		}
	}
	if !c.coverNote {
		c.coverNote = true
		c.setStatusLocked(StatusWarn, "Окно ChessVision закрывает часть доски — сдвиньте его в сторону, иначе ходы на закрытых клетках не видны")
	}
	return n
}

// coverIndex maps a screen cell (col,row from the top-left) to a square.
func coverIndex(col, row int, whiteAtBottom bool) int {
	if whiteAtBottom {
		return (7-row)*8 + col
	}
	return row*8 + (7 - col)
}

// verifyAlignment checks that the stored board rectangle sits exactly on the
// board. A checkerboard looks the same when shifted by whole squares, so a
// rectangle can end up one file or rank off; the pieces and the arrow are then
// one square away from where they belong. The squares just outside the
// rectangle give it away (see vision.AlignRect). When the offset is confirmed
// twice in a row the board is located and learned again.
func (c *Controller) verifyAlignment() {
	c.mu.Lock()
	if !c.m.Running || !c.m.Cfg.HasBoard || c.setupBusy {
		c.mu.Unlock()
		return
	}
	rect := c.boardScreenRect()
	c.mu.Unlock()
	sq := rect.Dx() / 8
	if sq < 12 {
		return
	}
	area := rect.Inset(-sq - 6).Intersect(c.screenArea())
	img, err := c.plat.Capture(area)
	if err != nil {
		return
	}
	c.blankOwn(img, area.Min)
	got, changed := vision.AlignRect(img, rect.Sub(area.Min))
	if !changed {
		c.mu.Lock()
		c.alignVotes = 0
		c.mu.Unlock()
		return
	}
	fixed := got.Add(area.Min)
	c.mu.Lock()
	if fixed != c.alignCand {
		c.alignCand, c.alignVotes = fixed, 1
		c.mu.Unlock()
		return
	}
	c.alignVotes++
	if c.alignVotes < 2 {
		c.mu.Unlock()
		return
	}
	c.alignVotes = 0
	c.setStatusLocked(StatusInfo, "Рамка доски была сдвинута на клетку — нахожу доску заново")
	c.wideSearch = true
	c.mu.Unlock()
	c.notify()
	c.AutoSetup()
}
