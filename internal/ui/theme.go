package ui

import "chessvision/internal/gfx"

// Palette of the light "pearl" theme.
var (
	colBg0     = gfx.Hex(0xF6F8FD)
	colBg1     = gfx.Hex(0xE4E9F6)
	colCard    = gfx.Hex(0xFAFBFF)
	colCardHi  = gfx.Hex(0xFFFFFF)
	colText    = gfx.Hex(0x151A2D)
	colMuted   = gfx.Hex(0x5A6480)
	colFaint   = gfx.Hex(0x98A1B9)
	colViolet  = gfx.Hex(0x6B5CFF)
	colCyan    = gfx.Hex(0x1DA1F2)
	colGreen   = gfx.Hex(0x16A36A)
	colAmber   = gfx.Hex(0xE08E0B)
	colRed     = gfx.Hex(0xE5486A)
	colLine    = gfx.RGBA(28, 40, 100, 24)
	colLineHi  = gfx.RGBA(28, 40, 100, 44)
	colBoardLt = gfx.Hex(0xEEF1FA)
	colBoardDk = gfx.Hex(0x8396CB)
	colWhite   = gfx.Hex(0xFFFFFF)
	colInk     = gfx.Hex(0x1B2340)
)

// glass is a faint ink tint used for hover plates, tracks and dividers.
func glass(a float64) gfx.Color {
	if a < 0 {
		a = 0
	} else if a > 1 {
		a = 1
	}
	return gfx.RGBA(28, 40, 100, uint8(a*255+0.5))
}

// paper is a translucent white plate (chips, pills).
func paper(a float64) gfx.Color {
	if a > 1 {
		a = 1
	}
	return gfx.RGBA(255, 255, 255, uint8(a*255+0.5))
}
