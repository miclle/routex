package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"slices"
	"strconv"
)

func rootRotationAuditProjection(row entity.AuditEvent) (rootRotationAudit, bool) {
	var record rootRotationAudit
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 64*1024 || row.ResourceType != "secret_rotation" || json.Unmarshal([]byte(*row.DetailsJSON), &record) != nil {
		return record, false
	}
	epoch, err := strconv.ParseUint(record.Epoch, 10, 64)
	if err != nil || epoch == 0 || strconv.FormatUint(epoch, 10) != record.Epoch || !credentialReplacementRequestID.MatchString(record.RequestID) || !rootRotationID.MatchString(record.RotationID) || record.RotationID != row.ResourceID || !rootKeyID.MatchString(record.KeyID) || !rootReason(record.Reason) || !slices.Contains([]string{"start", "resume", "retire", "rollback"}, record.Action) || row.Action != "secret_rotation."+record.Action || !slices.Contains([]string{"migrating", "blocked", "observing", "ready", "completed", "rolled_back"}, record.Status) {
		return record, false
	}
	return record, true
}
