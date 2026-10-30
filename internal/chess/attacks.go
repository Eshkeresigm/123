package chess

import "math/bits"

var (
	knightAtt [64]uint64
	kingAtt   [64]uint64
	pawnAtt   [2][64]uint64

	fileMask [64]uint64 // same file, excluding the square
	rankMask [64]uint64
	diagMask [64]uint64 // a1-h8 direction
	antiMask [64]uint64 // a8-h1 direction

	betweenBB [64][64]uint64
)

func init() {
	for sq := 0; sq < 64; sq++ {
		f, r := File(sq), Rank(sq)
		for _, d := range [][2]int{{1, 2}, {2, 1}, {-1, 2}, {-2, 1}, {1, -2}, {2, -1}, {-1, -2}, {-2, -1}} {
			if nf, nr := f+d[0], r+d[1]; nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				knightAtt[sq] |= 1 << uint(nr*8+nf)
			}
		}
		for df := -1; df <= 1; df++ {
			for dr := -1; dr <= 1; dr++ {
				if df == 0 && dr == 0 {
					continue
				}
				if nf, nr := f+df, r+dr; nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
					kingAtt[sq] |= 1 << uint(nr*8+nf)
				}
			}
		}
		for _, df := range []int{-1, 1} {
			if nf := f + df; nf >= 0 && nf < 8 {
				if r+1 < 8 {
					pawnAtt[White][sq] |= 1 << uint((r+1)*8+nf)
				}
				if r-1 >= 0 {
					pawnAtt[Black][sq] |= 1 << uint((r-1)*8+nf)
				}
			}
		}
		for i := 0; i < 8; i++ {
			if i != r {
				fileMask[sq] |= 1 << uint(i*8+f)
			}
			if i != f {
				rankMask[sq] |= 1 << uint(r*8+i)
			}
		}
		for d := -7; d <= 7; d++ {
			if d == 0 {
				continue
			}
			if nf, nr := f+d, r+d; nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				diagMask[sq] |= 1 << uint(nr*8+nf)
			}
			if nf, nr := f+d, r-d; nf >= 0 && nf < 8 && nr >= 0 && nr < 8 {
				antiMask[sq] |= 1 << uint(nr*8+nf)
			}
		}
	}
	for a := 0; a < 64; a++ {
		for b := 0; b < 64; b++ {
			if a == b {
				continue
			}
			df, dr := File(b)-File(a), Rank(b)-Rank(a)
			if df != 0 && dr != 0 && abs(df) != abs(dr) {
				continue
			}
			sf, sr := sign(df), sign(dr)
			var bb uint64
			for f, r := File(a)+sf, Rank(a)+sr; f != File(b) || r != Rank(b); f, r = f+sf, r+sr {
				bb |= 1 << uint(r*8+f)
			}
			betweenBB[a][b] = bb
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func sign(x int) int {
	switch {
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 0
}

// lineAtt is hyperbola quintessence: attacks of a slider on bit r along a
// line described by mask (which excludes r itself).
func lineAtt(r, occ, mask uint64) uint64 {
	m := occ & mask
	f := m - r
	rv := bits.Reverse64(bits.Reverse64(m) - bits.Reverse64(r))
	return (f ^ rv) & mask
}

// RookAttacks returns rook attacks from sq given occupancy.
func RookAttacks(sq int, occ uint64) uint64 {
	r := uint64(1) << uint(sq)
	return lineAtt(r, occ, fileMask[sq]) | lineAtt(r, occ, rankMask[sq])
}

// BishopAttacks returns bishop attacks from sq given occupancy.
func BishopAttacks(sq int, occ uint64) uint64 {
	r := uint64(1) << uint(sq)
	return lineAtt(r, occ, diagMask[sq]) | lineAtt(r, occ, antiMask[sq])
}

// QueenAttacks returns queen attacks.
func QueenAttacks(sq int, occ uint64) uint64 {
	return RookAttacks(sq, occ) | BishopAttacks(sq, occ)
}

// KnightAttacks returns knight attacks.
func KnightAttacks(sq int) uint64 { return knightAtt[sq] }

// KingAttacks returns king attacks.
func KingAttacks(sq int) uint64 { return kingAtt[sq] }

// PawnAttacks returns squares attacked by a pawn of colour c on sq.
func PawnAttacks(c Color, sq int) uint64 { return pawnAtt[c][sq] }

// Between returns squares strictly between a and b on a shared line.
func Between(a, b int) uint64 { return betweenBB[a][b] }
