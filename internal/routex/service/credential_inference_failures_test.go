package service

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func credentialInferenceTestState(credentialID string, at time.Time) entity.CredentialInferenceFailureState {
	return entity.CredentialInferenceFailureState{CredentialDigest: credentialInferenceDigest(credentialID), CredentialID: credentialID, UpdatedAt: at}
}

func credentialInferenceTestAttempt(credentialID, status string, n int, at time.Time) entity.CallAttempt {
	return entity.CallAttempt{ID: fmt.Sprintf("att_observer_%d", n), CredentialID: credentialID, AttemptNumber: n,
		Status: status, StartedAt: at.Add(-time.Second), CompletedAt: at, WorkEvidence: "unknown", NativeCompletionEvidence: "unknown"}
}

func TestCredentialInferenceFailureThresholdAndSuccessRearm(t *testing.T) {
	acceptedAt := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	state := credentialInferenceTestState("crd_exact", acceptedAt)
	var first entity.CredentialInferenceFailureEpisode
	for i := 1; i <= 6; i++ {
		attempt := credentialInferenceTestAttempt(state.CredentialID, "error", i, acceptedAt.Add(-time.Minute))
		attempt.ErrorCode = "process_interrupted"
		episode, err := advanceCredentialInferenceFailure(&state, attempt, acceptedAt)
		if err != nil || (episode != nil) != (i == 5) {
			t.Fatal("recorded error threshold changed", i, episode, err)
		}
		if episode != nil {
			first = *episode
			if !validCredentialInferenceEpisode(first) || first.CredentialID != "crd_exact" || first.TriggerAttemptID != attempt.ID || first.AcceptanceSequence != 5 || first.FailureCount != 5 || !first.RecordedAt.Equal(acceptedAt) {
				t.Fatal("trigger lost exact bounded evidence", first)
			}
		}
	}
	if state.ConsecutiveFailures != 5 || !state.Latched || state.AcceptanceSequence != 6 {
		t.Fatal("additional errors changed the latch", state)
	}
	success := credentialInferenceTestAttempt(state.CredentialID, "success", 7, acceptedAt.Add(-time.Hour))
	if episode, err := advanceCredentialInferenceFailure(&state, success, acceptedAt); err != nil || episode != nil || state.Latched || state.ConsecutiveFailures != 0 {
		t.Fatal("late recorded success did not rearm in acceptance order", state, err)
	}
	for i := 8; i <= 12; i++ {
		episode, err := advanceCredentialInferenceFailure(&state, credentialInferenceTestAttempt(state.CredentialID, "error", i, acceptedAt.Add(-2*time.Hour)), acceptedAt)
		if err != nil || (episode != nil) != (i == 12) {
			t.Fatal("new accepted episode threshold changed", episode, err)
		}
		if episode != nil && (episode.LastSuccessAttemptID != success.ID || episode.AcceptanceSequence != 12 || episode.TriggerAttemptID == first.TriggerAttemptID) {
			t.Fatal("new episode lost success anchor", episode)
		}
	}
	if first.AcceptanceSequence != 5 || first.LastSuccessAttemptID != "" || first.TriggerAttemptID != "att_observer_5" {
		t.Fatal("rearming rewrote retained episode", first)
	}
}

