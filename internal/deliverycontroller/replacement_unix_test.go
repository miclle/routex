//go:build darwin || linux

package deliverycontroller

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func replacementFixture(t *testing.T, mode string) (*deliveryFixture, os.FileInfo) {
	t.Helper()
	f := newDeliveryFixture(t, mode)
	path := filepath.Join(f.paths.output, fixtureSlot)
	writeFile(t, path, "old-local-inode-canary", 0600)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return f, before
}

func readReplacementOperation(t *testing.T, f *deliveryFixture) operation {
	t.Helper()
	root, err := privateRoot(f.cfg.StateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	op, readErr := readOperation(root, f.cfg)
	closeErr := root.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read replacement intent: %v %v", readErr, closeErr)
	}
	return op
}

func assertOldReplacementOutput(t *testing.T, f *deliveryFixture, before os.FileInfo) {
	t.Helper()
	path := filepath.Join(f.paths.output, fixtureSlot)
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("old protected inode was replaced before promotion")
	}
	raw, err := os.ReadFile(path) // Test fixture only; production never reads Key output.
	if err != nil || string(raw) != "old-local-inode-canary" {
		t.Fatal("old fixture bytes changed")
	}
}

func TestReplacementHCLChangesOnlyReviewedDestination(t *testing.T) {
	cfg, c := fixturePins(), fixtureConfiguration()
	cfg.DescriptorRevision, c.DescriptorRevision = "2", "2"
	uuid := "12345678-1234-4234-8234-123456789abc"
	got, err := stagingHCL(c, cfg, uuid)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(c.AgentConfiguration, "\n  destination = "+strconv.Quote(c.KeyFile)+"\n", "\n  destination = "+strconv.Quote(keyDirectory+"/"+stagingLeaf(cfg.SlotID, uuid))+"\n", 1)
	if got != want || c.AgentConfiguration == got {
		t.Fatal("staging changed more than the reviewed destination")
	}
	for _, change := range []string{
		strings.Replace(c.AgentConfiguration, "backup = false", "backup = true", 1),
		c.AgentConfiguration + "exec { command = [\"sh\"] }\n",
		strings.Replace(c.AgentConfiguration, "?version=1", "?version=2", 1),
	} {
		bad := c
		bad.AgentConfiguration = change
		if _, err := stagingHCL(bad, cfg, uuid); err == nil {
			t.Fatal("unsafe original HCL was rewritten instead of rejected")
		}
	}
	if _, err := stagingHCL(c, cfg, "../other"); err == nil {
		t.Fatal("invalid staging operation identity accepted")
	}
}

func TestReplacementAtomicPromotionAndExactReportRetry(t *testing.T) {
	f, before := replacementFixture(t, "replace-success")
	f.mu.Lock()
	f.loseFirst = true
	f.mu.Unlock()
	result, err := replace(context.Background(), f.cfg, f.paths)
	if err == nil || result.ReportRecorded || !result.AgentJoined || !result.GroupGone || result.OutputState != "written" {
		t.Fatalf("lost response replacement: %+v %v", result, err)
	}
	op := readReplacementOperation(t, f)
	after, err := os.Stat(filepath.Join(f.paths.output, fixtureSlot))
	if err != nil || os.SameFile(before, after) || after.Mode().Perm() != 0600 || op.Replacement == nil || !op.Replacement.Promoted || op.Phase != "report_pending" {
		t.Fatal("replacement did not retain exact promoted inode intent")
	}
	if _, err := os.Lstat(filepath.Join(f.paths.output, op.Replacement.Stage)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging file remains after rename")
	}
	raw, err := os.ReadFile(filepath.Join(f.paths.output, fixtureSlot))
	if err != nil || string(raw) != "replacement-inference-canary" {
		t.Fatal("fixture replacement output missing")
	}
	launches, err := os.ReadFile(f.launches)
	if err != nil {
		t.Fatal(err)
	}
	// Retry has no dependency on the output root or current file contents.
	if err := os.Rename(f.paths.output, f.paths.output+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Rename(f.paths.output+"-moved", f.paths.output); err != nil {
			t.Error(err)
		}
	})
	retry, err := RetryReport(context.Background(), f.cfg)
	if err != nil || !retry.ReportRecorded || retry.AgentJoined || retry.GroupGone || retry.OutputState != "written" {
		t.Fatalf("report retry: %+v %v", retry, err)
	}
	again, err := os.ReadFile(f.launches)
	if err != nil || !bytes.Equal(launches, again) || f.pulls.Load() != 3 || f.reports.Load() != 2 {
		t.Fatal("retry relaunched or reread assignment/output")
	}
	f.mu.Lock()
	same := len(f.intents) == 2 && bytes.Equal(f.intents[0], f.intents[1])
	f.mu.Unlock()
	if !same {
		t.Fatal("retry changed durable original report body")
	}
	for _, name := range []string{"operation.json", "controller.lock", "agent.hcl"} {
		body, readErr := os.ReadFile(filepath.Join(f.cfg.StateDirectory, name))
		if name == "operation.json" && readErr != nil {
			t.Fatal(readErr)
		}
		for _, secret := range []string{"old-local-inode-canary", "replacement-inference-canary", "secret-output-canary", "secret-error-canary", "independent-jwt-fixture", "rxdctl_"} {
			if bytes.Contains(body, []byte(secret)) {
				t.Fatal("plaintext leaked to saved controller state")
			}
		}
	}
}

