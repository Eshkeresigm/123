package chess

import (
	"fmt"
	"strings"
)

// ParseUCI resolves a UCI move string against the legal moves of p.
func (p *Position) ParseUCI(s string) (Move, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 4 {
		return NoMove, fmt.Errorf("bad move %q", s)
	}
	from, ok1 := ParseSquare(s[0:2])
	to, ok2 := ParseSquare(s[2:4])
	if !ok1 || !ok2 {
		return NoMove, fmt.Errorf("bad move %q", s)
	}
	promo := NoType
	if len(s) >= 5 {
		switch s[4] {
		case 'n':
			promo = Knight
		case 'b':
			promo = Bishop
		case 'r':
			promo = Rook
		case 'q':
			promo = Queen
		}
	}
	for _, m := range p.LegalMoves() {
		if m.From() == from && m.To() == to && m.Promo() == promo {
			return m, nil
		}
	}
	return NoMove, fmt.Errorf("illegal move %q", s)
}

// SAN formats m (which must be legal in p) in standard algebraic notation.
// With russian=true piece letters are Кр Ф Л С К.
func (p *Position) SAN(m Move, russian bool) string {
	from, to := m.From(), m.To()
	pc := p.Board[from]
	typ := pc.Type()
	var sb strings.Builder
	capture := p.Board[to] != NoPiece || (typ == Pawn && File(from) != File(to))
	switch {
	case typ == King && abs(File(to)-File(from)) == 2:
		short := to > from
		switch {
		case short && russian:
			sb.WriteString("0-0")
		case short:
			sb.WriteString("O-O")
		case russian:
			sb.WriteString("0-0-0")
		default:
			sb.WriteString("O-O-O")
		}
	case typ == Pawn:
		if capture {
			sb.WriteByte(byte('a' + File(from)))
			sb.WriteByte('x')
		}
		sb.WriteString(SqName(to))
		if pr := m.Promo(); pr != NoType {
			sb.WriteByte('=')
			sb.WriteString(letter(pr, russian))
		}
	default:
		sb.WriteString(letter(typ, russian))
		// disambiguation
		sameFile, sameRank, ambiguous := false, false, false
		for _, o := range p.LegalMoves() {
			if o == m || o.To() != to || o.From() == from || p.Board[o.From()] != pc {
				continue
			}
			ambiguous = true
			if File(o.From()) == File(from) {
				sameFile = true
			}
			if Rank(o.From()) == Rank(from) {
				sameRank = true
			}
		}
		if ambiguous {
			switch {
			case !sameFile:
				sb.WriteByte(byte('a' + File(from)))
			case !sameRank:
				sb.WriteByte(byte('1' + Rank(from)))
			default:
				sb.WriteString(SqName(from))
			}
		}
		if capture {
			sb.WriteByte('x')
		}
		sb.WriteString(SqName(to))
	}
	if q, ok := p.Make(m); ok {
		if q.InCheck() {
			if q.HasLegalMove() {
				sb.WriteByte('+')
			} else {
				sb.WriteByte('#')
			}
		}
	}
	return sb.String()
}

func letter(t PieceType, russian bool) string {
	if russian {
		return [...]string{"", "К", "С", "Л", "Ф", "Кр"}[t]
	}
	return [...]string{"", "N", "B", "R", "Q", "K"}[t]
}

// SANLine formats a sequence of moves starting from p ("1. e4 e5 2. Nf3").
func (p Position) SANLine(moves []Move, russian bool, maxMoves int) string {
	var sb strings.Builder
	cur := p
	for i, m := range moves {
		if maxMoves > 0 && i >= maxMoves {
			break
		}
		if i > 0 {
			sb.WriteByte(' ')
		}
		if cur.Side == White {
			fmt.Fprintf(&sb, "%d.", cur.Full)
		} else if i == 0 {
			fmt.Fprintf(&sb, "%d…", cur.Full)
		}
		if cur.Side == White || i == 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(cur.SAN(m, russian))
		q, ok := cur.Make(m)
		if !ok {
			break
		}
		cur = q
	}
	return strings.TrimSpace(sb.String())
}
