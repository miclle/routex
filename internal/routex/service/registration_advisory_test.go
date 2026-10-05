package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func managedAdvisorySubject(u entity.User) (entity.User, map[string]entity.RegistrationApprovalApplication) {
	id, actor, reason := "raa_01j00000000000000000000000", "usr_admin", "Reviewed account"
	decided := u.CreatedAt.Add(time.Minute)
	u.ApprovalApplicationID = &id
	app := entity.RegistrationApprovalApplication{ID: id, UserID: u.ID, UserCreatedAt: u.CreatedAt, CreatedAt: u.CreatedAt, State: "approved", Revision: memberRoleBaseline, DecidedAt: &decided, DecisionActorID: &actor, DecisionReason: &reason}
	return u, map[string]entity.RegistrationApprovalApplication{id: app}
}
func publishManagedAdvisory(auth *runtimeAuthorization, u entity.User, apps map[string]entity.RegistrationApprovalApplication) {
	_, proof := registrationAdmission(u, apps)
	if auth.UserAdmissions == nil {
		auth.UserAdmissions = map[string]runtimeAdmissionProof{}
	}
	auth.UserAdmissions[u.ID] = proof
}
func TestRegistrationManagedAdvisoriesPreserveApprovedCurrentProof(t *testing.T) {
	s, data := effectiveModelsFixture(t)
	data.Metadata.Subject, data.Metadata.Applications = managedAdvisorySubject(data.Metadata.Subject)
	auth := s.runtime.auth.Load()
	publishManagedAdvisory(auth, data.Metadata.Subject, data.Metadata.Applications)
	view := s.projectMemberEffectiveModels(data)
	if len(view.Items) != 1 || view.Items[0].Availability != "ready" || len(view.Items[0].Sources) != 2 || len(view.Items[0].Protocols) != 4 || len(view.Items[0].Sources[0].Protocols) != 4 || len(view.Items[0].Sources[1].Protocols) != 4 {
		t.Fatal("approved complete Personal/Team advisory lost ready protocols", view)
	}
	if view.Items[0].InputPrice.Rate.Amount != "0" || view.Items[0].OutputPrice.Rate.Amount != "0.000000000000000001" || view.Items[0].OutputPrice.Rate.Currency != "EUR" {
		t.Fatal("approval changed recorded price facts")
	}
	status, applied := s.memberModelsApplication(data.Metadata.Subject, data.Metadata.Grants, auth, data.Metadata.Applications)
	if status != "applied" || applied == nil || !*applied {
		t.Fatal("approved Personal grant application lost", status)
	}
	o, oa, targets, setting := memberOverviewProofFixture(t)
	subject, apps := managedAdvisorySubject(entity.User{ID: targets[0].id, CreatedAt: targets[0].created})
	oa.UserProofs = map[string]runtimeUserProof{subject.ID: {CreatedAt: subject.CreatedAt, Enabled: true}}
	publishManagedAdvisory(oa, subject, apps)
	if !o.memberOverviewSubjectApplied(oa, subject, targets[0], setting, "USD", apps) {
		t.Fatal("approved Member Overview application lost")
	}
	teams, ta, tt, calendar, tu, row := memberTeamsProofFixture(t)
	tu, apps = managedAdvisorySubject(tu)
	publishManagedAdvisory(ta, tu, apps)
	if !teams.memberTeamsApplied(ta, tu, row, tt[1], tt, calendar, "USD", apps) {
		t.Fatal("approved Member Teams application lost")
	}
}
func TestRegistrationManagedAdvisoriesRejectIncompleteOrStaleFacts(t *testing.T) {
	for _, name := range []string{"missing", "pending", "rejected", "app_alias", "user_alias", "creation_changed", "revision_changed", "disabled", "offboarded", "expired", "missing_published", "pointer_changed", "user_tombstone", "session_tombstone", "personal_tombstone"} {
		t.Run(name, func(t *testing.T) {
			s, data := effectiveModelsFixture(t)
			u, apps := managedAdvisorySubject(data.Metadata.Subject)
			data.Metadata.Subject = u
			data.Metadata.Applications = apps
			auth := s.runtime.auth.Load()
			publishManagedAdvisory(auth, u, apps)
			id := *u.ApprovalApplicationID
			app := apps[id]
			switch name {
			case "missing":
				delete(apps, id)
			case "pending":
				app.State = "pending"
				app.DecidedAt = nil
				app.DecisionActorID = nil
				app.DecisionReason = nil
				apps[id] = app
			case "rejected":
				app.State = "rejected"
				apps[id] = app
			case "app_alias":
				app.ID = strings.ToUpper(id)
				apps[id] = app
			case "user_alias":
				app.UserID = strings.ToUpper(u.ID)
				apps[id] = app
			case "creation_changed":
				app.UserCreatedAt = app.UserCreatedAt.Add(time.Microsecond)
				apps[id] = app
			case "revision_changed":
				app.Revision = strings.Repeat("b", 64)
				apps[id] = app
			case "disabled":
				data.Metadata.Subject.Disabled = true
			case "offboarded":
				data.Metadata.Subject.OffboardedAt = &app.CreatedAt
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "missing_published":
				delete(auth.UserAdmissions, u.ID)
			case "pointer_changed":
				other := *auth
				s.runtime.auth.Store(&other)
			case "user_tombstone":
				s.runtime.deniedUsers.Store(u.ID, true)
			case "session_tombstone":
				s.runtime.deniedSessionUsers.Store(u.ID, true)
			case "personal_tombstone":
				s.runtime.deniedPersonalGrants.Store(u.ID, true)
			}
			grantsBefore := personalGrantHash(data.Metadata.Grants)
			if s.memberEffectivePersonalProof(data.Metadata, auth, s.runtime.routes.Load()) {
				t.Fatal("denied/stale facts gained Personal proof")
			}
			if name != "personal_tombstone" && s.memberEffectiveTeamProof(data, data.Teams[0], auth, s.runtime.routes.Load()) {
				t.Fatal("denied/stale facts gained Team proof")
			}
			if name == "personal_tombstone" && !s.memberEffectiveTeamProof(data, data.Teams[0], auth, s.runtime.routes.Load()) {
				t.Fatal("Personal reduction changed independent Team proof")
			}
			if grantsBefore != personalGrantHash(data.Metadata.Grants) || len(data.Metadata.Grants) == 0 {
				t.Fatal("advisory mutated retained grants")
			}
		})
	}
}

