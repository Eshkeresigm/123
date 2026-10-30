package chess

import (
	"math/bits"
	"strconv"
	"strings"
)

// Position is a full chess position. It is a plain value type so that
// copy-make search works without an undo stack.
type Position struct {
	Pieces [12]uint64
	Occ    [2]uint64
	All    uint64
	Board  [64]Piece
	Side   Color
	Castle uint8
	EP     int8 // en-passant target square (only when a capture is possible) or -1
	Half   uint16
	Full   uint16
	Hash   uint64
}

var castleMask [64]uint8

func init() {
	for i := range castleMask {
		castleMask[i] = 15
	}
	castleMask[0] &^= CastleWQ
	castleMask[4] &^= CastleWK | CastleWQ
	castleMask[7] &^= CastleWK
	castleMask[56] &^= CastleBQ
	castleMask[60] &^= CastleBK | CastleBQ
	castleMask[63] &^= CastleBK
}

// StartFEN is the standard initial position.
const StartFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

// Start returns the initial position.
func Start() Position {
	p, _ := ParseFEN(StartFEN)
	return p
}

// Empty returns a position with no pieces.
func Empty() Position {
	var p Position
	for i := range p.Board {
		p.Board[i] = NoPiece
	}
	p.EP = -1
	p.Full = 1
	return p
}

func (p *Position) put(pc Piece, sq int) {
	b := uint64(1) << uint(sq)
	p.Pieces[pc] |= b
	p.Occ[pc.Color()] |= b
	p.All |= b
	p.Board[sq] = pc
	p.Hash ^= zPiece[pc][sq]
}

func (p *Position) remove(sq int) Piece {
	pc := p.Board[sq]
	b := uint64(1) << uint(sq)
	p.Pieces[pc] &^= b
	p.Occ[pc.Color()] &^= b
	p.All &^= b
	p.Board[sq] = NoPiece
	p.Hash ^= zPiece[pc][sq]
	return pc
}

// Set places a piece on an empty position (used when building positions
// by hand); call Rehash afterwards.
func (p *Position) Set(sq int, pc Piece) {
	if p.Board[sq] != NoPiece {
		p.remove(sq)
	}
	if pc != NoPiece {
		p.put(pc, sq)
	}
}

// ComputeHash recomputes the Zobrist key from scratch.
func (p *Position) ComputeHash() uint64 {
	var h uint64
	for sq := 0; sq < 64; sq++ {
		if pc := p.Board[sq]; pc != NoPiece {
			h ^= zPiece[pc][sq]
		}
	}
	h ^= zCastle[p.Castle&15]
	if p.EP >= 0 {
		h ^= zEP[int(p.EP)&7]
	}
	if p.Side == Black {
		h ^= zSide
	}
	return h
}

// Rehash refreshes Hash after manual edits.
func (p *Position) Rehash() { p.Hash = p.ComputeHash() }

// KingSq returns the king square of colour c, or -1.
func (p *Position) KingSq(c Color) int {
	b := p.Pieces[MakePiece(c, King)]
	if b == 0 {
		return -1
	}
	return bits.TrailingZeros64(b)
}

// AttackersTo returns all pieces of colour by attacking sq with the given occupancy.
func (p *Position) attackedBy(sq int, by Color, occ uint64) bool {
	if pawnAtt[by.Opp()][sq]&p.Pieces[MakePiece(by, Pawn)] != 0 {
		return true
	}
	if knightAtt[sq]&p.Pieces[MakePiece(by, Knight)] != 0 {
		return true
	}
	if kingAtt[sq]&p.Pieces[MakePiece(by, King)] != 0 {
		return true
	}
	diag := p.Pieces[MakePiece(by, Bishop)] | p.Pieces[MakePiece(by, Queen)]
	if diag != 0 && BishopAttacks(sq, occ)&diag != 0 {
		return true
	}
	orth := p.Pieces[MakePiece(by, Rook)] | p.Pieces[MakePiece(by, Queen)]
	if orth != 0 && RookAttacks(sq, occ)&orth != 0 {
		return true
	}
	return false
}

