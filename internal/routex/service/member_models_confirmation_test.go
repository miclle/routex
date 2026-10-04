package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"sync"
	"testing"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestMemberModelsConfirmationPublisherRelease(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	desired := memberModelsGrantIDs(data.Grants)
	svc.runtime.mu.Lock()
	first := svc.projectMemberModels("usr_reader", data)
	if first.RuntimeApplied != nil || first.ApplicationStatus != "unavailable" || !first.runtimeBusy {
		svc.runtime.mu.Unlock()
		t.Fatal("busy read claimed application")
	}
	var release sync.Once
	unlock := func() { release.Do(svc.runtime.mu.Unlock) }
	defer unlock()
	calls := 0
	result, err := svc.confirmMemberModels(context.Background(), data.Subject.ID, desired, func(context.Context) (*MemberModelsWorkspace, error) {
		calls++
		if calls == 1 {
			defer unlock()
		}
		return svc.projectMemberModels("usr_reader", data), nil
	})
	if err != nil || result == nil || !result.RuntimeApplied || calls != 2 {
		t.Fatalf("fresh read after publisher release: calls=%d result=%+v error=%v", calls, result, err)
	}
	if first.RuntimeApplied != nil {
		t.Fatal("earlier unavailable capture was rewritten")
	}
}

func TestMemberModelsConfirmationStopsOnFreshAuthorityOrStateChange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*MemberModelsWorkspace) error
		expected error
	}{
		{"write authority lost", func(v *MemberModelsWorkspace) error { v.CanEdit = false; return nil }, runtimeUnavailable},
		{"different target", func(v *MemberModelsWorkspace) error { v.UserID = "usr_other"; return nil }, runtimeUnavailable},
		{"different set", func(v *MemberModelsWorkspace) error { v.PersonalModels = nil; return nil }, runtimeUnavailable},
		{"explicit false", func(v *MemberModelsWorkspace) error {
			no := false
			v.RuntimeApplied = &no
			v.ApplicationStatus = "not_applied"
			return nil
		}, runtimeUnavailable},
		{"ordinary unavailable", func(v *MemberModelsWorkspace) error { v.runtimeBusy = false; return nil }, runtimeUnavailable},
		{"wrong unavailable status", func(v *MemberModelsWorkspace) error { v.ApplicationStatus = "not_applied"; return nil }, runtimeUnavailable},
		{"authorization error", func(*MemberModelsWorkspace) error { return apperrors.ErrForbidden }, apperrors.ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			desired := memberModelsGrantIDs(data.Grants)
			calls := 0
			_, err := svc.confirmMemberModels(context.Background(), data.Subject.ID, desired, func(context.Context) (*MemberModelsWorkspace, error) {
				calls++
				v := svc.projectMemberModels("usr_reader", data)
				v.RuntimeApplied = nil
				v.ApplicationStatus = "unavailable"
				v.runtimeBusy = true
				if calls == 2 {
					return v, tc.change(v)
				}
				return v, nil
			})
			if !errors.Is(err, tc.expected) || calls != 2 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestMemberModelsConfirmationRuntimeLossStopsBusyRetry(t *testing.T) {
	for _, name := range []string{"expired lease", "stopped runtime", "missing publication"} {
		t.Run(name, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			calls := 0
			_, err := svc.confirmMemberModels(context.Background(), data.Subject.ID, memberModelsGrantIDs(data.Grants), func(context.Context) (*MemberModelsWorkspace, error) {
				calls++
				v := svc.projectMemberModels("usr_reader", data)
				v.RuntimeApplied = nil
				v.ApplicationStatus = "unavailable"
				v.runtimeBusy = true
				switch name {
				case "expired lease":
					auth := *svc.runtime.auth.Load()
					auth.ValidUntil = time.Now().Add(-time.Second)
					svc.runtime.auth.Store(&auth)
				case "stopped runtime":
					svc.runtime.done = make(chan struct{})
					close(svc.runtime.done)
				case "missing publication":
					svc.runtime.auth.Store(nil)
				}
				return v, nil
			})
			if !errors.Is(err, runtimeUnavailable) || calls != 1 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestMemberModelsConfirmationCancellationAndOriginalDeadline(t *testing.T) {
	for _, name := range []string{"already canceled", "cancel waiting", "deadline waiting", "late applied capture"} {
		t.Run(name, func(t *testing.T) {
			svc, data, _ := memberModelsProjectionFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "deadline waiting" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer deadlineCancel()
			}
			if name == "already canceled" {
				cancel()
			}
			calls := 0
			_, err := svc.confirmMemberModels(ctx, data.Subject.ID, memberModelsGrantIDs(data.Grants), func(context.Context) (*MemberModelsWorkspace, error) {
				calls++
				v := svc.projectMemberModels("usr_reader", data)
				if name == "late applied capture" {
					cancel()
					return v, nil
				}
				v.RuntimeApplied = nil
				v.ApplicationStatus = "unavailable"
				v.runtimeBusy = true
				if name == "cancel waiting" {
					cancel()
				}
				return v, nil
			})
			expected := context.Canceled
			if name == "deadline waiting" {
				expected = context.DeadlineExceeded
			}
			if !errors.Is(err, expected) || calls > 1 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestMemberModelsConfirmationHasFiniteBusyReadBound(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	svc.runtime.mu.Lock()
	defer svc.runtime.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	calls := 0
	_, err := svc.confirmMemberModels(ctx, data.Subject.ID, memberModelsGrantIDs(data.Grants), func(context.Context) (*MemberModelsWorkspace, error) {
		calls++
		return svc.projectMemberModels("usr_reader", data), nil
	})
	if !errors.Is(err, runtimeUnavailable) || calls != memberModelsConfirmationReads || ctx.Err() != nil {
		t.Fatalf("unbounded busy read: calls=%d error=%v context=%v", calls, err, ctx.Err())
	}
}

func TestMemberModelsBusyMarkerIsPrivateAndSpecific(t *testing.T) {
	svc, data, _ := memberModelsProjectionFixture(t)
	svc.runtime.mu.Lock()
	busy := svc.projectMemberModels("usr_reader", data)
	svc.runtime.mu.Unlock()
	raw, err := json.Marshal(busy)
	if err != nil || !busy.runtimeBusy || strings.Contains(strings.ToLower(string(raw)), "busy") {
		t.Fatalf("busy cause entered wire: %s error=%v", raw, err)
	}
	auth := *svc.runtime.auth.Load()
	auth.ValidUntil = time.Now().Add(-time.Second)
	svc.runtime.auth.Store(&auth)
	expired := svc.projectMemberModels("usr_reader", data)
	if expired.runtimeBusy || expired.RuntimeApplied != nil || expired.ApplicationStatus != "unavailable" {
		t.Fatal("expired lease became busy")
	}
	svc.runtime = nil
	missing := svc.projectMemberModels("usr_reader", data)
	if missing.runtimeBusy || missing.RuntimeApplied != nil || missing.ApplicationStatus != "unavailable" {
		t.Fatal("missing runtime became busy")
	}
}
