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

func deliver(ctx context.Context, cfg Config, paths deliveryPaths) (result Result, runErr error) {
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
	if err = requireAbsent(output, cfg.SlotID); err != nil {
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
	if err = atomicWrite(state, "agent.hcl", []byte(config.AgentConfiguration)); err != nil {
		return result, err
	}
	op := newOperation(cfg)
	if op.validate(cfg) != nil || saveOperation(state, op) != nil {
		return result, errors.New("persist prepared operation failed")
	}
	// Check absence again immediately before launch. This is not a claim that
	// an external writer can never race us; the private directory is operator
	// controlled and must have only this delivery process as writer.
	if err = requireAbsent(output, cfg.SlotID); err != nil {
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
	if err = requireAbsent(output, cfg.SlotID); err != nil {
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
	} else if observeOutput(output, cfg.SlotID) != nil {
		op.Intent.OutputState = "failed"
		runErr = errors.New("agent exited without a protected output observation")
	} else {
		// The process has exited; the wire protocol has no completed state.
		// Never label a joined one-shot Agent as currently running.
		op.Intent.OutputState = "written"
	}
	result.AgentState, result.OutputState = op.Intent.AgentState, op.Intent.OutputState
	if saveOperation(state, op) != nil {
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
