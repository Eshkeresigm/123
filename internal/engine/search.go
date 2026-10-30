package engine

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"chessvision/internal/chess"
)

const (
	inf       = 32000
	mateScore = 30000
	mateBound = mateScore - 400
	maxPly    = 100
)

const (
	ttExact = iota + 1
	ttLower
	ttUpper
)

type ttEntry struct {
	key   uint64
	move  chess.Move
	score int16
	depth int8
	flag  uint8
}

// Builtin is the in-process alpha-beta engine.
type Builtin struct {
	mu   sync.Mutex
	tt   []ttEntry
	mask uint64
	hist [12][64]int32
}

// NewBuiltin creates the engine with a transposition table of about hashMB MiB.
func NewBuiltin(hashMB int) *Builtin {
	if hashMB < 4 {
		hashMB = 4
	}
	n := 1
	for n*16 < hashMB<<20 {
		n <<= 1
	}
	return &Builtin{tt: make([]ttEntry, n), mask: uint64(n - 1)}
}

// Name implements Engine.
func (b *Builtin) Name() string { return "ChessVision встроенный движок" }

// Close implements Engine.
func (b *Builtin) Close() error { return nil }

// NewGame implements Engine.
func (b *Builtin) NewGame() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tt {
		b.tt[i] = ttEntry{}
	}
	b.hist = [12][64]int32{}
}

var lmrTable [64][64]int

func init() {
	for d := 1; d < 64; d++ {
		for m := 1; m < 64; m++ {
			lmrTable[d][m] = int(0.75 + math.Log(float64(d))*math.Log(float64(m))/2.2)
		}
	}
}

type searcher struct {
	b        *Builtin
	ctx      context.Context
	deadline time.Time
	hasTime  bool
	start    time.Time
	nodes    uint64
	stopped  bool
	keys     []uint64 // game history + current path
	baseKeys int
	killers  [maxPly + 2][2]chess.Move
	pv       [maxPly + 2][maxPly + 2]chess.Move
	pvLen    [maxPly + 2]int
	rootMove chess.Move
	rootScr  int
	rootSet  bool
}

var errNoMoves = errors.New("позиция без легальных ходов")

// Analyze implements Engine.
func (b *Builtin) Analyze(ctx context.Context, pos chess.Position, history []uint64, lim Limits, report func(Info)) (Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	moves := pos.LegalMoves()
	if len(moves) == 0 {
		return Result{}, errNoMoves
	}
	s := &searcher{b: b, ctx: ctx, start: time.Now()}
	if lim.MoveTime > 0 {
		s.deadline = s.start.Add(lim.MoveTime)
		s.hasTime = true
	}
	s.keys = append(s.keys[:0], history...)
	s.keys = append(s.keys, pos.Hash)
	s.baseKeys = len(s.keys)
	maxDepth := lim.Depth
	if maxDepth <= 0 || maxDepth > maxPly-4 {
		maxDepth = maxPly - 4
	}
	// age the history table
	for i := range b.hist {
		for j := range b.hist[i] {
			b.hist[i][j] /= 2
		}
	}

	var res Result
	res.Best = moves[0]
	for d := 1; d <= maxDepth; d++ {
		s.rootSet = false
		score := s.negamax(&pos, d, -inf, inf, 0, true)
		if s.stopped {
			if s.rootSet && d > 1 {
				res.Best = s.rootMove
				res.Info.ScoreCP = s.rootScr
			}
			break
		}
		best := s.pv[0][0]
		if best == chess.NoMove {
			best = res.Best
		}
		res.Best = best
		info := s.makeInfo(d, score)
		res.Info = info
		if s.pvLen[0] > 1 {
			res.Ponder = s.pv[0][1]
		}
		if report != nil {
			report(info)
		}
		if len(moves) == 1 && d >= 4 {
			break
		}
		if score >= mateBound && d >= 3 && mateScore-score <= d {
			break // found a forced mate that is already fully resolved
		}
		if s.hasTime && time.Since(s.start) > time.Duration(float64(lim.MoveTime)*0.62) && d >= 6 {
			// the next iteration would likely not finish
			break
		}
	}
	res.Info.Nodes = s.nodes
	res.Info.Elapsed = time.Since(s.start)
	if res.Info.Elapsed > 0 {
		res.Info.NPS = uint64(float64(s.nodes) / res.Info.Elapsed.Seconds())
	}
	res.Lines = []Info{res.Info}
	return res, nil
}

