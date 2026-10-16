package service

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
)

func sharedFreshSQLTestIntent() sharedFreshDomainIntent {
	return sharedFreshDomainIntent{InstallationID: "led_01arz3ndektsv4rrffq69g5fav", TimeZone: "UTC", CalendarETag: "qst_original",
		BoundaryAt: sharedAuthorizationTestTime().Add(time.Nanosecond).UnixNano(), ReceiptCapacity: 100}
}

func TestSharedFreshOriginRetainsExactCapturedCoordinates(t *testing.T) {
	intent := sharedFreshSQLTestIntent()
	first, err := sharedFreshDomainOrigin(intent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sharedFreshDomainOrigin(intent)
	if err != nil || first.EvidenceDigest != second.EvidenceDigest || !bytes.Equal(first.EvidenceBody, second.EvidenceBody) ||
		first.BoundaryAt != intent.BoundaryAt || first.BoundaryAt%1000 != 1 {
		t.Fatal("identical retry changed captured identity or truncated boundary", err)
	}
	for _, change := range []func(*sharedFreshDomainIntent){
		func(in *sharedFreshDomainIntent) { in.BoundaryAt++ },
		func(in *sharedFreshDomainIntent) { in.CalendarETag = "qst_other" },
		func(in *sharedFreshDomainIntent) { in.TimeZone = "Etc/UTC" },
		func(in *sharedFreshDomainIntent) { in.InstallationID = "led_01arz3ndektsv4rrffq69g5faw" },
	} {
		changed := intent
		change(&changed)
		row, err := sharedFreshDomainOrigin(changed)
		if err != nil || row.EvidenceDigest == first.EvidenceDigest {
			t.Fatal("distinct valid original coordinates were normalized", err)
		}
	}
	for _, change := range []func(*sharedFreshDomainIntent){
		func(in *sharedFreshDomainIntent) { in.ReceiptCapacity = 0 },
		func(in *sharedFreshDomainIntent) { in.ReceiptCapacity = -1 },
		func(in *sharedFreshDomainIntent) { in.BoundaryAt = 0 },
		func(in *sharedFreshDomainIntent) { in.BoundaryAt = math.MaxInt64 },
		func(in *sharedFreshDomainIntent) { in.TimeZone = "Local" },
		func(in *sharedFreshDomainIntent) { in.CalendarETag = " qst_original" },
		func(in *sharedFreshDomainIntent) { in.InstallationID = strings.ToUpper(in.InstallationID) },
	} {
		invalid := intent
		change(&invalid)
		if _, err := sharedFreshDomainOrigin(invalid); !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatal("invalid captured intent became an origin", err)
		}
	}
}

func TestSharedFreshAndCalendarRejectNonTransaction(t *testing.T) {
	for _, db := range []*gorm.DB{nil, {}, {Statement: &gorm.Statement{Context: context.Background()}}} {
		if proof, err := initializeFreshSharedDomainSQL(db, sharedFreshSQLTestIntent()); proof != nil || !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatal("constructor accepted nontransaction", err)
		}
		if proof, err := readSharedEconomicCalendarSQL(db, sharedUsageReadTarget{}); proof != nil || !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatal("calendar accepted nontransaction", err)
		}
	}
}