// IsAttacked reports whether sq is attacked by colour by.
func (p *Position) IsAttacked(sq int, by Color) bool { return p.attackedBy(sq, by, p.All) }

// InCheck reports whether the side to move is in check.
func (p *Position) InCheck() bool {
	k := p.KingSq(p.Side)
	return k >= 0 && p.IsAttacked(k, p.Side.Opp())
}

// Make plays a pseudo-legal move and returns the resulting position. ok is
// false when the move leaves the mover's own king in check.
func (p *Position) Make(m Move) (q Position, ok bool) {
	q = *p
	from, to := m.From(), m.To()
	pc := q.Board[from]
	us := p.Side
	if pc == NoPiece || pc.Color() != us {
		return q, false
	}
	if q.EP >= 0 {
		q.Hash ^= zEP[int(q.EP)&7]
	}
	q.EP = -1
	q.Hash ^= zCastle[q.Castle&15]
	typ := pc.Type()
	captured := q.Board[to]
	reset := typ == Pawn || captured != NoPiece
	switch {
	case typ == Pawn && to == int(p.EP) && p.EP >= 0 && captured == NoPiece && File(from) != File(to):
		// en passant capture
		capSq := to - 8
		if us == Black {
			capSq = to + 8
		}
		q.remove(capSq)
		reset = true
	case captured != NoPiece:
		q.remove(to)
	}
	q.remove(from)
	if pr := m.Promo(); typ == Pawn && pr != NoType {
		q.put(MakePiece(us, pr), to)
	} else {
		q.put(pc, to)
	}
	if typ == King && abs(File(to)-File(from)) == 2 {
		// castling: move the rook
		var rf, rt int
		if to > from {
			rf, rt = from+3, from+1
		} else {
			rf, rt = from-4, from-1
		}
		if q.Board[rf] == MakePiece(us, Rook) {
			rk := q.remove(rf)
			q.put(rk, rt)
		}
	}
	if typ == Pawn && abs(to-from) == 16 {
		mid := (from + to) / 2
		enemyPawns := q.Pieces[MakePiece(us.Opp(), Pawn)]
		if pawnAtt[us][mid]&enemyPawns != 0 {
			q.EP = int8(mid)
			q.Hash ^= zEP[mid&7]
		}
	}
	q.Castle &= castleMask[from] & castleMask[to]
	q.Hash ^= zCastle[q.Castle&15]
	if reset {
		q.Half = 0
	} else {
		q.Half++
	}
	if us == Black {
		q.Full++
	}
	q.Side = us.Opp()
	q.Hash ^= zSide
	k := q.KingSq(us)
	if k < 0 || q.IsAttacked(k, q.Side) {
		return q, false
	}
	return q, true
}

// MakeNull passes the turn (used by search for null-move pruning).
func (p *Position) MakeNull() Position {
	q := *p
	if q.EP >= 0 {
		q.Hash ^= zEP[int(q.EP)&7]
	}
	q.EP = -1
	q.Side = p.Side.Opp()
	q.Hash ^= zSide
	q.Half++
	return q
}

