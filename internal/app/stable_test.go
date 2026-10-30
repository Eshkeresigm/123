package app

import (
	"testing"
	"time"

	"chessvision/internal/chess"
)

// While the engine thinks, the suggestion must not flip: it appears once,
// when the search is finished, and stays.
func TestSuggestionDoesNotFlip(t *testing.T) {
	fp := &fakePlatform{}
	start := chess.Start()
	fp.show(start.Board, chess.NoMove, true)
	c := New(fp, t.TempDir(), t.TempDir(), "")
	c.scanEvery = 40 * time.Millisecond
	c.Start()
	defer c.Close()
	c.SetMoveTime(700)
	c.SelectRegion()
	c.ToggleRun()
	seen := map[chess.Move]bool{}
	deadline := time.Now().Add(2500 * time.Millisecond)
	sawBestWhileAnalyzing := false
	for time.Now().Before(deadline) {
		m := c.Snapshot()
		if m.Best != chess.NoMove {
			seen[m.Best] = true
			if m.Analyzing {
				sawBestWhileAnalyzing = true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(seen) != 1 {
		t.Fatalf("suggestion flipped: %d different moves shown", len(seen))
	}
	if sawBestWhileAnalyzing {
		t.Error("a move was shown before the search finished")
	}
}
