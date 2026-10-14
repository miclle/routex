package deliverycontroller

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Private roots are provisioned by the operator. We do not chmod arbitrary
// existing paths or create directories underneath a server-provided name.
func privateRoot(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !owned(info, false) {
		return nil, errors.New("private directory unavailable")
	}
	if err = trustedParents(path); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("open private directory failed")
	}
	current, err := root.Stat(".")
	if err != nil || !os.SameFile(info, current) {
		// The opened root is rejected independently of its close outcome.
		_ = root.Close()
		return nil, errors.New("private directory changed")
	}
	return root, nil
}

func trustedParents(path string) error {
	// System aliases such as macOS /var are resolved only in ancestors. The
	// protected leaf itself must not be a symlink. Every resolved ancestor must
	// be controlled by root/the current UID; root-owned sticky temporary roots
	// are permitted because an unrelated UID cannot replace our private leaf.
	for ancestor := filepath.Dir(path); ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Lstat(ancestor)
		if err != nil || !owned(info, true) {
			return errors.New("untrusted protected path ancestor")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if !systemAlias(info) {
				return errors.New("non-system protected path alias")
			}
		} else if !info.IsDir() || info.Mode().Perm()&0022 != 0 && !trustedSticky(info) {
			return errors.New("untrusted protected path ancestor")
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return errors.New("resolve protected path failed")
	}
	for {
		info, err := os.Lstat(resolved)
		if err != nil || !info.IsDir() || !owned(info, true) || info.Mode().Perm()&0022 != 0 && !trustedSticky(info) {
			return errors.New("untrusted protected path ancestor")
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return nil
		}
		resolved = parent
	}
}

func readProtected(path string, limit int64) (raw []byte, runErr error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || trustedParents(path) != nil {
		return nil, errors.New("invalid protected file path")
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("open protected file directory failed")
	}
	defer func() {
		if root.Close() != nil && runErr == nil {
			clear(raw)
			raw = nil
			runErr = errors.New("close protected file directory failed")
		}
	}()
	file, err := openRegular(root, filepath.Base(path), 0600, limit, false)
	if err != nil {
		return nil, err
	}
	defer func() {
		if file.Close() != nil && runErr == nil {
			clear(raw)
			raw = nil
			runErr = errors.New("close protected file failed")
		}
	}()
	raw, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		clear(raw)
		return nil, errors.New("protected file is empty or exceeds its bound")
	}
	return raw, nil
}

func openRegular(root *os.Root, name string, mode os.FileMode, limit int64, allowRoot bool) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != mode || before.Size() < 1 || before.Size() > limit || !owned(before, allowRoot) || !singleLink(before) {
		return nil, errors.New("protected file metadata rejected")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|noFollowFlags(), 0)
	if err != nil {
		return nil, errors.New("open protected file failed")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm() != mode || after.Size() < 1 || after.Size() > limit || !owned(after, allowRoot) || !singleLink(after) {
		// The changed file is rejected independently of its close outcome.
		_ = file.Close()
		return nil, errors.New("protected file changed")
	}
	return file, nil
}

func requireAbsent(root *os.Root, name string) error {
	_, err := root.Lstat(name)
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("pre-existing output or operation refused")
	}
	return nil
}

func observeOutput(root *os.Root, slotID string) error {
	file, err := openRegular(root, slotID, 0600, maxKeyFileBytes, false)
	if err != nil {
		return err
	}
	// Stat only: the controller must never read, hash or transmit the API Key.
	return file.Close()
}

func atomicWrite(root *os.Root, name string, body []byte) (runErr error) {
	temporary := ".pending-" + rand.Text()
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollowFlags(), 0600)
	if err != nil {
		return errors.New("create controller state failed")
	}
	defer func() {
		if err := root.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) && runErr == nil {
			runErr = errors.New("remove temporary controller state failed")
		}
	}()
	_, writeErr := file.Write(body)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("persist controller state failed")
	}
	if root.Rename(temporary, name) != nil {
		return errors.New("replace controller state failed")
	}
	directory, err := root.Open(".")
	if err != nil {
		return errors.New("open controller state directory failed")
	}
	syncErr = directory.Sync()
	closeErr = directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("sync controller state directory failed")
	}
	return nil
}

func lockState(root *os.Root) (func() error, error) {
	file, err := root.OpenFile("controller.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollowFlags(), 0600)
	if err != nil {
		return nil, errors.New("controller state is locked; operator review required")
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil || closeErr != nil {
		return nil, errors.New("persist controller state lock failed")
	}
	directory, err := root.Open(".")
	if err != nil {
		return nil, errors.New("open controller lock directory failed")
	}
	syncErr, closeErr = directory.Sync(), directory.Close()
	if syncErr != nil || closeErr != nil {
		return nil, errors.New("sync controller state lock failed")
	}
	return func() error {
		if root.Remove("controller.lock") != nil {
			return errors.New("release controller state lock failed")
		}
		return nil
	}, nil
}
