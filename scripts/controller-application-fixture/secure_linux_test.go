//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPrivateReadLinuxRejectsUnsafeFiles(t *testing.T) {
	for _, name := range []string{"valid", "mode0644", "hardlink", "symlink", "ancestor_symlink", "ancestor_writable", "directory", "fifo", "oversize", "empty", "traversal"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "keys")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			key := []byte("rx_" + strings.Repeat("a", 43))
			path := filepath.Join(dir, "slot")
			if err := os.WriteFile(path, key, 0600); err != nil {
				t.Fatal(err)
			}
			relative := "keys/slot"
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "mode0644":
				must(os.Chmod(path, 0644))
			case "hardlink":
				must(os.Link(path, filepath.Join(dir, "second")))
			case "symlink":
				must(os.Rename(path, path+".real"))
				must(os.Symlink("slot.real", path))
			case "ancestor_symlink":
				must(os.Rename(dir, dir+".real"))
				must(os.Symlink("keys.real", dir))
			case "ancestor_writable":
				must(os.Chmod(dir, 0777))
			case "directory":
				must(os.Remove(path))
				must(os.Mkdir(path, 0600))
			case "fifo":
				must(os.Remove(path))
				must(syscall.Mkfifo(path, 0600))
			case "oversize":
				must(os.WriteFile(path, make([]byte, 4097), 0600))
			case "empty":
				must(os.WriteFile(path, nil, 0600))
			case "traversal":
				relative = "keys/../keys/slot"
			}
			fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = syscall.Close(fd) }()
			raw, err := readRelativePrivate(fd, relative, 4096, true)
			if name == "valid" {
				if err != nil || string(raw) != string(key) {
					t.Fatal("valid exact key rejected")
				}
			} else if err == nil {
				t.Fatal("unsafe private file accepted")
			}
		})
	}
}
func TestPrivateReadRejectsWrongOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("different-owner creation requires the owned root Linux test container")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "key")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(fd) }()
	if _, err := readRelativePrivate(fd, "key", 4096, true); err == nil {
		t.Fatal("foreign-owned installed key accepted")
	}
	// Explicit private input binds may retain the coordinator host UID. Their
	// owner exemption never applies to the installed Key reader.
	if _, err := readRelativePrivate(fd, "key", 4096, false); err != nil {
		t.Fatal("private bound config rejected")
	}
}
func TestPrivateReadRejectsWorldWritableRootAndAbsoluteRelativeName(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0777); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(fd) }()
	if _, err := readRelativePrivate(fd, "key", 4096, true); err == nil {
		t.Fatal("writable root accepted")
	}
	if _, err := readRelativePrivate(fd, "/etc/passwd", 4096, true); err == nil {
		t.Fatal("absolute path accepted")
	}
}

func TestRunAliasRequiresKnownSpellingAndExactOutputTree(t *testing.T) {
	for _, name := range []string{"relative", "absolute", "arbitrary_absolute", "escaped_relative", "wrong_tree", "distinct_directory", "missing"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(os.Mkdir(filepath.Join(root, "var"), 0700))
			canonical := filepath.Join(root, "run")
			must(os.Mkdir(canonical, 0700))
			alias := filepath.Join(root, "var", "run")
			switch name {
			case "relative", "wrong_tree":
				must(os.Symlink("../run", alias))
				if name == "wrong_tree" {
					canonical = filepath.Join(root, "other")
					must(os.Mkdir(canonical, 0700))
				}
			case "absolute":
				canonical = "/run"
				must(os.Symlink("/run", alias))
			case "arbitrary_absolute":
				must(os.Symlink(canonical, alias))
			case "escaped_relative":
				must(os.Symlink("../../run", alias))
			case "distinct_directory":
				must(os.Mkdir(alias, 0700))
			}
			err := verifyRunAliasAt(alias, canonical)
			if (err == nil) != (name == "relative" || name == "absolute") {
				t.Fatal("alias spelling or exact output identity was not enforced")
			}
		})
	}
}