// ParseFEN parses a FEN string. Missing trailing fields are tolerated.
func ParseFEN(fen string) (Position, error) {
	p := Empty()
	f := strings.Fields(fen)
	if len(f) < 1 {
		return p, &ErrFEN{"empty"}
	}
	rows := strings.Split(f[0], "/")
	if len(rows) != 8 {
		return p, &ErrFEN{"need 8 ranks"}
	}
	for i, row := range rows {
		rank := 7 - i
		file := 0
		for j := 0; j < len(row); j++ {
			c := row[j]
			if c >= '1' && c <= '8' {
				file += int(c - '0')
				continue
			}
			pc, ok := PieceFromChar(c)
			if !ok || file > 7 {
				return p, &ErrFEN{"bad piece placement"}
			}
			p.put(pc, rank*8+file)
			file++
		}
		if file != 8 {
			return p, &ErrFEN{"rank does not have 8 files"}
		}
	}
	p.Side = White
	if len(f) > 1 {
		switch f[1] {
		case "w":
		case "b":
			p.Side = Black
		default:
			return p, &ErrFEN{"bad side to move"}
		}
	}
	if len(f) > 2 && f[2] != "-" {
		for _, c := range f[2] {
			switch c {
			case 'K':
				p.Castle |= CastleWK
			case 'Q':
				p.Castle |= CastleWQ
			case 'k':
				p.Castle |= CastleBK
			case 'q':
				p.Castle |= CastleBQ
			default:
				return p, &ErrFEN{"bad castling field"}
			}
		}
	}
	if len(f) > 3 && f[3] != "-" {
		sq, ok := ParseSquare(f[3])
		if !ok {
			return p, &ErrFEN{"bad en-passant square"}
		}
		p.EP = int8(sq)
	}
	if len(f) > 4 {
		if n, err := strconv.Atoi(f[4]); err == nil && n >= 0 {
			p.Half = uint16(n)
		}
	}
	if len(f) > 5 {
		if n, err := strconv.Atoi(f[5]); err == nil && n > 0 {
			p.Full = uint16(n)
		}
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	p.sanitizeCastling()
	p.sanitizeEP()
	p.Hash = p.ComputeHash()
	return p, nil
}

// Validate performs basic sanity checks on the piece placement.
func (p *Position) Validate() error {
	if bits.OnesCount64(p.Pieces[MakePiece(White, King)]) != 1 || bits.OnesCount64(p.Pieces[MakePiece(Black, King)]) != 1 {
		return &ErrFEN{"each side needs exactly one king"}
	}
	pawns := p.Pieces[MakePiece(White, Pawn)] | p.Pieces[MakePiece(Black, Pawn)]
	if pawns&(0xFF|0xFF<<56) != 0 {
		return &ErrFEN{"pawn on first or last rank"}
	}
	for c := Color(0); c < 2; c++ {
		if bits.OnesCount64(p.Pieces[MakePiece(c, Pawn)]) > 8 || bits.OnesCount64(p.Occ[c]) > 16 {
			return &ErrFEN{"too many pieces"}
		}
	}
	// the side that is not on move must not be in check
	k := p.KingSq(p.Side.Opp())
	if p.IsAttacked(k, p.Side) {
		return &ErrFEN{"opponent king is in check"}
	}
	return nil
}

// sanitizeCastling drops rights whose king/rook are not on their home squares.
func (p *Position) sanitizeCastling() {
	if p.Board[4] != MakePiece(White, King) {
		p.Castle &^= CastleWK | CastleWQ
	}
	if p.Board[7] != MakePiece(White, Rook) {
		p.Castle &^= CastleWK
	}
	if p.Board[0] != MakePiece(White, Rook) {
		p.Castle &^= CastleWQ
	}
	if p.Board[60] != MakePiece(Black, King) {
		p.Castle &^= CastleBK | CastleBQ
	}
	if p.Board[63] != MakePiece(Black, Rook) {
		p.Castle &^= CastleBK
	}
	if p.Board[56] != MakePiece(Black, Rook) {
		p.Castle &^= CastleBQ
	}
}

// sanitizeEP removes an en-passant square nobody can use.
func (p *Position) sanitizeEP() {
	if p.EP < 0 {
		return
	}
	sq := int(p.EP)
	valid := false
	if p.Side == White && Rank(sq) == 5 && p.Board[sq-8] == MakePiece(Black, Pawn) && p.Board[sq] == NoPiece {
		valid = pawnAtt[Black][sq]&p.Pieces[MakePiece(White, Pawn)] != 0
	}
	if p.Side == Black && Rank(sq) == 2 && p.Board[sq+8] == MakePiece(White, Pawn) && p.Board[sq] == NoPiece {
		valid = pawnAtt[White][sq]&p.Pieces[MakePiece(Black, Pawn)] != 0
	}
	if !valid {
		p.EP = -1
	}
}

// FromBoard builds a position from a 64-entry placement (a1 = index 0).
// Castling rights are inferred from king/rook home squares and then
// filtered with the supplied mask (use 15 to keep all inferable rights).
func FromBoard(board [64]Piece, side Color, castleAllowed uint8, ep int) (Position, error) {
	p := Empty()
	for sq, pc := range board {
		if pc != NoPiece {
			p.put(pc, sq)
		}
	}
	p.Side = side
	p.Castle = 15 & castleAllowed
	if ep >= 0 {
		p.EP = int8(ep)
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	p.sanitizeCastling()
	p.sanitizeEP()
	p.Hash = p.ComputeHash()
	return p, nil
}

// FEN formats the position.
func (p Position) FEN() string {
	var sb strings.Builder
	for rank := 7; rank >= 0; rank-- {
		empty := 0
		for file := 0; file < 8; file++ {
			pc := p.Board[rank*8+file]
			if pc == NoPiece {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteByte(byte('0' + empty))
				empty = 0
			}
			sb.WriteByte(pc.Char())
		}
		if empty > 0 {
			sb.WriteByte(byte('0' + empty))
		}
		if rank > 0 {
			sb.WriteByte('/')
		}
	}
	sb.WriteByte(' ')
	if p.Side == White {
		sb.WriteByte('w')
	} else {
		sb.WriteByte('b')
	}
	sb.WriteByte(' ')
	if p.Castle&15 == 0 {
		sb.WriteByte('-')
	} else {
		if p.Castle&CastleWK != 0 {
			sb.WriteByte('K')
		}
		if p.Castle&CastleWQ != 0 {
			sb.WriteByte('Q')
		}
		if p.Castle&CastleBK != 0 {
			sb.WriteByte('k')
		}
		if p.Castle&CastleBQ != 0 {
			sb.WriteByte('q')
		}
	}
	sb.WriteByte(' ')
	if p.EP >= 0 {
		sb.WriteString(SqName(int(p.EP)))
	} else {
		sb.WriteByte('-')
	}
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(int(p.Half)))
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(int(p.Full)))
	return sb.String()
}

