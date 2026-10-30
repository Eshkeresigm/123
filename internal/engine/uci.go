package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"chessvision/internal/chess"
)

// UCIOptions are sent to the engine right after the handshake.
type UCIOptions struct {
	Threads int
	HashMB  int
	MultiPV int
	Extra   map[string]string // any other option name -> value
}

// UCI drives an external engine process over the UCI protocol.
type UCI struct {
	path  string
	name  string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	mu    sync.Mutex // one analysis at a time
	multi int
	dead  chan struct{}
}

// StartUCI launches the engine and performs the UCI handshake.
func StartUCI(path string, opt UCIOptions) (*UCI, error) {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	hideWindow(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	u := &UCI{path: path, cmd: cmd, stdin: in, lines: make(chan string, 1024), dead: make(chan struct{}), multi: 1}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			select {
			case u.lines <- strings.TrimSpace(sc.Text()):
			default:
				// drop info flood if nobody listens; keep important lines
				l := strings.TrimSpace(sc.Text())
				if !strings.HasPrefix(l, "info") {
					u.lines <- l
				}
			}
		}
		close(u.dead)
		close(u.lines)
	}()
	u.send("uci")
	deadline := time.After(12 * time.Second)
	for ok := false; !ok; {
		select {
		case l, alive := <-u.lines:
			if !alive {
				return nil, errors.New("движок завершился во время запуска")
			}
			if strings.HasPrefix(l, "id name ") {
				u.name = strings.TrimPrefix(l, "id name ")
			}
			if l == "uciok" {
				ok = true
			}
		case <-deadline:
			u.kill()
			return nil, errors.New("движок не ответил на команду uci")
		}
	}
	if u.name == "" {
		u.name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if opt.Threads > 0 {
		u.send(fmt.Sprintf("setoption name Threads value %d", opt.Threads))
	}
	if opt.HashMB > 0 {
		u.send(fmt.Sprintf("setoption name Hash value %d", opt.HashMB))
	}
	if opt.MultiPV > 1 {
		u.multi = opt.MultiPV
		u.send(fmt.Sprintf("setoption name MultiPV value %d", opt.MultiPV))
	}
	for k, v := range opt.Extra {
		u.send(fmt.Sprintf("setoption name %s value %s", k, v))
	}
	if err := u.ready(10 * time.Second); err != nil {
		u.kill()
		return nil, err
	}
	return u, nil
}

func (u *UCI) send(s string) {
	fmt.Fprintln(u.stdin, s)
}

func (u *UCI) ready(timeout time.Duration) error {
	u.send("isready")
	deadline := time.After(timeout)
	for {
		select {
		case l, alive := <-u.lines:
			if !alive {
				return errors.New("движок завершился")
			}
			if l == "readyok" {
				return nil
			}
		case <-deadline:
			return errors.New("движок не ответил на isready")
		}
	}
}

func (u *UCI) kill() {
	if u.cmd.Process != nil {
		_ = u.cmd.Process.Kill()
	}
	_ = u.cmd.Wait()
}

// Name implements Engine.
func (u *UCI) Name() string { return u.name }

// Path returns the executable path.
func (u *UCI) Path() string { return u.path }

// NewGame implements Engine.
func (u *UCI) NewGame() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.send("ucinewgame")
	_ = u.ready(5 * time.Second)
}

// Close implements Engine.
func (u *UCI) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.send("quit")
	done := make(chan struct{})
	go func() { _ = u.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		_ = u.cmd.Process.Kill()
		<-done
	}
	return nil
}

