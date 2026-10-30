package vision

import (
	"bytes"
	"encoding/gob"
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"

	"chessvision/internal/chess"
)

const maxSamplesPerClass = 160

// Profile is the learned look of a piece set.
type Profile struct {
	Samples [12][]Feat
	Empty   []Feat   // learned and synthetic descriptors of squares without a piece
	TOcc    float32  // minimum central ink coverage worth classifying
	RefDist float32  // typical distance between two instances of one piece type
	Learned int      // number of learn calls merged into this profile
	Version int      // format/algorithm version (stale profiles are discarded)
	Synth   [12]bool // classes completed from the opposite colour (never seen)
}

// ProfileVersion is bumped whenever features or learning change.
const ProfileVersion = 3

// Ready reports whether enough has been learned to recognise a full position.
func (p *Profile) Ready() bool {
	if p == nil {
		return false
	}
	for c := 0; c < 12; c++ {
		if len(p.Samples[c]) == 0 {
			return false
		}
	}
	return true
}

// Missing lists piece classes that have not been seen yet.
func (p *Profile) Missing() []chess.Piece {
	var out []chess.Piece
	for c := 0; c < 12; c++ {
		if p == nil || len(p.Samples[c]) == 0 {
			out = append(out, chess.Piece(c))
		}
	}
	return out
}

// squareRect returns the pixel rectangle of grid cell (col,row), row 0 on top.
func squareRect(b image.Rectangle, col, row int) image.Rectangle {
	x0 := b.Min.X + col*b.Dx()/8
	x1 := b.Min.X + (col+1)*b.Dx()/8
	y0 := b.Min.Y + row*b.Dy()/8
	y1 := b.Min.Y + (row+1)*b.Dy()/8
	return image.Rect(x0, y0, x1, y1)
}

// gridToSquare converts grid coordinates to a chess square given orientation.
func gridToSquare(col, row int, whiteAtBottom bool) int {
	if whiteAtBottom {
		return (7-row)*8 + col
	}
	return row*8 + (7 - col)
}

var augShifts = []float64{-0.035, 0, 0.035}

// Learn adds the pieces of a known placement to the profile. boardRect is
// the board area inside img.
func (p *Profile) Learn(img *image.RGBA, boardRect image.Rectangle, placement [64]chess.Piece, whiteAtBottom bool) error {
	if boardRect.Dx() < 80 || boardRect.Dy() < 80 {
		return errors.New("область доски слишком мала")
	}
	type occ struct {
		cov   float32
		piece chess.Piece
		sq    int
	}
	var minOcc = float32(1)
	var maxEmpty float32
	var newSamples [12][]Feat
	var newEmpty []Feat
	var origin [12][]int // source square of each sample (for reference distances)
	var originAug [12][]bool
	nPieces := 0
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			sq := gridToSquare(col, row, whiteAtBottom)
			r := squareRect(boardRect, col, row)
			pc := placement[sq]
			st := analyzeSquare(img, r, 0, 0)
			if pc == chess.NoPiece {
				if st.Coverage > maxEmpty {
					maxEmpty = st.Coverage
				}
				newEmpty = append(newEmpty, st.Feat)
				continue
			}
			nPieces++
			if st.Coverage < minOcc {
				minOcc = st.Coverage
			}
			for _, dy := range augShifts {
				for _, dx := range augShifts {
					s := st
					if dx != 0 || dy != 0 {
						s = analyzeSquare(img, r, dx*float64(r.Dx()), dy*float64(r.Dy()))
					}
					newSamples[pc] = append(newSamples[pc], s.Feat)
					origin[pc] = append(origin[pc], sq)
					originAug[pc] = append(originAug[pc], dx != 0 || dy != 0)
				}
			}
		}
	}
	if nPieces == 0 {
		return errors.New("в позиции нет фигур для обучения")
	}
	if minOcc < 0.02 {
		return errors.New("фигуры на снимке не видны: проверьте область доски")
	}
	// reference distance between different instances of the same piece type
	var refs []float64
	for c := 0; c < 12; c++ {
		for i, f := range newSamples[c] {
			if originAug[c][i] {
				continue
			}
			best := float32(math.MaxFloat32)
			for j, g := range newSamples[c] {
				if originAug[c][j] || origin[c][j] == origin[c][i] {
					continue
				}
				if d := dist2(&f, &g); d < best {
					best = d
				}
			}
			if best < math.MaxFloat32 {
				refs = append(refs, float64(best))
			}
		}
	}
	for c := 0; c < 12; c++ {
		p.Samples[c] = append(p.Samples[c], newSamples[c]...)
		if n := len(p.Samples[c]); n > maxSamplesPerClass {
			p.Samples[c] = append([]Feat(nil), p.Samples[c][n-maxSamplesPerClass:]...)
		}
	}
	if len(p.Empty) == 0 {
		p.Empty = syntheticEmpty()
	}
	p.Empty = append(p.Empty, newEmpty...)
	if len(p.Empty) > 120 {
		p.Empty = append(append([]Feat(nil), p.Empty[:20]...), p.Empty[len(p.Empty)-100:]...)
	}
	p.TOcc = 0.035
	if len(refs) > 0 {
		sort.Float64s(refs)
		ref := float32(refs[len(refs)/2])
		if p.Learned == 0 || ref > p.RefDist {
			p.RefDist = ref
		}
	}
	p.Learned++
	return nil
}

// Save writes the profile to a file.
func (p *Profile) Save(path string) error {
	p.Version = ProfileVersion
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(p); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// LoadProfile reads a profile from a file.
func LoadProfile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&p); err != nil {
		return nil, err
	}
	if p.Version != ProfileVersion {
		return nil, errors.New("устаревший профиль фигур")
	}
	return &p, nil
}

// FillMissing completes classes that were never seen on the board (for
// example both bishops of one colour in a middlegame) from the opposite
// colour: the ink shape is the same, only the brightness differs. It reports
// how many classes were synthesised. It does nothing unless at least the
// other colour has the class.
func (p *Profile) FillMissing() int {
	// brightness (alpha-weighted luma) of each colour's pieces
	body := func(c int) (float64, bool) {
		var a, al float64
		for t := 0; t < 6; t++ {
			for _, f := range p.Samples[c*6+t] {
				for i := 0; i < G*G; i++ {
					a += float64(f[i])
					al += float64(f[G*G+i])
				}
			}
		}
		if a <= 0 {
			return 0, false
		}
		return al / a, true
	}
	n := 0
	for t := 0; t < 6; t++ {
		for c := 0; c < 2; c++ {
			dst, src := c*6+t, (1-c)*6+t
			if len(p.Samples[dst]) > 0 || len(p.Samples[src]) == 0 {
				continue
			}
			lt, ok1 := body(c)
			ls, ok2 := body(1 - c)
			if !ok1 || !ok2 || ls <= 0 {
				continue
			}
			k := float32(lt / ls)
			for _, f := range p.Samples[src] {
				g := f
				for i := 0; i < G*G; i++ {
					v := f[G*G+i] * k
					if v > f[i] {
						v = f[i]
					}
					g[G*G+i] = v
				}
				p.Samples[dst] = append(p.Samples[dst], g)
			}
			p.Synth[dst] = true
			n++
		}
	}
	return n
}
