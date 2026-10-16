package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This dependency is deliberately private and has no configuration selector or
// exported installer. Construction checks an already initialized shared domain;
// it cannot initialize one, convert local history or certify executor exclusion.
// The eventual startup owner retains the exclusive journal lifetime. It must
// stop request admission, drain HTTP and flush delivery before closing that file.
type sharedGatewayRuntime struct {
	service *Service
	owner   sharedAdmissionOwner
	birth   time.Time
	spool   *durableSharedCallSpool
	timeout time.Duration
	stopped atomic.Bool
	flush   sync.Mutex
}

func newSharedGatewayRuntime(ctx context.Context, s *Service, journal *eventqueue.SharedSpool, registration admissionOwnerRegistrationReceipt, timeout time.Duration) (*sharedGatewayRuntime, error) {
	if ctx == nil || s == nil || s.db == nil || journal == nil || s.recorder != nil || s.admission != nil || s.sharedGateway != nil ||
		timeout <= 0 || timeout > 30*time.Second || !admissionOwnerRegistrationValid(registration.admissionOwnerRegistration) {
		return nil, errSharedAdmissionInvalid
	}
	identity, ok := s.currentSystemProcessIdentity()
	owner := registration.Owner
	if !ok || identity.ID != owner.IncarnationID || !identity.StartedAt.Equal(registration.InstanceStartedAt) ||
		journal.Identity() != (eventqueue.SharedSpoolIdentity{InstallationID: owner.InstallationID, SpoolID: owner.SpoolID, ModeRevision: owner.ModeRevision}) {
		return nil, errSharedAdmissionConflict
	}
	spool, err := newDurableSharedCallSpool(journal)
	if err != nil {
		return nil, err
	}
	op, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = s.authDB(op).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return err
		}
		if err := (&admissionOwnerStore{db: s.db}).CheckCurrentOwner(tx, owner, identity.StartedAt); err != nil {
			return err
		}
		var retained entity.AdmissionSpoolOwner
		query := admissionOwnerExact(admissionOwnerExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", owner.InstallationID), "spool_id", owner.SpoolID)
		if err := query.Take(&retained).Error; err != nil {
			return err
		}
		if retained.RegistrationID != registration.RegistrationID || !retained.RegisteredAt.Equal(registration.RegisteredAt) {
			return errSharedAdmissionConflict
		}
		_, err := readSharedEconomicCalendarSQL(tx, sharedUsageReadTarget{InstallationID: owner.InstallationID, ModeRevision: owner.ModeRevision})
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return &sharedGatewayRuntime{service: s, owner: owner, birth: identity.StartedAt, spool: spool, timeout: timeout}, nil
}

func (r *sharedGatewayRuntime) current(s *Service) bool {
	if r == nil || r.service != s || r.spool == nil || r.spool.journal == nil || r.stopped.Load() || s.recorder != nil || s.admission != nil {
		return false
	}
	identity, ok := s.currentSystemProcessIdentity()
	return ok && identity.ID == r.owner.IncarnationID && identity.StartedAt.Equal(r.birth) &&
		r.spool.journal.Identity() == (eventqueue.SharedSpoolIdentity{InstallationID: r.owner.InstallationID, SpoolID: r.owner.SpoolID, ModeRevision: r.owner.ModeRevision})
}

func (r *sharedGatewayRuntime) stop() { r.stopped.Store(true) }

func (s *Service) prepareSharedGatewayExecution(ctx context.Context, requestID string, plan *gatewayAttemptPlan, result *GatewayResult, payload map[string]json.RawMessage, options ...gatewayNativeOptions) (*sharedGatewayExecution, error) {
	r := s.sharedGateway
	if !r.current(s) || result == nil || result.sharedExecution != nil {
		return nil, errSharedDispatchForbidden
	}
	s.egressMu.RLock()
	defer s.egressMu.RUnlock()
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	limits, err := s.gatewayLimits(ctx, result)
	if err != nil {
		return nil, err
	}
	policies, data, err := s.gatewayQuotaPolicies(ctx, result, limits)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(limits, result.admissionLimits) || !reflect.DeepEqual(policies, result.admissionQuota) || data.Setting.TimeZone != result.quotaTimeZone {
		return nil, errSharedAdmissionConflict
	}
	manifest, err := captureGatewayReservationManifest(requestID, plan, result, data, payload, options...)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	fallback, err := gatewayFallbackPayload(requestID, result, now)
	if err != nil {
		return nil, err
	}
	request := sharedAdmissionRequest{RequestID: requestID, Reservation: manifest, OwnerStartedAt: r.birth,
		CalendarRevision: data.Setting.ETag, TimeZone: data.Setting.TimeZone, Policies: policies, Bound: result.quotaBound,
		Recovery: zeroQuotaSettlement(result.quotaBound), Fallback: fallback}
	op, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	err = s.authDB(op).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return err
		}
		if err := (&admissionOwnerStore{db: s.db}).CheckCurrentOwner(tx, r.owner, r.birth); err != nil {
			return err
		}
		identity := result.identity
		identity.selectedModelID = result.ModelID
		request.Authorization, err = captureSharedAuthorizationSQL(tx, identity, now)
		if err != nil {
			return err
		}
		if err := validateSharedAdmissionPoliciesSQL(tx, request.Authorization, policies, request.CalendarRevision, request.TimeZone, gatewayExecutionIP(ctx), now); err != nil {
			return err
		}
		calendar, err := readSharedEconomicCalendarSQL(tx, sharedUsageReadTarget{InstallationID: r.owner.InstallationID, ModeRevision: r.owner.ModeRevision})
		if err != nil {
			return err
		}
		if calendar.CalendarETag != request.CalendarRevision || calendar.TimeZone != request.TimeZone {
			return errSharedAdmissionConflict
		}
		return validateGatewayReservationAcceptSQL(tx, manifest)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	live, err := newSharedEconomicLiveAuthority(s, plan, result, gatewayExecutionIP(ctx), r.owner, request, data, payload, options...)
	if err != nil {
		return nil, err
	}
	authority := &sharedEconomicSQLStore{db: s.db, live: live}
	coordinator, err := newSharedAdmissionCoordinator(r.owner, request, r.spool, authority, sharedCoordinatorOptions{OperationTimeout: r.timeout})
	if err != nil {
		return nil, err
	}
	return &sharedGatewayExecution{runtime: r, result: result, coordinator: coordinator, authority: authority}, nil
}

// Ready facts are delivered only after authoritative economic settlement. This
// caller-driven operation installs no worker and cannot activate shared mode.
func (s *Service) flushSharedGatewayCalls(ctx context.Context) error {
	r := s.sharedGateway
	if r == nil || r.service != s || r.spool == nil || r.spool.journal == nil || s.recorder != nil || s.admission != nil {
		return callQueueUnavailable
	}
	r.flush.Lock()
	defer r.flush.Unlock()
	entries, err := r.spool.ReadDelivery(ctx, min(64, r.spool.journal.Capacity()))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fact, err := sharedCallPayload(entry.Receipt.Key.RequestID, entry.Fact)
		if err != nil {
			return err
		}
		op, cancel := context.WithTimeout(ctx, r.timeout)
		err = s.RecordCall(op, fact)
		if err == nil {
			err = r.spool.AckDelivery(op, entry.Receipt)
		}
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
