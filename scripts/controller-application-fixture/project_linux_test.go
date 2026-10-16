//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestProjectInstalledAndPredecessorFilesRetainProtection(t *testing.T) {
	for _, owner := range []bool{true, false} {
		for _, name := range []string{"valid", "mode0644", "symlink", "hardlink", "foreign_owner"} {
			t.Run(name+map[bool]string{true: "/installed", false: "/private_probe"}[owner], func(t *testing.T) {
				if name == "foreign_owner" && os.Geteuid() != 0 {
					t.Skip("different-owner creation requires the owned root Linux test container")
				}
				root := t.TempDir()
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal("prepare protected Project fixture failed")
					}
				}
				must(os.Chmod(root, 0700))
				key := []byte("rxp_" + strings.Repeat("a", 43))
				path := filepath.Join(root, "slot")
				must(os.WriteFile(path, key, 0600))
				switch name {
				case "mode0644":
					must(os.Chmod(path, 0644))
				case "symlink":
					must(os.Rename(path, path+".real"))
					must(os.Symlink("slot.real", path))
				case "hardlink":
					must(os.Link(path, path+".second"))
				case "foreign_owner":
					must(os.Chown(path, 12345, 12345))
				}
				fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
				if err != nil {
					t.Fatal("open protected Project root failed")
				}
				defer func() { _ = syscall.Close(fd) }()
				raw, err := readRelativePrivate(fd, "slot", 4096, owner)
				defer clear(raw)
				accepted := name == "valid" || name == "foreign_owner" && !owner
				if accepted {
					if err != nil || string(raw) != string(key) || !validKey(raw) {
						t.Fatal("protected Project bearer rejected")
					}
				} else if err == nil {
					t.Fatal("unsafe Project Key file accepted")
				}
			})
		}
	}
}
