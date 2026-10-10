package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// These fixture projections freeze V94 and the retained pre-V94 authentication
// columns. They are not startup schemas and never AutoMigrate current entities.
type oidcFixtureProviderV94 struct {
	ID                       string     `gorm:"primaryKey;size:30;check:ck_oidc_singleton,CHAR_LENGTH(id) = 4 AND ASCII(SUBSTRING(id,1,1)) = 111 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 100 AND ASCII(SUBSTRING(id,4,1)) = 99"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	Issuer                   string     `gorm:"size:2048;not null"`
	ClientID                 string     `gorm:"size:256;not null"`
	CallbackURL              string     `gorm:"size:2048;not null"`
	SecretGeneration         string     `gorm:"size:64;not null"`
	AuthCiphertext           string     `gorm:"type:text;not null"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (oidcFixtureProviderV94) TableName() string { return "oidc_providers" }

type oidcFixtureBindingV94 struct {
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:255;not null"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (oidcFixtureBindingV94) TableName() string { return "oidc_bindings" }

type oidcFixtureCeremonyV94 struct {
	ID               string     `gorm:"primaryKey;size:30"`
	StateHash        string     `gorm:"size:64;not null;uniqueIndex"`
	CookieHash       string     `gorm:"size:64;not null;uniqueIndex"`
	Purpose          string     `gorm:"size:20;not null"`
	Reason           string     `gorm:"size:1024;not null"`
	Status           string     `gorm:"size:20;not null"`
	ConfigRevision   string     `gorm:"size:64;not null"`
	PolicyRevision   string     `gorm:"size:64;not null"`
	UserID           string     `gorm:"size:30;not null"`
	UserCreatedAt    *time.Time `gorm:"precision:6"`
	PasswordDigest   string     `gorm:"size:64;not null"`
	MFAGeneration    string     `gorm:"size:64;not null"`
	SessionID        string     `gorm:"size:30;not null"`
	SessionCreatedAt *time.Time `gorm:"precision:6"`
	Subject          string     `gorm:"size:255;not null"`
	BindingID        string     `gorm:"size:30;not null"`
	BindingCreatedAt *time.Time `gorm:"precision:6"`
	ExpiresAt        time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	VerifiedAt       *time.Time `gorm:"precision:6"`
}

func (oidcFixtureCeremonyV94) TableName() string { return "oidc_ceremonies" }

type oidcFixtureSessionV94 struct {
	PrimaryMethod        string     `gorm:"size:20;not null;default:''"`
	OIDCBindingID        string     `gorm:"column:oidc_binding_id;size:30;not null;default:''"`
	OIDCBindingCreatedAt *time.Time `gorm:"column:oidc_binding_created_at;precision:6"`
	OIDCConfigRevision   string     `gorm:"column:oidc_config_revision;size:64;not null;default:''"`
	OIDCPolicyRevision   string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''"`
	OIDCUserCreatedAt    *time.Time `gorm:"column:oidc_user_created_at;precision:6"`
}

func (oidcFixtureSessionV94) TableName() string { return "sessions" }

type oidcFixtureMFAChallengeV94 struct {
	PrimaryMethod        string     `gorm:"size:20;not null;default:''"`
	OIDCBindingID        string     `gorm:"column:oidc_binding_id;size:30;not null;default:''"`
	OIDCBindingCreatedAt *time.Time `gorm:"column:oidc_binding_created_at;precision:6"`
	OIDCConfigRevision   string     `gorm:"column:oidc_config_revision;size:64;not null;default:''"`
	OIDCPolicyRevision   string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''"`
	OIDCUserCreatedAt    *time.Time `gorm:"column:oidc_user_created_at;precision:6"`
}

func (oidcFixtureMFAChallengeV94) TableName() string { return "mfa_challenges" }

type oidcFixtureSessionProofV94 struct {
	PrimaryMethod string `gorm:"check:ck_sessions_oidc_primary,(primary_method = '' AND oidc_binding_id = '' AND oidc_binding_created_at IS NULL AND oidc_config_revision = '' AND oidc_policy_revision = '' AND oidc_user_created_at IS NULL) OR (CHAR_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL)"`
}

func (oidcFixtureSessionProofV94) TableName() string { return "sessions" }

type oidcFixtureMFAProofV94 struct {
	PrimaryMethod string `gorm:"check:ck_mfa_challenges_oidc_primary,(primary_method = '' AND oidc_binding_id = '' AND oidc_binding_created_at IS NULL AND oidc_config_revision = '' AND oidc_policy_revision = '' AND oidc_user_created_at IS NULL) OR (CHAR_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL)"`
}

func (oidcFixtureMFAProofV94) TableName() string { return "mfa_challenges" }

type oidcHistoricalSessionV93 struct {
	ID        string    `gorm:"primaryKey;size:30"`
	UserID    string    `gorm:"size:30;not null"`
	TokenHash string    `gorm:"size:64;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}

func (oidcHistoricalSessionV93) TableName() string { return "sessions" }

type oidcHistoricalChallengeV93 struct {
	UserID         string    `gorm:"primaryKey;size:30"`
	Purpose        string    `gorm:"primaryKey;size:20"`
	TokenHash      string    `gorm:"size:64;not null"`
	PasswordDigest string    `gorm:"size:64;not null"`
	Generation     string    `gorm:"size:64;not null"`
	SessionID      string    `gorm:"size:30;not null"`
	Attempts       int       `gorm:"not null"`
	ExpiresAt      time.Time `gorm:"not null"`
}

func (oidcHistoricalChallengeV93) TableName() string { return "mfa_challenges" }

// Each deliberately incompatible column is isolated on the empty ceremony
// table, including optional births: the provider singleton intentionally has NULLs.
type oidcWrongWidthV94 struct {
	Reason string `gorm:"size:1023;not null"`
}

func (oidcWrongWidthV94) TableName() string { return "oidc_ceremonies" }

type oidcWrongTypeV94 struct {
	UserCreatedAt string `gorm:"size:40"`
}

func (oidcWrongTypeV94) TableName() string { return "oidc_ceremonies" }

type oidcWrongPrecisionV94 struct {
	UserCreatedAt *time.Time `gorm:"precision:3"`
}

func (oidcWrongPrecisionV94) TableName() string { return "oidc_ceremonies" }

type oidcWrongOptionalNullV94 struct {
	UserCreatedAt time.Time `gorm:"precision:6;not null"`
}

func (oidcWrongOptionalNullV94) TableName() string { return "oidc_ceremonies" }

type oidcWrongRequiredNullV94 struct {
	Reason *string `gorm:"size:1024"`
}

func (oidcWrongRequiredNullV94) TableName() string { return "oidc_ceremonies" }

type oidcWrongTimeDefaultV94 struct {
	UserCreatedAt *time.Time `gorm:"precision:6;default:CURRENT_TIMESTAMP(6)"`
}

func (oidcWrongTimeDefaultV94) TableName() string { return "oidc_ceremonies" }

type oidcNonuniqueOwnerIndexV94 struct {
	UserID string `gorm:"index:idx_oidc_bindings_user_id"`
}

func (oidcNonuniqueOwnerIndexV94) TableName() string { return "oidc_bindings" }

type oidcWrongOwnerIndexV94 struct {
	SubjectDigest string `gorm:"uniqueIndex:idx_oidc_bindings_user_id"`
}

func (oidcWrongOwnerIndexV94) TableName() string { return "oidc_bindings" }

type oidcExtraOwnerIndexV94 struct {
	UserID         string `gorm:"uniqueIndex:idx_oidc_bindings_user_id,priority:1"`
	ConfigRevision string `gorm:"uniqueIndex:idx_oidc_bindings_user_id,priority:2"`
}

func (oidcExtraOwnerIndexV94) TableName() string { return "oidc_bindings" }

type oidcHistoricalRootJobV94 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8)"`
}

func (oidcHistoricalRootJobV94) TableName() string { return "secret_rotation_jobs" }

type oidcHistoricalRootItemV94 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)"`
}

