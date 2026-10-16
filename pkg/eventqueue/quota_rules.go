package eventqueue

import (
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/miclle/routex/pkg/limits"
)

var (
	quotaDecimal  = regexp.MustCompile(`^(0|[1-9][0-9]{0,59})(\.[0-9]{1,18})?$`)
	quotaCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
	quotaDigest   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// ValidateQuotaBound checks the existing conservative-bound representation.
// Missing token or money dimensions remain unknown; validity is not proof of
// current pricing, policy, authorization, or reservation capacity.
func ValidateQuotaBound(bound QuotaBound) error { return validQuotaBound(bound) }

// ValidateQuotaSettlement checks each known dimension without converting a nil
// dimension to zero or inferring native completion.
func ValidateQuotaSettlement(actual QuotaSettlement) error { return validQuotaSettlement(actual) }

// ValidateQuotaRecovery validates both inputs before comparing known recovery
// dimensions to the original bound. Unproven dimensions remain unproven.
func ValidateQuotaRecovery(bound QuotaBound, recovery QuotaSettlement) error {
	if err := validQuotaBound(bound); err != nil {
		return err
	}
	if err := validQuotaSettlement(recovery); err != nil {
		return err
	}
	return validQuotaRecovery(bound, recovery)
}

// QuotaMonthBounds returns the original calendar calculation in Unix
// nanoseconds, including timezone and daylight-saving transitions. It neither
// reads a clock nor establishes accounting coverage.
func QuotaMonthBounds(instant int64, zone string) (int64, int64, error) {
	return quotaMonth(instant, zone)
}

// QuotaTokenDelta applies a nonnegative amount with sign +1 or -1. The original
// total is returned unchanged on overflow, underflow, or invalid input. Storage
// adapters must validate their persisted totals before calling this primitive.
func QuotaTokenDelta(total, amount, sign int64) (int64, error) {
	original := total
	if total < 0 {
		return original, ErrInvalid
	}
	if err := addToken(&total, amount, sign); err != nil {
		return original, err
	}
	return total, nil
}

// QuotaMoneyDelta applies an amount with the existing 60-integer/18-fractional
// decimal contract to a canonical nonnegative integer-unit accumulator (10^-18).
// Accumulators may grow beyond one amount's precision. It returns the original
// accumulator on failure and does not mutate any caller-owned state.
func QuotaMoneyDelta(currentUnits, currency, amount string, sign int64) (string, error) {
	if !quotaCurrency.MatchString(currency) || sign != 1 && sign != -1 {
		return currentUnits, ErrInvalid
	}
	if _, err := storedQuotaUnits(currentUnits); err != nil {
		return currentUnits, err
	}
	totals := map[string]string{currency: currentUnits}
	if err := addMoney(totals, currency, amount, sign); err != nil {
		return currentUnits, err
	}
	if result, ok := totals[currency]; ok {
		return result, nil
	}
	return "0", nil
}

// QuotaMoneyValue renders canonical nonnegative integer units as an exact
// decimal string. It accepts accumulated totals without narrowing their size.
func QuotaMoneyValue(units string) (string, error) {
	value, err := storedQuotaUnits(units)
	if err != nil {
		return "", err
	}
	return quotaDecimalValue(value), nil
}

func storedQuotaUnits(raw string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() < 0 || value.String() != raw {
		return nil, ErrInvalid
	}
	return value, nil
}

func quotaUnits(value string) (*big.Int, error) {
	if !quotaDecimal.MatchString(value) {
		return nil, ErrInvalid
	}
	whole, fraction, _ := strings.Cut(value, ".")
	digits := whole + fraction + strings.Repeat("0", 18-len(fraction))
	number, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, ErrInvalid
	}
	return number, nil
}

func quotaDecimalValue(units *big.Int) string {
	value := units.String()
	if len(value) <= 18 {
		value = strings.Repeat("0", 19-len(value)) + value
	}
	return strings.TrimRight(strings.TrimRight(value[:len(value)-18]+"."+value[len(value)-18:], "0"), ".")
}