// Analyze implements Engine.
func (u *UCI) Analyze(ctx context.Context, pos chess.Position, history []uint64, lim Limits, report func(Info)) (Result, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	select {
	case <-u.dead:
		return Result{}, errors.New("процесс движка завершён")
	default:
	}
	// drain stale lines
	for drained := false; !drained; {
		select {
		case _, alive := <-u.lines:
			if !alive {
				return Result{}, errors.New("процесс движка завершён")
			}
		default:
			drained = true
		}
	}
	u.send("position fen " + pos.FEN())
	cmd := "go"
	switch {
	case lim.MoveTime > 0 && lim.Depth > 0:
		cmd += fmt.Sprintf(" movetime %d depth %d", lim.MoveTime.Milliseconds(), lim.Depth)
	case lim.MoveTime > 0:
		cmd += fmt.Sprintf(" movetime %d", lim.MoveTime.Milliseconds())
	case lim.Depth > 0:
		cmd += fmt.Sprintf(" depth %d", lim.Depth)
	default:
		cmd += " infinite"
	}
	u.send(cmd)

	start := time.Now()
	lines := map[int]Info{}
	stopSent := false
	var stopDeadline <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if !stopSent {
				u.send("stop")
				stopSent = true
				stopDeadline = time.After(3 * time.Second)
			}
			// keep draining until bestmove, but do not wait on ctx again
			ctx = context.Background()
		case <-stopDeadline:
			return Result{}, errors.New("движок не ответил на stop")
		case l, alive := <-u.lines:
			if !alive {
				return Result{}, errors.New("движок неожиданно завершился")
			}
			switch {
			case strings.HasPrefix(l, "info "):
				if info, ok := parseInfo(l, pos); ok {
					info.Elapsed = time.Since(start)
					lines[info.MultiPV] = info
					if info.MultiPV == 1 && report != nil {
						report(info)
					}
				}
			case strings.HasPrefix(l, "bestmove"):
				f := strings.Fields(l)
				var res Result
				if len(f) >= 2 && f[1] != "(none)" && f[1] != "0000" {
					m, err := pos.ParseUCI(f[1])
					if err != nil {
						return Result{}, fmt.Errorf("движок вернул недопустимый ход %s", f[1])
					}
					res.Best = m
					if q, ok := pos.Make(m); ok && len(f) >= 4 && f[2] == "ponder" {
						if pm, err := q.ParseUCI(f[3]); err == nil {
							res.Ponder = pm
						}
					}
				} else {
					return Result{}, errNoMoves
				}
				keys := make([]int, 0, len(lines))
				for k := range lines {
					keys = append(keys, k)
				}
				sort.Ints(keys)
				for _, k := range keys {
					res.Lines = append(res.Lines, lines[k])
				}
				if len(res.Lines) > 0 {
					res.Info = res.Lines[0]
				}
				res.Info.Elapsed = time.Since(start)
				// the PV's first move may differ from bestmove only in rare races; trust bestmove
				if len(res.Info.PV) == 0 || res.Info.PV[0] != res.Best {
					res.Info.PV = append([]chess.Move{res.Best}, nil...)
				}
				return res, nil
			}
		}
	}
}

func parseInfo(line string, pos chess.Position) (Info, bool) {
	f := strings.Fields(line)
	info := Info{MultiPV: 1}
	hasScore := false
	for i := 1; i < len(f); i++ {
		switch f[i] {
		case "depth":
			if i+1 < len(f) {
				info.Depth, _ = strconv.Atoi(f[i+1])
				i++
			}
		case "multipv":
			if i+1 < len(f) {
				info.MultiPV, _ = strconv.Atoi(f[i+1])
				i++
			}
		case "nodes":
			if i+1 < len(f) {
				n, _ := strconv.ParseUint(f[i+1], 10, 64)
				info.Nodes = n
				i++
			}
		case "nps":
			if i+1 < len(f) {
				n, _ := strconv.ParseUint(f[i+1], 10, 64)
				info.NPS = n
				i++
			}
		case "score":
			if i+2 < len(f) {
				v, _ := strconv.Atoi(f[i+2])
				switch f[i+1] {
				case "cp":
					info.ScoreCP = v
					hasScore = true
				case "mate":
					info.IsMate = true
					info.Mate = v
					if v > 0 {
						info.ScoreCP = mateScore - 2*v
					} else {
						info.ScoreCP = -mateScore - 2*v
					}
					hasScore = true
				}
				i += 2
				// optional lowerbound/upperbound token
				if i+1 < len(f) && (f[i+1] == "lowerbound" || f[i+1] == "upperbound") {
					return info, false // ignore bound-only updates
				}
			}
		case "pv":
			cur := pos
			for _, tok := range f[i+1:] {
				m, err := cur.ParseUCI(tok)
				if err != nil {
					break
				}
				info.PV = append(info.PV, m)
				q, ok := cur.Make(m)
				if !ok {
					break
				}
				cur = q
			}
			i = len(f)
		}
	}
	return info, hasScore && info.Depth > 0 && len(info.PV) > 0
}

// FindExternalEngines looks for Stockfish-like executables in the given directories.
func FindExternalEngines(dirs ...string) []string {
	var found []string
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		for _, pat := range []string{"stockfish*.exe", "stockfish*", "Stockfish*"} {
			matches, _ := filepath.Glob(filepath.Join(d, pat))
			for _, m := range matches {
				low := strings.ToLower(m)
				if strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".txt") || strings.HasSuffix(low, ".nnue") || strings.HasSuffix(low, ".md") {
					continue
				}
				if !seen[low] {
					seen[low] = true
					found = append(found, m)
				}
			}
		}
	}
	sort.Strings(found)
	return found
}
