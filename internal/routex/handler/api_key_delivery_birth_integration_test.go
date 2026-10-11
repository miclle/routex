package handler

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type deliveryFixtureManagedBirthTarget struct {
	kind, owner, actor string
	authored           time.Time
	observed           atomic.Bool
}

// Install before any fixture Service starts. The immutable exact target stays
// armed until its assertion; additional matching creations must still fail.
func (f *deliveryFixture) installManagedBirthCallback() {
	f.t.Helper()
	const name = "test:delivery-managed-birth"
	if e := f.db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		target := f.managedBirth.Load()
		if target == nil {
			return
		}
		match := false
		switch key := tx.Statement.Dest.(type) {
		case *entity.APIKey:
			match = target.kind == "personal" && key.UserID == target.owner && key.DeliveryMode == "managed_vault" && key.Status == entity.KeyPending
		case *entity.ProjectKey:
			match = target.kind == "project" && key.ProjectID == target.owner && key.CreatorID == target.actor && key.DeliveryMode == "managed_vault" && key.Status == entity.KeyPending
		}
		if !match {
			return
		}
		if !target.observed.CompareAndSwap(false, true) {
			_ = tx.AddError(errors.New("unexpected additional managed creation"))
			return
		}
		tx.Statement.SetColumn("CreatedAt", target.authored)
	}); e != nil {
		f.t.Fatal(e)
	}
	// Registered before every Service cleanup, so registry removal follows joins.
	f.t.Cleanup(func() {
		f.managedBirth.Store(nil)
		if e := f.db.Callback().Create().Remove(name); e != nil {
			f.t.Error(e)
		}
	})
}

// Exercise real managed issuance with an authored sub-millisecond birth. The
// callback supplies no Key, claim, acknowledgement, identity or READY proof.
func deliveryFixtureManagedBirth(t *testing.T, f *deliveryFixture, kind, owner, actor string) func(string) {
	t.Helper()
	db := f.db
	authored := time.Now().UTC().Add(-time.Second).Truncate(time.Second).Add(123456 * time.Microsecond)
	target := &deliveryFixtureManagedBirthTarget{kind: kind, owner: owner, actor: actor, authored: authored}
	if !f.managedBirth.CompareAndSwap(nil, target) {
		t.Fatal("managed birth fixture is already armed")
	}
	t.Cleanup(func() { f.managedBirth.CompareAndSwap(target, nil) })
	return func(keyID string) {
		t.Helper()
		if !f.managedBirth.CompareAndSwap(target, nil) {
			t.Fatal("managed birth target changed before assertion")
		}
		if !target.observed.Load() || authored.Equal(authored.Truncate(time.Millisecond)) {
			t.Fatal("sub-millisecond managed birth was not exercised")
		}
		var birth time.Time
		if kind == "personal" {
			var key entity.APIKey
			if e := db.Take(&key, "id = ? AND user_id = ?", keyID, owner).Error; e != nil || key.ID != keyID || key.UserID != owner {
				t.Fatal("managed personal persisted identity changed")
			}
			birth = key.CreatedAt
		} else {
			var key entity.ProjectKey
			if e := db.Take(&key, "id = ? AND project_id = ?", keyID, owner).Error; e != nil || key.ID != keyID || key.ProjectID != owner || key.CreatorID != actor {
				t.Fatal("managed Project persisted identity changed")
			}
			birth = key.CreatedAt
		}
		// These are independent observations of the historical column contracts;
		// neither requires a particular rounding direction on the shorter column.
		if db.Name() == "mysql" && kind == "project" {
			if birth.Equal(authored) {
				t.Fatal("historical Project precision boundary was not exercised")
			}
		} else if !birth.Equal(authored) {
			t.Fatal("eligible full-precision birth changed")
		}
		var command entity.APIKeyDeliveryCommand
		if e := db.Where("key_id = ? AND owner_kind = ? AND owner_id = ?", keyID, kind, owner).Take(&command).Error; e != nil || command.KeyID != keyID || command.KeyBirth == nil || !command.KeyBirth.Equal(birth) || command.ClaimedAt == nil || command.AcknowledgedAt == nil || command.PublishedAt == nil || command.RevokedAt != nil {
			t.Fatal("managed command did not retain exact durable birth and genuine publication")
		}
		var binding entity.APIKeyVaultDeliveryBinding
		if e := db.Take(&binding, "id = ?", command.BindingID).Error; e != nil || binding.KeyID != keyID || binding.KeyKind != kind || !binding.KeyBirth.Equal(birth) || binding.AcknowledgedAt == nil || binding.Version != 1 {
			t.Fatal("managed binding did not retain exact durable birth and genuine acknowledgement")
		}
	}
}
