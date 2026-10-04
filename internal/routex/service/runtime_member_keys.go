package service

import (
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// This private publication proof includes retained disabled records, unlike the
// active authentication map. It contains no bearer, digest or display prefix.
type runtimePersonalKeyState struct {
	UserID   string
	Revision string
	Status   string
}

func publishedMemberKeyDisabled(auth *runtimeAuthorization, key entity.APIKey, now time.Time) bool {
	if auth == nil || !now.Before(auth.ValidUntil) || !memberKeyRevision.MatchString(key.LifecycleRevision) || key.Status != entity.KeyDisabled {
		return false
	}
	proof, exists := auth.PersonalKeyStates[key.ID]
	return exists && proof.UserID == key.UserID && proof.Revision == key.LifecycleRevision && proof.Status == entity.KeyDisabled
}

func runtimeMemberKeyStates(keys []entity.APIKey) map[string]runtimePersonalKeyState {
	result := map[string]runtimePersonalKeyState{}
	counts := map[string]int{}
	for _, key := range keys {
		counts[strings.ToLower(key.ID)]++
	}
	for _, key := range keys {
		if counts[strings.ToLower(key.ID)] != 1 || !memberKeyID.MatchString(key.ID) || !memberKeyUserID.MatchString(key.UserID) || !memberKeyRevision.MatchString(key.LifecycleRevision) {
			continue
		}
		if key.Status != entity.KeyPending && key.Status != entity.KeyActive && key.Status != entity.KeyDisabled && key.Status != entity.KeyRevoked {
			continue
		}
		result[key.ID] = runtimePersonalKeyState{UserID: key.UserID, Revision: key.LifecycleRevision, Status: key.Status}
	}
	return result
}
