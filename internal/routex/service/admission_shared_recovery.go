package service

import (
	"context"
	"time"

	"github.com/miclle/routex/pkg/routeattempt"
)

// Restart recovery can replay immutable history, but cannot accept a request,
// create an attempt, assert executor closure or obtain transport authority.
type sharedRecoveryAuthority interface {
	Read(context.Context, sharedAdmissionKey) (*sharedAuthorityReceipt, error)
	Checkpoint(context.Context, sharedAttemptCheckpoint) error
	Settle(context.Context, sharedTerminalIntent) (*sharedAuthorityReceipt, error)
}

type sharedRecoverySpool interface {
	Read(context.Context, sharedAdmissionKey) (*sharedSpoolRecord, error)
	ConfirmAcceptance(context.Context, sharedAdmissionKey, string) error
	ConfirmDispatch(context.Context, sharedDispatchReceipt) error
	MakeReady(context.Context, sharedTerminalIntent, string) error
}

type sharedRecoveryState string

const (
	sharedRecoveryPending sharedRecoveryState = "pending"
	sharedRecoveryReady   sharedRecoveryState = "ready"
)

// recoverSharedAdmission operates on one exact retained key, not a new owner or
// live coordinator. Without an original durable terminal it leaves the executor
// and unknown holds unresolved. Missing SQL history is an error, never authority
// to recreate an acceptance, discard a record or produce a zero settlement.
func recoverSharedAdmission(ctx context.Context, key sharedAdmissionKey, spool sharedRecoverySpool, authority sharedRecoveryAuthority, timeout time.Duration) (sharedRecoveryState, error) {
	if ctx == nil || spool == nil || authority == nil || timeout <= 0 || timeout > 30*time.Second {
		return "", errSharedAdmissionInvalid
	}
	op, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	local, err := spool.Read(op, key)
	if err != nil {
		return "", err
	}
	if err := validateSharedRecoveryLocal(local, key); err != nil {
		return "", err
	}
	receipt, err := authority.Read(op, key)
	if err != nil {
		return "", err
	}
	if err := validateSharedRecoveryReceipt(local, receipt); err != nil {
		return "", err
	}
	// Validate both complete views before acknowledging any historical transition.
	if err := spool.ConfirmAcceptance(op, key, receipt.ReceiptID); err != nil {
		return "", sharedUncertain(err)
	}
	// A terminal may follow an uncertain dispatch acknowledgement. The journal
	// correctly forbids adding dispatches after that terminal; its immutable
	// attempt attribution plus the validated SQL history suffice for settlement.
	if local.Terminal == nil {
		for _, dispatch := range receipt.Dispatches {
			if err := spool.ConfirmDispatch(op, dispatch); err != nil {
				return "", sharedUncertain(err)
			}
		}
	}
	for _, checkpoint := range local.Checkpoints[len(receipt.Checkpoints):] {
		captured, err := sharedCaptured(checkpoint)
		if err != nil {
			return "", err
		}
		if err := authority.Checkpoint(op, captured); err != nil {
			return "", sharedUncertain(err)
		}
	}
	if local.Terminal == nil {
		return sharedRecoveryPending, op.Err()
	}
	terminal, err := sharedCaptured(*local.Terminal)
	if err != nil {
		return "", err
	}
	settled, err := authority.Settle(op, terminal)
	if err != nil {
		return "", sharedUncertain(err)
	}
	if err := validateSharedRecoveryReceipt(local, settled); err != nil {
		return "", err
	}
	if settled.ReceiptID != receipt.ReceiptID || !sharedMatchesTerminal(settled.Terminal, local.Terminal) ||
		len(settled.Dispatches) != len(receipt.Dispatches) || len(settled.Checkpoints) != len(local.Checkpoints) {
		return "", errSharedAdmissionConflict
	}
	// The authority received a detached body; readiness still uses the unchanged
	// local original, even if a faulty adapter mutated its input.
	terminal, err = sharedCaptured(*local.Terminal)
	if err != nil {
		return "", err
	}
	if err := spool.MakeReady(op, terminal, settled.ReceiptID); err != nil {
		return "", sharedUncertain(err)
	}
	return sharedRecoveryReady, op.Err()
}

func validateSharedRecoveryLocal(local *sharedSpoolRecord, key sharedAdmissionKey) error {
	if local == nil || sharedIntentKey(local.Intent) != key || validateSharedSpoolAdmission(local.Intent) != nil ||
		len(local.Attempts) > routeattempt.MaxAttempts || len(local.Dispatches) > len(local.Attempts) ||
		len(local.Checkpoints) > len(local.Attempts) || local.ReceiptID != "" && !sharedIdentity(local.ReceiptID) ||
		local.Ready && local.Terminal == nil || local.ReceiptID == "" && (len(local.Attempts) != 0 || local.Terminal != nil) {
		return errSharedAdmissionConflict
	}
	for i, attempt := range local.Attempts {
		if attempt.Number != i+1 || validateSharedSpoolAttempt(local, attempt) != nil || validateSharedSpoolPrefix(local, attempt) != nil {
			return errSharedAdmissionConflict
		}
	}
	for i, dispatch := range local.Dispatches {
		if dispatch.Key != key || dispatch.Number != i+1 || dispatch.AttemptDigest != local.Attempts[i].Digest {
			return errSharedAdmissionConflict
		}
	}
	for i, checkpoint := range local.Checkpoints {
		if checkpoint.Number != i+1 || validateSharedSpoolCheckpoint(local, checkpoint) != nil {
			return errSharedAdmissionConflict
		}
	}
	if local.Terminal != nil && validateSharedSpoolTerminal(local, *local.Terminal) != nil {
		return errSharedAdmissionConflict
	}
	return nil
}

func validateSharedRecoveryReceipt(local *sharedSpoolRecord, receipt *sharedAuthorityReceipt) error {
	key := sharedIntentKey(local.Intent)
	if receipt == nil || receipt.Key != key || !sharedIdentity(receipt.ReceiptID) ||
		local.ReceiptID != "" && receipt.ReceiptID != local.ReceiptID ||
		len(receipt.Dispatches) > len(local.Attempts) || len(receipt.Dispatches) < len(local.Dispatches) ||
		len(receipt.Dispatches) < len(local.Checkpoints) || len(receipt.Checkpoints) > len(local.Checkpoints) ||
		len(receipt.Checkpoints)+1 < len(receipt.Dispatches) || len(receipt.Dispatches)+1 < len(local.Attempts) {
		return errSharedAdmissionConflict
	}
	for i, dispatch := range receipt.Dispatches {
		if dispatch.Key != key || dispatch.Number != i+1 || dispatch.AttemptDigest != local.Attempts[i].Digest {
			return errSharedAdmissionConflict
		}
	}
	for i, checkpoint := range receipt.Checkpoints {
		if checkpoint.Number != i+1 || checkpoint.Digest != local.Checkpoints[i].Digest || validateSharedSpoolCheckpoint(local, checkpoint) != nil {
			return errSharedAdmissionConflict
		}
	}
	if receipt.Terminal != nil && (!sharedMatchesTerminal(receipt.Terminal, local.Terminal) ||
		validateSharedSpoolTerminal(local, *receipt.Terminal) != nil || len(receipt.Dispatches) != len(local.Attempts) || len(receipt.Checkpoints) != len(local.Checkpoints)) {
		return errSharedAdmissionConflict
	}
	if local.Ready && receipt.Terminal == nil || local.Terminal != nil && len(receipt.Dispatches) != len(local.Attempts) {
		return errSharedAdmissionConflict
	}
	return nil
}
