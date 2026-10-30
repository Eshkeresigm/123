package engine

import (
	"context"
	"testing"
	"time"

	"chessvision/internal/chess"
)

// Whatever the position, the suggestion must be a legal move: in particular a
// king in check has to be answered.
func TestBestMoveAnswersCheck(t *testing.T) {
	fens := []struct {
		fen  string
		mate string // expected mating move, if any
	}{
		{"rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3", "g2g3"}, // not mate: just must be legal & answer check
		{"4k3/8/8/8/8/8/4q3/4K3 w - - 0 1", ""},                                   // Kxe2 only
		{"7k/5Q2/6K1/8/8/8/8/8 w - - 0 1", "f7g7"},                                // mate in 1
		{"r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 4 4", "h5f7"},
		{"3k4/8/3K4/8/8/8/8/3R4 b - - 0 1", ""},
		{"rnbqkbnr/ppp2ppp/8/3pp3/6Q1/8/PPPPPPPP/RNB1KBNR b KQkq - 0 3", ""},
		{"4r1k1/5ppp/8/8/8/8/5PPP/R3K3 w Q - 0 1", ""},
		{"r3k2r/8/8/8/8/8/8/4K2R b Kkq - 0 1", ""},
		{"8/8/8/8/8/5k2/5p2/5K2 w - - 0 1", ""},
		{"4k3/4R3/8/8/8/8/8/4K3 b - - 0 1", ""},
	}
	b := NewBuiltin(16)
	for _, tc := range fens {
		pos, err := chess.ParseFEN(tc.fen)
		if err != nil {
			t.Fatalf("%s: %v", tc.fen, err)
		}
		legal := pos.LegalMoves()
		if len(legal) == 0 {
			continue
		}
		res, err := b.Analyze(context.Background(), pos, nil, Limits{MoveTime: 150 * time.Millisecond}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.fen, err)
		}
		if !pos.IsLegal(res.Best) {
			t.Errorf("%s: illegal best %s (in check=%v)", tc.fen, res.Best, pos.InCheck())
		}
		if pos.InCheck() {
			q, ok := pos.Make(res.Best)
			if !ok || q.IsAttacked(q.KingSq(pos.Side), pos.Side.Opp()) {
				t.Errorf("%s: %s does not resolve the check", tc.fen, res.Best)
			}
		}
		if tc.mate == "f7g7" || tc.mate == "h5f7" {
			if res.Best.String() != tc.mate {
				t.Errorf("%s: want mate %s, got %s", tc.fen, tc.mate, res.Best)
			}
		}
	}
}