type advisorySQLConnector struct{ f *effectiveSQLFixture }

func (c advisorySQLConnector) Connect(context.Context) (driver.Conn, error) {
	return advisorySQLConnection{effectiveSQLConnection: effectiveSQLConnection{fixture: c.f}}, nil
}
func (c advisorySQLConnector) Driver() driver.Driver { return advisorySQLDriver(c) }

type advisorySQLDriver advisorySQLConnector

func (d advisorySQLDriver) Open(string) (driver.Conn, error) {
	return advisorySQLConnector(d).Connect(context.Background())
}

type advisorySQLConnection struct{ effectiveSQLConnection }

func (c advisorySQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, `FROM "registration_approval_applications"`) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.fixture.queries = append(c.fixture.queries, q)
		if c.fixture.failTable == "approval" {
			return nil, errors.New("controlled approval read outage")
		}
		rows := []entity.RegistrationApprovalApplication{}
		for _, a := range c.fixture.data.Metadata.Applications {
			rows = append(rows, a)
		}
		return effectiveSQLRows(rows)
	}
	return c.effectiveSQLConnection.QueryContext(ctx, q, args)
}
func TestRegistrationManagedEffectiveReadLoadsCurrentApplications(t *testing.T) {
	for _, outage := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "outage"}[outage], func(t *testing.T) {
			s, data := effectiveModelsFixture(t)
			data.Metadata.Subject, data.Metadata.Applications = managedAdvisorySubject(data.Metadata.Subject)
			auth := s.runtime.auth.Load()
			publishManagedAdvisory(auth, data.Metadata.Subject, data.Metadata.Applications)
			before := data.Metadata.Subject
			grants := personalGrantHash(data.Metadata.Grants)
			f := &effectiveSQLFixture{data: data, permissions: map[string]bool{"members.read": true, "teams.read_all": true, "providers.read": true, "prices.read": true}}
			if outage {
				f.failTable = "approval"
			}
			pool := sql.OpenDB(advisorySQLConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			s.db = db
			result, err := s.MemberEffectiveModels(context.Background(), "usr_reader", data.Metadata.Subject.ID)
			if outage {
				if err == nil || result != nil {
					t.Fatal("current application outage invented readiness")
				}
				return
			}
			if err != nil || len(result.Items) != 1 || result.Items[0].Availability != "ready" || len(result.Items[0].Protocols) != 4 {
				t.Fatal("real-GORM current application hydration not used", err, result)
			}
			applicationReads, actorReads := 0, 0
			for _, query := range f.queries {
				if strings.Contains(query, `FROM "registration_approval_applications"`) {
					applicationReads++
				}
				if strings.Contains(query, `FROM "users"`) && strings.Contains(query, "SELECT *") {
					actorReads++
				}
			}
			if len(f.queries) != 22 || applicationReads != 1 || actorReads != 1 || !f.options.ReadOnly || f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("managed read did not add exactly one bounded RR application query", len(f.queries), f.options)
			}
			if !reflect.DeepEqual(before, data.Metadata.Subject) || grants != personalGrantHash(data.Metadata.Grants) {
				t.Fatal("read changed User/grant facts")
			}
		})
	}
}

