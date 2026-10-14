//go:build !darwin && !linux

package deliverycontroller

import (
	"errors"
	"os"
	"os/exec"
)

func noFollowFlags() int             { return 0 }
func owned(os.FileInfo, bool) bool   { return false }
func singleLink(os.FileInfo) bool    { return false }
func trustedSticky(os.FileInfo) bool { return false }
func systemAlias(os.FileInfo) bool   { return false }
func isolate(*exec.Cmd) error        { return errors.New("controller platform is unsupported") }
func signalGroup(int, bool) error    { return errors.New("controller platform is unsupported") }
func groupExists(int) bool           { return true }
