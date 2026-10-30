package chess

var (
	zPiece  [13][64]uint64
	zCastle [16]uint64
	zEP     [8]uint64
	zSide   uint64
)

func init() {
	// deterministic splitmix64 so hashes are stable between runs
	s := uint64(0x9E3779B97F4A7C15)
	next := func() uint64 {
		s += 0x9E3779B97F4A7C15
		z := s
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		return z ^ (z >> 31)
	}
	for p := 0; p < 12; p++ {
		for sq := 0; sq < 64; sq++ {
			zPiece[p][sq] = next()
		}
	}
	for i := range zCastle {
		zCastle[i] = next()
	}
	for i := range zEP {
		zEP[i] = next()
	}
	zSide = next()
}
