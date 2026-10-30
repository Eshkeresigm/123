package chess

import "math/bits"

// MoveList is a fixed-capacity move buffer (no allocation in search).
type MoveList struct {
	M [256]Move
	N int
}

func (l *MoveList) add(m Move) {
	l.M[l.N] = m
	l.N++
}

// Slice returns the generated moves.
func (l *MoveList) Slice() []Move { return l.M[:l.N] }

const (
	rank2 = uint64(0xFF) << 8
	rank7 = uint64(0xFF) << 48
	rank3 = uint64(0xFF) << 16
	rank6 = uint64(0xFF) << 40
)

// GenPseudo generates pseudo-legal moves. With capturesOnly it produces
// captures, en passant and queen promotions only (for quiescence search).
func (p *Position) GenPseudo(l *MoveList, capturesOnly bool) {
	us := p.Side
	them := us.Opp()
	own := p.Occ[us]
	enemy := p.Occ[them]
	all := p.All
	targets := ^own
	if capturesOnly {
		targets = enemy
	}

	// pawns
	pawns := p.Pieces[MakePiece(us, Pawn)]
	if us == White {
		single := (pawns << 8) &^ all
		double := ((single & rank3) << 8) &^ all
		promoRank := uint64(0xFF) << 56
		if !capturesOnly {
			for b := single &^ promoRank; b != 0; b &= b - 1 {
				to := bits.TrailingZeros64(b)
				l.add(NewMove(to-8, to))
			}
			for b := double; b != 0; b &= b - 1 {
				to := bits.TrailingZeros64(b)
				l.add(NewMove(to-16, to))
			}
		}
		for b := single & promoRank; b != 0; b &= b - 1 {
			to := bits.TrailingZeros64(b)
			if capturesOnly {
				l.add(NewPromo(to-8, to, Queen))
				continue
			}
			l.add(NewPromo(to-8, to, Queen))
			l.add(NewPromo(to-8, to, Rook))
			l.add(NewPromo(to-8, to, Bishop))
			l.add(NewPromo(to-8, to, Knight))
		}
	} else {
		single := (pawns >> 8) &^ all
		double := ((single & rank6) >> 8) &^ all
		promoRank := uint64(0xFF)
		if !capturesOnly {
			for b := single &^ promoRank; b != 0; b &= b - 1 {
				to := bits.TrailingZeros64(b)
				l.add(NewMove(to+8, to))
			}
			for b := double; b != 0; b &= b - 1 {
				to := bits.TrailingZeros64(b)
				l.add(NewMove(to+16, to))
			}
		}
		for b := single & promoRank; b != 0; b &= b - 1 {
			to := bits.TrailingZeros64(b)
			if capturesOnly {
				l.add(NewPromo(to+8, to, Queen))
				continue
			}
			l.add(NewPromo(to+8, to, Queen))
			l.add(NewPromo(to+8, to, Rook))
			l.add(NewPromo(to+8, to, Bishop))
			l.add(NewPromo(to+8, to, Knight))
		}
	}
	for b := pawns; b != 0; b &= b - 1 {
		from := bits.TrailingZeros64(b)
		att := pawnAtt[us][from] & enemy
		for a := att; a != 0; a &= a - 1 {
			to := bits.TrailingZeros64(a)
			if (us == White && Rank(to) == 7) || (us == Black && Rank(to) == 0) {
				l.add(NewPromo(from, to, Queen))
				if !capturesOnly {
					l.add(NewPromo(from, to, Rook))
					l.add(NewPromo(from, to, Bishop))
					l.add(NewPromo(from, to, Knight))
				}
			} else {
				l.add(NewMove(from, to))
			}
		}
		if p.EP >= 0 && pawnAtt[us][from]&(1<<uint(p.EP)) != 0 {
			l.add(NewMove(from, int(p.EP)))
		}
	}

	// knights
	for b := p.Pieces[MakePiece(us, Knight)]; b != 0; b &= b - 1 {
		from := bits.TrailingZeros64(b)
		for a := knightAtt[from] & targets; a != 0; a &= a - 1 {
			l.add(NewMove(from, bits.TrailingZeros64(a)))
		}
	}
	// bishops & queens (diagonal)
	for b := p.Pieces[MakePiece(us, Bishop)]; b != 0; b &= b - 1 {
		from := bits.TrailingZeros64(b)
		for a := BishopAttacks(from, all) & targets; a != 0; a &= a - 1 {
			l.add(NewMove(from, bits.TrailingZeros64(a)))
		}
	}
	for b := p.Pieces[MakePiece(us, Rook)]; b != 0; b &= b - 1 {
		from := bits.TrailingZeros64(b)
		for a := RookAttacks(from, all) & targets; a != 0; a &= a - 1 {
			l.add(NewMove(from, bits.TrailingZeros64(a)))
		}
	}
	for b := p.Pieces[MakePiece(us, Queen)]; b != 0; b &= b - 1 {
		from := bits.TrailingZeros64(b)
		for a := QueenAttacks(from, all) & targets; a != 0; a &= a - 1 {
			l.add(NewMove(from, bits.TrailingZeros64(a)))
		}
	}
	// king
	if kb := p.Pieces[MakePiece(us, King)]; kb != 0 {
		from := bits.TrailingZeros64(kb)
		for a := kingAtt[from] & targets; a != 0; a &= a - 1 {
			l.add(NewMove(from, bits.TrailingZeros64(a)))
		}
		if !capturesOnly {
			p.genCastles(l, from, us)
		}
	}
}

