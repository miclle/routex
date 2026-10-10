package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSAMLAdmissionBudgetSeparatesLocalAndSignatureWork(t *testing.T) {
	admitted := time.Now()
	outer, cancel := context.WithDeadline(context.Background(), admitted.Add(10*time.Second))
	defer cancel()
	local, release := samlLocalContext(outer, admitted)
	defer release()
	deadline, ok := local.Deadline()
	if !ok || !deadline.Equal(admitted.Add(5*time.Second)) {
		t.Fatal("local budget was not captured at admission")
	}
	outerDeadline, ok := outer.Deadline()
	if !ok || !outerDeadline.Equal(admitted.Add(10*time.Second)) || outer.Err() != nil {
		t.Fatal("local budget shortened signature operation")
	}
	// Contexts inherited by later database phases and cleanup keep that same deadline.
	for range 3 {
		phase, phaseCancel := context.WithCancel(local)
		phaseDeadline, present := phase.Deadline()
		phaseCancel()
		if !present || !phaseDeadline.Equal(deadline) {
			t.Fatal("local phase renewed admission deadline")
		}
	}
}

func TestSAMLExpiredLocalAdmissionCannotRenewAfterSignatureWork(t *testing.T) {
	outer, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admitted := time.Now().Add(-6 * time.Second)
	local, release := samlLocalContext(outer, admitted)
	defer release()
	if !errors.Is(local.Err(), context.DeadlineExceeded) || outer.Err() != nil {
		t.Fatal("expired local budget did not remain separate from signature budget")
	}
	phase, phaseCancel := context.WithCancel(local)
	defer phaseCancel()
	deadline, ok := phase.Deadline()
	if !ok || !deadline.Equal(admitted.Add(5*time.Second)) || !errors.Is(phase.Err(), context.DeadlineExceeded) {
		t.Fatal("late local phase regained a transaction budget")
	}
}

func TestSAMLAdmissionBudgetPreservesShorterParentAndCancellation(t *testing.T) {
	admitted := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), admitted.Add(time.Second))
	defer cancel()
	outer, outerCancel := context.WithTimeout(parent, 10*time.Second)
	defer outerCancel()
	local, release := samlLocalContext(outer, admitted)
	defer release()
	deadline, ok := local.Deadline()
	parentDeadline, _ := parent.Deadline()
	if !ok || !deadline.Equal(parentDeadline) {
		t.Fatal("local budget extended shorter parent")
	}
	cancel()
	if !errors.Is(local.Err(), context.Canceled) || !errors.Is(outer.Err(), context.Canceled) {
		t.Fatal("parent cancellation lost")
	}
}
