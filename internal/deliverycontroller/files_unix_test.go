//go:build darwin || linux

package deliverycontroller

import (
	"os"
	"path/filepath"
	"testing"
)

func privateDirectory(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedFilesRefuseSymlinksHardlinksAndModes(t *testing.T) {
	directory := privateDirectory(t)
	path := filepath.Join(directory, "bearer")
	writeFile(t, path, "fixture", 0600)
	if raw, err := readProtected(path, 51); err != nil || string(raw) != "fixture" {
		t.Fatal("protected file rejected")
	}
	link := filepath.Join(directory, "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(link, 51); err == nil {
		t.Fatal("symlink credential accepted")
	}
	if err := os.Link(path, filepath.Join(directory, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path, 51); err == nil {
		t.Fatal("hardlinked credential accepted")
	}
	if err := os.Remove(filepath.Join(directory, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path, 51); err == nil {
		t.Fatal("public credential accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path, 3); err == nil {
		t.Fatal("oversized credential accepted")
	}
	root, err := privateRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	if requireAbsent(root, "symlink") == nil {
		t.Fatal("existing symlink treated as absent")
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if r, err := privateRoot(directory); err == nil {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("public state directory accepted")
	}
}

func TestDurableIntentAndStrandedLock(t *testing.T) {
	directory := privateDirectory(t)
	root, err := privateRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := fixturePins()
	op := newOperation(cfg)
	if err := op.validate(cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveOperation(root, op); err != nil {
		t.Fatal(err)
	}
	reloaded, err := readOperation(root, cfg)
	if err != nil || reloaded != op {
		t.Fatal("durable operation changed")
	}
	op.Phase, op.Intent.OutputState = "report_pending", "written"
	if err := saveOperation(root, op); err != nil {
		t.Fatal(err)
	}
	reloaded, err = readOperation(root, cfg)
	if err != nil || reloaded != op {
		t.Fatal("final exact intent did not survive reload")
	}
	cfg.DescriptorRevision = "2"
	if _, err := readOperation(root, cfg); err == nil {
		t.Fatal("new assignment replaced saved intent")
	}
	release, err := lockState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockState(root); err == nil {
		t.Fatal("duplicate process lock accepted")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}
