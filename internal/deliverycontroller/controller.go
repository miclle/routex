package deliverycontroller

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Result is a local observation and advisory report, never application proof.
type Result struct {
	ReportRecorded bool
	AgentJoined    bool
	GroupGone      bool
	AgentState     string
	OutputState    string
}

// Deliver executes one explicitly pinned configuration with Vault Agent 1.21.4.
// Existing output and saved operations are preserved and refuse a new launch.
func Deliver(ctx context.Context, cfg Config) (Result, error) {
	return deliver(ctx, cfg, deliveryPaths{jwt: jwtFile, output: keyDirectory})
}

// Test fixtures use private equivalents; the public entry point always supplies
// the fixed generated JWT/output paths and never accepts them from an operator.
type deliveryPaths struct{ jwt, output string }

func deliver(ctx context.Context, cfg Config, paths deliveryPaths) (Result, error) {
	return executeDelivery(ctx, cfg, paths, false)
}

// Replace applies one explicitly assigned revision to the current protected local
// slot inode. It does not identify its old Key or establish application adoption.
func Replace(ctx context.Context, cfg Config) (Result, error) {
	return replace(ctx, cfg, deliveryPaths{jwt: jwtFile, output: keyDirectory})
}

func replace(ctx context.Context, cfg Config, paths deliveryPaths) (Result, error) {
	return executeDelivery(ctx, cfg, paths, true)
}

