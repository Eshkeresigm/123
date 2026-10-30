// Package app is the brain of ChessVision: it ties screen capture, board
// recognition, game tracking and the engines together and exposes a model
// the interface can draw. Everything OS-specific lives behind Platform.
package app

import "image"

// Platform is what the controller needs from the operating system.
type Platform interface {
	// Capture grabs the given screen rectangle (virtual-screen pixels).
	Capture(r image.Rectangle) (*image.RGBA, error)
	// PickRegion lets the user drag a rectangle on the screen.
	PickRegion() (image.Rectangle, bool, error)
	// Clipboard access.
	GetClipboard() (string, error)
	SetClipboard(s string) error
	// ShowOverlay displays a click-through, always-on-top picture exactly over r.
	ShowOverlay(r image.Rectangle, img *image.RGBA)
	HideOverlay()
}
