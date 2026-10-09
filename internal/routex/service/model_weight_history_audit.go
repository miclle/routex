package service

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/miclle/routex/internal/routex/entity"
)

// Complete versions carry the retained weight sets; the audit safely references
// their IDs/digests instead of multiplying 1,000 rows beyond the audit budget.
func modelWeightAuditProjection(row entity.AuditEvent) (modelWeightAudit, bool) {
	var result modelWeightAudit
	if row.ResourceType != "model" || !validAdminModelTarget(row.ResourceID) || row.DetailsJSON == nil || len(*row.DetailsJSON) > 8*1024 {
		return result, false
	}
	dec := json.NewDecoder(bytes.NewBufferString(*row.DetailsJSON))
	dec.DisallowUnknownFields()
	if dec.Decode(&result) != nil || dec.Decode(new(any)) != io.EOF || !validMemberRoleDigest(result.BeforeDigest) || !validMemberRoleDigest(result.AfterDigest) || result.BindingCount < 0 || result.BindingCount > 1000 {
		return result, false
	}
	for _, v := range []*string{result.SourceVersionID, result.SavedVersionID, result.TargetVersionID} {
		if v != nil && !modelWeightID(*v, "mwv") {
			return result, false
		}
	}
	if result.Effect != "changed" && result.Effect != "noop" || result.Effect == "changed" && (result.SavedVersionID == nil || result.SourceVersionID == nil || result.BeforeDigest == result.AfterDigest) || result.Effect == "noop" && result.BeforeDigest != result.AfterDigest {
		return result, false
	}
	switch row.Action {
	case "model.weights.update":
		if result.RequestID != nil || result.TargetVersionID != nil || result.Reason != nil || result.Effect != "changed" {
			return result, false
		}
	case "model.weights.rollback":
		if result.RequestID == nil || !credentialReplacementRequestID.MatchString(*result.RequestID) || result.TargetVersionID == nil || result.Reason == nil || !validRoleDefinitionReason(*result.Reason) {
			return result, false
		}
	default:
		return result, false
	}
	return result, true
}
