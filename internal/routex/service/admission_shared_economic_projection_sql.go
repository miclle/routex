package service

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sharedEconomicExpiryBatch = 4096

// A contribution owns its subtraction data. Expiry never reacquires an old
// receipt after account locks, and never deletes immutable call history.
type sharedEconomicProjectionBody struct {
	Version      int
	RequestID    string
	Account      string
	AccountBirth time.Time
	AdmittedAt   int64
	MonthStart   int64
	MonthEnd     int64
	Kind         string
	Window       string
	ExpiresAt    *int64
	Quota        *eventqueue.QuotaCounterContribution
}

type sharedEconomicWindow struct {
	Name string
	End  int64
}

func sharedEconomicWindows(admitted, start, end int64) ([]sharedEconomicWindow, error) {
	if admitted <= 0 || admitted > math.MaxInt64-int64(7*24*time.Hour) || start < 0 || start > admitted || end <= admitted {
		return nil, errSharedAdmissionConflict
	}
	return []sharedEconomicWindow{{"minute", admitted + int64(time.Minute)}, {"five_hours", admitted + int64(5*time.Hour)},
		{"seven_days", admitted + int64(7*24*time.Hour)}, {"month_" + strconv.FormatInt(start, 10), end}}, nil
}

func sharedEconomicProjectionDecode(row entity.AdmissionEconomicContribution, account entity.AdmissionEconomicAccount) (sharedEconomicProjectionBody, error) {
	var body sharedEconomicProjectionBody
	if !sharedEconomicDecodeBody(row.ContributionBody, &body) || body.Version != 1 || !sharedIdentity(body.RequestID) ||
		!admissionOwnerJournal.MatchString(account.InstallationID) || !sharedIdentity(account.Account) ||
		row.InstallationID != account.InstallationID || row.AccountDigest != account.AccountDigest ||
		account.AccountDigest != sharedEconomicIdentityDigest(account.Account) || body.Account != account.Account ||
		!admissionOwnerTimeValid(body.AccountBirth) || !body.AccountBirth.Equal(account.Birth) ||
		row.RequestID != body.RequestID || row.RequestDigest != sharedEconomicIdentityDigest(body.RequestID) ||
		row.Kind != body.Kind || row.Window != body.Window || !sharedEconomicSameExpiry(row.ExpiresAt, body.ExpiresAt) {
		return body, errSharedAdmissionConflict
	}
	windows, err := sharedEconomicWindows(body.AdmittedAt, body.MonthStart, body.MonthEnd)
	if err != nil || account.Birth.UnixNano() <= 0 || account.Birth.UnixNano() > body.AdmittedAt {
		return body, errSharedAdmissionConflict
	}
	switch body.Kind {
	case "rpm":
		if body.Window != "minute" || body.ExpiresAt == nil || *body.ExpiresAt != windows[0].End || body.Quota != nil {
			return body, errSharedAdmissionConflict
		}
	case "executor":
		if body.Window != "active" || body.ExpiresAt != nil || body.Quota != nil {
			return body, errSharedAdmissionConflict
		}
	case "active_quota":
		if body.Window != "active" || body.ExpiresAt != nil || body.Quota == nil || body.Quota.Settled ||
			body.Quota.Actual.Tokens != nil || body.Quota.Actual.Money != nil || body.Quota.Actual.Currency != "" {
			return body, errSharedAdmissionConflict
		}
	case "terminal_quota":
		validWindow := false
		for _, window := range windows {
			if body.Window == window.Name && body.ExpiresAt != nil && *body.ExpiresAt == window.End {
				validWindow = true
			}
		}
		if !validWindow || body.Quota == nil || !body.Quota.Settled {
			return body, errSharedAdmissionConflict
		}
	default:
		return body, errSharedAdmissionConflict
	}
	if body.Quota != nil && (eventqueue.ValidateQuotaBound(body.Quota.Bound) != nil || eventqueue.ValidateQuotaSettlement(body.Quota.Actual) != nil) {
		return body, errSharedAdmissionConflict
	}
	return body, nil
}

