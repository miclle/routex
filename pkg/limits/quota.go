package limits

import (
	"math/big"

	"github.com/miclle/routex/pkg/pricing"
)

func normalizeQuota(p Policy) (Policy, error) {
	for _, value := range []*int64{p.Tokens5H, p.Tokens7D, p.TokensMonth, p.TPM} {
		if value != nil && (*value < 0 || *value > MaxInteger) {
			return Policy{}, ErrInvalid
		}
	}
	if p.MoneyMonth == nil {
		p.Currency = ""
		return p, nil
	}
	amount, err := pricing.Decimal(*p.MoneyMonth)
	if err != nil || !pricing.Currency(p.Currency) {
		return Policy{}, ErrInvalid
	}
	p.MoneyMonth = &amount
	return p, nil
}
func MoneyMinimum(a, b *string) *string {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	left, ok := new(big.Rat).SetString(*a)
	if !ok {
		return nil
	}
	right, ok := new(big.Rat).SetString(*b)
	if !ok {
		return nil
	}
	if left.Cmp(right) < 0 {
		return a
	}
	return b
}
func narrowerQuota(parent, child Policy) bool {
	for _, pair := range [][2]*int64{{parent.Tokens5H, child.Tokens5H}, {parent.Tokens7D, child.Tokens7D}, {parent.TokensMonth, child.TokensMonth}, {parent.TPM, child.TPM}} {
		if pair[0] != nil && pair[1] != nil && *pair[1] > *pair[0] {
			return false
		}
	}
	if parent.MoneyMonth != nil && child.MoneyMonth != nil {
		if parent.Currency != child.Currency {
			return false
		}
		left, ok := new(big.Rat).SetString(*parent.MoneyMonth)
		if !ok {
			return false
		}
		right, ok := new(big.Rat).SetString(*child.MoneyMonth)
		if !ok || right.Cmp(left) > 0 {
			return false
		}
	}
	return true
}
func HasTokenQuota(policy Policy) bool {
	return policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil
}
func HasQuota(policy Policy) bool { return HasTokenQuota(policy) || policy.MoneyMonth != nil }
