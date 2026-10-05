package service

import (
	"context"
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"strings"
	"testing"
	"time"
)

func TestRegistrationAdmissionExactContinuity(t *testing.T) {
	now := time.Date(2026, 10, 5, 1, 2, 3, 456789000, time.UTC)
	id := "raa_01j00000000000000000000000"
	actor := "usr_actor"
	reason := "Reviewed exact account"
	decided := now.Add(time.Minute)
	u := entity.User{ID: "usr_subject", CreatedAt: now, ApprovalApplicationID: &id}
	a := entity.RegistrationApprovalApplication{ID: id, UserID: u.ID, UserCreatedAt: now, CreatedAt: now, State: "approved", Revision: strings.Repeat("a", 64), DecidedAt: &decided, DecisionActorID: &actor, DecisionReason: &reason}
	good := map[string]entity.RegistrationApprovalApplication{id: a}
	summary, proof := registrationAdmission(u, good)
	if summary.Status != "approved" || !summary.AdmissionEligible || !proof.Eligible {
		t.Fatal("exact approved source rejected")
	}
	cases := []struct {
		name   string
		change func(*entity.User, *entity.RegistrationApprovalApplication)
	}{
		{"pending", func(_ *entity.User, a *entity.RegistrationApprovalApplication) {
			a.State = "pending"
			a.DecidedAt = nil
			a.DecisionActorID = nil
			a.DecisionReason = nil
		}},
		{"rejected", func(_ *entity.User, a *entity.RegistrationApprovalApplication) { a.State = "rejected" }},
		{"disabled", func(u *entity.User, _ *entity.RegistrationApprovalApplication) { u.Disabled = true }},
		{"offboarded", func(u *entity.User, _ *entity.RegistrationApprovalApplication) { u.OffboardedAt = &now }},
		{"changed creation", func(_ *entity.User, a *entity.RegistrationApprovalApplication) {
			a.UserCreatedAt = now.Add(time.Microsecond)
		}},
		{"alias user", func(_ *entity.User, a *entity.RegistrationApprovalApplication) { a.UserID = strings.ToUpper(a.UserID) }},
		{"unknown state", func(_ *entity.User, a *entity.RegistrationApprovalApplication) { a.State = "APPROVED" }},
		{"missing decision", func(_ *entity.User, a *entity.RegistrationApprovalApplication) { a.DecisionActorID = nil }},
		{"empty link", func(u *entity.User, _ *entity.RegistrationApprovalApplication) { v := ""; u.ApprovalApplicationID = &v }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, b := u, a
			c.change(&v, &b)
			s, p := registrationAdmission(v, map[string]entity.RegistrationApprovalApplication{id: b})
			if s.AdmissionEligible || p.Eligible {
				t.Fatal("inexact/denied source admitted")
			}
		})
	}
	u.ApprovalApplicationID = nil
	s, p := registrationAdmission(u, nil)
	if s.Status != "not_required" || !p.Eligible {
		t.Fatal("explicit retained NULL should be unmanaged")
	}
}
func TestRegistrationApprovalStrictInput(t *testing.T) {
	for _, raw := range []string{`{"decision":"approve","reason":"review"}`, `{"decision":"reject","reason":"审批理由"}`} {
		var in MemberApprovalInput
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"decision":"approve","decision":"reject","reason":"r"}`, `{"decision":"approve","\u0064ecision":"approve","reason":"r"}`, `{"decision":null,"reason":"r"}`, `{"decision":"approve","reason":" r"}`, `{"decision":"approve","reason":"r","disabled":false}`, `{"decision":"approve","reason":"\ud800"}`, `{"decision":"APPROVE","reason":"r"}`} {
		var in MemberApprovalInput
		if json.Unmarshal([]byte(raw), &in) == nil {
			t.Fatalf("accepted invalid body %q", raw)
		}
	}
}
func TestRegistrationPolicyABAAndLoginExcludedFromReview(t *testing.T) {
	setting := entity.GovernanceSetting{RegistrationPolicyRevision: strings.Repeat("a", 64)}
	before := registrationPolicyETag(setting)
	setting.RegistrationPolicyRevision = strings.Repeat("b", 64)
	if before == registrationPolicyETag(setting) {
		t.Fatal("ABA generation omitted")
	}
	now := time.Now().UTC()
	actor := entity.User{ID: "usr_actor", CreatedAt: now, MemberRoleRevision: strings.Repeat("a", 64)}
	target := entity.User{ID: "usr_subject", CreatedAt: now, UpdatedAt: now}
	a := registrationApprovalETag(actor, target, nil, "proof")
	target.LastLoginAt = &now
	if a != registrationApprovalETag(actor, target, nil, "proof") {
		t.Fatal("login should not invalidate approval review")
	}
	target.Disabled = true
	if a == registrationApprovalETag(actor, target, nil, "proof") {
		t.Fatal("lifecycle omitted from review")
	}
}
func TestRegistrationPublishedDenialRequiresExactFacts(t *testing.T) {
	now := time.Now().UTC()
	p := runtimeAdmissionProof{CreatedAt: now, ApplicationID: "raa_01j00000000000000000000000", ApplicationCreatedAt: now, Revision: strings.Repeat("a", 64), State: "rejected", Eligible: false}
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	auth := &runtimeAuthorization{ValidUntil: now.Add(time.Hour), UserAdmissions: map[string]runtimeAdmissionProof{"usr_subject": p}}
	s.runtime.auth.Store(auth)
	if !s.registrationAdmissionPublished(context.Background(), "usr_subject", p) {
		t.Fatal("exact published denial should apply")
	}
	wrong := p
	wrong.Revision = strings.Repeat("b", 64)
	if s.registrationAdmissionPublished(context.Background(), "usr_subject", wrong) {
		t.Fatal("old denial borrowed new decision")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if s.registrationAdmissionPublished(canceled, "usr_subject", p) {
		t.Fatal("canceled proof")
	}
	close(s.runtime.done)
	if s.registrationAdmissionPublished(context.Background(), "usr_subject", p) {
		t.Fatal("stopped proof")
	}
}

func TestRegistrationRuntimeAdmissionIndependentOfRoutingDigest(t *testing.T) {
	s, data, projectBearer, personalBearer := projectRuntimeFixture(t)
	user := data.Users[0]
	applicationID := "raa_01j00000000000000000000000"
	user.ApprovalApplicationID = &applicationID
	data.Users[0] = user
	data.TeamSessionData = &teamSessionRuntimeData{
		Sessions:    []entity.Session{{ID: "ses_subject", UserID: user.ID, TokenHash: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Hour)}},
		Teams:       []entity.Team{{ID: "tea_subject", Status: entity.ResourceActive, CreatedAt: user.CreatedAt}},
		Memberships: []entity.TeamMembership{{ID: "tmm_subject", TeamID: "tea_subject", UserID: user.ID, Role: entity.TeamMember, Status: entity.ResourceActive}},
	}
	app := entity.RegistrationApprovalApplication{ID: applicationID, UserID: user.ID, UserCreatedAt: user.CreatedAt, CreatedAt: user.CreatedAt, State: "pending", Revision: strings.Repeat("a", 64)}
	data.ApprovalApplications = []entity.RegistrationApprovalApplication{app}
	digest, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	s.runtime.auth.Store(auth)
	if _, err := s.AuthenticateAPIKey(context.Background(), personalBearer); err == nil {
		t.Fatal("pending Personal Key admitted")
	}
	if _, err := s.AuthenticateAPIKey(context.Background(), projectBearer); err == nil {
		t.Fatal("Project with only pending manager admitted")
	}
	if len(auth.TeamSessions) != 0 || len(auth.Teams["tea_subject"].Members) != 0 || auth.PersonalGrantStates[user.ID].Enabled {
		t.Fatal("pending subject retained usable native authority")
	}
	// A terminal approved fact changes authentication, not Provider route bytes.
	now := time.Now().UTC()
	actor, reason := "usr_reviewer", "Reviewed exact account"
	app.State = "approved"
	app.Revision = strings.Repeat("b", 64)
	app.DecidedAt = &now
	app.DecisionActorID = &actor
	app.DecisionReason = &reason
	data.ApprovalApplications = []entity.RegistrationApprovalApplication{app}
	currentDigest, err := runtimeDigest(data)
	if err != nil || currentDigest != digest {
		t.Fatal("approval changed Provider route digest", err)
	}
	auth = buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	s.runtime.auth.Store(auth)
	if _, err := s.AuthenticateAPIKey(context.Background(), personalBearer); err != nil {
		t.Fatal("approved Personal authority missing", err)
	}
	key, err := s.AuthenticateAPIKey(context.Background(), projectBearer)
	if err != nil || key.ProjectID != "prj_one" || key.Key.UserID != "" {
		t.Fatal("approved manager altered Project bearer attribution", err)
	}
	if len(auth.TeamSessions) != 1 || auth.Teams["tea_subject"].Members[user.ID] != "tmm_subject" {
		t.Fatal("approved exact Team authority missing")
	}
	data.Users[0].CreatedAt = user.CreatedAt.Add(time.Microsecond)
	current := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	if current.UserAdmissions[user.ID].Eligible || len(current.TeamSessions) != 0 {
		t.Fatal("recreated identity borrowed immutable approval")
	}
	if !auth.UserAdmissions[user.ID].Eligible {
		t.Fatal("immutable authorization borrowed later source mutation")
	}
}

func TestRegistrationLegacyAdvisoryCannotBorrowManagedDecision(t *testing.T) {
	now := time.Now().UTC()
	user := entity.User{ID: "usr_subject", CreatedAt: now}
	_, wanted := registrationAdmission(user, nil)
	auth := &runtimeAuthorization{ValidUntil: now.Add(time.Minute), UserAdmissions: map[string]runtimeAdmissionProof{user.ID: wanted}}
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	s.runtime.auth.Store(auth)
	if !s.registrationAdvisoryPublished(auth, user, nil) {
		t.Fatal("explicit complete NULL proof rejected")
	}
	id := "raa_01j00000000000000000000000"
	user.ApprovalApplicationID = &id
	if s.registrationAdvisoryPublished(auth, user, nil) {
		t.Fatal("unmanaged publication borrowed managed target")
	}
	wanted.ApplicationID = id
	wanted.State = "approved"
	auth.UserAdmissions[user.ID] = wanted
	if s.registrationAdvisoryPublished(auth, user, nil) {
		t.Fatal("advisory inferred current decision from publication alone")
	}
}

func registrationAdvisoryFixture() (*Service, *runtimeAuthorization, entity.User, map[string]entity.RegistrationApprovalApplication, time.Time) {
	now := time.Date(2026, 10, 5, 1, 2, 3, 456789000, time.UTC)
	applicationID, actor, reason := "raa_01j00000000000000000000000", "usr_reviewer", "Reviewed exact account"
	decided := now.Add(time.Minute)
	user := entity.User{ID: "usr_subject", CreatedAt: now, ApprovalApplicationID: &applicationID}
	application := entity.RegistrationApprovalApplication{ID: applicationID, UserID: user.ID, UserCreatedAt: now, CreatedAt: now, State: "approved", Revision: strings.Repeat("a", 64), DecidedAt: &decided, DecisionActorID: &actor, DecisionReason: &reason}
	applications := map[string]entity.RegistrationApprovalApplication{applicationID: application}
	_, proof := registrationAdmission(user, applications)
	auth := &runtimeAuthorization{ValidUntil: now.Add(time.Hour), UserAdmissions: map[string]runtimeAdmissionProof{user.ID: proof}}
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}, attemptNow: func() time.Time { return now }}
	s.runtime.auth.Store(auth)
	return s, auth, user, applications, now
}

func TestRegistrationAdvisoryUsesCurrentSnapshotWithoutPublisher(t *testing.T) {
	for _, lifecycle := range []string{"stopped", "synchronous-only"} {
		t.Run(lifecycle, func(t *testing.T) {
			s, auth, user, applications, _ := registrationAdvisoryFixture()
			if lifecycle == "stopped" {
				close(s.runtime.done)
			} else {
				s.runtime.done = nil
			}
			if !s.registrationAdvisoryPublished(auth, user, applications) {
				t.Fatal("current exact leased admission snapshot was called unapplied")
			}
			if s.registrationAdmissionPublished(context.Background(), user.ID, auth.UserAdmissions[user.ID]) {
				t.Fatal("advisory snapshot bypassed active approval confirmation")
			}
		})
	}
}

func TestRegistrationAdvisoryCurrentSnapshotExactFences(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Service, **runtimeAuthorization, *entity.User, map[string]entity.RegistrationApprovalApplication, time.Time)
	}{
		{"missing runtime", func(s *Service, _ **runtimeAuthorization, _ *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			s.runtime = nil
		}},
		{"missing captured snapshot", func(_ *Service, auth **runtimeAuthorization, _ *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			*auth = nil
		}},
		{"missing installed snapshot", func(s *Service, _ **runtimeAuthorization, _ *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			s.runtime.auth.Store(nil)
		}},
		{"replaced snapshot", func(s *Service, auth **runtimeAuthorization, _ *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			replacement := **auth
			s.runtime.auth.Store(&replacement)
		}},
		{"expired lease", func(_ *Service, auth **runtimeAuthorization, _ *entity.User, _ map[string]entity.RegistrationApprovalApplication, now time.Time) {
			(*auth).ValidUntil = now
		}},
		{"missing exact User proof", func(_ *Service, auth **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			delete((*auth).UserAdmissions, user.ID)
		}},
		{"User alias", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			user.ID = strings.ToUpper(user.ID)
		}},
		{"zero birth", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			user.CreatedAt = time.Time{}
		}},
		{"changed birth", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			user.CreatedAt = user.CreatedAt.Add(time.Microsecond)
		}},
		{"Disabled", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			user.Disabled = true
		}},
		{"offboarded", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, now time.Time) {
			user.OffboardedAt = &now
		}},
		{"missing application", func(_ *Service, _ **runtimeAuthorization, user *entity.User, apps map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			delete(apps, *user.ApprovalApplicationID)
		}},
		{"changed application link", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			id := "raa_01j00000000000000000000001"
			user.ApprovalApplicationID = &id
		}},
		{"managed publication with NULL source", func(_ *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			user.ApprovalApplicationID = nil
		}},
		{"User tombstone", func(s *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			s.runtime.deniedUsers.Store(user.ID, uint64(1))
		}},
		{"Session User tombstone", func(s *Service, _ **runtimeAuthorization, user *entity.User, _ map[string]entity.RegistrationApprovalApplication, _ time.Time) {
			s.runtime.deniedSessionUsers.Store(user.ID, uint64(1))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, auth, user, apps, now := registrationAdvisoryFixture()
			close(s.runtime.done)
			c.change(s, &auth, &user, apps, now)
			if s.registrationAdvisoryPublished(auth, user, apps) {
				t.Fatal("inexact or denied snapshot was called applied")
			}
		})
	}
	applicationCases := []struct {
		name   string
		change func(*entity.RegistrationApprovalApplication)
	}{
		{"application alias", func(a *entity.RegistrationApprovalApplication) { a.ID = strings.ToUpper(a.ID) }},
		{"application birth", func(a *entity.RegistrationApprovalApplication) { a.CreatedAt = a.CreatedAt.Add(time.Microsecond) }},
		{"application revision", func(a *entity.RegistrationApprovalApplication) { a.Revision = strings.Repeat("b", 64) }},
		{"pending", func(a *entity.RegistrationApprovalApplication) {
			a.State = "pending"
			a.DecidedAt = nil
			a.DecisionActorID = nil
			a.DecisionReason = nil
		}},
		{"rejected", func(a *entity.RegistrationApprovalApplication) { a.State = "rejected" }},
		{"unknown", func(a *entity.RegistrationApprovalApplication) { a.State = "unknown" }},
		{"missing decision", func(a *entity.RegistrationApprovalApplication) { a.DecisionActorID = nil }},
	}
	for _, c := range applicationCases {
		t.Run(c.name, func(t *testing.T) {
			s, auth, user, apps, _ := registrationAdvisoryFixture()
			close(s.runtime.done)
			a := apps[*user.ApprovalApplicationID]
			c.change(&a)
			apps[*user.ApprovalApplicationID] = a
			if s.registrationAdvisoryPublished(auth, user, apps) {
				t.Fatal("changed SQL admission borrowed approved publication")
			}
		})
	}
	proofCases := []struct {
		name   string
		change func(*runtimeAdmissionProof)
	}{
		{"published User birth", func(p *runtimeAdmissionProof) { p.CreatedAt = p.CreatedAt.Add(time.Microsecond) }},
		{"published application link", func(p *runtimeAdmissionProof) { p.ApplicationID += "x" }},
		{"published application birth", func(p *runtimeAdmissionProof) { p.ApplicationCreatedAt = p.ApplicationCreatedAt.Add(time.Microsecond) }},
		{"published application revision", func(p *runtimeAdmissionProof) { p.Revision = strings.Repeat("b", 64) }},
		{"published state", func(p *runtimeAdmissionProof) { p.State = "pending" }},
		{"published eligibility", func(p *runtimeAdmissionProof) { p.Eligible = false }},
	}
	for _, c := range proofCases {
		t.Run(c.name, func(t *testing.T) {
			s, auth, user, apps, _ := registrationAdvisoryFixture()
			close(s.runtime.done)
			proof := auth.UserAdmissions[user.ID]
			c.change(&proof)
			auth.UserAdmissions[user.ID] = proof
			if s.registrationAdvisoryPublished(auth, user, apps) {
				t.Fatal("different published admission proof was called applied")
			}
		})
	}
}

func TestRegistrationAdvisoryRechecksSnapshotAndDenialFences(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Service, *runtimeAuthorization, entity.User)
	}{
		{"runtime replaced", func(s *Service, auth *runtimeAuthorization, _ entity.User) {
			replacement := &gatewayRuntime{}
			replacement.auth.Store(auth)
			s.runtime = replacement
		}},
		{"snapshot replaced", func(s *Service, auth *runtimeAuthorization, _ entity.User) {
			replacement := *auth
			s.runtime.auth.Store(&replacement)
		}},
		{"User tombstone", func(s *Service, _ *runtimeAuthorization, user entity.User) {
			s.runtime.deniedUsers.Store(user.ID, uint64(1))
		}},
		{"Session User tombstone", func(s *Service, _ *runtimeAuthorization, user entity.User) {
			s.runtime.deniedSessionUsers.Store(user.ID, uint64(1))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, auth, user, apps, now := registrationAdvisoryFixture()
			close(s.runtime.done)
			calls := 0
			s.attemptNow = func() time.Time {
				calls++
				if calls == 2 {
					c.change(s, auth, user)
				}
				return now
			}
			if s.registrationAdvisoryPublished(auth, user, apps) || calls != 2 {
				t.Fatal("late changed snapshot/fence bypassed final check", calls)
			}
		})
	}
	t.Run("lease expired between checks", func(t *testing.T) {
		s, auth, user, apps, now := registrationAdvisoryFixture()
		close(s.runtime.done)
		calls := 0
		s.attemptNow = func() time.Time {
			calls++
			if calls == 2 {
				return auth.ValidUntil
			}
			return now
		}
		if s.registrationAdvisoryPublished(auth, user, apps) || calls != 2 {
			t.Fatal("expired final snapshot lease was called applied", calls)
		}
	})
}