func sharedEconomicSameExpiry(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// Callers derive quota from the accepted intent or validated terminal history.
// This constructor creates no receipt, authorization or economic authority.
func newSharedEconomicProjection(receipt entity.AdmissionEconomicReceipt, account entity.AdmissionEconomicAccount, kind, window string, quota *eventqueue.QuotaCounterContribution) (entity.AdmissionEconomicContribution, error) {
	row := entity.AdmissionEconomicContribution{InstallationID: receipt.InstallationID, RequestDigest: receipt.RequestDigest,
		RequestID: receipt.RequestID, AccountDigest: account.AccountDigest, Kind: kind, Window: window}
	intent, err := sharedEconomicReceiptIntent(receipt)
	if err != nil {
		return row, err
	}
	exactAccount := false
	for _, policy := range intent.Policies {
		if policy.Account == account.Account && policy.CreatedAt.Equal(account.Birth) {
			exactAccount = true
		}
	}
	if !exactAccount || quota != nil && !reflect.DeepEqual(quota.Bound, intent.Bound) {
		return row, errSharedAdmissionConflict
	}
	windows, err := sharedEconomicWindows(receipt.AdmittedAt, receipt.MonthStart, receipt.MonthEnd)
	if err != nil {
		return row, err
	}
	if kind == "rpm" || kind == "terminal_quota" {
		for _, value := range windows {
			if value.Name == window {
				end := value.End
				row.ExpiresAt = &end
			}
		}
	}
	body := sharedEconomicProjectionBody{Version: 1, RequestID: row.RequestID, Account: account.Account,
		AccountBirth: account.Birth.UTC(), AdmittedAt: receipt.AdmittedAt, MonthStart: receipt.MonthStart, MonthEnd: receipt.MonthEnd,
		Kind: kind, Window: window, ExpiresAt: row.ExpiresAt, Quota: quota}
	row.ContributionBody, err = sharedEconomicCanonicalBody(body)
	if err != nil {
		return row, err
	}
	if _, err := sharedEconomicProjectionDecode(row, account); err != nil {
		return row, err
	}
	return row, nil
}

// A new transition inserts its durable subtraction record before changing the
// projection. The caller rolls both back on error. An existing row conflicts;
// acknowledgement replay belongs to the immutable receipt transition.
func insertSharedEconomicProjectionSQL(tx *gorm.DB, account *entity.AdmissionEconomicAccount, row entity.AdmissionEconomicContribution) error {
	if account == nil {
		return errSharedAdmissionInvalid
	}
	if _, err := sharedEconomicProjectionDecode(row, *account); err != nil {
		return err
	}
	result := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errSharedAdmissionConflict
	}
	return applySharedEconomicProjectionSQL(tx, account, row, 1)
}

func deleteSharedEconomicProjectionSQL(tx *gorm.DB, account *entity.AdmissionEconomicAccount, row entity.AdmissionEconomicContribution) error {
	if err := applySharedEconomicProjectionSQL(tx, account, row, -1); err != nil {
		return err
	}
	q := sharedEconomicAccountQuery(tx, account.InstallationID, account.AccountDigest)
	q = sharedEconomicExact(q, "request_digest", row.RequestDigest)
	q = sharedEconomicExact(q, "kind", row.Kind)
	result := sharedEconomicExact(q, "window_name", row.Window).Delete(&entity.AdmissionEconomicContribution{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errSharedAdmissionConflict
	}
	return nil
}

// New callers already hold governance/owner/current-authority/domain/request.
// Historical settlement starts at domain/request. Both lock sorted accounts
// before projections; absent account rows are serialized by the domain lock.
func lockSharedEconomicAccountsSQL(tx *gorm.DB, installation string, policies []eventqueue.QuotaLimit, admittedAt int64, createMissing bool) ([]entity.AdmissionEconomicAccount, error) {
	if !admissionOwnerJournal.MatchString(installation) || len(policies) < 1 || len(policies) > 2 || admittedAt <= 0 {
		return nil, errSharedAdmissionInvalid
	}
	ordered := slices.Clone(policies)
	slices.SortFunc(ordered, func(a, b eventqueue.QuotaLimit) int { return compareReservationSQLID(a.Account, b.Account) })
	accounts := make([]entity.AdmissionEconomicAccount, 0, len(ordered))
	for i, policy := range ordered {
		if !sharedIdentity(policy.Account) || !admissionOwnerTimeValid(policy.CreatedAt) || policy.CreatedAt.UnixNano() <= 0 ||
			policy.CreatedAt.UnixNano() > admittedAt || i > 0 && ordered[i-1].Account == policy.Account {
			return nil, errSharedAdmissionConflict
		}
		var row entity.AdmissionEconomicAccount
		query := sharedEconomicAccountQuery(tx, installation, sharedEconomicIdentityDigest(policy.Account)).Clauses(clause.Locking{Strength: "UPDATE"})
		err := query.Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !createMissing {
				return nil, errSharedAdmissionConflict
			}
			row = entity.AdmissionEconomicAccount{InstallationID: installation, AccountDigest: sharedEconomicIdentityDigest(policy.Account), Account: policy.Account, Birth: policy.CreatedAt}
			result := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&row)
			if result.Error != nil {
				return nil, result.Error
			}
			if result.RowsAffected != 1 {
				return nil, errSharedAdmissionConflict
			}
		} else if err != nil {
			return nil, err
		}
		if row.InstallationID != installation || row.AccountDigest != sharedEconomicIdentityDigest(policy.Account) || row.Account != policy.Account ||
			!row.Birth.Equal(policy.CreatedAt) || row.RPMCount < 0 || row.ExecutorCount < 0 {
			return nil, errSharedAdmissionConflict
		}
		accounts = append(accounts, row)
	}
	return accounts, nil
}

func sharedEconomicAccountQuery(tx *gorm.DB, installation, accountDigest string) *gorm.DB {
	q := sharedEconomicExact(sharedAuthorizationDB(tx), "installation_id", installation)
	return sharedEconomicExact(q, "account_digest", accountDigest)
}

