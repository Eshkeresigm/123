package main

import "fmt"

func fmtSscan(s string, a ...any) (int, error) {
	return fmt.Sscanf(s, "%d,%d,%d,%d", a...)
}
