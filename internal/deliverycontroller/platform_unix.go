//go:build darwin || linux

package deliverycontroller

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func noFollowFlags() int { return syscall.O_NOFOLLOW | syscall.O_NONBLOCK }

func owned(info os.FileInfo, allowRoot bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == uint32(os.Geteuid()) || allowRoot && stat.Uid == 0)
}

func singleLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

func trustedSticky(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode()&os.ModeSticky != 0
}

func systemAlias(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func isolate(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func signalGroup(pid int, kill bool) error {
	signal := syscall.SIGTERM
	if kill {
		signal = syscall.SIGKILL
	}
	err := syscall.Kill(-pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func groupExists(pid int) bool {
	return !errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH)
}