func readSharedEconomicCounterSQL(tx *gorm.DB, account entity.AdmissionEconomicAccount, window string) (entity.AdmissionEconomicCounter, eventqueue.QuotaCounter, bool, error) {
	row := entity.AdmissionEconomicCounter{}
	counter := eventqueue.QuotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
	q := sharedEconomicAccountQuery(tx, account.InstallationID, account.AccountDigest).Clauses(clause.Locking{Strength: "UPDATE"})
	err := sharedEconomicExact(q, "window_name", window).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, counter, false, nil
	}
	if err != nil {
		return row, counter, false, err
	}
	if row.InstallationID != account.InstallationID || row.AccountDigest != account.AccountDigest || row.Window != window ||
		!sharedEconomicDecodeBody(row.CounterBody, &counter) || eventqueue.ValidateQuotaCounter(counter) != nil {
		return row, counter, true, errSharedAdmissionConflict
	}
	return row, counter, true, nil
}

func applySharedEconomicProjectionSQL(tx *gorm.DB, account *entity.AdmissionEconomicAccount, row entity.AdmissionEconomicContribution, sign int64) error {
	if account == nil || sign != 1 && sign != -1 {
		return errSharedAdmissionInvalid
	}
	body, err := sharedEconomicProjectionDecode(row, *account)
	if err != nil {
		return err
	}
	if body.Quota == nil {
		field, prior := "rpm_count", account.RPMCount
		if body.Kind == "executor" {
			field, prior = "executor_count", account.ExecutorCount
		}
		next, err := eventqueue.QuotaTokenDelta(prior, 1, sign)
		if err != nil {
			return err
		}
		q := sharedEconomicAccountQuery(tx, account.InstallationID, account.AccountDigest).Model(&entity.AdmissionEconomicAccount{})
		result := q.Update(field, next)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		if body.Kind == "executor" {
			account.ExecutorCount = next
		} else {
			account.RPMCount = next
		}
		return nil
	}
	prior, counter, present, err := readSharedEconomicCounterSQL(tx, *account, body.Window)
	if err != nil {
		return err
	}
	next, err := eventqueue.QuotaCounterDelta(counter, *body.Quota, sign)
	if err != nil {
		return err
	}
	q := sharedEconomicAccountQuery(tx, account.InstallationID, account.AccountDigest).Model(&entity.AdmissionEconomicCounter{})
	q = sharedEconomicExact(q, "window_name", body.Window)
	if sharedEconomicCounterEmpty(next) {
		if !present {
			return nil
		}
		result := q.Delete(&entity.AdmissionEconomicCounter{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		return nil
	}
	encoded, err := sharedEconomicCanonicalBody(next)
	if err != nil {
		return err
	}
	var result *gorm.DB
	if present {
		// A known-zero contribution can leave a nonempty counter unchanged.
		// The locked, validated row already proves that exact projection; MySQL
		// legitimately reports zero changed rows for an identical UPDATE.
		if bytes.Equal(encoded, prior.CounterBody) {
			return nil
		}
		result = q.Update("counter_body", encoded)
	} else {
		result = sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&entity.AdmissionEconomicCounter{
			InstallationID: account.InstallationID, AccountDigest: account.AccountDigest, Window: body.Window, CounterBody: encoded})
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errSharedAdmissionConflict
	}
	return nil
}

func sharedEconomicCounterEmpty(counter eventqueue.QuotaCounter) bool {
	return counter.TokensUsed == 0 && counter.TokensHeld == 0 && counter.TokensUnknown == 0 && counter.MoneyUnknown == 0 &&
		len(counter.MoneyUsed) == 0 && len(counter.MoneyHeld) == 0
}

// A bounded overflow probe refuses admission/settlement rather than leaving a
// partially expired projection. Caller owns the transaction and rolls it back
// on any error. Active/executor contributions have no expiry and are never read
// by this query. Receipt and invalid-bound rows are never deleted here.
func expireSharedEconomicProjectionsSQL(tx *gorm.DB, account *entity.AdmissionEconomicAccount, instant int64) error {
	if account == nil || instant <= 0 {
		return errSharedAdmissionInvalid
	}
	var rows []entity.AdmissionEconomicContribution
	q := sharedEconomicAccountQuery(tx, account.InstallationID, account.AccountDigest).Clauses(clause.Locking{Strength: "UPDATE"})
	if err := q.Where("expires_at <= ?", instant).Order("expires_at, request_digest, kind, window_name").Limit(sharedEconomicExpiryBatch + 1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > sharedEconomicExpiryBatch {
		return errSharedEconomicUnavailable
	}
	for _, row := range rows {
		if row.ExpiresAt == nil || *row.ExpiresAt > instant || row.Kind != "rpm" && row.Kind != "terminal_quota" {
			return errSharedAdmissionConflict
		}
		if err := deleteSharedEconomicProjectionSQL(tx, account, row); err != nil {
			return err
		}
	}
	return tx.Statement.Context.Err()
}
