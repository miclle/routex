package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestMemberStateExactRuntimeLifecycleProof(t *testing.T) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	subject := entity.User{ID: "usr_target", CreatedAt: now}
	for _, mode := range []string{"active", "disabled", "nil_runtime", "nil_done", "stopped", "cancelled", "nil_auth", "expired", "missing", "created", "enabled", "user_tombstone", "session_tombstone", "pointer", "lease_second", "cancel_second", "stop_second"} {
		t.Run(mode, func(t *testing.T) {
			rt := &gatewayRuntime{done: make(chan struct{})}
			auth := &runtimeAuthorization{ValidUntil: now.Add(time.Minute), UserProofs: map[string]runtimeUserProof{subject.ID: {CreatedAt: subject.CreatedAt, Enabled: true}}}
			rt.auth.Store(auth)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			u := subject
			clockCalls := 0
			s := &Service{runtime: rt, attemptNow: func() time.Time {
				clockCalls++
				if clockCalls == 2 {
					switch mode {
					case "pointer":
						rt.auth.Store(&runtimeAuthorization{ValidUntil: auth.ValidUntil, UserProofs: auth.UserProofs})
					case "cancel_second":
						cancel()
					case "stop_second":
						close(rt.done)
					case "lease_second":
						return now.Add(time.Minute)
					}
				}
				return now
			}}
			switch mode {
			case "disabled":
				u.Disabled = true
				auth.UserProofs[u.ID] = runtimeUserProof{CreatedAt: u.CreatedAt}
			case "nil_runtime":
				s.runtime = nil
			case "nil_done":
				rt.done = nil
			case "stopped":
				close(rt.done)
			case "cancelled":
				cancel()
			case "nil_auth":
				rt.auth.Store(nil)
			case "expired":
				auth.ValidUntil = now
			case "missing":
				delete(auth.UserProofs, u.ID)
			case "created":
				auth.UserProofs[u.ID] = runtimeUserProof{CreatedAt: now.Add(time.Microsecond), Enabled: true}
			case "enabled":
				u.Disabled = true
			case "user_tombstone":
				rt.deniedUsers.Store(u.ID, uint64(1))
			case "session_tombstone":
				rt.deniedSessionUsers.Store(u.ID, uint64(1))
			}
			got := s.memberStateRuntimeApplied(ctx, u)
			if got != (mode == "active" || mode == "disabled") {
				t.Fatal("unproved lifecycle", mode, got)
			}
		})
	}
}
func TestMemberStatePublishedBuilderExcludesDisabledCredentials(t *testing.T) {
	now := time.Now().UTC()
	user := entity.User{ID: "usr_target", CreatedAt: now, Disabled: true}
	data := &runtimeData{Users: []entity.User{user}, Keys: []entity.APIKey{{ID: "key_personal", UserID: user.ID, Status: entity.KeyActive, TokenHash: "personal"}}, TeamSessionData: &teamSessionRuntimeData{Sessions: []entity.Session{{ID: "ses_target", UserID: user.ID, TokenHash: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Hour)}}}}
	auth := buildRuntimeAuthorization(data, now.Add(time.Minute))
	if len(auth.Keys) != 0 || len(auth.KeysByID) != 0 || len(auth.TeamSessions) != 0 || auth.UserProofs[user.ID].Enabled {
		t.Fatal("disabled user credential published")
	}
	user.Disabled = false
	user.OffboardedAt = &now
	data.Users = []entity.User{user}
	auth = buildRuntimeAuthorization(data, now.Add(time.Minute))
	if len(auth.TeamSessions) != 0 || auth.UserProofs[user.ID].Enabled {
		t.Fatal("offboarded Team Session published")
	}
}

func TestMemberStateReadDoesNotWaitForRuntimeMutex(t *testing.T) {
	s, f := roleSQLService(t, 0)
	subject := f.data.users["usr_target"]
	rt := &gatewayRuntime{done: make(chan struct{})}
	rt.auth.Store(&runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), UserProofs: map[string]runtimeUserProof{subject.ID: {CreatedAt: subject.CreatedAt, Enabled: true}}})
	s.runtime = rt
	rt.mu.Lock()
	defer rt.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type outcome struct {
		result *MemberStateRecord
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := s.GetMemberState(ctx, "usr_admin", subject.ID)
		completed <- outcome{result, err}
	}()
	select {
	case got := <-completed:
		if got.err != nil || got.result == nil || !got.result.AccountAccessRuntimeApplied {
			t.Fatal("published lifecycle not independently readable", got.err)
		}
	case <-ctx.Done():
		t.Fatal("review attempted to wait for runtime mutex")
	}
}
