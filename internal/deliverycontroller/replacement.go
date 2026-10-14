package deliverycontroller

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
)

// These are local filesystem facts, never an API Key identity or content proof.
type fileIdentity struct {
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
	UID        uint32 `json:"uid"`
	Mode       uint32 `json:"mode"`
	Size       int64  `json:"size"`
	ModifiedAt int64  `json:"modified_at"`
}

type replacementOperation struct {
	Old      fileIdentity `json:"old"`
	Stage    string       `json:"stage"`
	New      fileIdentity `json:"new"`
	Promoted bool         `json:"promoted"`
}

func (f fileIdentity) valid() bool {
	return f.Inode != 0 && f.UID == uint32(os.Geteuid()) && f.Mode == 0600 && f.Size > 0 && f.Size <= maxKeyFileBytes
}

func (r replacementOperation) validate(op operation) error {
	newRecorded := r.New != (fileIdentity{})
	if !r.Old.valid() || r.Stage != stagingLeaf(op.SlotID, op.Intent.RequestID) || newRecorded && (!r.New.valid() || r.New.Device != r.Old.Device || r.New.Inode == r.Old.Inode) {
		return errors.New("invalid replacement filesystem intent")
	}
	if r.Promoted != (op.Intent.OutputState == "written") || r.Promoted && (!newRecorded || op.Phase != "report_pending" && op.Phase != "reported") || op.Phase == "prepared" && newRecorded || op.Phase == "promote_pending" && (r.Promoted || !newRecorded) {
		return errors.New("invalid replacement promotion phase")
	}
	return nil
}

func stagingLeaf(slot, uuid string) string { return ".routex-" + slot + "-" + uuid + ".pending" }

// Validate the whole server grammar before changing exactly its quoted output
// token. All authentication, guarded template and version-one facts stay verbatim.
func stagingHCL(c configuration, cfg Config, uuid string) (string, error) {
	if c.validate(cfg) != nil || !uuidPattern.MatchString(uuid) {
		return "", errors.New("replacement configuration rejected")
	}
	prefix := "\n  destination = "
	token := prefix + strconv.Quote(c.KeyFile) + "\n"
	if strings.Count(c.AgentConfiguration, token) != 1 {
		return "", errors.New("replacement destination is ambiguous")
	}
	start := strings.Index(c.AgentConfiguration, token) + len(prefix)
	staged := c
	staged.KeyFile = keyDirectory + "/" + stagingLeaf(cfg.SlotID, uuid)
	staged.AgentConfiguration = c.AgentConfiguration[:start] + strconv.Quote(staged.KeyFile) + c.AgentConfiguration[start+len(strconv.Quote(c.KeyFile)):]
	if validateHCL(staged, cfg.AllowLoopbackHTTP) != nil {
		return "", errors.New("staged configuration rejected")
	}
	return staged.AgentConfiguration, nil
}

func inspectIdentity(file *os.File) (fileIdentity, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !owned(info, false) || !singleLink(info) {
		return fileIdentity{}, errors.New("protected output identity rejected")
	}
	value, err := platformFileIdentity(info)
	if err != nil || !value.valid() {
		return fileIdentity{}, errors.New("protected output metadata rejected")
	}
	return value, nil
}

func exactFileIdentity(root *os.Root, name string, held *os.File, expected fileIdentity) error {
	current, err := openRegular(root, name, 0600, maxKeyFileBytes, false)
	if err != nil {
		return err
	}
	identity, inspectErr := inspectIdentity(current)
	closeErr := current.Close()
	heldIdentity, heldErr := inspectIdentity(held)
	if inspectErr != nil || closeErr != nil || heldErr != nil || identity != expected || heldIdentity != expected {
		return errors.New("reviewed output identity changed")
	}
	return nil
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return errors.New("open protected directory for sync failed")
	}
	syncErr, closeErr := directory.Sync(), directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("sync protected directory failed")
	}
	return nil
}

func lockSlot(root *os.Root, slot, uuid string) (func() error, error) {
	if !slotPattern.MatchString(slot) || !uuidPattern.MatchString(uuid) {
		return nil, errors.New("invalid slot lock identity")
	}
	name := ".routex-" + slot + ".lock"
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|noFollowFlags(), 0600)
	if err != nil {
		return nil, errors.New("slot output is locked; operator review required")
	}
	_, writeErr := file.Write([]byte(uuid))
	info, statErr := file.Stat()
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || statErr != nil || syncErr != nil || closeErr != nil || syncRoot(root) != nil {
		return nil, errors.New("persist slot lock failed")
	}
	return func() error {
		current, err := root.Lstat(name)
		if err != nil || !os.SameFile(info, current) || !current.Mode().IsRegular() || !owned(current, false) || !singleLink(current) || current.Mode().Perm() != 0600 {
			return errors.New("slot lock identity changed")
		}
		if root.Remove(name) != nil {
			return errors.New("release slot lock failed")
		}
		return syncRoot(root)
	}, nil
}

func promoteReplacement(ctx context.Context, state, output *os.Root, op *operation, cfg Config, old *os.File) (runErr error) {
	if ctx.Err() != nil {
		return errors.New("replacement interrupted before promotion")
	}
	stage, err := openRegular(output, op.Replacement.Stage, 0600, maxKeyFileBytes, false)
	if err != nil {
		return errors.New("replacement staging output rejected")
	}
	defer func() {
		if stage.Close() != nil && runErr == nil {
			runErr = errors.New("close staging output failed")
		}
	}()
	identity, err := inspectIdentity(stage)
	if err != nil || identity.Device != op.Replacement.Old.Device || stage.Sync() != nil || exactFileIdentity(output, op.Replacement.Stage, stage, identity) != nil || syncRoot(output) != nil {
		return errors.New("replacement staging persistence rejected")
	}
	op.Replacement.New = identity
	op.Phase = "promote_pending"
	if op.validate(cfg) != nil || saveOperation(state, *op) != nil {
		return errors.New("persist pre-promotion intent failed")
	}
	if exactFileIdentity(output, cfg.SlotID, old, op.Replacement.Old) != nil || exactFileIdentity(output, op.Replacement.Stage, stage, identity) != nil {
		return errors.New("replacement output changed before promotion")
	}
	// Rename has atomic visibility within this root, not compare-and-swap against
	// a hostile same-UID writer. The operator must enforce exclusive writers.
	if ctx.Err() != nil {
		return errors.New("replacement interrupted before rename")
	}
	if output.Rename(op.Replacement.Stage, cfg.SlotID) != nil {
		return errors.New("replace slot output failed")
	}
	if syncRoot(output) != nil {
		return errors.New("replacement durability is uncertain; output preserved")
	}
	if exactFileIdentity(output, cfg.SlotID, stage, identity) != nil {
		return errors.New("replacement identity changed after promotion")
	}
	op.Replacement.Promoted = true
	return nil
}
