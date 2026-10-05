package service

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type memberMetadataAudit struct {
	UserID     string `json:"user_id"`
	BeforeName string `json:"before_name"`
	AfterName  string `json:"after_name"`
	Reason     string `json:"reason"`
}

func appendMemberMetadataAudit(tx *gorm.DB, actorID string, before entity.User, input MemberMetadataInput) error {
	raw, err := json.Marshal(memberMetadataAudit{UserID: before.ID, BeforeName: before.Name, AfterName: input.Name, Reason: input.Reason})
	if err != nil {
		return err
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detail := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: "member.metadata.update", ResourceType: "user", ResourceID: before.ID, DetailsJSON: &detail}).Error
}
func memberMetadataAuditProjection(row entity.AuditEvent) (memberMetadataAudit, bool) {
	var result memberMetadataAudit
	if row.Action != "member.metadata.update" || row.ResourceType != "user" || !safeTeamSessionID(row.ResourceID) || row.DetailsJSON == nil || len(*row.DetailsJSON) > 64*1024 {
		return result, false
	}
	raw := []byte(*row.DetailsJSON)
	if !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return result, false
	}
	if _, err := decodeDefaultLimitObject(raw, []string{"user_id", "before_name", "after_name", "reason"}); err != nil {
		return result, false
	}
	if json.Unmarshal(raw, &result) != nil {
		return result, false
	}
	return result, result.UserID == row.ResourceID && utf8.ValidString(result.BeforeName) && utf8.RuneCountInString(result.BeforeName) <= 100 && result.BeforeName != result.AfterName && validCatalogLabel(result.AfterName) && validCredentialMetadataReason(result.Reason) && strings.TrimSpace(result.Reason) == result.Reason
}