func TestReplacementRefusesChangedAuthorityAndCancellationBeforePromotion(t *testing.T) {
	for _, mode := range []string{"denied", "changed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f, before := replacementFixture(t, "replace-success")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.mu.Lock()
			f.denyThirdPull = mode == "denied"
			f.changeThirdHCL = mode == "changed"
			if mode == "canceled" {
				f.beforePull = func(count int32) {
					if count == 3 {
						cancel()
					}
				}
			}
			f.mu.Unlock()
			result, err := replace(ctx, f.cfg, f.paths)
			if err == nil || !result.AgentJoined || !result.GroupGone || result.OutputState == "written" || f.reports.Load() != 0 {
				t.Fatalf("post-Agent authority loss promoted: %+v %v", result, err)
			}
			assertOldReplacementOutput(t, f, before)
			op := readReplacementOperation(t, f)
			if op.Phase != "prepared" || op.Intent.OutputState != "unknown" {
				t.Fatal("unconfirmed promotion gained written evidence")
			}
			for _, name := range []string{filepath.Join(f.cfg.StateDirectory, "controller.lock"), filepath.Join(f.paths.output, ".routex-"+fixtureSlot+".lock")} {
				if _, err := os.Stat(name); err != nil {
					t.Fatal("uncertain replacement lock was released")
				}
			}
		})
	}
}

func TestReplacementFailurePreservesOldAndBlocksRelaunch(t *testing.T) {
	f, before := replacementFixture(t, "replace-failed")
	result, err := replace(context.Background(), f.cfg, f.paths)
	if err == nil || !result.AgentJoined || !result.GroupGone || result.OutputState == "written" {
		t.Fatalf("failed Agent promoted: %+v %v", result, err)
	}
	assertOldReplacementOutput(t, f, before)
	launches, err := os.ReadFile(f.launches)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replace(context.Background(), f.cfg, f.paths); err == nil {
		t.Fatal("failed replacement relaunched automatically")
	}
	again, err := os.ReadFile(f.launches)
	if err != nil || !bytes.Equal(launches, again) || f.reports.Load() != 0 {
		t.Fatal("stranded replacement was relaunched or reported")
	}
}

func TestSlotLockFencesIndependentStateDirectories(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "replacement"}[replacement], func(t *testing.T) {
			f := newDeliveryFixture(t, "replace-success")
			root, err := privateRoot(f.paths.output)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			}()
			unlock, err := lockSlot(root, fixtureSlot, "12345678-1234-4234-8234-123456789abc")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := unlock(); err != nil {
					t.Error(err)
				}
			}()
			f.cfg.StateDirectory = privateDirectory(t)
			if _, err := executeDelivery(context.Background(), f.cfg, f.paths, replacement); err == nil {
				t.Fatal("another state directory bypassed slot ownership")
			}
			if f.pulls.Load() != 0 || f.reports.Load() != 0 {
				t.Fatal("locked slot dispatched machine work")
			}
			if _, err := os.Stat(f.launches); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("locked slot launched Agent")
			}
		})
	}
}