func (p *Position) genCastles(l *MoveList, from int, us Color) {
	them := us.Opp()
	if us == White {
		if from != 4 {
			return
		}
		if p.Castle&CastleWK != 0 && p.Board[7] == MakePiece(White, Rook) &&
			p.All&(1<<5|1<<6) == 0 &&
			!p.IsAttacked(4, them) && !p.IsAttacked(5, them) && !p.IsAttacked(6, them) {
			l.add(NewMove(4, 6))
		}
		if p.Castle&CastleWQ != 0 && p.Board[0] == MakePiece(White, Rook) &&
			p.All&(1<<1|1<<2|1<<3) == 0 &&
			!p.IsAttacked(4, them) && !p.IsAttacked(3, them) && !p.IsAttacked(2, them) {
			l.add(NewMove(4, 2))
		}
	} else {
		if from != 60 {
			return
		}
		if p.Castle&CastleBK != 0 && p.Board[63] == MakePiece(Black, Rook) &&
			p.All&(1<<61|1<<62) == 0 &&
			!p.IsAttacked(60, them) && !p.IsAttacked(61, them) && !p.IsAttacked(62, them) {
			l.add(NewMove(60, 62))
		}
		if p.Castle&CastleBQ != 0 && p.Board[56] == MakePiece(Black, Rook) &&
			p.All&(1<<57|1<<58|1<<59) == 0 &&
			!p.IsAttacked(60, them) && !p.IsAttacked(59, them) && !p.IsAttacked(58, them) {
			l.add(NewMove(60, 58))
		}
	}
}

// LegalMoves returns all legal moves with the resulting positions skipped.
func (p *Position) LegalMoves() []Move {
	var l MoveList
	p.GenPseudo(&l, false)
	out := make([]Move, 0, l.N)
	for _, m := range l.Slice() {
		if _, ok := p.Make(m); ok {
			out = append(out, m)
		}
	}
	return out
}

// HasLegalMove reports whether the side to move has any legal move.
func (p *Position) HasLegalMove() bool {
	var l MoveList
	p.GenPseudo(&l, false)
	for _, m := range l.Slice() {
		if _, ok := p.Make(m); ok {
			return true
		}
	}
	return false
}

// IsLegal reports whether m is a legal move in p.
func (p *Position) IsLegal(m Move) bool {
	for _, x := range p.LegalMoves() {
		if x == m {
			return true
		}
	}
	return false
}

// Result describes a terminal state.
type Result int

const (
	Ongoing Result = iota
	Checkmate
	Stalemate
	DrawMaterial
	DrawFifty
)

// Status classifies the position (repetition is handled by the caller).
func (p *Position) Status() Result {
	if !p.HasLegalMove() {
		if p.InCheck() {
			return Checkmate
		}
		return Stalemate
	}
	if p.InsufficientMaterial() {
		return DrawMaterial
	}
	if p.Half >= 100 {
		return DrawFifty
	}
	return Ongoing
}

// Perft counts leaf nodes to the given depth.
func (p *Position) Perft(depth int) uint64 {
	if depth == 0 {
		return 1
	}
	var l MoveList
	p.GenPseudo(&l, false)
	var n uint64
	for _, m := range l.Slice() {
		q, ok := p.Make(m)
		if !ok {
			continue
		}
		if depth == 1 {
			n++
		} else {
			n += q.Perft(depth - 1)
		}
	}
	return n
}