func (s *searcher) makeInfo(depth, score int) Info {
	info := Info{Depth: depth, Nodes: s.nodes, Elapsed: time.Since(s.start), MultiPV: 1}
	if info.Elapsed > 0 {
		info.NPS = uint64(float64(s.nodes) / info.Elapsed.Seconds())
	}
	if score >= mateBound {
		info.IsMate = true
		info.Mate = (mateScore - score + 1) / 2
	} else if score <= -mateBound {
		info.IsMate = true
		info.Mate = -((mateScore + score + 1) / 2)
	}
	info.ScoreCP = score
	for i := 0; i < s.pvLen[0]; i++ {
		info.PV = append(info.PV, s.pv[0][i])
	}
	return info
}

func (s *searcher) checkStop() {
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			s.stopped = true
			return
		default:
		}
	}
	if s.hasTime && time.Now().After(s.deadline) {
		s.stopped = true
	}
}

func scoreToTT(score, ply int) int {
	if score >= mateBound {
		return score + ply
	}
	if score <= -mateBound {
		return score - ply
	}
	return score
}

func scoreFromTT(score, ply int) int {
	if score >= mateBound {
		return score - ply
	}
	if score <= -mateBound {
		return score + ply
	}
	return score
}

func (s *searcher) isDraw(p *chess.Position) bool {
	if p.Half >= 100 {
		return true
	}
	// repetition: look back through same-side positions within the halfmove clock
	n := len(s.keys)
	lim := imin(int(p.Half), n-1)
	count := 0
	for i := 4; i <= lim; i += 2 {
		if s.keys[n-1-i] == p.Hash {
			count++
			// a repetition inside the search path or a third occurrence overall is a draw
			if n-1-i >= s.baseKeys-1 || count >= 2 {
				return true
			}
		}
	}
	return p.InsufficientMaterial()
}

func hasNonPawn(p *chess.Position, c chess.Color) bool {
	return p.Pieces[chess.MakePiece(c, chess.Knight)]|p.Pieces[chess.MakePiece(c, chess.Bishop)]|
		p.Pieces[chess.MakePiece(c, chess.Rook)]|p.Pieces[chess.MakePiece(c, chess.Queen)] != 0
}

var seeVal = [7]int{100, 320, 330, 500, 900, 20000, 0}

func (s *searcher) scoreMoves(p *chess.Position, ml *chess.MoveList, scores *[256]int32, ttMove chess.Move, ply int) {
	for i := 0; i < ml.N; i++ {
		m := ml.M[i]
		var sc int32
		switch {
		case m == ttMove:
			sc = 1 << 30
		default:
			to := m.To()
			victim := p.Board[to]
			mover := p.Board[m.From()]
			isEP := mover.Type() == chess.Pawn && chess.File(m.From()) != chess.File(to) && victim == chess.NoPiece
			switch {
			case victim != chess.NoPiece || isEP:
				v := 0
				if victim != chess.NoPiece {
					v = seeVal[victim.Type()]
				} else {
					v = 100
				}
				sc = 1<<24 + int32(v*16-seeVal[mover.Type()]/10)
				if m.Promo() == chess.Queen {
					sc += 1 << 20
				}
			case m.Promo() == chess.Queen:
				sc = 1<<24 + 1<<19
			case m.Promo() != chess.NoType:
				sc = -1 << 20
			case m == s.killers[ply][0]:
				sc = 1<<23 + 2
			case m == s.killers[ply][1]:
				sc = 1<<23 + 1
			default:
				sc = s.b.hist[mover][to]
			}
		}
		scores[i] = sc
	}
}