// PlacementKey returns just the piece-placement part of the FEN.
func (p *Position) PlacementKey() [64]Piece { return p.Board }

// InsufficientMaterial reports a dead-drawn material situation.
func (p *Position) InsufficientMaterial() bool {
	if p.Pieces[MakePiece(White, Pawn)]|p.Pieces[MakePiece(Black, Pawn)]|
		p.Pieces[MakePiece(White, Rook)]|p.Pieces[MakePiece(Black, Rook)]|
		p.Pieces[MakePiece(White, Queen)]|p.Pieces[MakePiece(Black, Queen)] != 0 {
		return false
	}
	minors := bits.OnesCount64(p.Pieces[MakePiece(White, Knight)] | p.Pieces[MakePiece(Black, Knight)] |
		p.Pieces[MakePiece(White, Bishop)] | p.Pieces[MakePiece(Black, Bishop)])
	if minors <= 1 {
		return true
	}
	// only bishops, all on one colour complex
	if p.Pieces[MakePiece(White, Knight)]|p.Pieces[MakePiece(Black, Knight)] == 0 {
		bishops := p.Pieces[MakePiece(White, Bishop)] | p.Pieces[MakePiece(Black, Bishop)]
		const light = 0x55AA55AA55AA55AA
		if bishops&light == 0 || bishops&^light == 0 {
			return true
		}
	}
	return false
}