func executeDelivery(ctx context.Context, cfg Config, paths deliveryPaths, replacement bool) (result Result, runErr error) {
	if cfg.validate() != nil {
		return result, errors.New("invalid controller configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.budget())
	defer cancel()
	state, err := privateRoot(cfg.StateDirectory)
	if err != nil {
		return result, err
	}
	defer func() {
		if state.Close() != nil && runErr == nil {
			runErr = errors.New("close controller state failed")
		}
	}()
	unlock, err := lockState(state)
	if err != nil {
		return result, err
	}
	keepLock := false
	defer func() {
		if keepLock {
			return
		}
		if err := unlock(); err != nil && runErr == nil {
			runErr = err
		}
	}()
	if requireAbsent(state, "operation.json") != nil || requireAbsent(state, "agent.hcl") != nil {
		return result, errors.New("saved operation exists; use report-only retry")
	}
	output, err := privateRoot(paths.output)
	if err != nil {
		return result, err
	}
	defer func() {
		if output.Close() != nil && runErr == nil {
			runErr = errors.New("close controller output directory failed")
		}
	}()
	op := newOperation(cfg)
	if op.validate(cfg) != nil {
		return result, errors.New("prepare delivery identity failed")
	}
	releaseSlot, err := lockSlot(output, cfg.SlotID, op.Intent.RequestID)
	if err != nil {
		return result, err
	}
	defer func() {
		if !keepLock {
			if err := releaseSlot(); err != nil && runErr == nil {
				runErr = err
			}
		}
	}()
	var oldFile *os.File
	if replacement {
		if cfg.DescriptorRevision == "1" {
			return result, errors.New("replacement requires an assigned successor revision")
		}
		oldFile, err = openRegular(output, cfg.SlotID, 0600, maxKeyFileBytes, false)
		if err != nil {
			return result, err
		}
		defer func() {
			if oldFile.Close() != nil && runErr == nil {
				runErr = errors.New("close reviewed output failed")
			}
		}()
		old, err := inspectIdentity(oldFile)
		if err != nil {
			return result, err
		}
		op.SchemaVersion = 2
		op.Replacement = &replacementOperation{Old: old, Stage: stagingLeaf(cfg.SlotID, op.Intent.RequestID)}
	}
	checkOutput := func() error {
		if !replacement {
			return requireAbsent(output, cfg.SlotID)
		}
		if err := exactFileIdentity(output, cfg.SlotID, oldFile, op.Replacement.Old); err != nil {
			return err
		}
		return requireAbsent(output, op.Replacement.Stage)
	}
	if err = checkOutput(); err != nil {
		return result, err
	}
	machine, err := newMachineClient(cfg)
	if err != nil {
		return result, err
	}
	config, err := machine.pull(ctx)
	if err != nil {
		return result, err
	}
	agentIdentity, err := pinnedAgent(ctx, cfg)
	if err != nil {
		keepLock = errors.Is(err, errProcessUnclosed)
		return result, err
	}
	// Validate the workload JWT's protection without reading its contents; only
	// Agent needs that independently supplied credential.
	jwtRoot, err := os.OpenRoot(filepath.Dir(paths.jwt))
	if err != nil || trustedParents(paths.jwt) != nil {
		if jwtRoot != nil {
			// A close failure cannot make this rejected JWT directory valid.
			_ = jwtRoot.Close()
		}
		return result, errors.New("workload JWT directory unavailable")
	}
	jwt, err := openRegular(jwtRoot, filepath.Base(paths.jwt), 0600, 16<<10, false)
	rootCloseErr := jwtRoot.Close()
	if err != nil {
		return result, errors.New("workload JWT metadata rejected")
	}
	if jwt.Close() != nil || rootCloseErr != nil {
		return result, errors.New("close workload JWT inspection failed")
	}
	agentHCL := config.AgentConfiguration
	if replacement {
		agentHCL, err = stagingHCL(config, cfg, op.Intent.RequestID)
		if err != nil {
			return result, err
		}
	}
	if err = atomicWrite(state, "agent.hcl", []byte(agentHCL)); err != nil {
		return result, err
	}
	if op.validate(cfg) != nil || saveOperation(state, op) != nil {
		return result, errors.New("persist prepared operation failed")
	}
	// Check absence again immediately before launch. This is not a claim that
	// an external writer can never race us; the private directory is operator
	// controlled and must have only this delivery process as writer.
	if err = checkOutput(); err != nil {
		return result, err
	}
	// Reauthorize after local preparation without substituting a new assignment
	// or changing reviewed HCL. A digest includes private facts and is opaque;
	// exact public configuration equality is independently required here.
	current, err := machine.pull(ctx)
	if err != nil || current != config {
		return result, errors.New("launch reauthorization changed or is unavailable")
	}
	if _, err = verifyAgentExecutable(ctx, cfg, agentIdentity); err != nil {
		return result, err
	}
	if cfg.validate() != nil || ctx.Err() != nil || current.validate(cfg) != nil {
		return result, errors.New("launch budget or pinned configuration is no longer valid")
	}
	if err = checkOutput(); err != nil {
		return result, err
	}
	// The trusted operator must not mutate the pathname between this final
	// inode/content check and exec, or rewrite the private configuration file.
	process := runProcess(ctx, cfg.AgentExecutable, []string{"agent", "-config=" + filepath.Join(cfg.StateDirectory, "agent.hcl"), "-exit-after-auth=true"}, io.Discard)
	keepLock = process.Started && (!process.Joined || !process.GroupGone)
	result.AgentJoined, result.GroupGone = process.Joined, process.GroupGone
	op.Phase = "report_pending"
	if !process.Succeeded || !process.Joined || !process.GroupGone {
		if process.Joined && process.GroupGone || !process.Started {
			op.Intent.AgentState = "failed"
		}
		runErr = errors.New("agent delivery failed or process cleanup is unconfirmed")
	} else if replacement {
		// Agent may run for most of the budget. Recheck the exact assignment
		// after its owned process has closed, before changing the old output.
		current, err = machine.pull(ctx)
		if err != nil || current != config || ctx.Err() != nil {
			keepLock = true
			return result, errors.New("promotion reauthorization changed or is unavailable")
		}
		if err = promoteReplacement(ctx, state, output, &op, cfg, oldFile); err != nil {
			// A started replacement with uncertain file promotion cannot be
			// retried as another Agent run or silently restored.
			keepLock = true
			return result, err
		}
		op.Phase = "report_pending"
		op.Intent.OutputState = "written"
	} else if observeOutput(output, cfg.SlotID) != nil {
		op.Intent.OutputState = "failed"
		runErr = errors.New("agent exited without a protected output observation")
	} else {
		// The process has exited; the wire protocol has no completed state.
		// Never label a joined one-shot Agent as currently running.
		op.Intent.OutputState = "written"
	}
	if replacement && (!process.Succeeded || !process.Joined || !process.GroupGone) {
		keepLock = true
	}
	result.AgentState, result.OutputState = op.Intent.AgentState, op.Intent.OutputState
	if saveOperation(state, op) != nil {
		if replacement {
			keepLock = true
		}
		return result, errors.New("persist final report intent failed; output preserved")
	}
	if process.Joined && process.GroupGone || !process.Started {
		if state.Remove("agent.hcl") != nil {
			return result, errors.New("remove joined Agent configuration failed")
		}
	}
	if ctx.Err() != nil {
		return result, errors.New("delivery interrupted; exact report intent retained")
	}
	if keepLock {
		if process.Joined && process.GroupGone {
			return result, errors.New("replacement requires operator review; slot remains locked")
		}
		return result, errProcessUnclosed
	}
	if err = machine.report(ctx, op.Intent); err != nil {
		return result, err
	}
	result.ReportRecorded = true
	op.Phase = "reported"
	if saveOperation(state, op) != nil {
		return result, errors.New("report recorded; local receipt persistence failed")
	}
	return result, runErr
}

// RetryReport resubmits only the durable exact non-secret intent. It never pulls
// configuration, starts Agent, inspects output, reloads applications or infers
// that an interrupted process was joined. Pins must match the saved operation.
func RetryReport(ctx context.Context, cfg Config) (result Result, runErr error) {
	if cfg.validate() != nil {
		return result, errors.New("invalid controller configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.budget())
	defer cancel()
	state, err := privateRoot(cfg.StateDirectory)
	if err != nil {
		return result, err
	}
	defer func() {
		if state.Close() != nil && runErr == nil {
			runErr = errors.New("close controller state failed")
		}
	}()
	unlock, err := lockState(state)
	if err != nil {
		return result, err
	}
	defer func() {
		if err := unlock(); err != nil && runErr == nil {
			runErr = err
		}
	}()
	op, err := readOperation(state, cfg)
	if err != nil {
		return result, err
	}
	// Prepared after an interruption is deliberately unknown/unknown. The
	// operator must establish process closure before clearing a stranded lock;
	// this command cannot manufacture process evidence from stored state.
	if op.Phase == "prepared" {
		op.Phase = "report_pending"
		if saveOperation(state, op) != nil {
			return result, errors.New("persist unknown report intent failed")
		}
	}
	machine, err := newMachineClient(cfg)
	if err != nil {
		return result, err
	}
	result.AgentState, result.OutputState = op.Intent.AgentState, op.Intent.OutputState
	if err = machine.report(ctx, op.Intent); err != nil {
		return result, err
	}
	result.ReportRecorded = true
	op.Phase = "reported"
	if saveOperation(state, op) != nil {
		return result, errors.New("report recorded; local receipt persistence failed")
	}
	return result, nil
}
