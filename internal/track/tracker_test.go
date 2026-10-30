package track

import (
	"testing"

	"chessvision/internal/chess"
)

func head(fen string) string {
	n := 0
	for i := range fen {
		if fen[i] == ' ' {
			n++
			if n == 4 {
				return fen[:i]
			}
		}
	}
	return fen
}

type rng uint64

func (r *rng) next(n int) int {
	*r = *r*6364136223846793005 + 1442695040888963407
	return int((*r >> 33) % rng(n))
}

// feed sends b twice (as two consecutive frames) and returns the last event.
func feedStable(tr *Tracker, b [64]chess.Piece) Event {
	var ev Event
	for i := 0; i < 3; i++ {
		if e := tr.Feed(b); e.Kind != None {
			ev = e
		}
	}
	return ev
}

func TestFollowsRandomGameWithNoise(t *testing.T) {
	r := rng(42)
	for game := 0; game < 30; game++ {
		tr := New()
		p := chess.Start()
		ev := feedStable(tr, p.Board)
		if ev.Kind != NewGame {
			t.Fatalf("game %d: start not detected: %v", game, ev)
		}
		for ply := 0; ply < 70; ply++ {
			moves := p.LegalMoves()
			if len(moves) == 0 {
				break
			}
			m := moves[r.next(len(moves))]
			q, _ := p.Make(m)
			// a transient frame in which the piece is lifted (square empty), seen once
			if r.next(3) == 0 {
				glitch := q.Board
				glitch[m.To()] = chess.NoPiece
				if e := tr.Feed(glitch); e.Kind != None {
					t.Fatalf("game %d ply %d: glitch accepted: %v", game, ply, e)
				}
			}
			ev := feedStable(tr, q.Board)
			if ev.Kind != Move || len(ev.Moves) != 1 || ev.Moves[0] != m {
				t.Fatalf("game %d ply %d: want move %s got %+v (fen %s)", game, ply, m, ev, p.FEN())
			}
			p = q
			got := tr.Position()
			if got.FEN() != p.FEN() {
				t.Fatalf("game %d ply %d: position drift\n got %s\nwant %s", game, ply, got.FEN(), p.FEN())
			}
		}
	}
}

func TestMissedFrameTwoPlies(t *testing.T) {
	tr := New()
	p := chess.Start()
	feedStable(tr, p.Board)
	m1, _ := p.ParseUCI("e2e4")
	p1, _ := p.Make(m1)
	m2, _ := p1.ParseUCI("e7e5")
	p2, _ := p1.Make(m2)
	ev := feedStable(tr, p2.Board)
	if ev.Kind != Moves || len(ev.Moves) != 2 || ev.Moves[0] != m1 || ev.Moves[1] != m2 {
		t.Fatalf("got %+v", ev)
	}
	if tr.Position().FEN() != p2.FEN() {
		t.Fatalf("fen %s want %s", tr.Position().FEN(), p2.FEN())
	}
}

func TestTakebackAndNewGame(t *testing.T) {
	tr := New()
	p := chess.Start()
	feedStable(tr, p.Board)
	var boards [][64]chess.Piece
	for _, u := range []string{"e2e4", "e7e5", "g1f3"} {
		m, _ := p.ParseUCI(u)
		p, _ = p.Make(m)
		boards = append(boards, p.Board)
		feedStable(tr, p.Board)
	}
	if ev := feedStable(tr, boards[0]); ev.Kind != Takeback {
		t.Fatalf("takeback not detected: %+v", ev)
	}
	if tr.Position().Side != chess.Black || len(tr.History()) != 1 {
		t.Fatalf("bad state after takeback: %s", tr.Position().FEN())
	}
	if ev := feedStable(tr, startBoard()); ev.Kind != Takeback && ev.Kind != NewGame {
		t.Fatalf("return to start: %+v", ev)
	}
	// play on, then a brand-new game from a different position state
	feedStable(tr, boards[1])
	if ev := feedStable(tr, startBoard()); ev.Kind != NewGame && ev.Kind != Takeback {
		t.Fatalf("new game: %+v", ev)
	}
}

func TestMidGameAdoptionAndSideFlip(t *testing.T) {
	// game position where black is actually to move, but we assume white
	p, _ := chess.ParseFEN("r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R b KQkq - 2 3")
	tr := New()
	tr.SetAssumedSide(chess.White)
	if ev := feedStable(tr, p.Board); ev.Kind != Adopted {
		t.Fatalf("adopt: %+v", ev)
	}
	if !tr.SideUnsure() {
		t.Fatal("side must be flagged as guessed")
	}
	m, _ := p.ParseUCI("g8f6")
	q, _ := p.Make(m)
	ev := feedStable(tr, q.Board)
	if ev.Kind != Flipped || ev.Moves[0] != m {
		t.Fatalf("flip not detected: %+v", ev)
	}
	if head(tr.Position().FEN()) != head(q.FEN()) {
		t.Fatalf("fen %s want %s", tr.Position().FEN(), q.FEN())
	}
	if tr.SideUnsure() {
		t.Fatal("side should be certain now")
	}
}

func TestResyncOnGarbage(t *testing.T) {
	tr := New()
	feedStable(tr, startBoard())
	other, _ := chess.ParseFEN("4k3/8/8/8/8/8/4P3/4K3 w - - 0 1")
	var last Event
	for i := 0; i < 8; i++ {
		if e := tr.Feed(other.Board); e.Kind != None {
			last = e
		}
	}
	if last.Kind != Resync {
		t.Fatalf("expected resync, got %+v", last)
	}
	if tr.Position().Board != other.Board {
		t.Fatal("position not adopted")
	}
}

func TestPromotionAndCastlingTracked(t *testing.T) {
	p, _ := chess.ParseFEN("r3k2r/1P6/8/8/8/8/8/R3K2R w KQkq - 0 1")
	tr := New()
	tr.SetPosition(p, false)
	for _, u := range []string{"e1g1", "h8h7", "b7a8q"} {
		m, err := p.ParseUCI(u)
		if err != nil {
			t.Fatal(u, err)
		}
		q, _ := p.Make(m)
		ev := feedStable(tr, q.Board)
		if ev.Kind != Move || ev.Moves[0] != m {
			t.Fatalf("%s: %+v", u, ev)
		}
		p = q
	}
	if tr.Position().FEN() != p.FEN() {
		t.Fatalf("fen %s want %s", tr.Position().FEN(), p.FEN())
	}
}

// A king in check settles whose move it is when a position is taken from the
// screen, even if the assumed side is the other one.
func TestAdoptUsesCheckToFindSide(t *testing.T) {
	// black king on e8 is in check from the rook on e1; only Black can be on move
	pos, err := chess.ParseFEN("4k3/8/8/8/8/8/8/4R1K1 b - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	for _, assume := range []chess.Color{chess.White, chess.Black} {
		tr := New()
		tr.SetAssumedSide(assume)
		var ev Event
		for i := 0; i < 6 && ev.Kind == None; i++ {
			ev = tr.Feed(pos.Board)
		}
		if ev.Kind != Adopted {
			t.Fatalf("assume=%v: event %v", assume, ev.Kind)
		}
		if tr.Position().Side != chess.Black || tr.SideUnsure() {
			t.Errorf("assume=%v: side=%v unsure=%v", assume, tr.Position().Side, tr.SideUnsure())
		}
	}
}