func TestReplacementRefusesChangedOldOrUnprotectedStage(t *testing.T) {
	for _, mode := range []string{"old_inode", "stage_mode", "stage_symlink"} {
		t.Run(mode, func(t *testing.T) {
			f, before := replacementFixture(t, "replace-success")
			var external os.FileInfo
			f.mu.Lock()
			f.beforePull = func(count int32) {
				if count != 3 {
					return
				}
				switch mode {
				case "old_inode":
					path := filepath.Join(f.paths.output, fixtureSlot)
					temp := filepath.Join(f.paths.output, "external-output")
					if err := os.WriteFile(temp, []byte("external-writer-canary"), 0600); err != nil {
						t.Error(err)
						return
					}
					if err := os.Rename(temp, path); err != nil {
						t.Error(err)
						return
					}
					value, err := os.Stat(path)
					if err != nil {
						t.Error(err)
					}
					f.mu.Lock()
					external = value
					f.mu.Unlock()
				default:
					entries, err := os.ReadDir(f.paths.output)
					if err != nil {
						t.Error(err)
						return
					}
					for _, entry := range entries {
						if strings.HasSuffix(entry.Name(), ".pending") {
							path := filepath.Join(f.paths.output, entry.Name())
							if mode == "stage_mode" {
								if err := os.Chmod(path, 0644); err != nil {
									t.Error(err)
								}
							} else {
								if err := os.Remove(path); err != nil {
									t.Error(err)
									return
								}
								if err := os.Symlink(fixtureSlot, path); err != nil {
									t.Error(err)
								}
							}
						}
					}
				}
			}
			f.mu.Unlock()
			result, err := replace(context.Background(), f.cfg, f.paths)
			if err == nil || result.OutputState == "written" || f.reports.Load() != 0 {
				t.Fatalf("changed inode/stage promoted: %+v %v", result, err)
			}
			if mode == "old_inode" {
				now, err := os.Stat(filepath.Join(f.paths.output, fixtureSlot))
				f.mu.Lock()
				same := err == nil && external != nil && os.SameFile(external, now)
				f.mu.Unlock()
				if err != nil || !same {
					t.Fatal("controller overwrote changed external inode")
				}
			} else {
				assertOldReplacementOutput(t, f, before)
			}
		})
	}
}

