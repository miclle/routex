//go:build !linux

package main

// Actual capsule file validation deliberately fails closed outside Linux.
func privateRead(_ string, _ int, _ bool) ([]byte, error) { return nil, errInvalid }
