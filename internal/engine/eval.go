package engine

import (
	"math/bits"

	"chessvision/internal/chess"
)

// Piece-square tables are generated procedurally from simple, well known
// chess heuristics (centralisation, king safety, advanced pawns...).
var (
	pstMG [12][64]int
	pstEG [12][64]int
)

var (
	valMG = [6]int{82, 337, 365, 477, 1025, 0}
	valEG = [6]int{94, 281, 297, 512, 936, 0}
)

var phaseW = [6]int{0, 1, 1, 2, 4, 0}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	advMG := [8]int{0, 0, 2, 8, 18, 34, 58, 0}
	advEG := [8]int{0, 0, 6, 16, 32, 58, 96, 0}
	for sq := 0; sq < 64; sq++ {
		f, r := sq&7, sq>>3
		cf := imin(f, 7-f) // 0..3
		cr := imin(r, 7-r)
		var mg, eg [6]int

		// pawn
		mg[chess.Pawn] = advMG[r]
		eg[chess.Pawn] = advEG[r]
		if r >= 1 && r <= 4 {
			switch f {
			case 3, 4:
				mg[chess.Pawn] += 9
			case 2, 5:
				mg[chess.Pawn] += 3
			case 0, 7:
				mg[chess.Pawn] -= 6
			}
		}
		// knight
		mg[chess.Knight] = 7*cf + 7*cr - 28
		eg[chess.Knight] = 5*cf + 5*cr - 20
		if r == 0 {
			mg[chess.Knight] -= 8
		}
		// bishop
		mg[chess.Bishop] = 4*cf + 4*cr - 14
		eg[chess.Bishop] = 3*cf + 3*cr - 10
		if f == r || f+r == 7 {
			mg[chess.Bishop] += 5
		}
		if r == 0 {
			mg[chess.Bishop] -= 7
		}
		// rook
		if r == 6 {
			mg[chess.Rook] += 18
			eg[chess.Rook] += 14
		}
		if f == 3 || f == 4 {
			mg[chess.Rook] += 5
		}
		if r == 0 && (f == 3 || f == 4) {
			mg[chess.Rook] += 6
		}
		// queen
		mg[chess.Queen] = 2*cf + 2*cr - 6
		eg[chess.Queen] = 4*cf + 4*cr - 12
		// king
		if r == 0 {
			switch f {
			case 6:
				mg[chess.King] = 32
			case 7:
				mg[chess.King] = 20
			case 1, 2:
				mg[chess.King] = 26
			case 0:
				mg[chess.King] = 12
			default:
				mg[chess.King] = -12
			}
		} else {
			mg[chess.King] = -28 - 14*(r-1)
		}
		eg[chess.King] = 12*cf + 12*cr - 30

		for t := 0; t < 6; t++ {
			w := chess.MakePiece(chess.White, chess.PieceType(t))
			b := chess.MakePiece(chess.Black, chess.PieceType(t))
			pstMG[w][sq] = valMG[t] + mg[t]
			pstEG[w][sq] = valEG[t] + eg[t]
			pstMG[b][sq^56] = valMG[t] + mg[t]
			pstEG[b][sq^56] = valEG[t] + eg[t]
		}
	}
}

var passedBonus = [8]int{0, 4, 8, 16, 32, 58, 96, 0}

func fileBB(f int) uint64 { return 0x0101010101010101 << uint(f) }

// frontSpan returns the squares in front of sq for colour c on its own and
// adjacent files.
func frontSpan(c chess.Color, sq int) uint64 {
	f, r := sq&7, sq>>3
	var files uint64 = fileBB(f)
	if f > 0 {
		files |= fileBB(f - 1)
	}
	if f < 7 {
		files |= fileBB(f + 1)
	}
	if c == chess.White {
		if r >= 7 {
			return 0
		}
		return files & (^uint64(0) << uint((r+1)*8))
	}
	if r <= 0 {
		return 0
	}
	return files & ((uint64(1) << uint(r*8)) - 1)
}