func quotaMonth(instant int64, zone string) (int64, int64, error) {
	location, err := time.LoadLocation(zone)
	if err != nil {
		return 0, 0, ErrInvalid
	}
	now := time.Unix(0, instant).In(location)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	if start.UnixNano() < 0 || end.UnixNano() <= instant {
		return 0, 0, ErrInvalid
	}
	return start.UnixNano(), end.UnixNano(), nil
}

func validQuotaBound(bound QuotaBound) error {
	if bound.Tokens != nil && *bound.Tokens < 0 {
		return ErrInvalid
	}
	if bound.Money != nil {
		if _, err := quotaUnits(*bound.Money); err != nil {
			return err
		}
		if !quotaCurrency.MatchString(bound.Currency) {
			return ErrInvalid
		}
	}
	if bound.Revision != "" && !validKey.MatchString(bound.Revision) || len(bound.PriceRevision) > 64 || bound.BasisDigest != "" && !quotaDigest.MatchString(bound.BasisDigest) {
		return ErrInvalid
	}
	return nil
}

func validQuotaSettlement(actual QuotaSettlement) error {
	if actual.Tokens != nil && *actual.Tokens < 0 {
		return ErrInvalid
	}
	if actual.Money != nil {
		if _, err := quotaUnits(*actual.Money); err != nil {
			return err
		}
		if !quotaCurrency.MatchString(actual.Currency) {
			return ErrInvalid
		}
	}
	return nil
}

func validQuotaRecovery(bound QuotaBound, recovery QuotaSettlement) error {
	if recovery.Tokens != nil && bound.Tokens != nil && *recovery.Tokens > *bound.Tokens {
		return ErrQuotaBound
	}
	if recovery.Money == nil || bound.Money == nil {
		return nil
	}
	if recovery.Currency != bound.Currency {
		return ErrQuotaCurrency
	}
	observed, _ := quotaUnits(*recovery.Money)
	reserved, _ := quotaUnits(*bound.Money)
	if observed.Cmp(reserved) > 0 {
		return ErrQuotaBound
	}
	return nil
}

func addToken(total *int64, value, sign int64) error {
	if value < 0 || sign != 1 && sign != -1 {
		return ErrInvalid
	}
	if sign == 1 {
		if *total > math.MaxInt64-value {
			return ErrInvalid
		}
		*total += value
	} else {
		if *total < value {
			return ErrInvalid
		}
		*total -= value
	}
	return nil
}

func addMoney(totals map[string]string, currency, value string, sign int64) error {
	units, err := quotaUnits(value)
	if err != nil {
		return err
	}
	previous := new(big.Int)
	if raw, ok := totals[currency]; ok {
		var valid bool
		previous, valid = previous.SetString(raw, 10)
		if !valid || previous.Sign() < 0 {
			return ErrInvalid
		}
	}
	if sign == 1 {
		previous.Add(previous, units)
	} else {
		previous.Sub(previous, units)
	}
	if previous.Sign() < 0 {
		return ErrInvalid
	}
	if previous.Sign() == 0 {
		delete(totals, currency)
	} else {
		totals[currency] = previous.String()
	}
	return nil
}

// QuotaDecisionFrame contains caller-established accounting coordinates. These
// values do not establish coverage, calendar/current-policy validity, or birth
// authority; a storage adapter must obtain them inside its coherent transaction.
type QuotaDecisionFrame struct {
	Instant, MonthStart, CoverageStart int64
}

// QuotaPolicyView supplies validated counters from one coherent snapshot.
// Accessors are read-only and called lazily in the original rejection order;
// month may be read separately for token and money policies. Money totals use
// integer units, and nil/zero fields must not invent historical coverage.
type QuotaPolicyView struct {
	Active       QuotaCounter
	Window       func(string) (QuotaCounter, error)
	InvalidBound func(string) (bool, error)
}

// QuotaPolicyRules is a captured calculation, not current authorization or an
// admission permit. Construct it explicitly; its zero value is invalid.
type QuotaPolicyRules struct {
	valid                        bool
	policy                       QuotaLimit
	bound                        QuotaBound
	frame                        QuotaDecisionFrame
	created                      int64
	tokenBehavior, moneyBehavior string
}