func TestRegistrationManagedAdvisoriesUseLeasedSynchronousSnapshot(t *testing.T) {
	for _, name := range []string{"stopped", "nil_done"} {
		t.Run(name, func(t *testing.T) {
			withoutPublisher := func(s *Service) {
				if name == "stopped" {
					close(s.runtime.done)
				} else {
					s.runtime.done = nil
				}
			}
			s, data := effectiveModelsFixture(t)
			data.Metadata.Subject, data.Metadata.Applications = managedAdvisorySubject(data.Metadata.Subject)
			auth := s.runtime.auth.Load()
			publishManagedAdvisory(auth, data.Metadata.Subject, data.Metadata.Applications)
			withoutPublisher(s)
			grantsBefore := personalGrantHash(data.Metadata.Grants)
			view := s.projectMemberEffectiveModels(data)
			if len(view.Items) != 1 || view.Items[0].Availability != "ready" || len(view.Items[0].Sources) != 2 || len(view.Items[0].Protocols) != 4 {
				t.Fatal("current synchronous Personal/Team snapshot lost ready protocols", view)
			}
			if view.Items[0].InputPrice.Rate.Amount != "0" || view.Items[0].OutputPrice.Rate.Amount != "0.000000000000000001" || view.Items[0].OutputPrice.Rate.Currency != "EUR" || grantsBefore != personalGrantHash(data.Metadata.Grants) {
				t.Fatal("synchronous advisory changed price or grant facts")
			}
			status, applied := s.memberModelsApplication(data.Metadata.Subject, data.Metadata.Grants, auth, data.Metadata.Applications)
			if status != "applied" || applied == nil || !*applied {
				t.Fatal("synchronous Personal grant application lost", status)
			}
			if s.registrationAdmissionPublished(context.Background(), data.Metadata.Subject.ID, auth.UserAdmissions[data.Metadata.Subject.ID]) {
				t.Fatal("read advisory established active approval confirmation")
			}
			o, oa, targets, setting := memberOverviewProofFixture(t)
			subject, apps := managedAdvisorySubject(entity.User{ID: targets[0].id, CreatedAt: targets[0].created})
			oa.UserProofs = map[string]runtimeUserProof{subject.ID: {CreatedAt: subject.CreatedAt, Enabled: true}}
			publishManagedAdvisory(oa, subject, apps)
			withoutPublisher(o)
			if !o.memberOverviewSubjectApplied(oa, subject, targets[0], setting, "USD", apps) {
				t.Fatal("synchronous Member Overview application lost")
			}
			teams, ta, tt, calendar, tu, row := memberTeamsProofFixture(t)
			tu, apps = managedAdvisorySubject(tu)
			publishManagedAdvisory(ta, tu, apps)
			withoutPublisher(teams)
			if !teams.memberTeamsApplied(ta, tu, row, tt[1], tt, calendar, "USD", apps) {
				t.Fatal("synchronous Member Teams application lost")
			}
		})
	}
}
