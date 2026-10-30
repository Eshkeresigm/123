// Package engine contains the built-in alpha-beta searcher and a UCI client
// for external engines such as Stockfish.
package engine

import (
	"context"
	"fmt"
	"time"

	"chessvision/internal/chess"
)

// Limits bound a search. Zero fields mean "unbounded".
type Limits struct {
	MoveTime time.Duration
	Depth    int
}

// Info is a progress report of a running search.
type Info struct {
	Depth   int
	ScoreCP int  // centipawns from the side to move's point of view
	IsMate  bool // when true Mate holds moves to mate (negative: being mated)
	Mate    int
	Nodes   uint64
	NPS     uint64
	Elapsed time.Duration
	PV      []chess.Move
	MultiPV int // 1-based line index (1 = best)
}

// Result is the final answer of a search.
type Result struct {
	Best   chess.Move
	Ponder chess.Move
	Info   Info
	Lines  []Info // best info per MultiPV line (Lines[0] is the best)
}

// Engine analyses positions.
type Engine interface {
	Name() string
	// Analyze searches pos. history holds Zobrist hashes of earlier game
	// positions (for repetition detection). report may be nil.
	Analyze(ctx context.Context, pos chess.Position, history []uint64, lim Limits, report func(Info)) (Result, error)
	// NewGame clears learned state (hash tables).
	NewGame()
	Close() error
}

// ScoreString renders an evaluation like "+0.35", "-1.20" or "#3".
func (i Info) ScoreString() string {
	if i.IsMate {
		if i.Mate < 0 {
			return fmt.Sprintf("#-%d", -i.Mate)
		}
		return fmt.Sprintf("#%d", i.Mate)
	}
	return fmt.Sprintf("%+.2f", float64(i.ScoreCP)/100)
}

// WhiteScore converts the score to White's point of view given the side to move.
func (i Info) WhiteScore(stm chess.Color) Info {
	if stm == chess.Black {
		i.ScoreCP = -i.ScoreCP
		i.Mate = -i.Mate
	}
	return i
}