// NewQuotaPolicyRules validates a standalone bound and the existing quota-specific
// policy header, preserving absent dimensions and capturing copies. The caller must still
// check/persist exact account birth, validate/read the active counter, and supply
// current locked accounting snapshots before Check. It proves no SQL authority,
// capacity attestation, coverage completeness, or real admission.
func NewQuotaPolicyRules(policy QuotaLimit, bound QuotaBound, frame QuotaDecisionFrame) (QuotaPolicyRules, error) {
	if err := validQuotaBound(bound); err != nil {
		return QuotaPolicyRules{}, err
	}
	return newQuotaPolicyRules(policy, bound, frame)
}

// Local admission already validated its bound before entering checkQuota. Keep
// policy validation ahead of birth persistence and all counter reads there.
func newQuotaPolicyRules(policy QuotaLimit, bound QuotaBound, frame QuotaDecisionFrame) (QuotaPolicyRules, error) {
	tokenBehavior, tokenErr := limits.CanonicalMonthlyBehavior(policy.TokensMonthBehavior)
	moneyBehavior, moneyErr := limits.CanonicalMonthlyBehavior(policy.MoneyMonthBehavior)
	if tokenErr != nil || moneyErr != nil || policy.PersonalKey && policy.ProjectKey || policy.TeamMember && (policy.PersonalKey || policy.ProjectKey) || (tokenBehavior != "" || moneyBehavior != "") && !monthlyBehaviorAccount(policy.Account) && (!policy.PersonalKey || !personalKeyBehaviorAccount(policy.Account)) && (!policy.ProjectKey || !projectKeyBehaviorAccount(policy.Account)) && (!policy.TeamMember || !teamMemberBehaviorAccount(policy.Account)) {
		return QuotaPolicyRules{}, ErrInvalid
	}
	if !validKey.MatchString(policy.Revision) {
		return QuotaPolicyRules{}, ErrInvalid
	}
	created := int64(0)
	if !policy.CreatedAt.IsZero() {
		created = policy.CreatedAt.UnixNano()
		if created <= 0 || created > frame.Instant {
			return QuotaPolicyRules{}, ErrInvalid
		}
	}
	policy.Tokens5H = copyQuotaRuleValue(policy.Tokens5H)
	policy.Tokens7D = copyQuotaRuleValue(policy.Tokens7D)
	policy.TokensMonth = copyQuotaRuleValue(policy.TokensMonth)
	policy.TPM = copyQuotaRuleValue(policy.TPM)
	policy.MoneyMonth = copyQuotaRuleValue(policy.MoneyMonth)
	policy.RPM = copyQuotaRuleValue(policy.RPM)
	policy.Concurrency = copyQuotaRuleValue(policy.Concurrency)
	bound.Tokens = copyQuotaRuleValue(bound.Tokens)
	bound.Money = copyQuotaRuleValue(bound.Money)
	return QuotaPolicyRules{valid: true, policy: policy, bound: bound, frame: frame, created: created, tokenBehavior: tokenBehavior, moneyBehavior: moneyBehavior}, nil
}

func copyQuotaRuleValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// Check applies the established ordered quota decisions without modifying the
// supplied counters. Passing Check alone does not reserve or establish usage;
// the caller owns atomic checks/writes for every conjunctive parent account.
func (rules QuotaPolicyRules) Check(view QuotaPolicyView) error {
	if !rules.valid {
		return ErrInvalid
	}
	policy, bound, frame := rules.policy, rules.bound, rules.frame
	instant, monthStart, created := frame.Instant, frame.MonthStart, rules.created
	metadata := quotaMetadata{CoverageStart: frame.CoverageStart}
	tokenBehavior, moneyBehavior := rules.tokenBehavior, rules.moneyBehavior
	active := view.Active
	constrained := false
	for _, window := range []struct {
		name    string
		start   int64
		ceiling *int64
	}{{"minute", instant - int64(time.Minute), policy.TPM}, {"five_hours", instant - int64(5*time.Hour), policy.Tokens5H}, {"seven_days", instant - int64(7*24*time.Hour), policy.Tokens7D}, {monthWindow(monthStart), monthStart, policy.TokensMonth}} {
		if window.ceiling == nil {
			continue
		}
		constrained = true
		if *window.ceiling < 0 {
			return ErrInvalid
		}
		soft := window.name == monthWindow(monthStart) && tokenBehavior == limits.MonthlyBehaviorAlertOnly
		if *window.ceiling == 0 && !soft {
			return ErrQuotaTokens
		}
		if window.start < metadata.CoverageStart && created < metadata.CoverageStart {
			return ErrQuotaCoverage
		}
		if bound.Tokens == nil {
			return ErrQuotaBound
		}
		used, err := readQuotaPolicyWindow(view, window.name)
		if err != nil {
			return err
		}
		if used.TokensUnknown > 0 || active.TokensUnknown > 0 {
			return ErrQuotaUnknown
		}
		if !soft {
			remaining := *window.ceiling
			for _, amount := range []int64{used.TokensUsed, used.TokensHeld, active.TokensUsed, active.TokensHeld, *bound.Tokens} {
				if amount > remaining {
					return ErrQuotaTokens
				}
				remaining -= amount
			}
		}
	}
	if policy.MoneyMonth != nil {
		constrained = true
		maximum, err := quotaUnits(*policy.MoneyMonth)
		if err != nil {
			return err
		}
		soft := moneyBehavior == limits.MonthlyBehaviorAlertOnly
		if maximum.Sign() == 0 && !soft {
			return ErrQuotaMoney
		}
		if !quotaCurrency.MatchString(policy.Currency) {
			return ErrInvalid
		}
		if monthStart < metadata.CoverageStart && created < metadata.CoverageStart {
			return ErrQuotaCoverage
		}
		if bound.Money == nil {
			return ErrQuotaBound
		}
		if bound.Currency != policy.Currency {
			return ErrQuotaCurrency
		}
		used, err := readQuotaPolicyWindow(view, monthWindow(monthStart))
		if err != nil {
			return err
		}
		if used.MoneyUnknown > 0 || active.MoneyUnknown > 0 {
			return ErrQuotaUnknown
		}
		for _, totals := range []map[string]string{used.MoneyUsed, used.MoneyHeld, active.MoneyUsed, active.MoneyHeld} {
			for currency, raw := range totals {
				if currency != policy.Currency {
					return ErrQuotaCurrency
				}
				amount, ok := new(big.Int).SetString(raw, 10)
				if !ok {
					return ErrInvalid
				}
				maximum.Sub(maximum, amount)
			}
		}
		reservation, _ := quotaUnits(*bound.Money)
		if maximum.Cmp(reservation) < 0 && !soft {
			return ErrQuotaMoney
		}
	}
	if constrained {
		if view.InvalidBound == nil {
			return ErrInvalid
		}
		invalidBound, err := view.InvalidBound(bound.Revision)
		if err != nil {
			return err
		}
		if bound.Revision == "" || invalidBound {
			return ErrQuotaBound
		}
	}
	return nil
}

func readQuotaPolicyWindow(view QuotaPolicyView, name string) (QuotaCounter, error) {
	if view.Window == nil {
		return QuotaCounter{}, ErrInvalid
	}
	return view.Window(name)
}

// Aggregate monthly accounts use bounded ASCII identities. Team-member pair
// digests require the separate trusted current-membership proof; an account
// prefix alone never enables their monthly modes.
func monthlyBehaviorAccount(account string) bool {
	if suffix, ok := strings.CutPrefix(account, "project_"); ok {
		return strings.HasPrefix(suffix, "prj_") && len(suffix) > len("prj_") && len(suffix) <= 30 && validKey.MatchString(suffix)
	}
	if strings.HasPrefix(account, "user_") && len(account) > len("user_") {
		return true
	}
	suffix, ok := strings.CutPrefix(account, "team_")
	return ok && !strings.HasPrefix(account, "team_member_") && len(suffix) > 0 && len(suffix) <= 30 && validKey.MatchString(suffix)
}

func projectKeyBehaviorAccount(account string) bool {
	suffix, ok := strings.CutPrefix(account, "key_")
	return ok && strings.HasPrefix(suffix, "pky_") && len(suffix) > len("pky_") && len(suffix) <= 30 && validKey.MatchString(suffix)
}

func personalKeyBehaviorAccount(account string) bool {
	suffix, ok := strings.CutPrefix(account, "key_")
	return ok && len(suffix) > 0 && len(suffix) <= 30 && validKey.MatchString(suffix)
}