func pickMove(ml *chess.MoveList, scores *[256]int32, from int) chess.Move {
	best := from
	for j := from + 1; j < ml.N; j++ {
		if scores[j] > scores[best] {
			best = j
		}
	}
	ml.M[from], ml.M[best] = ml.M[best], ml.M[from]
	scores[from], scores[best] = scores[best], scores[from]
	return ml.M[from]
}

func (s *searcher) negamax(p *chess.Position, depth, alpha, beta, ply int, canNull bool) int {
	s.pvLen[ply] = ply
	if s.stopped {
		return 0
	}
	if ply > 0 && s.isDraw(p) {
		return 0
	}
	inCheck := p.InCheck()
	if inCheck && ply < maxPly-2 {
		depth++
	}
	if depth <= 0 || ply >= maxPly-1 {
		return s.quiesce(p, alpha, beta, ply)
	}
	s.nodes++
	if s.nodes&2047 == 0 {
		s.checkStop()
		if s.stopped {
			return 0
		}
	}
	if ply > 0 {
		alpha = imax(alpha, -mateScore+ply)
		beta = imin(beta, mateScore-ply-1)
		if alpha >= beta {
			return alpha
		}
	}
	isPV := beta-alpha > 1

	idx := p.Hash & s.b.mask
	e := s.b.tt[idx]
	ttMove := chess.NoMove
	if e.key == p.Hash {
		ttMove = e.move
		if ply > 0 && int(e.depth) >= depth {
			sc := scoreFromTT(int(e.score), ply)
			switch e.flag {
			case ttExact:
				return sc
			case ttLower:
				if sc >= beta {
					return sc
				}
			case ttUpper:
				if sc <= alpha {
					return sc
				}
			}
		}
	}

	staticEval := 0
	if !inCheck {
		staticEval = Evaluate(p)
	}
	if !isPV && !inCheck && ply > 0 {
		// reverse futility
		if depth <= 6 && staticEval-85*depth >= beta && beta > -mateBound {
			return staticEval
		}
		// null move
		if canNull && depth >= 3 && staticEval >= beta && hasNonPawn(p, p.Side) {
			r := 3 + depth/4
			np := p.MakeNull()
			s.keys = append(s.keys, np.Hash)
			sc := -s.negamax(&np, depth-1-r, -beta, -beta+1, ply+1, false)
			s.keys = s.keys[:len(s.keys)-1]
			if s.stopped {
				return 0
			}
			if sc >= beta {
				if sc >= mateBound {
					sc = beta
				}
				return sc
			}
		}
	}
	if ttMove == chess.NoMove && depth >= 5 && !inCheck {
		depth-- // internal iterative reduction
	}

	var ml chess.MoveList
	var scores [256]int32
	p.GenPseudo(&ml, false)
	s.scoreMoves(p, &ml, &scores, ttMove, ply)

	bestScore := -inf
	bestMove := chess.NoMove
	origAlpha := alpha
	legal := 0
	var quietsTried [64]chess.Move
	nq := 0
	for i := 0; i < ml.N; i++ {
		m := pickMove(&ml, &scores, i)
		child, ok := p.Make(m)
		if !ok {
			continue
		}
		legal++
		mover := p.Board[m.From()]
		isCapture := p.Board[m.To()] != chess.NoPiece || (mover.Type() == chess.Pawn && chess.File(m.From()) != chess.File(m.To()))
		isQuiet := !isCapture && m.Promo() == chess.NoType
		givesCheck := false
		if isQuiet && !inCheck && depth <= 3 {
			givesCheck = child.InCheck()
		}
		if !isPV && !inCheck && isQuiet && legal > 1 && !givesCheck && bestScore > -mateBound {
			if depth <= 3 && staticEval+110*depth+90 <= alpha {
				continue // futility
			}
			if depth <= 4 && legal > 4+depth*depth {
				continue // late move pruning
			}
		}
		s.keys = append(s.keys, child.Hash)
		newDepth := depth - 1
		var sc int
		if legal == 1 {
			sc = -s.negamax(&child, newDepth, -beta, -alpha, ply+1, true)
		} else {
			red := 0
			if depth >= 3 && isQuiet && !inCheck && legal > 3 {
				red = lmrTable[imin(depth, 63)][imin(legal, 63)]
				if isPV {
					red--
				}
				if m == s.killers[ply][0] || m == s.killers[ply][1] {
					red--
				}
				if s.b.hist[mover][m.To()] > 4000 {
					red--
				}
				red = imax(0, imin(red, newDepth-1))
			}
			sc = -s.negamax(&child, newDepth-red, -alpha-1, -alpha, ply+1, true)
			if sc > alpha && red > 0 {
				sc = -s.negamax(&child, newDepth, -alpha-1, -alpha, ply+1, true)
			}
			if sc > alpha && sc < beta {
				sc = -s.negamax(&child, newDepth, -beta, -alpha, ply+1, true)
			}
		}
		s.keys = s.keys[:len(s.keys)-1]
		if s.stopped {
			return 0
		}
		if isQuiet && nq < len(quietsTried) {
			quietsTried[nq] = m
			nq++
		}
		if sc > bestScore {
			bestScore = sc
			bestMove = m
			if sc > alpha {
				alpha = sc
				s.pv[ply][ply] = m
				for j := ply + 1; j < s.pvLen[ply+1]; j++ {
					s.pv[ply][j] = s.pv[ply+1][j]
				}
				s.pvLen[ply] = imax(s.pvLen[ply+1], ply+1)
				if ply == 0 {
					s.rootMove, s.rootScr, s.rootSet = m, sc, true
				}
				if sc >= beta {
					if isQuiet {
						if s.killers[ply][0] != m {
							s.killers[ply][1] = s.killers[ply][0]
							s.killers[ply][0] = m
						}
						bonus := int32(depth * depth)
						if bonus > 400 {
							bonus = 400
						}
						s.b.hist[mover][m.To()] += bonus
						for k := 0; k < nq-1; k++ {
							q := quietsTried[k]
							s.b.hist[p.Board[q.From()]][q.To()] -= bonus
						}
					}
					break
				}
			}
		}
	}
	if legal == 0 {
		if inCheck {
			return -mateScore + ply
		}
		return 0
	}
	flag := uint8(ttExact)
	switch {
	case bestScore <= origAlpha:
		flag = ttUpper
	case bestScore >= beta:
		flag = ttLower
	}
	if e.key != p.Hash || depth >= int(e.depth)-1 || flag == ttExact {
		s.b.tt[idx] = ttEntry{key: p.Hash, move: bestMove, score: int16(scoreToTT(bestScore, ply)), depth: int8(depth), flag: flag}
	}
	return bestScore
}

