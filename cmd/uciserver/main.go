// uciserver exposes the built-in engine over the UCI protocol. It is used by
// the test-suite to exercise the external-engine client, and works as a
// stand-alone UCI engine as well.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"chessvision/internal/chess"
	"chessvision/internal/engine"
)

func main() {
	eng := engine.NewBuiltin(64)
	pos := chess.Start()
	var cancel context.CancelFunc
	var wg sync.WaitGroup
	out := bufio.NewWriter(os.Stdout)
	var mu sync.Mutex
	say := func(s string) {
		mu.Lock()
		fmt.Fprintln(out, s)
		out.Flush()
		mu.Unlock()
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "uci":
			say("id name ChessVision-UCI-Test")
			say("option name Hash type spin default 64 min 1 max 1024")
			say("uciok")
		case "isready":
			say("readyok")
		case "ucinewgame":
			eng.NewGame()
		case "setoption":
		case "position":
			if len(f) >= 2 && f[1] == "fen" {
				p, err := chess.ParseFEN(strings.Join(f[2:], " "))
				if err == nil {
					pos = p
				}
			} else {
				pos = chess.Start()
			}
		case "go":
			lim := engine.Limits{}
			for i := 1; i+1 < len(f); i++ {
				switch f[i] {
				case "movetime":
					n, _ := strconv.Atoi(f[i+1])
					lim.MoveTime = time.Duration(n) * time.Millisecond
				case "depth":
					n, _ := strconv.Atoi(f[i+1])
					lim.Depth = n
				}
			}
			ctx, c := context.WithCancel(context.Background())
			cancel = c
			p := pos
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := eng.Analyze(ctx, p, nil, lim, func(in engine.Info) {
					pv := make([]string, len(in.PV))
					for i, m := range in.PV {
						pv[i] = m.UCI()
					}
					score := fmt.Sprintf("cp %d", in.ScoreCP)
					if in.IsMate {
						score = fmt.Sprintf("mate %d", in.Mate)
					}
					say(fmt.Sprintf("info depth %d score %s nodes %d nps %d pv %s", in.Depth, score, in.Nodes, in.NPS, strings.Join(pv, " ")))
				})
				if err != nil {
					say("bestmove (none)")
					return
				}
				say("bestmove " + res.Best.UCI())
			}()
		case "stop":
			if cancel != nil {
				cancel()
			}
		case "quit":
			if cancel != nil {
				cancel()
			}
			wg.Wait()
			return
		}
	}
}
