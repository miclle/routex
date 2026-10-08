//go:build !darwin && !linux

package main

import (
	"errors"
	"os/exec"
)

func isolate(*exec.Cmd) error     { return errors.New("integration supervision requires macOS or Linux") }
func signalGroup(int, bool) error { return errors.New("process groups unavailable") }
func groupExists(int) bool        { return true }
