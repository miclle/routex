//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEvidenceInputRejectsUnsafeFiles(t *testing.T) {
	for _, name := range []string{"valid", "public", "symlink", "hardlink", "fifo", "oversize", "writable_ancestor"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(os.Chmod(root, 0700))
			must(os.Mkdir(filepath.Join(root, "input"), 0700))
			path := filepath.Join(root, "input", "config.json")
			must(os.WriteFile(path, []byte(`{"private":true}`), 0600))
			switch name {
			case "public":
				must(os.Chmod(path, 0644))
			case "symlink":
				must(os.Rename(path, path+".real"))
				must(os.Symlink("config.json.real", path))
			case "hardlink":
				must(os.Link(path, path+".link"))
			case "fifo":
				must(os.Remove(path))
				must(syscall.Mkfifo(path, 0600))
			case "oversize":
				must(os.WriteFile(path, make([]byte, 8193), 0600))
			case "writable_ancestor":
				must(os.Chmod(filepath.Dir(path), 0777))
			}
			fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
			must(err)
			defer func() { _ = syscall.Close(fd) }()
			raw, err := readRelativePrivate(fd, "input/config.json", 8192, false)
			if name == "valid" {
				if err != nil || string(raw) != `{"private":true}` {
					t.Fatal("private evidence input rejected")
				}
			} else if err == nil {
				t.Fatal("unsafe evidence input accepted")
			}
		})
	}
}
