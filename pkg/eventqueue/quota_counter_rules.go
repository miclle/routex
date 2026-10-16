package eventqueue

// QuotaCounterContribution keeps independently known settled usage separate
// from the original conservative bound. Settled means the economic contribution
// is terminal; it is not executor-closure or native-completion evidence.
// A nil terminal dimension retains its original hold (or unknown count).
type QuotaCounterContribution struct {
	Bound   QuotaBound
	Actual  QuotaSettlement
	Settled bool
}

// ValidateQuotaCounter checks a complete storage projection without narrowing
// canonical integer-unit money totals to decimal amount precision.
func ValidateQuotaCounter(counter QuotaCounter) error {
	if counter.TokensUsed < 0 || counter.TokensHeld < 0 || counter.TokensUnknown < 0 || counter.MoneyUnknown < 0 || counter.MoneyUsed == nil || counter.MoneyHeld == nil {
		return ErrInvalid
	}
	for _, totals := range []map[string]string{counter.MoneyUsed, counter.MoneyHeld} {
		for currency, units := range totals {
			if !quotaCurrency.MatchString(currency) {
				return ErrInvalid
			}
			if _, err := storedQuotaUnits(units); err != nil {
				return err
			}
		}
	}
	return nil
}

// QuotaCounterDelta adds or removes one validated contribution with sign +1 or
// -1. It copies maps and returns the unchanged input on failure. Applying a
// delta alone establishes no accounting authority, birth, coverage or reserve.
func QuotaCounterDelta(counter QuotaCounter, contribution QuotaCounterContribution, sign int64) (QuotaCounter, error) {
	if sign != 1 && sign != -1 {
		return counter, ErrInvalid
	}
	if err := ValidateQuotaCounter(counter); err != nil {
		return counter, err
	}
	if err := validQuotaBound(contribution.Bound); err != nil {
		return counter, err
	}
	if err := validQuotaSettlement(contribution.Actual); err != nil {
		return counter, err
	}
	next := counter
	next.MoneyUsed = copyQuotaCounterMoney(counter.MoneyUsed)
	next.MoneyHeld = copyQuotaCounterMoney(counter.MoneyHeld)
	if err := applyQuotaCounterDelta(&next, contribution.Bound, contribution.Actual, contribution.Settled, sign); err != nil {
		return counter, err
	}
	return next, nil
}

func copyQuotaCounterMoney(totals map[string]string) map[string]string {
	copied := make(map[string]string, len(totals))
	for currency, units := range totals {
		copied[currency] = units
	}
	return copied
}

// Local storage already validated its projection and receipt. Keep the original
// arithmetic and error order; SQL consumers use the validated public wrapper.
func applyQuotaCounterDelta(counter *QuotaCounter, bound QuotaBound, actual QuotaSettlement, settled bool, sign int64) error {
	var err error
	tokens, money, currency := bound.Tokens, bound.Money, bound.Currency
	tokenSettled, moneySettled := false, false
	if settled {
		if actual.Tokens != nil {
			tokens = actual.Tokens
			tokenSettled = true
		}
		if actual.Money != nil {
			money = actual.Money
			currency = actual.Currency
			moneySettled = true
		}
	}
	tokenTarget := &counter.TokensHeld
	if tokenSettled {
		tokenTarget = &counter.TokensUsed
	}
	if tokens == nil {
		err = addToken(&counter.TokensUnknown, 1, sign)
	} else {
		err = addToken(tokenTarget, *tokens, sign)
	}
	if err != nil {
		return err
	}
	moneyTarget := counter.MoneyHeld
	if moneySettled {
		moneyTarget = counter.MoneyUsed
	}
	if money == nil {
		err = addToken(&counter.MoneyUnknown, 1, sign)
	} else {
		err = addMoney(moneyTarget, currency, *money, sign)
	}
	if err != nil {
		return err
	}
	return nil
}
