// Package chess implements a compact bitboard chess rules engine:
// position, legal move generation, FEN, UCI/SAN notation and hashing.
package chess

import (
	"fmt"
	"strings"
)

// Color of a side.
type Color uint8

const (
	White Color = 0
	Black Color = 1
)

// Opp returns the opposite colour.
func (c Color) Opp() Color { return c ^ 1 }

// PieceType is a colourless piece kind.
type PieceType uint8

const (
	Pawn PieceType = iota
	Knight
	Bishop
	Rook
	Queen
	King
	NoType
)

// Piece is colour*6+type, or NoPiece for an empty square.
type Piece uint8

// NoPiece marks an empty square.
const NoPiece Piece = 12

// MakePiece builds a piece from colour and type.
func MakePiece(c Color, t PieceType) Piece { return Piece(uint8(c)*6 + uint8(t)) }

// Color returns the piece colour (undefined for NoPiece).
func (p Piece) Color() Color { return Color(p / 6) }

// Type returns the piece type (NoType for NoPiece).
func (p Piece) Type() PieceType {
	if p >= NoPiece {
		return NoType
	}
	return PieceType(p % 6)
}

const pieceChars = "PNBRQKpnbrqk"

// Char returns the FEN letter of the piece ('.' for none).
func (p Piece) Char() byte {
	if p >= NoPiece {
		return '.'
	}
	return pieceChars[p]
}

// PieceFromChar parses a FEN piece letter.
func PieceFromChar(c byte) (Piece, bool) {
	i := strings.IndexByte(pieceChars, c)
	if i < 0 {
		return NoPiece, false
	}
	return Piece(i), true
}

// Square helpers. a1 = 0, h1 = 7, a8 = 56, h8 = 63.

// File returns the file 0..7 of a square.
func File(sq int) int { return sq & 7 }

// Rank returns the rank 0..7 of a square.
func Rank(sq int) int { return sq >> 3 }

// SqName returns "e4" style name.
func SqName(sq int) string {
	if sq < 0 || sq > 63 {
		return "--"
	}
	return string([]byte{byte('a' + File(sq)), byte('1' + Rank(sq))})
}

// ParseSquare parses "e4".
func ParseSquare(s string) (int, bool) {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return 0, false
	}
	return int(s[1]-'1')*8 + int(s[0]-'a'), true
}

// Move is packed as from | to<<6 | promo<<12 where promo is 0 (none) or
// the PieceType (Knight..Queen). The zero Move means "no move".
type Move uint16

// NoMove is the null value.
const NoMove Move = 0

// NewMove builds a non-promotion move.
func NewMove(from, to int) Move { return Move(from | to<<6) }

// NewPromo builds a promotion move to t (Knight..Queen).
func NewPromo(from, to int, t PieceType) Move { return Move(from | to<<6 | int(t)<<12) }

// From square.
func (m Move) From() int { return int(m & 63) }

// To square.
func (m Move) To() int { return int(m>>6) & 63 }

// Promo returns the promotion piece type or NoType.
func (m Move) Promo() PieceType {
	p := PieceType(m >> 12)
	if p == 0 || p > Queen {
		return NoType
	}
	return p
}

// UCI formats the move as e2e4 / e7e8q.
func (m Move) UCI() string {
	if m == NoMove {
		return "0000"
	}
	s := SqName(m.From()) + SqName(m.To())
	switch m.Promo() {
	case Knight:
		s += "n"
	case Bishop:
		s += "b"
	case Rook:
		s += "r"
	case Queen:
		s += "q"
	}
	return s
}

func (m Move) String() string { return m.UCI() }

// Castling right bits.
const (
	CastleWK = 1
	CastleWQ = 2
	CastleBK = 4
	CastleBQ = 8
)

// ErrFEN is returned for malformed FEN strings.
type ErrFEN struct{ Msg string }

func (e *ErrFEN) Error() string { return fmt.Sprintf("FEN: %s", e.Msg) }
