//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ChessVision is a Windows application (build with GOOS=windows).")
	os.Exit(1)
}
