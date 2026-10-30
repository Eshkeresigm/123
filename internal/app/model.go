package app

import (
	"chessvision/internal/chess"
	"chessvision/internal/engine"
)

// StatusKind colours the status line.
type StatusKind int

const (
	StatusInfo StatusKind = iota
	StatusOK
	StatusWarn
	StatusError
)

// HistLine is one numbered full move of the game list.
type HistLine struct {
	Num          int
	White, Black string
}

// Config holds the persisted settings.
type Config struct {
	Region        [4]int // selected screen area (x0,y0,x1,y1)
	HasRegion     bool
	BoardRect     [4]int // detected board in screen coordinates
	HasBoard      bool
	WhiteAtBottom bool
	HintBoth      bool // analyse for both sides instead of only the user's
	MoveTimeMs    int  // 0 = until the position changes
	UseExternal   bool
	ExternalPath  string
	Overlay       bool
	AlwaysOnTop   bool
	Russian       bool // Russian piece letters in notation
	WinX, WinY    int
	HasWinPos     bool
}

// DefaultConfig returns first-run settings.
func DefaultConfig() Config {
	return Config{WhiteAtBottom: true, MoveTimeMs: 1500, Overlay: false, Russian: true}
}

// Model is a snapshot of everything the interface shows.
type Model struct {
	Cfg        Config
	Running    bool
	Calibrated bool
	Learned    int // number of piece types learned (0..12)

	Board      [64]chess.Piece // last recognised board
	HaveBoard  bool
	Pos        chess.Position
	HavePos    bool
	SideUnsure bool
	LastMove   chess.Move
	Quality    float64

	Best      chess.Move
	BestSAN   string
	Info      engine.Info // score from the side to move's point of view
	InfoSTM   chess.Color
	HaveInfo  bool
	Analyzing bool
	PVText    string
	EvalWhite float64 // evaluation in pawns from White's view (clamped), for the bar
	Result    string  // "Мат", "Пат", ... when the game is over

	History []HistLine

	Status     string
	StatusKind StatusKind

	EngineName    string
	ExternalFound bool
	DL            struct {
		Active bool
		Frac   float64
		Msg    string
	}
	Scans   int
	Version string
}
