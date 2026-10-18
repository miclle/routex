//go:build !linux

package main

// Native Linux stdin/UID qualification has no unsupported-platform fallback.
func stdinBounded() ([]byte, error) { return nil, errRejected }