// Evaluate returns a static score in centipawns from the side to move's view.
func Evaluate(p *chess.Position) int {
	var mg, eg, phase int
	mgS := [2]int{}
	egS := [2]int{}
	wp := p.Pieces[chess.MakePiece(chess.White, chess.Pawn)]
	bp := p.Pieces[chess.MakePiece(chess.Black, chess.Pawn)]
	pawns := [2]uint64{wp, bp}
	allPawns := wp | bp

	for c := chess.White; c <= chess.Black; c++ {
		own := p.Occ[c]
		enemyPawnAtt := uint64(0)
		for b := pawns[c.Opp()]; b != 0; b &= b - 1 {
			enemyPawnAtt |= chess.PawnAttacks(c.Opp(), bits.TrailingZeros64(b))
		}
		bishops := 0
		for t := chess.Pawn; t <= chess.King; t++ {
			pc := chess.MakePiece(c, t)
			for b := p.Pieces[pc]; b != 0; b &= b - 1 {
				sq := bits.TrailingZeros64(b)
				mgS[c] += pstMG[pc][sq]
				egS[c] += pstEG[pc][sq]
				phase += phaseW[t]
				switch t {
				case chess.Pawn:
					fwd := frontSpan(c, sq)
					if fwd&pawns[c.Opp()] == 0 && fwd&fileBB(sq&7)&pawns[c] == 0 {
						r := sq >> 3
						if c == chess.Black {
							r = 7 - r
						}
						mgS[c] += passedBonus[r] / 2
						egS[c] += passedBonus[r]
					}
					f := sq & 7
					adj := uint64(0)
					if f > 0 {
						adj |= fileBB(f - 1)
					}
					if f < 7 {
						adj |= fileBB(f + 1)
					}
					if adj&pawns[c] == 0 {
						mgS[c] -= 10
						egS[c] -= 14
					}
					if fileBB(f)&pawns[c]&^(1<<uint(sq)) != 0 {
						mgS[c] -= 8
						egS[c] -= 12
					}
				case chess.Knight:
					m := bits.OnesCount64(chess.KnightAttacks(sq) &^ own &^ enemyPawnAtt)
					mgS[c] += 4 * (m - 4)
					egS[c] += 4 * (m - 4)
				case chess.Bishop:
					bishops++
					m := bits.OnesCount64(chess.BishopAttacks(sq, p.All) &^ own &^ enemyPawnAtt)
					mgS[c] += 5 * (m - 6)
					egS[c] += 5 * (m - 6)
				case chess.Rook:
					m := bits.OnesCount64(chess.RookAttacks(sq, p.All) &^ own &^ enemyPawnAtt)
					mgS[c] += 2 * (m - 7)
					egS[c] += 4 * (m - 7)
					fb := fileBB(sq & 7)
					switch {
					case fb&allPawns == 0:
						mgS[c] += 22
						egS[c] += 10
					case fb&pawns[c] == 0:
						mgS[c] += 11
						egS[c] += 6
					}
				case chess.Queen:
					m := bits.OnesCount64(chess.QueenAttacks(sq, p.All) &^ own &^ enemyPawnAtt)
					mgS[c] += 1 * (m - 13)
					egS[c] += 2 * (m - 13)
				case chess.King:
					// pawn shield in front of the king (middlegame)
					f, r := sq&7, sq>>3
					shield := 0
					for df := -1; df <= 1; df++ {
						nf := f + df
						if nf < 0 || nf > 7 {
							continue
						}
						for dr := 1; dr <= 2; dr++ {
							nr := r + dr
							if c == chess.Black {
								nr = r - dr
							}
							if nr < 0 || nr > 7 {
								continue
							}
							if pawns[c]&(1<<uint(nr*8+nf)) != 0 {
								shield++
								break
							}
						}
					}
					mgS[c] += 9*shield - 12
					// open file next to the king
					for df := -1; df <= 1; df++ {
						nf := f + df
						if nf >= 0 && nf <= 7 && fileBB(nf)&allPawns == 0 {
							mgS[c] -= 14
						}
					}
				}
			}
		}
		if bishops >= 2 {
			mgS[c] += 28
			egS[c] += 48
		}
	}
	mg = mgS[chess.White] - mgS[chess.Black]
	eg = egS[chess.White] - egS[chess.Black]
	if phase > 24 {
		phase = 24
	}
	score := (mg*phase + eg*(24-phase)) / 24
	if p.Side == chess.Black {
		score = -score
	}
	return score + 10 // tempo
}
