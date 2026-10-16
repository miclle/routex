//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Walk from a held root descriptor. Every component is opened relative to its
// verified predecessor, so replacing a pathname cannot redirect a later open.
func privateRead(path string, limit int, owner bool) ([]byte, error) {
	if limit < 1 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errInvalid
	}

	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errInvalid
	}
	defer func() { _ = syscall.Close(fd) }()
	return readRelativePrivate(fd, strings.TrimPrefix(path, "/"), limit, owner)
}

func readRelativePrivate(root int, path string, limit int, owner bool) ([]byte, error) {
	if limit < 1 || path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errInvalid
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "." || part == ".." || part == "" {
			return nil, errInvalid
		}
	}
	fd, err := syscall.Dup(root)
	if err != nil {
		return nil, errInvalid
	}
	defer func() { _ = syscall.Close(fd) }()
	var rootStat syscall.Stat_t
	if syscall.Fstat(fd, &rootStat) != nil || rootStat.Mode&syscall.S_IFMT != syscall.S_IFDIR || rootStat.Mode&0022 != 0 {
		return nil, errInvalid
	}
	for _, part := range parts[:len(parts)-1] {
		next, e := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return nil, errInvalid
		}
		var stat syscall.Stat_t
		if syscall.Fstat(next, &stat) != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR || stat.Mode&0022 != 0 {
			_ = syscall.Close(next)
			return nil, errInvalid
		}
		_ = syscall.Close(fd)
		fd = next
	}
	fileFD, err := syscall.Openat(fd, parts[len(parts)-1], syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errInvalid
	}
	f := os.NewFile(uintptr(fileFD), "private-input")
	defer func() { _ = f.Close() }()
	var stat syscall.Stat_t
	if syscall.Fstat(fileFD, &stat) != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Mode&07777 != 0600 || stat.Nlink != 1 || stat.Size < 1 || stat.Size > int64(limit) || (owner && stat.Uid != uint32(os.Geteuid())) {
		return nil, errInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	var after syscall.Stat_t
	if err != nil || len(raw) > limit || int64(len(raw)) != stat.Size || syscall.Fstat(fileFD, &after) != nil || after.Size != stat.Size || after.Mtim != stat.Mtim || after.Ctim != stat.Ctim || after.Mode != stat.Mode || after.Nlink != 1 || after.Uid != stat.Uid {
		clear(raw)
		return nil, errInvalid
	}
	return raw, nil
}
func verifyRunAlias() error {
	return verifyRunAliasAt("/var/run", "/run")
}

func verifyRunAliasAt(aliasPath, canonicalPath string) error {
	st, err := os.Lstat(aliasPath)
	if err != nil {
		return errInvalid
	}
	if st.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(aliasPath)
		if err != nil || (target != "/run" && target != "../run") {
			return errInvalid
		}
		st, err = os.Stat(aliasPath)
		if err != nil {
			return errInvalid
		}
	}
	// Both explicitly accepted spellings must resolve to the exact held output
	// tree. A real, distinct /var/run does not prove that its output was read.
	a, err := os.Stat(canonicalPath)
	if err != nil || !os.SameFile(st, a) {
		return errInvalid
	}
	return nil
}