func TestSharedEconomicCalendarKeepsHistoricalAndCurrentRevisionsSeparate(t *testing.T) {
	intent := sharedFreshSQLTestIntent()
	origin, err := sharedFreshDomainOrigin(intent)
	if err != nil {
		t.Fatal(err)
	}
	target := sharedUsageReadTarget{InstallationID: intent.InstallationID, ModeRevision: 1}
	binding := entity.LimitInstallation{ID: 1, JournalID: intent.InstallationID, Initialized: true}
	mode := entity.AdmissionMode{ID: 1, InstallationID: intent.InstallationID, Mode: "shared", Revision: 1}
	quota := entity.QuotaSetting{ID: 1, AccountingStarted: true, TimeZone: intent.TimeZone, ETag: "qst_later"}
	domain := entity.AdmissionEconomicDomain{InstallationID: intent.InstallationID, FormatVersion: 1, Initialized: true,
		CoverageStart: intent.BoundaryAt, ClockHighWater: intent.BoundaryAt + int64(time.Hour), ReceiptCapacity: 100, ReceiptCount: 1}
	proof, err := sharedEconomicCalendarView(binding, mode, quota, domain, origin, target)
	if err != nil || proof.CalendarETag != quota.ETag || proof.CalendarRevisionAtFreeze != intent.CalendarETag || proof.ClockHighWater != domain.ClockHighWater {
		t.Fatal("current same-zone save or progressed clock lost immutable origin", err)
	}
	for _, test := range []struct {
		name string
		edit func(*entity.QuotaSetting, *entity.AdmissionEconomicDomain, *entity.AdmissionEconomicOrigin)
	}{
		{"unfrozen_setting", func(q *entity.QuotaSetting, _ *entity.AdmissionEconomicDomain, _ *entity.AdmissionEconomicOrigin) {
			q.AccountingStarted = false
		}},
		{"calendar_alias", func(q *entity.QuotaSetting, _ *entity.AdmissionEconomicDomain, _ *entity.AdmissionEconomicOrigin) {
			q.TimeZone = "Etc/UTC"
		}},
		{"invalid_current_etag", func(q *entity.QuotaSetting, _ *entity.AdmissionEconomicDomain, _ *entity.AdmissionEconomicOrigin) {
			q.ETag = "invalid etag"
		}},
		{"coverage_changed", func(_ *entity.QuotaSetting, d *entity.AdmissionEconomicDomain, _ *entity.AdmissionEconomicOrigin) {
			d.CoverageStart++
		}},
		{"clock_regressed", func(_ *entity.QuotaSetting, d *entity.AdmissionEconomicDomain, _ *entity.AdmissionEconomicOrigin) {
			d.ClockHighWater = d.CoverageStart - 1
		}},
		{"unknown_domain", func(_ *entity.QuotaSetting, d *entity.AdmissionEconomicDomain, _ *entity.AdmissionEconomicOrigin) {
			d.Initialized = false
		}},
		{"missing_origin", func(_ *entity.QuotaSetting, _ *entity.AdmissionEconomicDomain, o *entity.AdmissionEconomicOrigin) {
			*o = entity.AdmissionEconomicOrigin{}
		}},
		{"foreign_origin", func(_ *entity.QuotaSetting, _ *entity.AdmissionEconomicDomain, o *entity.AdmissionEconomicOrigin) {
			o.InstallationID = "led_01arz3ndektsv4rrffq69g5faw"
		}},
		{"noncanonical_origin", func(_ *entity.QuotaSetting, _ *entity.AdmissionEconomicDomain, o *entity.AdmissionEconomicOrigin) {
			o.EvidenceBody = append([]byte(" "), o.EvidenceBody...)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			q, d, o := quota, domain, origin
			test.edit(&q, &d, &o)
			if proof, err := sharedEconomicCalendarView(binding, mode, q, d, o, target); proof != nil || !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("inconsistent stored coordinates became calendar proof", err)
			}
		})
	}
}

func TestSharedFreshCoverageUsesRealAccountBirthAndConjunctiveParents(t *testing.T) {
	intent := sharedFreshSQLTestIntent()
	origin, err := sharedFreshDomainOrigin(intent)
	if err != nil {
		t.Fatal(err)
	}
	instant := origin.BoundaryAt + int64(time.Hour)
	monthStart, _, err := eventqueue.QuotaMonthBounds(instant, origin.TimeZone)
	if err != nil || monthStart >= origin.CoverageStart {
		t.Fatal("test requires partial first calendar month", err)
	}
	ceiling, amount := int64(100), int64(1)
	view := eventqueue.QuotaPolicyView{Window: func(string) (eventqueue.QuotaCounter, error) { return eventqueue.QuotaCounter{}, nil },
		InvalidBound: func(string) (bool, error) { return false, nil }}
	for _, test := range []struct {
		name  string
		birth time.Time
		want  error
	}{
		{"new_admin_or_root_key", time.Unix(0, origin.CoverageStart+int64(time.Second)), nil},
		{"older_parent", time.Unix(0, origin.CoverageStart-int64(time.Second)), eventqueue.ErrQuotaCoverage},
		{"unknown_parent", time.Time{}, eventqueue.ErrQuotaCoverage},
	} {
		t.Run(test.name, func(t *testing.T) {
			rules, err := eventqueue.NewQuotaPolicyRules(eventqueue.QuotaLimit{Account: "user_existing", Revision: "policy_original", CreatedAt: test.birth, TokensMonth: &ceiling},
				eventqueue.QuotaBound{Tokens: &amount, Revision: "bound_original"}, eventqueue.QuotaDecisionFrame{Instant: instant, MonthStart: monthStart, CoverageStart: origin.CoverageStart})
			if err != nil {
				t.Fatal(err)
			}
			if err := rules.Check(view); !errors.Is(err, test.want) {
				t.Fatal("real birth was reinterpreted as coverage boundary", err, test.want)
			}
		})
	}
	t.Run("new_account_still_requires_original_bound_revision", func(t *testing.T) {
		rules, err := eventqueue.NewQuotaPolicyRules(eventqueue.QuotaLimit{Account: "user_existing", Revision: "policy_original",
			CreatedAt: time.Unix(0, origin.CoverageStart+int64(time.Second)), TokensMonth: &ceiling},
			eventqueue.QuotaBound{Tokens: &amount}, eventqueue.QuotaDecisionFrame{Instant: instant, MonthStart: monthStart, CoverageStart: origin.CoverageStart})
		if err != nil {
			t.Fatal(err)
		}
		if err := rules.Check(view); !errors.Is(err, eventqueue.ErrQuotaBound) {
			t.Fatal("new account bypassed original reservation bound", err)
		}
	})
}
