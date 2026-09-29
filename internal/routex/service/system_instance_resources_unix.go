//go:build darwin || linux

package service

import (
	"os"
	"path/filepath"
	"syscall"
)

func systemInstanceFilesystemUsage(path string) (int64, int64, bool) {
	path = filepath.Clean(path)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, 0, false
		}
		path = parent
	}
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil || stat.Bsize <= 0 {
		return 0, 0, false
	}
	total := uint64(stat.Bsize) * stat.Blocks
	available := uint64(stat.Bsize) * stat.Bavail
	if total == 0 || available > total || total > uint64(mathMaxInt64) {
		return 0, 0, false
	}
	return int64(total - available), int64(total), true
}

const mathMaxInt64 = int64(^uint64(0) >> 1)
