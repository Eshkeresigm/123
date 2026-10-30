package chess

import "testing"

func TestPerft(t *testing.T) {
	cases := []struct {
		name  string
		fen   string
		nodes []uint64
	}{
		{"startpos", StartFEN, []uint64{20, 400, 8902, 197281, 4865609}},
		{"kiwipete", "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", []uint64{48, 2039, 97862, 4085603}},
		{"pos3", "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1", []uint64{14, 191, 2812, 43238, 674624}},
		{"pos4", "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1", []uint64{6, 264, 9467, 422333}},
		{"pos5", "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8", []uint64{44, 1486, 62379, 2103487}},
		{"pos6", "r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 10", []uint64{46, 2079, 89890, 3894594}},
	}
	for _, c := range cases {
		p, err := ParseFEN(c.fen)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for d, want := range c.nodes {
			if testing.Short() && want > 500000 {
				continue
			}
			if got := p.Perft(d + 1); got != want {
				t.Errorf("%s perft(%d) = %d, want %d", c.name, d+1, got, want)
			}
		}
	}
}

func TestFENRoundTrip(t *testing.T) {
	for _, fen := range []string{
		StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq - 0 2",
		"8/8/8/8/8/8/8/K6k b - - 12 40",
	} {
		p, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("%s: %v", fen, err)
		}
		if got := p.FEN(); got != fen {
			t.Errorf("round trip\n got %s\nwant %s", got, fen)
		}
		if p.Hash != p.ComputeHash() {
			t.Errorf("hash mismatch for %s", fen)
		}
	}
}

func TestIncrementalHash(t *testing.T) {
	p, _ := ParseFEN("r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	var walk func(q Position, d int)
	walk = func(q Position, d int) {
		if d == 0 {
			return
		}
		var l MoveList
		q.GenPseudo(&l, false)
		for _, m := range l.Slice() {
			n, ok := q.Make(m)
			if !ok {
				continue
			}
			if n.Hash != n.ComputeHash() {
				t.Fatalf("incremental hash mismatch after %s in %s", m, q.FEN())
			}
			walk(n, d-1)
		}
	}
	walk(p, 3)
}

func TestSANAndUCI(t *testing.T) {
	p := Start()
	steps := []struct{ uci, san string }{
		{"e2e4", "e4"}, {"e7e5", "e5"}, {"g1f3", "Nf3"}, {"b8c6", "Nc6"},
		{"f1b5", "Bb5"}, {"g8f6", "Nf6"}, {"e1g1", "O-O"}, {"f6e4", "Nxe4"},
	}
	for _, s := range steps {
		m, err := p.ParseUCI(s.uci)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.SAN(m, false); got != s.san {
			t.Errorf("SAN(%s) = %s, want %s", s.uci, got, s.san)
		}
		p, _ = func() (Position, bool) { return p.Make(m) }()
	}
	// disambiguation and mate
	q, err := ParseFEN("6k1/8/8/8/8/8/8/R3K2R w KQ - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := q.ParseUCI("a1d1")
	if got := q.SAN(m, false); got != "Rd1" {
		t.Errorf("got %s want Rd1", got)
	}
	dis, err := ParseFEN("7k/8/8/8/8/8/8/R5RK w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	m, _ = dis.ParseUCI("a1d1")
	if got := dis.SAN(m, false); got != "Rad1" {
		t.Errorf("got %s want Rad1", got)
	}
	mate, _ := ParseFEN("6k1/5ppp/8/8/8/8/8/R3K3 w - - 0 1")
	m, _ = mate.ParseUCI("a1a8")
	if got := mate.SAN(m, false); got != "Ra8#" {
		t.Errorf("got %s want Ra8#", got)
	}
	if got := mate.SAN(m, true); got != "Лa8#" {
		t.Errorf("got %s want Лa8#", got)
	}
}

func TestEnPassantAndPromotion(t *testing.T) {
	p, _ := ParseFEN("rnbqkbnr/ppp1pppp/8/8/3pP3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 3")
	m, err := p.ParseUCI("d4e3")
	if err != nil {
		t.Fatal(err)
	}
	q, ok := p.Make(m)
	if !ok || q.Board[28] != NoPiece {
		t.Fatalf("en passant failed: %s", q.FEN())
	}
	pp, _ := ParseFEN("8/P6k/8/8/8/8/8/K7 w - - 0 1")
	m, err = pp.ParseUCI("a7a8q")
	if err != nil {
		t.Fatal(err)
	}
	q, _ = pp.Make(m)
	if q.Board[56] != MakePiece(White, Queen) {
		t.Fatalf("promotion failed")
	}
}

func TestStatus(t *testing.T) {
	cases := []struct {
		fen  string
		want Result
	}{
		{"7k/5Q2/6K1/8/8/8/8/8 b - - 0 1", Stalemate},
		{"6k1/5ppp/8/8/8/8/8/R3K2r w - - 0 1", Ongoing},
		{"R5k1/5ppp/8/8/8/8/8/4K3 b - - 0 1", Checkmate},
		{"8/8/8/8/8/8/8/K1k5 w - - 0 1", DrawMaterial},
	}
	for _, c := range cases {
		p, err := ParseFEN(c.fen)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Status(); got != c.want {
			t.Errorf("%s: got %v want %v", c.fen, got, c.want)
		}
	}
}