func TestCredentialInferenceFailureUnknownAndCanceledBarriers(t *testing.T) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, barrier := range []string{"canceled", "unknown", "future", "zero_time", "reversed_time"} {
		for _, initiallyLatched := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/latched_%t", barrier, initiallyLatched), func(t *testing.T) {
				state := credentialInferenceTestState("crd_exact", at)
				prefix := 4
				if initiallyLatched {
					prefix = 5
				}
				for i := 1; i <= prefix; i++ {
					if _, err := advanceCredentialInferenceFailure(&state, credentialInferenceTestAttempt(state.CredentialID, "error", i, at), at); err != nil {
						t.Fatal(err)
					}
				}
				attempt := credentialInferenceTestAttempt(state.CredentialID, "error", prefix+1, at)
				switch barrier {
				case "canceled", "unknown":
					attempt.Status = barrier
				case "future":
					attempt.CompletedAt = at.Add(time.Second)
				case "zero_time":
					attempt.StartedAt = time.Time{}
				case "reversed_time":
					attempt.StartedAt = at.Add(time.Second)
				}
				if episode, err := advanceCredentialInferenceFailure(&state, attempt, at); err != nil || episode != nil || state.ConsecutiveFailures != 0 || state.Latched != initiallyLatched {
					t.Fatal("unknown/canceled outcome was inferred or rearmed", state, episode, err)
				}
				for i := prefix + 2; i <= prefix+6; i++ {
					episode, err := advanceCredentialInferenceFailure(&state, credentialInferenceTestAttempt(state.CredentialID, "error", i, at), at)
					if err != nil || (episode != nil) != (!initiallyLatched && i == prefix+6) {
						t.Fatal("barrier was bridged or rearmed", episode, err)
					}
				}
			})
		}
	}
}

func TestCredentialInferenceFailureExactIdentityAndInvalidState(t *testing.T) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if credentialInferenceDigest("crd_exact") == credentialInferenceDigest("CRD_EXACT") || credentialInferenceDigest("crd_exact") == credentialInferenceDigest("crd_exact ") {
		t.Fatal("Credential identities were normalized")
	}
	valid := credentialInferenceTestState("crd_exact", at)
	for _, invalid := range []string{"alias", "digest", "sequence", "overflow", "latch", "count", "last_attempt", "time"} {
		state := valid
		attempt := credentialInferenceTestAttempt(state.CredentialID, "error", 1, at)
		switch invalid {
		case "alias":
			attempt.CredentialID = strings.ToUpper(state.CredentialID)
		case "digest":
			state.CredentialDigest = strings.Repeat("0", 64)
		case "sequence":
			state.AcceptanceSequence = -1
		case "overflow":
			state.AcceptanceSequence, state.LastAttemptID = math.MaxInt64, "att_previous"
		case "latch":
			state.Latched = true
		case "count":
			state.ConsecutiveFailures = 6
		case "last_attempt":
			state.LastAttemptID = "att_previous"
		case "time":
			state.UpdatedAt = time.Time{}
		}
		before := state
		if episode, err := advanceCredentialInferenceFailure(&state, attempt, at); err == nil || episode != nil || !reflect.DeepEqual(before, state) {
			t.Fatal("invalid observation changed observer", invalid, episode, err)
		}
	}
}

func TestCredentialInferenceCurrentEpisodeProof(t *testing.T) {
	state := entity.CredentialInferenceFailureState{CredentialID: "CRD_exact", Latched: true, LastTriggerAttemptID: "att_new", LastSuccessAttemptID: "att_success"}
	episode := entity.CredentialInferenceFailureEpisode{CredentialID: "CRD_exact", TriggerAttemptID: "att_new", LastSuccessAttemptID: "att_success"}
	if !credentialInferenceEpisodeActive(state, episode) {
		t.Fatal("exact current episode proof rejected")
	}
	for _, mutate := range []func(*entity.CredentialInferenceFailureState){
		func(s *entity.CredentialInferenceFailureState) { s.Latched = false },
		func(s *entity.CredentialInferenceFailureState) { s.CredentialID = "crd_exact" },
		func(s *entity.CredentialInferenceFailureState) { s.LastTriggerAttemptID = "att_old" },
		func(s *entity.CredentialInferenceFailureState) { s.LastSuccessAttemptID = "att_other_success" },
	} {
		changed := state
		mutate(&changed)
		if credentialInferenceEpisodeActive(changed, episode) {
			t.Fatal("historical or recovered episode accepted as current", changed)
		}
	}
	body := notificationDeliveryBody(entity.NotificationDeliveryIntent{Kind: notificationKindCredentialInference, CredentialID: "CRD_exact"})
	if !strings.Contains(body, "Five consecutive inference failures were recorded") || !strings.Contains(body, "recorded success resolves") || !strings.Contains(body, "CRD_exact") {
		t.Fatal("historical delivery copy lost source facts or recovery meaning", body)
	}
}
