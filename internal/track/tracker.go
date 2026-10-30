// Package track follows a game across a stream of recognised boards. It turns
// "the board looks like this now" into legal moves, so the engine always gets
// a complete position (side to move, castling rights, en passant, history).
package track

import (
	"chessvision/internal/chess"
)

// Kind classifies what a frame changed.
type Kind int

const (
	None     Kind = iota // nothing new
	NewGame              // the initial position appeared
	Move                 // one move was played
	Moves                // two moves (one frame was missed)
	Takeback             // the game went back to an earlier position
	Resync               // position taken over from the screen without history
	Adopted              // first position adopted (mid-game start)
	Flipped              // side to move corrected after a resync
	Invalid              // the board cannot be a legal position
)

// Event is the outcome of Feed.
type Event struct {
	Kind  Kind
	Moves []chess.Move
}

// Entry is one ply of the tracked game.
type Entry struct {
	Before chess.Position // position the move was played from
	Move   chess.Move
	After  chess.Position
}

type node struct {
	pos  chess.Position
	move chess.Move
	prev chess.Position
}

// Tracker follows the game.
type Tracker struct {
	stack      []node
	have       bool
	assume     chess.Color
	sideUnsure bool
	cand       [64]chess.Piece
	candN      int
	// StableFrames is how many identical frames are needed before a change is trusted.
	StableFrames int
	// ResyncFrames is how many identical unexplained frames trigger a resync.
	ResyncFrames int
}

// New creates an empty tracker.
func New() *Tracker {
	return &Tracker{StableFrames: 2, ResyncFrames: 6}
}

// SetAssumedSide sets whose turn it is assumed to be when a position is
// taken over from the screen without history (normally the user's colour).
func (t *Tracker) SetAssumedSide(c chess.Color) { t.assume = c }

// Reset forgets everything.
func (t *Tracker) Reset() {
	t.stack = nil
	t.have = false
	t.sideUnsure = false
	t.candN = 0
}

// Known reports whether a position is being tracked.
func (t *Tracker) Known() bool { return t.have }

// Position returns the current position.
func (t *Tracker) Position() chess.Position {
	if !t.have {
		return chess.Start()
	}
	return t.stack[len(t.stack)-1].pos
}

// SideUnsure is true when the side to move was guessed.
func (t *Tracker) SideUnsure() bool { return t.sideUnsure }

// SetPosition replaces the tracked game by a root position (e.g. from FEN).
func (t *Tracker) SetPosition(p chess.Position, sideUnsure bool) {
	t.stack = []node{{pos: p}}
	t.have = true
	t.sideUnsure = sideUnsure
	t.candN = 0
}

// Hashes returns the Zobrist keys of the positions before the current one.
func (t *Tracker) Hashes() []uint64 {
	if len(t.stack) < 2 {
		return nil
	}
	out := make([]uint64, 0, len(t.stack)-1)
	for _, n := range t.stack[:len(t.stack)-1] {
		out = append(out, n.pos.Hash)
	}
	return out
}

// History returns the played plies (oldest first).
func (t *Tracker) History() []Entry {
	var out []Entry
	for i := 1; i < len(t.stack); i++ {
		out = append(out, Entry{Before: t.stack[i].prev, Move: t.stack[i].move, After: t.stack[i].pos})
	}
	return out
}

func (t *Tracker) push(m chess.Move, q chess.Position) {
	cur := t.stack[len(t.stack)-1].pos
	t.stack = append(t.stack, node{pos: q, move: m, prev: cur})
}

func startBoard() [64]chess.Piece {
	p := chess.Start()
	return p.Board
}

// matchMove returns the legal move of pos leading to board b.
func matchMove(pos chess.Position, b [64]chess.Piece) (chess.Move, chess.Position, bool) {
	var l chess.MoveList
	pos.GenPseudo(&l, false)
	for _, m := range l.Slice() {
		q, ok := pos.Make(m)
		if ok && q.Board == b {
			return m, q, true
		}
	}
	return chess.NoMove, chess.Position{}, false
}

func flipSide(p chess.Position) (chess.Position, bool) {
	q := p
	q.Side = p.Side.Opp()
	q.EP = -1
	q.Rehash()
	if q.Validate() != nil {
		return p, false
	}
	return q, true
}

