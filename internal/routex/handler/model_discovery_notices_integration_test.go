package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Runs at the end of the existing catalog lifecycle and retains its real HTTP,
// encryption, authorization and transaction machinery without another app birth.
func testModelDiscoveryNotices(t *testing.T, db *gorm.DB, svc *service.Service, router *fox.Engine, admin SessionResponse, cookie *http.Cookie, store *secretstore.Store) {
	t.Helper()
	var payload atomic.Value
	payload.Store(`{"data":[{"id":"already-manual"},{"id":"new-one"},{"id":"new-one"}]}`)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Error("discovery notice dispatched a native call")
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload.Load().(string)))
	}))
	defer remote.Close()
	request := systemStatusRequest(t, router, cookie, admin.CSRFToken)
	provider := decodeCatalogResponse[ProviderResponse](t, request("POST", "/api/v1/admin/providers", map[string]any{"name": "Discovery notice supplier", "connection_name": "Notice connection", "base_url": remote.URL + "/v1", "protocol": "openai_chat", "credential_name": "Notice credential", "secret": "notice-test-only"}), 201)
	connection := provider.Connections[0]
	credential := connection.Credentials[0]
	sourceRows := func() []entity.ModelDiscoveryNotice {
		t.Helper()
		var rows []entity.ModelDiscoveryNotice
		if err := db.Where("connection_id = ?", connection.ID).Order("recorded_at, audit_id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	decodeCatalogResponse[ProviderModelResponse](t, request("POST", "/api/v1/admin/connections/"+connection.ID+"/models", map[string]string{"upstream_name": "already-manual"}), 201)
	if len(sourceRows()) != 0 {
		t.Fatal("manual Model creation invented discovery")
	}
	var rejectSource, rejectProjection atomic.Bool
	const fault = "test:model-discovery-transaction"
	if err := db.Callback().Create().Before("gorm:create").Register(fault, func(tx *gorm.DB) {
		if tx.Statement.Table == "model_discovery_notices" && rejectSource.Load() {
			_ = tx.AddError(errors.New("controlled discovery source failure"))
		}
		if row, ok := tx.Statement.Dest.(*entity.OperationalAlertOccurrence); ok && row.SourceType == "model_discovery" && rejectProjection.Load() {
			_ = tx.AddError(errors.New("controlled projection failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(fault); err != nil {
			t.Error(err)
		}
	}()
	verify := func() *httptest.ResponseRecorder {
		return request("POST", "/api/v1/admin/credentials/"+credential.ID+"/verify", map[string]any{})
	}
	rejectSource.Store(true)
	expectStatus(t, verify(), 500)
	if len(sourceRows()) != 0 {
		t.Fatal("rolled-back verification left a source")
	}
	var models int64
	if err := db.Model(&entity.ProviderModel{}).Where("connection_id = ?", connection.ID).Count(&models).Error; err != nil || models != 1 {
		t.Fatal("source failure failed to roll back new Model", err)
	}
	rejectSource.Store(false)
	rejectProjection.Store(true)
	first := decodeCatalogResponse[VerifyCredentialResponse](t, verify(), 200)
	if !first.Verified || first.DiscoveredModels != 2 {
		t.Fatal("real first discovery failed")
	}
	sources := sourceRows()
	if len(sources) != 1 || sources[0].ModelCount != 1 || sources[0].ProviderID != provider.ID || sources[0].ConnectionID != connection.ID {
		t.Fatal("first-recorded count or exact scope is wrong", sources)
	}
	var occurrences int64
	if err := db.Model(&entity.OperationalAlertOccurrence{}).Where("source_type = ? AND source_id = ?", "model_discovery", sources[0].AuditID).Count(&occurrences).Error; err != nil || occurrences != 0 {
		t.Fatal("projection failure was not rolled back", err)
	}
	rejectProjection.Store(false)
	// A fresh service reads the same durable source and original Session; recovery
	// must not require another upstream discovery or an invented current catalog.
	reopened, err := service.New(context.Background(), db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	recoveredRouter := fox.New()
	New(reopened).RegisterRoutes(recoveredRouter)
	recovered := systemStatusRequest(t, recoveredRouter, cookie, admin.CSRFToken)
	expectStatus(t, recovered("GET", "/api/v1/admin/alerts", nil), 200)
	var alert entity.OperationalAlert
	if err := db.Where("group_key = ?", "model_discovery:"+connection.ID).Take(&alert).Error; err != nil || alert.FirstRecordedModelCount != 1 || alert.OccurrenceCount != 1 || alert.Severity != "low" {
		t.Fatal("durable recovery changed source facts", err, alert)
	}
	var notice entity.Notification
	if err := db.Where("alert_id = ? AND recipient_id = ?", alert.ID, admin.User.ID).Take(&notice).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, recovered("POST", "/api/v1/notifications/"+notice.ID+"/read", nil), 200)
	for range 2 {
		if !decodeCatalogResponse[VerifyCredentialResponse](t, verify(), 200).Verified {
			t.Fatal("repeat verification failed")
		}
	}
	if !reflect.DeepEqual(sources, sourceRows()) {
		t.Fatal("repeat discovery rewrote retained source")
	}
	var retained entity.Notification
	if err := db.Where("id = ?", notice.ID).Take(&retained).Error; err != nil || !retained.Read {
		t.Fatal("repeat discovery reset read state", err)
	}
	payload.Store(`{"data":[{"id":"already-manual"},{"id":"new-one"},{"id":"new-two"},{"id":"new-three"}]}`)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Go(func() { results <- verify().Code })
	}
	wg.Wait()
	close(results)
	for status := range results {
		if status != 200 {
			t.Fatal("concurrent discovery failed", status)
		}
	}
	sources = sourceRows()
	if len(sources) != 2 || sources[1].ModelCount != 2 {
		t.Fatal("concurrent discovery duplicated first-recorded facts", sources)
	}
	alert = entity.OperationalAlert{}
	if err := db.Where("group_key = ?", "model_discovery:"+connection.ID).Take(&alert).Error; err != nil || alert.OccurrenceCount != 2 || alert.FirstRecordedModelCount != 2 {
		t.Fatal("latest count confused with occurrence count", alert, err)
	}
	page := decodeCatalogResponse[service.NotificationPage](t, recovered("GET", "/api/v1/notifications?severity=low", nil), 200)
	found := false
	for _, item := range page.Items {
		if item.AlertID == alert.ID {
			found = true
			if item.FirstRecordedModelCount != 2 || item.DeliveryStatus != "" || item.Severity != "low" || item.Read {
				t.Fatal("invalid discovery inbox envelope", item)
			}
		}
	}
	if !found {
		t.Fatal("low-severity discovery missing from inbox")
	}
	var mail int64
	if err := db.Model(&entity.NotificationDeliveryIntent{}).Where("kind = ?", "models_first_recorded").Count(&mail).Error; err != nil || mail != 0 {
		t.Fatal("low discovery source created email", err)
	}
	// Malformed successful HTTP discovery is a failure, not a partial source.
	payload.Store(`{"data":[{"id":"not-committed"},{}]}`)
	if decodeCatalogResponse[VerifyCredentialResponse](t, verify(), 200).Verified {
		t.Fatal("partial discovery accepted")
	}
	if !reflect.DeepEqual(sources, sourceRows()) {
		t.Fatal("failed partial discovery added source facts")
	}
	if err := db.Model(&entity.ProviderModel{}).Where("connection_id = ? AND upstream_name = ?", connection.ID, "not-committed").Count(&models).Error; err != nil || models != 0 {
		t.Fatal("partial discovery created Model", err)
	}
	_, outsiderCookie, outsiderCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "discovery-outsider", nil)
	outsider := systemStatusRequest(t, router, outsiderCookie, outsiderCSRF)
	outsiderPage := decodeCatalogResponse[service.NotificationPage](t, outsider("GET", "/api/v1/notifications", nil), 200)
	for _, item := range outsiderPage.Items {
		if item.Kind == "models_first_recorded" {
			t.Fatal("nonoperator received operational discovery")
		}
	}
	expectStatus(t, outsider("POST", "/api/v1/notifications/"+notice.ID+"/read", nil), 404)
	reader, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "discovery-reader", []string{"system.read"})
	readerRequest := systemStatusRequest(t, router, readerCookie, readerCSRF)
	expectStatus(t, readerRequest("GET", "/api/v1/admin/alerts?severity=low", nil), 200)
	expectStatus(t, readerRequest("PATCH", "/api/v1/admin/alerts/"+alert.ID, map[string]any{"state": "resolved", "etag": alert.ETag}), 403)
	// Granting a new reader and replaying does not backfill historical recipients.
	var newInbox int64
	if err := db.Model(&entity.Notification{}).Where("alert_id = ? AND recipient_id = ?", alert.ID, reader.User.ID).Count(&newInbox).Error; err != nil || newInbox != 0 {
		t.Fatal("replay added historical recipient", err)
	}
	payload.Store(`{"data":[{"id":"already-manual"},{"id":"new-one"},{"id":"new-two"},{"id":"new-three"},{"id":"new-four"}]}`)
	if !decodeCatalogResponse[VerifyCredentialResponse](t, verify(), 200).Verified {
		t.Fatal("reader positive discovery failed")
	}
	var readerNotice entity.Notification
	if err := db.Where("alert_id = ? AND recipient_id = ?", alert.ID, reader.User.ID).Take(&readerNotice).Error; err != nil {
		t.Fatal("eligible reader did not receive new occurrence", err)
	}
	expectStatus(t, readerRequest("POST", "/api/v1/notifications/"+readerNotice.ID+"/read", nil), 200)
	if _, err := svc.SetMemberRoles(context.Background(), admin.User.ID, reader.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	denied := decodeCatalogResponse[service.NotificationPage](t, readerRequest("GET", "/api/v1/notifications?severity=low", nil), 200)
	if len(denied.Items) != 0 {
		t.Fatal("revoked system.read retained discovery inbox")
	}
	expectStatus(t, readerRequest("POST", "/api/v1/notifications/"+readerNotice.ID+"/read", nil), 404)
	var originalRead entity.Notification
	if err := db.Where("id = ?", readerNotice.ID).Take(&originalRead).Error; err != nil || !originalRead.Read {
		t.Fatal("permission withdrawal rewrote read state", err)
	}
	ledger := personalKeyBehaviorLedger(t, db)
	if len(ledger) != 112 || ledger[110].Version != 111 || ledger[109].Version != 110 {
		t.Fatal("retained discovery peel control requires exact current V109 head")
	}
	peelErr := modelDiscoveryPeelV108(db, ledger)
	if peelErr == nil || peelErr.Error() != "retained discovery source prevents peel" || !reflect.DeepEqual(ledger, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("retained discovery did not reject peel without changing receipts", peelErr)
	}
	modelDiscoveryEnvelopeControls(t, db, sources[0], alert, notice, admin.User.ID)
	raw, _ := json.Marshal(page)
	for _, forbidden := range []string{"notice-test-only", "already-manual", "new-one", "new-two", "audit_id", "connection_id", "credential_id"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("discovery inbox exposed private/source payload", forbidden)
		}
	}
}

func modelDiscoveryEnvelopeControls(t *testing.T, db *gorm.DB, source entity.ModelDiscoveryNotice, alert entity.OperationalAlert, notice entity.Notification, recipientID string) {
	t.Helper()
	rollback := errors.New("control rollback")
	// Pair FK and primary-key rejection with genuine successful inserts/deletes,
	// keeping the same source facts so an unrelated CHECK cannot satisfy them.
	var audit entity.AuditEvent
	if err := db.Where("id = ?", source.AuditID).Take(&audit).Error; err != nil {
		t.Fatal("discovery source audit control", err)
	}
	for _, control := range []string{"valid source", "missing audit", "duplicate source", "referenced audit", "unreferenced audit"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			candidate := source
			candidate.AuditID = "aud_01j00000000000000000000000"
			if control == "valid source" || control == "unreferenced audit" {
				copy := audit
				copy.ID = candidate.AuditID
				if err := tx.Create(&copy).Error; err != nil {
					return err
				}
			}
			var result error
			switch control {
			case "valid source":
				result = tx.Create(&candidate).Error
			case "missing audit":
				result = tx.Create(&candidate).Error
				if !errors.Is(result, gorm.ErrForeignKeyViolated) {
					return errors.New("missing discovery audit FK accepted or failed for another reason")
				}
				return rollback
			case "duplicate source":
				result = tx.Create(&source).Error
				if !errors.Is(result, gorm.ErrDuplicatedKey) {
					return errors.New("duplicate discovery source accepted or failed for another reason")
				}
				return rollback
			case "referenced audit":
				result = tx.Where("id = ?", source.AuditID).Delete(&entity.AuditEvent{}).Error
				if !errors.Is(result, gorm.ErrForeignKeyViolated) {
					return errors.New("discovery audit deletion accepted or failed for another reason")
				}
				return rollback
			case "unreferenced audit":
				deleted := tx.Where("id = ?", candidate.AuditID).Delete(&entity.AuditEvent{})
				if deleted.RowsAffected != 1 {
					return errors.New("unreferenced discovery audit control was not deleted")
				}
				result = deleted.Error
			}
			if result != nil {
				return result
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("discovery provenance constraint control", control, err)
		}
	}
	for _, target := range []struct {
		model   any
		id      string
		changes []map[string]any
	}{
		{&entity.ModelDiscoveryNotice{}, source.AuditID, []map[string]any{nil, {"model_count": 0}, {"model_count": 2001}, {"provider_id": ""}, {"connection_id": ""}, {"recorded_at": nil}}},
		{&entity.OperationalAlert{}, alert.ID, []map[string]any{nil, {"kind": "MODELS_FIRST_RECORDED"}, {"kind": "models_first_recorded "}, {"severity": "LOW"}, {"severity": "medium"}, {"detail_code": "First_recorded"}, {"first_recorded_model_count": 0}, {"first_recorded_model_count": 2001}, {"subject_type": "model"}, {"subject_id": ""}}},
		{&entity.OperationalAlertOccurrence{}, notice.LatestOccurrenceID, []map[string]any{nil, {"source_type": "MODEL_DISCOVERY"}, {"source_type": "model_discovery "}, {"detail_code": "First_recorded"}, {"first_recorded_model_count": 0}, {"first_recorded_model_count": 2001}, {"subject_type": "model"}, {"source_id": ""}}},
		{&entity.Notification{}, notice.ID, []map[string]any{nil, {"severity": "medium"}, {"kind": "MODELS_FIRST_RECORDED"}, {"detail_code": "other"}, {"first_recorded_model_count": 0}, {"first_recorded_model_count": 2001}, {"subject_type": "model"}}},
	} {
		for _, change := range target.changes {
			err := db.Transaction(func(tx *gorm.DB) error {
				key := "id"
				if _, ok := target.model.(*entity.ModelDiscoveryNotice); ok {
					key = "audit_id"
				}
				values := change
				if change == nil {
					column := "first_recorded_model_count"
					if key == "audit_id" {
						column = "model_count"
					}
					var stored []int
					if err := tx.Model(target.model).Where(key+" = ?", target.id).Pluck(column, &stored).Error; err != nil {
						return err
					}
					if len(stored) != 1 || stored[0] < 1 || stored[0] > 2000 {
						return errors.New("valid discovery CHECK control baseline missing or invalid")
					}
					// Change the stored value so MySQL's changed-row count proves the
					// positive CHECK control reached this exact valid row.
					count := 1
					if stored[0] == count {
						count = 2
					}
					values = map[string]any{column: count}
				}
				result := tx.Model(target.model).Where(key+" = ?", target.id).Updates(values)
				if change == nil && (result.Error != nil || result.RowsAffected != 1) {
					return errors.New("valid discovery CHECK control failed")
				}
				if change != nil && result.Error == nil {
					return errors.New("invalid discovery envelope accepted")
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(target.model, change, err)
			}
		}
	}
	// The valid medium-severity intent control shares all references with the
	// prohibited new-kind case, isolating the no-discovery-mail CHECK.
	for _, kind := range []string{"system_job_failure", "models_first_recorded"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			at := time.Now().UTC()
			intent := entity.NotificationDeliveryIntent{ID: "ndl_discovery_control", OccurrenceID: notice.LatestOccurrenceID, RecipientID: recipientID, RecipientEmail: "control@example.invalid", Kind: kind, Severity: "medium", DetailCode: "publication_failed", SMTPETag: "rev_control", Status: "pending", NextAttemptAt: at, CreatedAt: at, UpdatedAt: at}
			e := tx.Create(&intent).Error
			if kind == "system_job_failure" && e != nil {
				return e
			}
			if kind == "models_first_recorded" && e == nil {
				return errors.New("discovery mail CHECK missing")
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("mail constraint control", kind, err)
		}
	}
}