func (s *searcher) quiesce(p *chess.Position, alpha, beta, ply int) int {
	s.pvLen[ply] = ply
	s.nodes++
	if s.nodes&2047 == 0 {
		s.checkStop()
		if s.stopped {
			return 0
		}
	}
	if ply >= maxPly-1 {
		return Evaluate(p)
	}
	inCheck := p.InCheck()
	best := -inf
	stand := 0
	if !inCheck {
		stand = Evaluate(p)
		if stand >= beta {
			return stand
		}
		if stand > alpha {
			alpha = stand
		}
		best = stand
	}
	var ml chess.MoveList
	var scores [256]int32
	p.GenPseudo(&ml, !inCheck)
	s.scoreMoves(p, &ml, &scores, chess.NoMove, ply)
	legal := 0
	for i := 0; i < ml.N; i++ {
		m := pickMove(&ml, &scores, i)
		if !inCheck {
			victim := p.Board[m.To()]
			gain := 100
			if victim != chess.NoPiece {
				gain = seeVal[victim.Type()]
			}
			if m.Promo() == chess.Queen {
				gain += 800
			}
			if stand+gain+180 < alpha {
				continue // delta pruning
			}
		}
		child, ok := p.Make(m)
		if !ok {
			continue
		}
		legal++
		sc := -s.quiesce(&child, -beta, -alpha, ply+1)
		if s.stopped {
			return 0
		}
		if sc > best {
			best = sc
			if sc > alpha {
				alpha = sc
				if sc >= beta {
					return sc
				}
			}
		}
	}
	if inCheck && legal == 0 {
		return -mateScore + ply
	}
	return best
}
