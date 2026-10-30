package engine

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"chessvision/internal/chess"
)

func buildUCIServer(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "uciserver")
	cmd := exec.Command("go", "build", "-o", exe, "chessvision/cmd/uciserver")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build uciserver: %v\n%s", err, out)
	}
	return exe
}

func TestUCIClient(t *testing.T) {
	exe := buildUCIServer(t)
	u, err := StartUCI(exe, UCIOptions{Threads: 2, HashMB: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if u.Name() != "ChessVision-UCI-Test" {
		t.Errorf("name = %q", u.Name())
	}
	p, _ := chess.ParseFEN("6k1/5ppp/8/8/8/8/8/R3K3 w - - 0 1")
	var infos int
	res, err := u.Analyze(context.Background(), p, nil, Limits{MoveTime: 500 * time.Millisecond}, func(Info) { infos++ })
	if err != nil {
		t.Fatal(err)
	}
	if res.Best.UCI() != "a1a8" || !res.Info.IsMate || res.Info.Mate != 1 {
		t.Errorf("got %s %s", res.Best, res.Info.ScoreString())
	}
	if infos == 0 {
		t.Error("no info reports")
	}
	// cancel an infinite search
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	start := time.Now()
	res, err = u.Analyze(ctx, chess.Start(), nil, Limits{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second || res.Best == chess.NoMove {
		t.Errorf("stop handling slow/bad: %v %v", time.Since(start), res.Best)
	}
	// finished game position
	mate, _ := chess.ParseFEN("R5k1/5ppp/8/8/8/8/8/4K3 b - - 0 1")
	if _, err := u.Analyze(context.Background(), mate, nil, Limits{MoveTime: 100 * time.Millisecond}, nil); err == nil {
		t.Error("expected error for checkmated position")
	}
}

func TestParseInfo(t *testing.T) {
	p := chess.Start()
	in, ok := parseInfo("info depth 18 seldepth 25 multipv 2 score cp -34 nodes 1000 nps 500000 time 2 pv e2e4 e7e5 g1f3", p)
	if !ok || in.Depth != 18 || in.MultiPV != 2 || in.ScoreCP != -34 || len(in.PV) != 3 {
		t.Fatalf("bad parse: %+v ok=%v", in, ok)
	}
	in, ok = parseInfo("info depth 9 score mate -3 pv e2e4", p)
	if !ok || !in.IsMate || in.Mate != -3 {
		t.Fatalf("bad mate parse: %+v", in)
	}
	if _, ok := parseInfo("info depth 9 score cp 20 lowerbound pv e2e4", p); ok {
		t.Fatal("bound updates must be ignored")
	}
}
