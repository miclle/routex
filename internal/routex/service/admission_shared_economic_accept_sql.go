package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	"reflect"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/routeattempt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Live request inputs never come from a recovered spool. In particular, the IP
// and retained source holders belong to this process and are not durable proof.
// Keeping this separate lets historical settlement work after revocation.
type sharedEconomicLiveAuthority struct {
	mu       sync.Mutex
	accepted *sharedAdmissionKey
	service  *Service
	plan     *gatewayAttemptPlan
	result   *GatewayResult
	clientIP netip.Addr
	owner    sharedAdmissionOwner
	original sharedAdmissionRequest
}

func (live *sharedEconomicLiveAuthority) confirmAdmission(key sharedAdmissionKey) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.accepted != nil && *live.accepted != key {
		return errSharedAdmissionConflict
	}
	copy := key
	live.accepted = &copy
	return nil
}

func (live *sharedEconomicLiveAuthority) acceptedHere(key sharedAdmissionKey) bool {
	live.mu.Lock()
	defer live.mu.Unlock()
	return live.accepted != nil && *live.accepted == key
}

func newSharedEconomicLiveAuthority(s *Service, plan *gatewayAttemptPlan, result *GatewayResult, clientIP netip.Addr, owner sharedAdmissionOwner, request sharedAdmissionRequest, data *runtimeQuotaData, payload map[string]json.RawMessage, options ...gatewayNativeOptions) (*sharedEconomicLiveAuthority, error) {
	if s == nil || s.runtime == nil || plan == nil || result == nil || !clientIP.IsValid() || clientIP.Zone() != "" || clientIP != clientIP.Unmap() ||
		!admissionOwnerIdentityValid(owner, request.OwnerStartedAt) || request.Reservation == nil ||
		request.Reservation.Model.ID != result.ModelID || request.Reservation.Protocol != result.NativeProtocol() ||
		plan.modelID != result.ModelID || plan.protocol != result.NativeProtocol() || plan.snapshotID != request.Reservation.SnapshotID ||
		!reflect.DeepEqual(result.quotaBound, request.Bound) {
		return nil, errSharedAdmissionInvalid
	}
	if _, err := gatewayReservationSQLSelection(request.Reservation, nil); err != nil {
		return nil, err
	}
	// Bind the supplied durable manifest to the actual classified request and
	// complete prepared plan, not merely to its self-consistent SQL representation.
	captured, err := captureGatewayReservationManifest(request.RequestID, plan, result, data, payload, options...)
	if err != nil || !reflect.DeepEqual(captured, request.Reservation) {
		return nil, errSharedAdmissionConflict
	}
	initial, err := sharedCallPayload(request.RequestID, request.Fallback)
	if err != nil || !request.Authorization.matchesCall(initial) || initial.UserID != result.UserID || initial.ProjectID != result.ProjectID ||
		initial.TeamID != result.TeamID || initial.TeamMembershipID != result.TeamMembershipID || initial.KeyID != result.KeyID ||
		initial.ModelID != result.ModelID || initial.Protocol != result.NativeProtocol() || initial.ModelName != result.ModelName ||
		plan.userID != result.UserID || plan.projectID != result.ProjectID || plan.keyID != result.KeyID {
		return nil, errSharedAdmissionConflict
	}
	original, err := sharedCaptured(request)
	if err != nil {
		return nil, err
	}
	live := &sharedEconomicLiveAuthority{service: s, plan: plan, result: result, clientIP: clientIP, owner: owner, original: original}
	return live, nil
}

