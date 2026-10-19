//go:build !linux

package main

import "os"

// This diagnostic capability has no unsupported-platform transport fallback.
func main() { os.Exit(1) }
