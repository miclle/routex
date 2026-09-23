// Package pricing implements the finite, exact-decimal RouteX text pricing adapter.
package pricing

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
)

var ErrInvalid = errors.New("invalid pricing configuration or normalized usage")
var ErrUnpriced = errors.New("required price or exchange rate is not configured")
var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$`)

// Decimal accepts unsigned plain decimal strings with at most 18 integer and
// 18 fractional digits. Exponent notation and binary floating point are excluded.
func Decimal(value string) (string, error) {
	if !decimalPattern.MatchString(value) {
		return "", ErrInvalid
	}
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value, nil
}
func rational(value string) (*big.Rat, error) {
	v, err := Decimal(value)
	if err != nil {
		return nil, err
	}
	r, ok := new(big.Rat).SetString(v)
	if !ok {
		return nil, ErrInvalid
	}
	return r, nil
}

// Round uses half-even rounding to 18 fractional digits. It is applied once per
// currency-converted component; the total is the exact sum of those components.
func rounded(r *big.Rat) string {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	num := new(big.Int).Mul(r.Num(), scale)
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(num, r.Denom(), rem)
	cmp := new(big.Int).Lsh(rem, 1).Cmp(r.Denom())
	if cmp > 0 || (cmp == 0 && q.Bit(0) == 1) {
		q.Add(q, big.NewInt(1))
	}
	digits := q.String()
	if len(digits) <= 18 {
		digits = strings.Repeat("0", 19-len(digits)) + digits
	}
	value := digits[:len(digits)-18] + "." + digits[len(digits)-18:]
	return strings.TrimRight(strings.TrimRight(value, "0"), ".")
}
func Currency(value string) bool {
	switch value {
	case "USD", "CNY", "EUR", "GBP", "JPY", "HKD", "SGD":
		return true
	}
	return false
}
