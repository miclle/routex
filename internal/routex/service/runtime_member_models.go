package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type runtimePersonalGrantState struct {
	CreatedAt      time.Time
	Enabled        bool
	Revision, Hash string
}

func personalGrantHash(grants []entity.UserModelGrant) string {
	rows := slices.Clone(grants)
	slices.SortFunc(rows, func(a, b entity.UserModelGrant) int { return strings.Compare(a.ModelID, b.ModelID) })
	// Explicit projection: nil provenance and an empty set are part of the proof.
	type row struct {
		UserID, ModelID string
		CreatedAt       time.Time
		Source          *string
	}
	values := make([]row, 0, len(rows))
	for _, g := range rows {
		values = append(values, row{g.UserID, g.ModelID, g.CreatedAt.UTC(), g.SourceRequestID})
	}
	raw, _ := json.Marshal(values)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func runtimePersonalGrantStates(data *runtimeData) map[string]runtimePersonalGrantState {
	grouped := map[string][]entity.UserModelGrant{}
	for _, g := range data.Grants {
		grouped[g.UserID] = append(grouped[g.UserID], g)
	}
	result := map[string]runtimePersonalGrantState{}
	for _, u := range data.Users {
		result[u.ID] = runtimePersonalGrantState{u.CreatedAt, !u.Disabled && u.OffboardedAt == nil, u.PersonalGrantRevision, personalGrantHash(grouped[u.ID])}
	}
	return result
}
func (s *Service) memberModelsApplication(subject entity.User, grants []entity.UserModelGrant, auth *runtimeAuthorization) (string, *bool) {
	if subject.Disabled || subject.OffboardedAt != nil {
		value := false
		return "not_applied", &value
	}
	if s.runtime == nil || auth == nil || s.runtime.auth.Load() != auth || !time.Now().Before(auth.ValidUntil) {
		return "unavailable", nil
	}
	value := false
	proof, exists := auth.PersonalGrantStates[subject.ID]
	if exists && !subject.Disabled && subject.OffboardedAt == nil && proof.Enabled && !subject.CreatedAt.IsZero() && proof.CreatedAt.Equal(subject.CreatedAt) && personalModelETag(subject.PersonalGrantRevision) && proof.Revision == subject.PersonalGrantRevision && proof.Hash == personalGrantHash(grants) && !runtimeDenied(&s.runtime.deniedPersonalGrants, subject.ID) && !runtimeDenied(&s.runtime.deniedUsers, subject.ID) {
		value = true
	}
	if value {
		return "applied", &value
	}
	return "not_applied", &value
}

// Only Personal Keys consult this reduction fence. Team sessions and Project
// Keys retain their own authority and never borrow a User's Personal grants.
func (s *Service) personalPreparedGrantAllowed(auth *runtimeAuthorization, userID, keyID, modelID, revision string) bool {
	if runtimeDenied(&s.runtime.deniedPersonalGrants, userID) {
		return false
	}
	key, exists := auth.KeysByID[keyID]
	if !exists || !auth.PersonalGrantStates[userID].Enabled || key.ProjectID != "" || key.Key.UserID != userID || !slices.Contains(key.Models, modelID) {
		return false
	}
	return revision == "" || auth.PersonalGrantStates[userID].Revision == revision
}
