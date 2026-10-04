package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"strings"
	"testing"
	"time"
)

func TestRootSecretObservationRequiresContinuousExactGeneration(t *testing.T) {
	start := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	last := start.Add(300 * time.Second)
	now := last
	svc := &Service{rootNow: func() time.Time { return now }}
	job := entity.SecretRotationJob{Status: "ready", ObservationStartedAt: &start, ObservationLastConfirmedAt: &last, VerifiedProcessID: "ins_current", VerifiedSnapshotID: "cfg_current"}
	proof := entity.SecretProcessVerification{ProcessID: "ins_current", RuntimeSnapshotID: "cfg_current"}
	if !svc.rootObservationEligible(job, proof) {
		t.Fatal("continuous300s not eligible")
	}
	for _, mode := range []string{"early", "gap", "new_process", "new_snapshot", "future"} {
		t.Run(mode, func(t *testing.T) {
			copy := job
			p := proof
			now = last
			switch mode {
			case "early":
				now = start.Add(299 * time.Second)
			case "gap":
				now = last.Add(16 * time.Second)
			case "new_process":
				p.ProcessID = "ins_new"
			case "new_snapshot":
				p.RuntimeSnapshotID = "cfg_new"
			case "future":
				at := now.Add(time.Second)
				copy.ObservationLastConfirmedAt = &at
			}
			if svc.rootObservationEligible(copy, p) {
				t.Fatal("authority or continuity fabricated", mode)
			}
		})
	}
}
func TestRootSecretInputBoundsAndReviewIdentity(t *testing.T) {
	uuid := "11111111-1111-4111-8111-111111111111"
	etag := strings.Repeat("a", 64)
	if !rootInput(uuid, etag, "Reviewed rotation") || !rootReason(strings.Repeat("界", 1000)) {
		t.Fatal("valid bounded intent rejected")
	}
	for _, reason := range []string{"", " trailing", "trailing ", "line\nbreak", string([]byte{0xff}), strings.Repeat("界", 1001)} {
		if rootInput(uuid, etag, reason) {
			t.Fatal("invalid reason accepted")
		}
	}
	if rootInput("11111111-1111-1111-8111-111111111111", etag, "reason") {
		t.Fatal("UUID notv4 accepted")
	}
	p := entity.SecretWritePolicy{ETag: etag}
	if rootReviewETag("usr_a", p, nil) == rootReviewETag("usr_A", p, nil) {
		t.Fatal("actor aliases share review")
	}
}
func TestRootSecretJobProgressHasNoInventedDenominator(t *testing.T) {
	job := entity.SecretRotationJob{ID: "job", Status: "blocked", Phase: "migration", CountsJSON: `{"storage_revisions":{"Scanned":9007199254740993,"Rewrapped":1}}`, BlockerCode: "ciphertext_invalid"}
	view := rootRotationView(job, true, false)
	if len(view.Domains) != 5 || view.Domains[3].Scanned != "9007199254740993" || len(view.AllowedActions) != 2 {
		t.Fatal("exact retained history lost")
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"percentage", "ciphertext\"", "lease_token", "ProofCiphertext"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private progress escaped")
		}
	}
	job.Status = "completed"
	if len(rootRotationView(job, true, true).AllowedActions) != 0 {
		t.Fatal("terminal job can replay")
	}
}
func TestRootSecretAllHistoricalDomainsAndReferenceIdentity(t *testing.T) {
	if len(rootDomains) != 5 {
		t.Fatal("partial-domain rotation")
	}
	for _, domain := range rootDomains {
		spec, err := rootSpec(domain)
		if err != nil || spec.ciphertext == "" {
			t.Fatal("missing actual envelope domain")
		}
		reference := rootReference(domain, "subject", "generation")
		if reference == "" {
			t.Fatal("missing exact reference")
		}
	}
	if rootReference("smtp_settings", "1", "generation") != "smtp:1:generation" {
		t.Fatal("SMTP immutable reference changed")
	}
	if rootSafeIdentity("id_alias ", 64) || rootSafeIdentity("id:foreign", 64) {
		t.Fatal("ambiguous reference accepted")
	}
}

func TestRootSecretTypedAuditNeverExposesArbitraryHistory(t *testing.T) {
	record := rootRotationAudit{RequestID: "11111111-1111-4111-8111-111111111111", RotationID: "srt_01aaaaaaaaaaaaaaaaaaaaaaaa", Action: "start", KeyID: "new", Epoch: "2", Reason: "Reviewed target", Status: "migrating"}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	value := string(raw)
	row := entity.AuditEvent{Action: "secret_rotation.start", ResourceType: "secret_rotation", ResourceID: record.RotationID, DetailsJSON: &value}
	if _, ok := rootRotationAuditProjection(row); !ok {
		t.Fatal("typed audit rejected")
	}
	value = value[:len(value)-1] + `,"ciphertext":"must_not_render","private":{"key":"must_not_render"}}`
	projection, ok := rootRotationAuditProjection(row)
	if !ok {
		t.Fatal("safe projection rejected unknown fields")
	}
	encoded, err := json.Marshal(projection)
	if err != nil || strings.Contains(string(encoded), "must_not_render") {
		t.Fatal("arbitrary history rendered")
	}
	for _, mode := range []string{"target", "resource", "action", "epoch", "reason"} {
		copy := row
		change := record
		switch mode {
		case "target":
			copy.ResourceID = "srt_other"
		case "resource":
			copy.ResourceType = "user"
		case "action":
			copy.Action = "secret_rotation.retire"
		case "epoch":
			change.Epoch = "02"
		case "reason":
			change.Reason = "line\nbreak"
		}
		body, e := json.Marshal(change)
		if e != nil {
			t.Fatal(e)
		}
		text := string(body)
		copy.DetailsJSON = &text
		if _, ok := rootRotationAuditProjection(copy); ok {
			t.Fatal("unrecorded authority projected", mode)
		}
	}
}
