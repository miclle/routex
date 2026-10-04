package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

func (s *Service) resolveGatewayIdentityAttachments(ctx context.Context, result *GatewayResult, owner attachmentOwner, plan *gatewayAttachmentPlan) (map[string]gatewayAttachmentData, error) {
	if result.identity.team == nil {
		return s.resolveGatewayAttachments(ctx, owner, plan)
	}
	identity := result.identity.team
	owner = teamAttachmentOwner(identity)
	authorize := s.teamNativeAttachmentAuthorization(ctx, identity, result.ModelID)
	rows := make(map[string]entity.StorageObject, len(plan.Occurrences))
	// Validate the complete reference set before any storage I/O. A valid first
	// object cannot make an unauthorized later reference observable to storage.
	for _, objectID := range plan.UniqueObjectIDs() {
		row, err := s.scopedAttachment(ctx, owner, objectID, authorize)
		if err != nil {
			return nil, gatewayTeamAttachmentReadError(ctx, err)
		}
		rows[objectID] = row
	}
	if err := validateTeamAttachmentMetadata(plan, rows, time.Now().UTC()); err != nil {
		return nil, err
	}
	resolved := make(map[string]gatewayAttachmentData, len(rows))
	for _, objectID := range plan.UniqueObjectIDs() {
		row, data, err := s.readScopedAttachment(ctx, owner, objectID, authorize)
		if err != nil {
			return nil, gatewayTeamAttachmentReadError(ctx, err)
		}
		resolved[objectID] = gatewayAttachmentData{ObjectID: row.ID, Name: row.Name, MIME: row.MIME, Data: data}
	}
	if err := authorize(s.authDB(ctx)); err != nil {
		return nil, gatewayTeamAttachmentReadError(ctx, err)
	}
	return resolved, nil
}

func validateTeamAttachmentMetadata(plan *gatewayAttachmentPlan, rows map[string]entity.StorageObject, now time.Time) error {
	var total int64
	for _, id := range plan.UniqueObjectIDs() {
		row, exists := rows[id]
		if !exists || row.ID != id || row.OwnerKind != entity.StorageOwnerTeam || row.CreatorUserID == nil || row.CreatorMembershipID == nil || *row.CreatorUserID == "" || *row.CreatorMembershipID == "" || !attachmentReadableAt(row, now) {
			return gatewayError(404, "attachment_not_found", "The attachment is unavailable in this Team context.")
		}
		if row.Size <= 0 || row.Size > 2<<20 {
			return gatewayAttachmentLimitError()
		}
		total += row.Size
		if total > gatewayAttachmentMaxRawBytes {
			return gatewayAttachmentLimitError()
		}
	}
	for _, ref := range plan.Occurrences {
		if !gatewayAttachmentMIMEMatches(ref.Kind, rows[ref.ObjectID].MIME) {
			return unsupportedGatewayAttachmentReference()
		}
	}
	return nil
}

func gatewayTeamAttachmentReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return gatewayAttachmentContextError(ctx.Err())
	}
	if _, ok := err.(*GatewayError); ok {
		return err
	}
	return gatewayAttachmentReadError(err)
}

// Native invocation authority comes only from the captured published lease.
// Object metadata reads never delegate authority to Control Plane tables.
func (s *Service) teamNativeAttachmentAuthorization(ctx context.Context, identity *TeamSessionIdentity, modelID string) attachmentAuthorization {
	return func(_ *gorm.DB) error { return s.ReauthorizeTeamSession(ctx, identity, modelID) }
}