func (oidcHistoricalRootItemV94) TableName() string { return "secret_rotation_items" }

type oidcHistoricalRootProcessV94 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"`
}

func (oidcHistoricalRootProcessV94) TableName() string { return "secret_process_verifications" }

func testOIDCMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	legacyMigrationBeforeV96(t, db)
	// This historical fixture may reconstruct V94 only on an exclusively owned
	// integration database with untouched, disabled V95 objects and no OAuth proof.
	initialLedger := personalKeyBehaviorLedger(t, db)
	if len(initialLedger) != 95 {
		t.Fatal("historical V94 fixture requires exact V95 boundary")
	}
	for i, row := range initialLedger {
		if row.Version != i+1 {
			t.Fatal("noncontiguous current ledger")
		}
	}
	for table, want := range map[string]int64{"oauth_providers": 1, "oauth_bindings": 0, "oauth_ceremonies": 0} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatal("historical fixture requires empty OAuth objects", table, count, err)
		}
	}
	var oauthDefault entity.OAuthProvider
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&oauthDefault, "id = ?", "oauth").Error; err != nil {
		t.Fatal(err)
	}
	if oauthDefault.CreatedAt.IsZero() || oauthDefault.UpdatedAt.IsZero() {
		t.Fatal("missing default OAuth birth")
	}
	oauthDefault.CreatedAt = time.Time{}
	oauthDefault.UpdatedAt = time.Time{}
	if !reflect.DeepEqual(oauthDefault, entity.OAuthProvider{ID: "oauth", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0", ScopesJSON: "[]", SubjectPathJSON: "[]"}) {
		t.Fatal("historical fixture cannot remove configured OAuth provenance")
	}
	for _, table := range []string{"sessions", "mfa_challenges"} {
		var count int64
		if err := db.Table(table).Where("oauth_binding_id <> '' OR oauth_binding_created_at IS NOT NULL OR oauth_config_revision <> '' OR oauth_policy_revision <> '' OR oauth_user_created_at IS NOT NULL OR primary_method = ?", "oauth").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("historical fixture contains OAuth primary", table, count, err)
		}
	}
	if err := database.MigrateThrough(ctx, db, 94); err == nil {
		t.Fatal("bounded migration accepted newer V95 ledger")
	}
	if !reflect.DeepEqual(initialLedger, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("rejected bound changed current ledger")
	}
	result := db.Table("schema_migrations").Where("version = ?", 95).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("remove only later V95 ledger in owned fixture", result.Error, result.RowsAffected)
	}
	for _, check := range []struct {
		model any
		name  string
	}{
		{&oidcFixtureSessionProofV94{}, "ck_sessions_oidc_primary"}, {&oidcFixtureMFAProofV94{}, "ck_mfa_challenges_oidc_primary"},
		{&oidcHistoricalRootJobV94{}, "ck_secret_inventory_version"}, {&oidcHistoricalRootJobV94{}, "ck_secret_rotation_domain"},
		{&oidcHistoricalRootItemV94{}, "ck_secret_item_domain"}, {&oidcHistoricalRootProcessV94{}, "ck_secret_process_inventory_version"},
	} {
		if db.Migrator().HasConstraint(check.model, check.name) {
			if err := db.Migrator().DropConstraint(check.model, check.name); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Migrator().CreateConstraint(check.model, check.name); err != nil {
			t.Fatal("restore exact frozen V94 constraint", check.name, err)
		}
	}
	ledger := personalKeyBehaviorLedger(t, db)
	if len(ledger) != 94 {
		t.Fatal("OIDC requires the exact 94-version ledger")
	}
	for i, row := range ledger {
		if row.Version != i+1 {
			t.Fatal("noncontiguous released migration prefix", i)
		}
	}
	assertLedger := func(present bool) {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		want := 93
		if present {
			want = 94
		}
		if len(rows) != want || !reflect.DeepEqual(ledger[:93], rows[:93]) || present && rows[93].Version != 94 {
			t.Fatal("OIDC changed a released ledger row or recorded failed DDL")
		}
	}
	remove94 := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 94).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V94", r.Error, r.RowsAffected)
		}
		assertLedger(false)
	}
	migrate := func() {
		t.Helper()
		if err := database.MigrateThrough(ctx, db, 94); err != nil {
			t.Fatal("restore V94", err)
		}
		assertLedger(true)
	}
	readProvider := func() oidcFixtureProviderV94 {
		t.Helper()
		var row oidcFixtureProviderV94
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", "oidc").Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	assertDefault := func() {
		t.Helper()
		for table, want := range map[string]int64{"oidc_providers": 1, "oidc_bindings": 0, "oidc_ceremonies": 0} {
			var count int64
			if err := db.Table(table).Count(&count).Error; err != nil || count != want {
				t.Fatal("fresh OIDC table", table, count, err)
			}
		}
		row := readProvider()
		if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			t.Fatal("singleton birth absent")
		}
		row.CreatedAt, row.UpdatedAt = time.Time{}, time.Time{}
		want := oidcFixtureProviderV94{ID: "oidc", ReviewRevision: strings.Repeat("0", 64), ConfigRevision: strings.Repeat("0", 64), PolicyRevision: strings.Repeat("0", 64), SecretGeneration: "0"}
		if !reflect.DeepEqual(row, want) {
			t.Fatal("default singleton invents enabled configuration or verifier provenance")
		}
	}
	assertDefault()
	birth := time.Now().UTC().Truncate(time.Microsecond)
	user := entity.User{ID: "usr_oidc_migration", Email: "oidc-migration@example.invalid", Name: "Retained local member", Role: entity.RoleMember, PasswordHash: "retained-not-an-authentication-secret", PersonalGrantRevision: strings.Repeat("0", 64)}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	session := oidcHistoricalSessionV93{ID: "ses_oidc_migration", UserID: user.ID, TokenHash: strings.Repeat("a", 64), ExpiresAt: birth.Add(time.Hour), CreatedAt: birth}
	challenge := oidcHistoricalChallengeV93{UserID: user.ID, Purpose: "login", TokenHash: strings.Repeat("b", 64), PasswordDigest: strings.Repeat("c", 64), Generation: strings.Repeat("d", 64), SessionID: session.ID, Attempts: 2, ExpiresAt: birth.Add(time.Minute)}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	readOld := func() (oidcHistoricalSessionV93, oidcHistoricalChallengeV93) {
		t.Helper()
		var s oidcHistoricalSessionV93
		var c oidcHistoricalChallengeV93
		// Explicit original result shapes stay stable across the twelve ADD/DROPs.
		if err := db.Select("id", "user_id", "token_hash", "expires_at", "created_at").Take(&s, "id = ?", session.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Select("user_id", "purpose", "token_hash", "password_digest", "generation", "session_id", "attempts", "expires_at").Take(&c, "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose).Error; err != nil {
			t.Fatal(err)
		}
		return s, c
	}
	originalSession, originalChallenge := readOld()
	retained := func() {
		t.Helper()
		s, c := readOld()
		if !reflect.DeepEqual(s, originalSession) || !reflect.DeepEqual(c, originalChallenge) {
			t.Fatal("V94 changed retained local Session/MFA facts")
		}
	}
	blankProvenance := func() {
		t.Helper()
		var s oidcFixtureSessionV94
		var c oidcFixtureMFAChallengeV94
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&s, "id = ?", session.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&c, "user_id = ? AND purpose = ?", challenge.UserID, challenge.Purpose).Error; err != nil {
			t.Fatal(err)
		}
		if s != (oidcFixtureSessionV94{}) || c != (oidcFixtureMFAChallengeV94{}) {
			t.Fatal("migration fabricated OIDC login provenance")
		}
	}
	checks := []struct {
		model, proof any
		name         string
	}{{&oidcFixtureSessionV94{}, &oidcFixtureSessionProofV94{}, "ck_sessions_oidc_primary"}, {&oidcFixtureMFAChallengeV94{}, &oidcFixtureMFAProofV94{}, "ck_mfa_challenges_oidc_primary"}}
	fields := []string{"PrimaryMethod", "OIDCBindingID", "OIDCBindingCreatedAt", "OIDCConfigRevision", "OIDCPolicyRevision", "OIDCUserCreatedAt"}
	for _, check := range checks {
		if err := db.Migrator().DropConstraint(check.proof, check.name); err != nil {
			t.Fatal(err)
		}
		for _, field := range fields {
			if err := db.Migrator().DropColumn(check.model, field); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, model := range []any{&oidcFixtureCeremonyV94{}, &oidcFixtureBindingV94{}, &oidcFixtureProviderV94{}} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	remove94()
	retained()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 94) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent V94 upgrade", err)
		}
	}
	migrate()
	assertDefault()
	retained()
	blankProvenance()
	providerBaseline := readProvider()
	current := func() {
		t.Helper()
		retained()
		blankProvenance()
		assertLedger(true)
		if !reflect.DeepEqual(providerBaseline, readProvider()) {
			t.Fatal("V94 replay replaced singleton birth/configuration")
		}
		for _, check := range checks {
			if !db.Migrator().HasConstraint(check.proof, check.name) {
				t.Fatal("missing primary proof check", check.name)
			}
		}
		for _, idx := range []struct {
			model any
			name  string
		}{
			{&oidcFixtureBindingV94{}, "idx_oidc_bindings_user_id"},
			{&oidcFixtureBindingV94{}, "idx_oidc_bindings_subject_digest"},
			{&oidcFixtureCeremonyV94{}, "idx_oidc_ceremonies_state_hash"},
			{&oidcFixtureCeremonyV94{}, "idx_oidc_ceremonies_cookie_hash"},
			{&oidcFixtureCeremonyV94{}, "idx_oidc_ceremonies_expires_at"},
		} {
			if !db.Migrator().HasIndex(idx.model, idx.name) {
				t.Fatal("missing exact V94 index", idx.name)
			}
		}
	}
	// Interrupted additive DDL: keep five installed columns and all new tables.
	if err := db.Migrator().DropConstraint(checks[0].proof, checks[0].name); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(checks[0].model, "OIDCPolicyRevision"); err != nil {
		t.Fatal(err)
	}
	remove94()
	migrate()
	current()
	remove94()
	migrate()
	migrate()
	current()

	for _, bad := range []struct {
		name, field string
		model       any
	}{
		{"width", "Reason", &oidcWrongWidthV94{}},
		{"type", "UserCreatedAt", &oidcWrongTypeV94{}},
		{"precision", "UserCreatedAt", &oidcWrongPrecisionV94{}},
		{"optional birth NOT NULL", "UserCreatedAt", &oidcWrongOptionalNullV94{}},
		{"required reason nullable", "Reason", &oidcWrongRequiredNullV94{}},
		{"invented birth default", "UserCreatedAt", &oidcWrongTimeDefaultV94{}},
	} {
		current() // A valid exact schema/actor history precedes every negative.
		var count int64
		if err := db.Table("oidc_ceremonies").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("schema negative requires empty ceremony table", err)
		}
		if err := db.Migrator().AlterColumn(bad.model, bad.field); err != nil {
			t.Fatal("install incompatible fixture", bad.name, err)
		}
		remove94()
		if err := database.MigrateThrough(ctx, db, 94); err == nil {
			t.Fatal("incompatible OIDC column accepted", bad.name)
		}
		assertLedger(false)
		retained()
		if err := db.Migrator().AlterColumn(&oidcFixtureCeremonyV94{}, bad.field); err != nil {
			t.Fatal("restore exact frozen column", bad.name, err)
		}
		migrate()
		current()
	}
	const ownerIndex = "idx_oidc_bindings_user_id"
	for _, bad := range []struct {
		name  string
		model any
	}{
		{"not unique", &oidcNonuniqueOwnerIndexV94{}},
		{"wrong column", &oidcWrongOwnerIndexV94{}},
		{"extra column", &oidcExtraOwnerIndexV94{}},
	} {
		current()
		if err := database.DropIndex(db, &oidcFixtureBindingV94{}, ownerIndex); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(bad.model, ownerIndex); err != nil {
			t.Fatal(err)
		}
		remove94()
		if err := database.MigrateThrough(ctx, db, 94); err == nil {
			t.Fatal("wrong-shape OIDC index accepted", bad.name)
		}
		assertLedger(false)
		retained()
		if err := database.DropIndex(db, &oidcFixtureBindingV94{}, ownerIndex); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(&oidcFixtureBindingV94{}, ownerIndex); err != nil {
			t.Fatal(err)
		}
		migrate()
		current()
	}
	// Unique owner and byte-digest keys are independently exercised. The digest
	// is authoritative for exact subjects; no case-insensitive Subject uniqueness
	// is introduced by this fixture.
	binding := oidcFixtureBindingV94{ID: "oib_migration", UserID: user.ID, UserCreatedAt: birth, ConfigRevision: strings.Repeat("e", 64), Subject: "ExactSubject", SubjectDigest: strings.Repeat("f", 64), CreatedAt: birth}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatal("positive binding", err)
	}
	readBinding := func() oidcFixtureBindingV94 {
		t.Helper()
		var row oidcFixtureBindingV94
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "id = ?", binding.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	bindingBaseline := readBinding()
	for _, field := range []string{"owner", "subject digest"} {
		if !reflect.DeepEqual(bindingBaseline, readBinding()) {
			t.Fatal("positive binding prerequisite")
		}
		candidate := binding
		candidate.ID = "oib_duplicate"
		if field == "owner" {
			candidate.SubjectDigest = strings.Repeat("1", 64)
		} else {
			candidate.UserID = "usr_other_oidc"
		}
		if err := db.Create(&candidate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Fatal("missing unique binding fence", field, err)
		}
		var count int64
		if err := db.Table("oidc_bindings").Count(&count).Error; err != nil || count != 1 || !reflect.DeepEqual(bindingBaseline, readBinding()) {
			t.Fatal("unique rejection changed bindings", field, err)
		}
	}
	// Updating the existing singleton cannot be masked by a duplicate primary
	// key on a case-insensitive database; every alias must hit the exact CHECK.
	for _, alias := range []string{"OIDC", "oidC", "oidc ", "other"} {
		if !reflect.DeepEqual(providerBaseline, readProvider()) {
			t.Fatal("positive singleton prerequisite")
		}
		if err := db.Model(&oidcFixtureProviderV94{}).Where("id = ?", "oidc").UpdateColumn("ID", alias).Error; err == nil {
			t.Fatal("singleton alias accepted", alias)
		}
		if !reflect.DeepEqual(providerBaseline, readProvider()) {
			t.Fatal("singleton rejection rewrote configuration")
		}
	}
	// Exercise both new existing-table CHECKs using valid complete provenance
	// before each malformed update; no application validator substitutes for DB.
	for _, target := range []struct {
		model any
		query string
		args  []any
	}{
		{&oidcFixtureSessionV94{}, "id = ?", []any{session.ID}},
		{&oidcFixtureMFAChallengeV94{}, "user_id = ? AND purpose = ?", []any{challenge.UserID, challenge.Purpose}},
	} {
		valid := map[string]any{"primary_method": "oidc", "oidc_binding_id": binding.ID, "oidc_binding_created_at": birth, "oidc_config_revision": strings.Repeat("e", 64), "oidc_policy_revision": strings.Repeat("2", 64), "oidc_user_created_at": birth}
		readProof := func() oidcFixtureSessionV94 {
			t.Helper()
			var row oidcFixtureSessionV94
			// Use the known fixed target table and fixed six-column projection.
			table := "sessions"
			if _, ok := target.model.(*oidcFixtureMFAChallengeV94); ok {
				table = "mfa_challenges"
			}
			if err := db.Table(table).Select("primary_method", "oidc_binding_id", "oidc_binding_created_at", "oidc_config_revision", "oidc_policy_revision", "oidc_user_created_at").Where(target.query, target.args...).Take(&row).Error; err != nil {
				t.Fatal(err)
			}
			return row
		}
		for _, bad := range []map[string]any{
			{"primary_method": "OIDC"}, {"oidc_binding_created_at": nil}, {"oidc_user_created_at": nil},
			{"oidc_binding_id": ""}, {"oidc_config_revision": "short"}, {"oidc_policy_revision": ""}, {"primary_method": ""},
		} {
			r := db.Model(target.model).Where(target.query, target.args...).Updates(valid)
			if r.Error != nil || r.RowsAffected != 1 {
				t.Fatal("positive complete OIDC proof", r.Error, r.RowsAffected)
			}
			baseline := readProof()
			if baseline.PrimaryMethod != "oidc" || baseline.OIDCBindingCreatedAt == nil || baseline.OIDCUserCreatedAt == nil {
				t.Fatal("positive OIDC proof not stored")
			}
			if err := db.Model(target.model).Where(target.query, target.args...).Updates(bad).Error; err == nil {
				t.Fatal("partial/uppercase OIDC proof accepted", bad)
			}
			if !reflect.DeepEqual(baseline, readProof()) {
				t.Fatal("rejected OIDC proof changed retained provenance")
			}
			// Make the next positive an actual transition on MySQL too; an
			// unchanged UPDATE may legitimately report zero affected rows.
			local := map[string]any{"primary_method": "", "oidc_binding_id": "", "oidc_binding_created_at": nil, "oidc_config_revision": "", "oidc_policy_revision": "", "oidc_user_created_at": nil}
			if err := db.Model(target.model).Where(target.query, target.args...).Updates(local).Error; err != nil {
				t.Fatal("restore local between proof controls", err)
			}
		}
		local := map[string]any{"primary_method": "", "oidc_binding_id": "", "oidc_binding_created_at": nil, "oidc_config_revision": "", "oidc_policy_revision": "", "oidc_user_created_at": nil}
		if err := db.Model(target.model).Where(target.query, target.args...).Updates(local).Error; err != nil {
			t.Fatal("restore retained local proof", err)
		}
		if got := readProof(); got != (oidcFixtureSessionV94{}) {
			t.Fatal("local proof not blank")
		}
	}
	current()
	// Successful repeated startup must preserve the actual retained binding and
	// singleton, not merely their counts or freshly synthesized timestamps.
	remove94()
	migrate()
	current()
	if !reflect.DeepEqual(bindingBaseline, readBinding()) {
		t.Fatal("V94 repeat changed exact binding subject/birth")
	}
	// Return the owned fixture to full current without changing any retained V1–V93 row.
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore current after bounded historical fixture", err)
	}
	finalLedger := personalKeyBehaviorLedger(t, db)
	if len(finalLedger) != 97 || finalLedger[96].Version != 97 || !reflect.DeepEqual(ledger[:93], finalLedger[:93]) || finalLedger[94].Version != 95 || finalLedger[95].Version != 96 {
		t.Fatal("historical closure lost current suffix or retained prefix")
	}

}