func TestReplacementPromotePendingRetryNeverInfersFileSuccess(t *testing.T) {
	f, _ := replacementFixture(t, "replace-success")
	output, err := privateRoot(f.paths.output)
	if err != nil {
		t.Fatal(err)
	}
	old, err := openRegular(output, fixtureSlot, 0600, maxKeyFileBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := inspectIdentity(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	op := newOperation(f.cfg)
	op.SchemaVersion = 2
	op.Phase = "promote_pending"
	stageName := stagingLeaf(fixtureSlot, op.Intent.RequestID)
	writeFile(t, filepath.Join(f.paths.output, stageName), "staged-inference-canary", 0600)
	stage, err := openRegular(output, stageName, 0600, maxKeyFileBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	newIdentity, err := inspectIdentity(stage)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	op.Replacement = &replacementOperation{Old: identity, New: newIdentity, Stage: stageName}
	if op.validate(f.cfg) != nil {
		t.Fatal("valid unknown crash intent rejected")
	}
	// Simulate crash with either side of rename visible: reporting must not
	// inspect that file or promote/relaunch, regardless of its current bytes.
	state, err := privateRoot(f.cfg.StateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveOperation(state, op); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = lockSlot(output, fixtureSlot, op.Intent.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := RetryReport(context.Background(), f.cfg)
	if err != nil || !result.ReportRecorded || result.OutputState != "unknown" || result.AgentJoined || result.GroupGone || f.pulls.Load() != 0 {
		t.Fatalf("crash retry invented promotion: %+v %v", result, err)
	}
	after := readReplacementOperation(t, f)
	if !reflect.DeepEqual(after.Replacement, op.Replacement) || after.Phase != "reported" {
		t.Fatal("retry altered filesystem crash evidence")
	}
	if _, err := os.Stat(f.launches); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("crash retry launched Agent")
	}
	if _, err := os.Stat(filepath.Join(f.paths.output, ".routex-"+fixtureSlot+".lock")); err != nil {
		t.Fatal("report retry unlocked uncertain slot")
	}
}

// Err changes only at the second explicit gate, after the pre-promotion journal
// was synced. It supplies no production hook and does not change any deadline.
type cancelAtRenameContext struct {
	context.Context
	checks int
}

func (c *cancelAtRenameContext) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}

func TestReplacementCancellationAtRenameAndPostRenameCrashStayUnknown(t *testing.T) {
	for _, mode := range []string{"cancel_at_rename", "crash_after_rename"} {
		t.Run(mode, func(t *testing.T) {
			f, before := replacementFixture(t, "replace-success")
			output, err := privateRoot(f.paths.output)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := output.Close(); err != nil {
					t.Error(err)
				}
			}()
			state, err := privateRoot(f.cfg.StateDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := state.Close(); err != nil {
					t.Error(err)
				}
			}()
			old, err := openRegular(output, fixtureSlot, 0600, maxKeyFileBytes, false)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := old.Close(); err != nil {
					t.Error(err)
				}
			}()
			identity, err := inspectIdentity(old)
			if err != nil {
				t.Fatal(err)
			}
			op := newOperation(f.cfg)
			op.SchemaVersion = 2
			op.Replacement = &replacementOperation{Old: identity, Stage: stagingLeaf(fixtureSlot, op.Intent.RequestID)}
			writeFile(t, filepath.Join(f.paths.output, op.Replacement.Stage), "staged-inference-canary", 0600)
			if err := saveOperation(state, op); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "cancel_at_rename" {
				ctx = &cancelAtRenameContext{Context: context.Background()}
			}
			promoteErr := promoteReplacement(ctx, state, output, &op, f.cfg, old)
			if mode == "cancel_at_rename" {
				if promoteErr == nil {
					t.Fatal("cancellation at final gate did not stop rename")
				}
				assertOldReplacementOutput(t, f, before)
			} else {
				if promoteErr != nil {
					t.Fatal(promoteErr)
				}
				after, err := os.Stat(filepath.Join(f.paths.output, fixtureSlot))
				if err != nil || os.SameFile(before, after) {
					t.Fatal("fixture rename did not happen")
				}
				// Deliberately omit the final written journal to model process death
				// after atomic rename but before durable report intent replacement.
			}
			saved, err := readOperation(state, f.cfg)
			if err != nil || saved.Phase != "promote_pending" || saved.Replacement.Promoted || saved.Intent.OutputState != "unknown" {
				t.Fatal("crash window was reported as written")
			}
			result, err := RetryReport(context.Background(), f.cfg)
			if err != nil || !result.ReportRecorded || result.OutputState != "unknown" || f.pulls.Load() != 0 {
				t.Fatalf("unknown journal was inferred from file: %+v %v", result, err)
			}
		})
	}
}

func TestReplacementJournalRejectsInventedPromotion(t *testing.T) {
	f, _ := replacementFixture(t, "replace-success")
	root, err := privateRoot(f.paths.output)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	file, err := openRegular(root, fixtureSlot, 0600, maxKeyFileBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := inspectIdentity(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	valid := newOperation(f.cfg)
	valid.SchemaVersion = 2
	valid.Replacement = &replacementOperation{Old: identity, Stage: stagingLeaf(fixtureSlot, valid.Intent.RequestID)}
	if valid.validate(f.cfg) != nil {
		t.Fatal("prepared replacement journal rejected")
	}
	for name, mutate := range map[string]func(*operation){
		"written_without_promotion": func(op *operation) { op.Phase = "report_pending"; op.Intent.OutputState = "written" },
		"promotion_without_stage": func(op *operation) {
			op.Phase = "report_pending"
			op.Intent.OutputState = "written"
			op.Replacement.Promoted = true
		},
		"stage_is_old_inode":     func(op *operation) { op.Phase = "promote_pending"; op.Replacement.New = identity },
		"stage_traversal":        func(op *operation) { op.Replacement.Stage = "../other" },
		"unprotected_old":        func(op *operation) { op.Replacement.Old.Mode = 0644 },
		"old_schema_replacement": func(op *operation) { op.SchemaVersion = 1 },
		"missing_replacement":    func(op *operation) { op.Replacement = nil },
	} {
		t.Run(name, func(t *testing.T) {
			op := valid
			replacement := *valid.Replacement
			op.Replacement = &replacement
			mutate(&op)
			if op.validate(f.cfg) == nil {
				t.Fatal("invalid saved filesystem evidence accepted")
			}
		})
	}
}

func TestReplacementSuccessReleasesOnlyOwnedLocks(t *testing.T) {
	f, _ := replacementFixture(t, "replace-success")
	result, err := replace(context.Background(), f.cfg, f.paths)
	if err != nil || !result.ReportRecorded || !result.AgentJoined || !result.GroupGone || result.AgentState != "unknown" || result.OutputState != "written" {
		t.Fatalf("successful explicit replacement: %+v %v", result, err)
	}
	op := readReplacementOperation(t, f)
	if op.Phase != "reported" || !op.Replacement.Promoted || f.pulls.Load() != 3 || f.reports.Load() != 1 {
		t.Fatal("successful replacement lacks final exact receipt")
	}
	for _, path := range []string{filepath.Join(f.cfg.StateDirectory, "controller.lock"), filepath.Join(f.cfg.StateDirectory, "agent.hcl"), filepath.Join(f.paths.output, ".routex-"+fixtureSlot+".lock"), filepath.Join(f.paths.output, op.Replacement.Stage)} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("successful replacement retained owned transient artifact")
		}
	}
}

func TestReplacementRequiresExistingProtectedOutputAndSuccessor(t *testing.T) {
	for _, mode := range []string{"first_revision", "missing_output"} {
		t.Run(mode, func(t *testing.T) {
			f, before := replacementFixture(t, "replace-success")
			if mode == "first_revision" {
				f.cfg.DescriptorRevision = "1"
			} else {
				if err := os.Remove(filepath.Join(f.paths.output, fixtureSlot)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := replace(context.Background(), f.cfg, f.paths); err == nil {
				t.Fatal("unreviewed initial replacement accepted")
			}
			if f.pulls.Load() != 0 || f.reports.Load() != 0 {
				t.Fatal("invalid replacement dispatched machine work")
			}
			if mode == "first_revision" {
				assertOldReplacementOutput(t, f, before)
			}
			if _, err := os.Stat(f.launches); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid replacement launched Agent")
			}
		})
	}
}
