package service

import (
	"context"
	"slices"
	"time"
)

const memberModelsConfirmationReads = 8

// The transaction and publication have already finished. Each capture renews
// reader/target authority; this loop never repeats a mutation or publication.
func (s *Service) confirmMemberModels(ctx context.Context, userID string, desired []string, capture func(context.Context) (*MemberModelsWorkspace, error)) (*MemberModelsWriteResult, error) {
	delay := 25 * time.Millisecond
	for attempt := 0; attempt < memberModelsConfirmationReads; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !s.memberModelsConfirmationRuntimeLive() {
			return nil, runtimeUnavailable
		}
		current, err := capture(ctx)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if current == nil {
			return nil, runtimeUnavailable
		}
		ids := make([]string, 0, len(current.PersonalModels))
		for _, row := range current.PersonalModels {
			ids = append(ids, row.ID)
		}
		// A later writer or authority change cannot confirm this submitted set.
		if current.UserID != userID || !current.CanEdit || !slices.Equal(ids, desired) || !s.memberModelsConfirmationRuntimeLive() {
			return nil, runtimeUnavailable
		}
		if current.RuntimeApplied != nil {
			if !*current.RuntimeApplied || current.ApplicationStatus != "applied" {
				return nil, runtimeUnavailable
			}
			return &MemberModelsWriteResult{UserID: userID, ModelIDs: ids, ETag: current.ETag, RuntimeApplied: true, Confirmation: "current_model_grants"}, nil
		}
		if !current.runtimeBusy || current.ApplicationStatus != "unavailable" || attempt+1 == memberModelsConfirmationReads {
			return nil, runtimeUnavailable
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, 400*time.Millisecond)
	}
	return nil, runtimeUnavailable
}

func (s *Service) memberModelsConfirmationRuntimeLive() bool {
	if s.runtime == nil {
		return false
	}
	select {
	case <-s.runtime.done:
		return false
	default:
	}
	auth := s.runtime.auth.Load()
	return auth != nil && time.Now().Before(auth.ValidUntil)
}