func (live *sharedEconomicLiveAuthority) check(ctx context.Context, intent sharedAdmissionIntent, selected *routeattempt.Attempt) error {
	if live == nil || live.service == nil || live.plan == nil || live.result == nil || intent.Owner != live.owner ||
		!reflect.DeepEqual(intent.sharedAdmissionRequest, live.original) || !live.clientIP.IsValid() || live.clientIP.Zone() != "" || live.clientIP != live.clientIP.Unmap() {
		return errSharedDispatchForbidden
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := live.service.reauthorizeGatewayPublicName(ctx, live.result); err != nil {
		return err
	}
	candidates, err := gatewayReservationSQLSelection(intent.Reservation, selected)
	if err != nil {
		return err
	}
	available := false
	for _, candidate := range candidates {
		attempt := routeattempt.Attempt{Number: 1, TargetID: candidate.Binding.ID, ConnectionID: candidate.Connection.ID,
			CredentialID: candidate.Credential.Identity.ID, Protocol: intent.Reservation.Protocol}
		if selected != nil {
			attempt.Number = selected.Number
		}
		route, _, found := live.plan.Candidate(attempt)
		if !found || route.ProviderModelID != candidate.ProviderModel.ID || route.SnapshotID != intent.Reservation.SnapshotID ||
			!route.CredentialBirth.Equal(candidate.Credential.Identity.Birth) || !route.BindingBirth.Equal(candidate.Binding.Birth) ||
			gatewayReservationTransportIdentity(*route) != candidate.TransportIdentity || route.EgressRevision != candidate.EgressRevision {
			return errSharedAdmissionConflict
		}
		eligible, err := live.service.gatewayAttemptEligible(ctx, live.plan, attempt)
		if err != nil {
			return err
		}
		available = available || eligible
	}
	if !available {
		return errSharedDispatchForbidden
	}
	return ctx.Err()
}

// Keep process-local egress/limit writer order outside the SQL transaction.
// SQL writer locks then intersect revocation, policy, route and owner changes.
func (s *sharedEconomicSQLStore) lockLive() (func(), error) {
	if s == nil || s.db == nil || s.live == nil || s.live.service == nil {
		return nil, errSharedDispatchForbidden
	}
	svc := s.live.service
	svc.egressMu.RLock()
	svc.limitMu.RLock()
	return func() { svc.limitMu.RUnlock(); svc.egressMu.RUnlock() }, nil
}

func (s *sharedEconomicSQLStore) validateCurrentSQL(tx *gorm.DB, intent sharedAdmissionIntent, selected *routeattempt.Attempt, now time.Time) error {
	if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
		return err
	}
	if err := (&admissionOwnerStore{db: s.db}).CheckCurrentOwner(sharedAuthorizationDB(tx), intent.Owner, intent.OwnerStartedAt); err != nil {
		return err
	}
	if err := validateSharedAuthorizationSQL(tx, intent.Authorization, now); err != nil {
		return err
	}
	if err := validateSharedAdmissionPoliciesSQL(tx, intent.Authorization, intent.Policies, intent.CalendarRevision, intent.TimeZone, s.live.clientIP, now); err != nil {
		return err
	}
	if err := requireSharedEconomicCalendarSQL(tx, intent.CalendarRevision, intent.TimeZone); err != nil {
		return err
	}
	if selected == nil {
		return validateGatewayReservationAcceptSQL(tx, intent.Reservation)
	}
	return validateGatewayReservationDispatchSQL(tx, intent.Reservation, *selected)
}

func (s *sharedEconomicSQLStore) Accept(ctx context.Context, intent sharedAdmissionIntent) (*sharedAuthorityReceipt, error) {
	return s.acceptSharedEconomicSQL(ctx, intent, time.Now().UTC())
}