// Feed processes one recognised board.
func (t *Tracker) Feed(b [64]chess.Piece) Event {
	if !t.have {
		return t.feedUnknown(b)
	}
	cur := t.stack[len(t.stack)-1].pos
	if b == cur.Board {
		t.candN = 0
		return Event{}
	}
	if b == t.cand {
		t.candN++
	} else {
		t.cand, t.candN = b, 1
	}
	if t.candN < t.StableFrames {
		return Event{}
	}
	// 1. a single legal move
	if m, q, ok := matchMove(cur, b); ok {
		t.push(m, q)
		t.candN = 0
		return Event{Kind: Move, Moves: []chess.Move{m}}
	}
	// 2. the initial position again = new game
	if b == startBoard() && t.candN >= t.StableFrames {
		t.SetPosition(chess.Start(), false)
		return Event{Kind: NewGame}
	}
	// 3. takeback to an earlier position
	for i := len(t.stack) - 2; i >= 0; i-- {
		if t.stack[i].pos.Board == b {
			t.stack = t.stack[:i+1]
			t.candN = 0
			return Event{Kind: Takeback}
		}
	}
	// 4. two plies (a frame was missed)
	var l1 chess.MoveList
	cur.GenPseudo(&l1, false)
	var found [][2]chess.Move
	var foundPos []chess.Position
	var mid []chess.Position
	for _, m1 := range l1.Slice() {
		q1, ok := cur.Make(m1)
		if !ok {
			continue
		}
		if m2, q2, ok := matchMove(q1, b); ok {
			found = append(found, [2]chess.Move{m1, m2})
			mid = append(mid, q1)
			foundPos = append(foundPos, q2)
		}
	}
	if len(found) == 1 {
		t.push(found[0][0], mid[0])
		t.push(found[0][1], foundPos[0])
		t.candN = 0
		return Event{Kind: Moves, Moves: []chess.Move{found[0][0], found[0][1]}}
	}
	// 5. the side to move may have been guessed wrongly after a resync
	if t.sideUnsure {
		if f, ok := flipSide(cur); ok {
			if m, q, ok := matchMove(f, b); ok {
				t.stack[len(t.stack)-1].pos = f
				t.push(m, q)
				t.sideUnsure = false
				t.candN = 0
				return Event{Kind: Flipped, Moves: []chess.Move{m}}
			}
		}
	}
	// 6. give up on history and adopt the screen
	if t.candN >= t.ResyncFrames {
		pos, sure, err := t.fromScreen(b)
		if err != nil {
			return Event{Kind: Invalid}
		}
		t.SetPosition(pos, !sure)
		return Event{Kind: Resync}
	}
	return Event{}
}

func (t *Tracker) feedUnknown(b [64]chess.Piece) Event {
	if b == t.cand {
		t.candN++
	} else {
		t.cand, t.candN = b, 1
	}
	if t.candN < t.StableFrames+1 {
		return Event{}
	}
	if b == startBoard() {
		t.SetPosition(chess.Start(), false)
		return Event{Kind: NewGame}
	}
	pos, sure, err := t.fromScreen(b)
	if err != nil {
		return Event{Kind: Invalid}
	}
	t.SetPosition(pos, !sure)
	return Event{Kind: Adopted}
}

// fromScreen builds a position from a recognised board. The side to move is
// the assumed one unless a check settles it: the side that is NOT on move can
// never be in check, so a king under attack decides whose turn it is. sure
// reports that the side was decided by such evidence (or by the only legal
// reading) rather than guessed.
func (t *Tracker) fromScreen(b [64]chess.Piece) (pos chess.Position, sure bool, err error) {
	pos, err = chess.FromBoard(b, t.assume, 15, -1)
	if err == nil {
		// the assumed side is legal; if the other side is legal too it is a guess
		_, err2 := chess.FromBoard(b, t.assume.Opp(), 15, -1)
		return pos, err2 != nil, nil
	}
	pos, err2 := chess.FromBoard(b, t.assume.Opp(), 15, -1)
	if err2 != nil {
		return pos, false, err
	}
	return pos, true, nil
}
