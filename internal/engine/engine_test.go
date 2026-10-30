package engine

import (
	"context"
	"testing"
	"time"

	"chessvision/internal/chess"
)

func analyze(t *testing.T, fen string, lim Limits) Result {
	t.Helper()
	p, err := chess.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	e := NewBuiltin(32)
	r, err := e.Analyze(context.Background(), p, nil, lim, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsLegal(r.Best) {
		t.Fatalf("illegal best move %s in %s", r.Best, fen)
	}
	return r
}

// bruteMateIn1 returns a move that mates immediately, if any.
func bruteMateIn1(p chess.Position) (chess.Move, bool) {
	for _, m := range p.LegalMoves() {
		q, _ := p.Make(m)
		if q.InCheck() && !q.HasLegalMove() {
			return m, true
		}
	}
	return chess.NoMove, false
}

// bruteMateIn2 reports whether the side to move forces mate within two moves.
func bruteMateIn2(p chess.Position) bool {
	for _, m := range p.LegalMoves() {
		q, _ := p.Make(m)
		replies := q.LegalMoves()
		if len(replies) == 0 {
			if q.InCheck() {
				return true
			}
			continue
		}
		all := true
		for _, r := range replies {
			qq, _ := q.Make(r)
			if _, ok := bruteMateIn1(qq); !ok {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func TestMatesAgainstBruteForce(t *testing.T) {
	rng := uint64(12345)
	next := func(n int) int {
		rng = rng*6364136223846793005 + 1442695040888963407
		return int((rng >> 33) % uint64(n))
	}
	e := NewBuiltin(16)
	found1, found2 := 0, 0
	for iter := 0; iter < 4000 && (found1 < 12 || found2 < 8); iter++ {
		var board [64]chess.Piece
		for i := range board {
			board[i] = chess.NoPiece
		}
		place := func(pc chess.Piece) {
			for {
				sq := next(64)
				if board[sq] == chess.NoPiece {
					board[sq] = pc
					return
				}
			}
		}
		place(chess.MakePiece(chess.White, chess.King))
		place(chess.MakePiece(chess.Black, chess.King))
		place(chess.MakePiece(chess.White, chess.Queen))
		if next(2) == 0 {
			place(chess.MakePiece(chess.White, chess.Rook))
		}
		pos, err := chess.FromBoard(board, chess.White, 0, -1)
		if err != nil {
			continue
		}
		if _, ok := bruteMateIn1(pos); ok {
			if found1 >= 12 {
				continue
			}
			found1++
			r, _ := e.Analyze(context.Background(), pos, nil, Limits{Depth: 4}, nil)
			q, _ := pos.Make(r.Best)
			if !(q.InCheck() && !q.HasLegalMove()) || !r.Info.IsMate || r.Info.Mate != 1 {
				t.Errorf("mate in 1 missed in %s: %s (%s)", pos.FEN(), r.Best, r.Info.ScoreString())
			}
		} else if bruteMateIn2(pos) {
			if found2 >= 8 {
				continue
			}
			found2++
			r, _ := e.Analyze(context.Background(), pos, nil, Limits{Depth: 6}, nil)
			if !r.Info.IsMate || r.Info.Mate != 2 {
				t.Errorf("mate in 2 missed in %s: %s (%s)", pos.FEN(), r.Best, r.Info.ScoreString())
			}
		}
	}
	t.Logf("verified %d mate-in-1 and %d mate-in-2 positions", found1, found2)
	if found1 < 5 || found2 < 3 {
		t.Fatalf("not enough generated mate positions (%d, %d)", found1, found2)
	}
}

func TestTactics(t *testing.T) {
	cases := []struct{ fen, move string }{
		{"6k1/5ppp/8/8/8/8/8/R3K3 w - - 0 1", "a1a8"},
		{"r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 4 4", "h5f7"},
		{"4k3/8/8/3q4/8/8/8/3RK3 w - - 0 1", "d1d5"},
	}
	for _, c := range cases {
		r := analyze(t, c.fen, Limits{MoveTime: 2 * time.Second, Depth: 9})
		if r.Best.UCI() != c.move {
			t.Errorf("%s: best %s, want %s (score %s)", c.fen, r.Best, c.move, r.Info.ScoreString())
		}
	}
	// Win At Chess style positions (informational: log only)
	for _, c := range []struct{ fen, move string }{
		{"2rr3k/pp3pp1/1nnqbN1p/3pN3/2pP4/2P3Q1/PPB4P/R4RK1 w - - 0 1", "g3g6"},
		{"8/7p/5k2/5p2/p1p2P2/Pr1pPK2/1P1R3P/8 b - - 0 1", "b3b2"},
		{"5rk1/1ppb3p/p1pb4/6q1/3P1p1r/2P1R2P/PP1BQ1P1/5RKN w - - 0 1", "e3g3"},
	} {
		r := analyze(t, c.fen, Limits{MoveTime: 3 * time.Second})
		t.Logf("WAC %s -> %s (expected %s) depth %d score %s", c.fen, r.Best, c.move, r.Info.Depth, r.Info.ScoreString())
	}
}

func TestStalemateAvoidance(t *testing.T) {
	// white is up a queen; a naive engine might stalemate. Must never pick Qf7/Qg6 stalemating moves.
	fen := "7k/8/6K1/8/8/8/8/5Q2 w - - 0 1"
	p, _ := chess.ParseFEN(fen)
	e := NewBuiltin(16)
	for i := 0; i < 12; i++ {
		r, err := e.Analyze(context.Background(), p, nil, Limits{MoveTime: 400 * time.Millisecond, Depth: 8}, nil)
		if err != nil {
			t.Fatal(err)
		}
		q, _ := p.Make(r.Best)
		if q.Status() == chess.Stalemate {
			t.Fatalf("engine stalemated with %s in %s", r.Best, p.FEN())
		}
		if q.Status() == chess.Checkmate {
			return
		}
		// pretend the only defence
		reply := q.LegalMoves()
		if len(reply) == 0 {
			t.Fatalf("no reply but not mate")
		}
		p, _ = q.Make(reply[0])
	}
	t.Fatal("did not deliver mate within 12 moves")
}

func TestSpeedAndCancel(t *testing.T) {
	p := chess.Start()
	e := NewBuiltin(64)
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	start := time.Now()
	r, err := e.Analyze(ctx, p, nil, Limits{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Errorf("cancel took %v", time.Since(start))
	}
	t.Logf("startpos: best=%s depth=%d nps=%d score=%s", r.Best, r.Info.Depth, r.Info.NPS, r.Info.ScoreString())
	if r.Info.Depth < 6 {
		t.Errorf("too shallow: %d", r.Info.Depth)
	}
}