func (s *sharedEconomicSQLStore) acceptSharedEconomicSQL(ctx context.Context, intent sharedAdmissionIntent, now time.Time) (*sharedAuthorityReceipt, error) {
	if ctx == nil || validateSharedSpoolAdmission(intent) != nil || now.UnixNano() <= 0 ||
		!admissionOwnerIdentityValid(intent.Owner, intent.OwnerStartedAt) {
		return nil, errSharedAdmissionInvalid
	}
	body, err := sharedEconomicCanonicalBody(intent)
	if err != nil {
		return nil, err
	}
	release, err := s.lockLive()
	if err != nil {
		return nil, err
	}
	defer release()
	if err := s.live.check(ctx, intent, nil); err != nil {
		return nil, err
	}
	var result *sharedAuthorityReceipt
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.validateCurrentSQL(tx, intent, nil, now); err != nil {
			return err
		}
		domain, err := lockSharedEconomicDomain(tx, intent.Owner.InstallationID)
		if err != nil {
			return err
		}
		prior, local, err := readSharedEconomicHistorySQL(tx, sharedIntentKey(intent))
		if err == nil {
			if !bytes.Equal(prior.IntentBody, body) || !prior.OwnerStartedAt.Equal(intent.OwnerStartedAt) {
				return errSharedAdmissionConflict
			}
			result = sharedEconomicAuthorityView(local)
			return s.live.check(ctx, intent, nil)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if domain.ReceiptCount >= domain.ReceiptCapacity {
			return eventqueue.ErrFull
		}
		instant := max(now.UnixNano(), domain.ClockHighWater)
		if instant > math.MaxInt64-int64(7*24*time.Hour) || instant < intent.OwnerStartedAt.UnixNano() || instant < intent.Authorization.EvaluatedAt.UnixNano() {
			return errSharedAdmissionConflict
		}
		start, end, err := eventqueue.QuotaMonthBounds(instant, intent.TimeZone)
		if err != nil {
			return err
		}
		accounts, err := lockSharedEconomicAccountsSQL(tx, domain.InstallationID, intent.Policies, instant, true)
		if err != nil {
			return err
		}
		for i := range accounts {
			if err := expireSharedEconomicProjectionsSQL(tx, &accounts[i], instant); err != nil {
				return err
			}
		}
		if err := checkSharedEconomicAdmissionSQL(tx, domain, accounts, intent.Policies, intent.Bound, instant, start); err != nil {
			return err
		}
		receiptID, err := id.NewPrefixed("adm")
		if err != nil {
			return err
		}
		row := entity.AdmissionEconomicReceipt{InstallationID: domain.InstallationID, RequestDigest: sharedEconomicIdentityDigest(intent.RequestID), RequestID: intent.RequestID,
			ExecutionID: intent.ExecutionID, ExecutionDigest: sharedEconomicIdentityDigest(intent.ExecutionID), ReceiptID: receiptID, ReceiptDigest: sharedEconomicIdentityDigest(receiptID),
			SpoolID: intent.Owner.SpoolID, IncarnationID: intent.Owner.IncarnationID, ModeRevision: int64(intent.Owner.ModeRevision), Fence: int64(intent.Owner.Fence),
			OwnerStartedAt: intent.OwnerStartedAt, AdmissionDigest: intent.Digest, IntentBody: body, AdmittedAt: instant, MonthStart: start, MonthEnd: end}
		if _, err := sharedEconomicReceiptIntent(row); err != nil {
			return err
		}
		inserted := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&row)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		for i := range accounts {
			for _, contribution := range []struct{ kind, window string }{{"rpm", "minute"}, {"executor", "active"}, {"active_quota", "active"}} {
				var quota *eventqueue.QuotaCounterContribution
				if contribution.kind == "active_quota" {
					quota = &eventqueue.QuotaCounterContribution{Bound: intent.Bound}
				}
				projection, err := newSharedEconomicProjection(row, accounts[i], contribution.kind, contribution.window, quota)
				if err != nil {
					return err
				}
				if err := insertSharedEconomicProjectionSQL(tx, &accounts[i], projection); err != nil {
					return err
				}
			}
		}
		updated := sharedEconomicExact(sharedAuthorizationDB(tx).Model(&entity.AdmissionEconomicDomain{}), "installation_id", domain.InstallationID).
			Where("receipt_count = ? AND clock_high_water = ?", domain.ReceiptCount, domain.ClockHighWater).
			Updates(map[string]any{"receipt_count": domain.ReceiptCount + 1, "clock_high_water": instant})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		result = &sharedAuthorityReceipt{Key: sharedIntentKey(intent), ReceiptID: receiptID}
		return s.live.check(ctx, intent, nil)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, sharedEconomicSQLError(err)
	}
	if err := s.live.confirmAdmission(result.Key); err != nil {
		return nil, err
	}
	return result, nil
}
