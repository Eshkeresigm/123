// Package assets embeds the fonts used by the interface.
package assets

import _ "embed"

// UIRegular is Carlito Regular (SIL OFL 1.1), a Calibri-compatible humanist sans.
//
//go:embed UI-Regular.ttf
var UIRegular []byte

// UIBold is Carlito Bold (SIL OFL 1.1).
//
//go:embed UI-Bold.ttf
var UIBold []byte

// Symbols is DejaVu Sans (Bitstream Vera licence); its chess glyphs draw the pieces.
//
//go:embed Symbols.ttf
var Symbols []byte
