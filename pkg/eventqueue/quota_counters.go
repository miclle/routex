package eventqueue

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

func counterKey(account, window string) []byte { return []byte(account + "\x00" + window) }
func monthWindow(start int64) string           { return fmt.Sprintf("month_%d", start) }
func readQuotaCounter(tx *bolt.Tx, account, window string) (quotaCounter, error) {
	counter := quotaCounter{MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
	raw := tx.Bucket(quotaCounterBucket).Get(counterKey(account, window))
	if raw != nil && json.Unmarshal(raw, &counter) != nil {
		return counter, ErrInvalid
	}
	if counter.MoneyUsed == nil || counter.MoneyHeld == nil || counter.TokensUsed < 0 || counter.TokensHeld < 0 || counter.TokensUnknown < 0 || counter.MoneyUnknown < 0 {
		return counter, ErrInvalid
	}
	for _, totals := range []map[string]string{counter.MoneyUsed, counter.MoneyHeld} {
		for currency, raw := range totals {
			amount, ok := new(big.Int).SetString(raw, 10)
			if !quotaCurrency.MatchString(currency) || !ok || amount.Sign() < 0 || amount.String() != raw {
				return counter, ErrInvalid
			}
		}
	}
	return counter, nil
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
func applyQuotaCounter(tx *bolt.Tx, account, window string, entry QuotaReceipt, sign int64) error {
	counter, err := readQuotaCounter(tx, account, window)
	if err != nil {
		return err
	}
	tokens, money, currency := entry.Bound.Tokens, entry.Bound.Money, entry.Bound.Currency
	tokenSettled, moneySettled := false, false
	if entry.State != "active" {
		if entry.Actual.Tokens != nil {
			tokens = entry.Actual.Tokens
			tokenSettled = true
		}
		if entry.Actual.Money != nil {
			money = entry.Actual.Money
			currency = entry.Actual.Currency
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
	encoded, err := json.Marshal(counter)
	if err != nil {
		return ErrInvalid
	}
	if counter.TokensUsed == 0 && counter.TokensHeld == 0 && counter.TokensUnknown == 0 && counter.MoneyUnknown == 0 && len(counter.MoneyUsed) == 0 && len(counter.MoneyHeld) == 0 {
		return tx.Bucket(quotaCounterBucket).Delete(counterKey(account, window))
	}
	return tx.Bucket(quotaCounterBucket).Put(counterKey(account, window), encoded)
}
func quotaCounterView(counter quotaCounter) QuotaUsage {
	result := QuotaUsage{TokensUsed: counter.TokensUsed, TokensHeld: counter.TokensHeld, TokensUnknown: counter.TokensUnknown, MoneyUnknown: counter.MoneyUnknown, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
	for currency, raw := range counter.MoneyUsed {
		units, _ := new(big.Int).SetString(raw, 10)
		result.MoneyUsed[currency] = quotaDecimalValue(units)
	}
	for currency, raw := range counter.MoneyHeld {
		units, _ := new(big.Int).SetString(raw, 10)
		result.MoneyHeld[currency] = quotaDecimalValue(units)
	}
	return result
}
func expiryKey(instant int64, kind byte, identity string) []byte {
	key := make([]byte, 9)
	binary.BigEndian.PutUint64(key, uint64(instant))
	key[8] = kind
	return append(key, identity...)
}
func quotaWindowEnds(entry QuotaReceipt) map[string]int64 {
	return map[string]int64{"minute": entry.AdmittedAt + int64(time.Minute), "five_hours": entry.AdmittedAt + int64(5*time.Hour), "seven_days": entry.AdmittedAt + int64(7*24*time.Hour), monthWindow(entry.MonthStart): entry.MonthEnd}
}
func projectTerminalQuota(tx *bolt.Tx, entry QuotaReceipt, instant int64) error {
	latest := entry.MonthEnd
	for window, end := range quotaWindowEnds(entry) {
		if end < entry.AdmittedAt {
			return ErrInvalid
		}
		if end > latest {
			latest = end
		}
		if end <= instant {
			continue
		}
		for _, account := range entry.Accounts {
			if err := applyQuotaCounter(tx, account.Account, window, entry, 1); err != nil {
				return err
			}
			if err := tx.Bucket(quotaExpiryBucket).Put(expiryKey(end, 'c', account.Account+"\x00"+window+"\x00"+entry.RequestID), []byte{}); err != nil {
				return err
			}
		}
	}
	return tx.Bucket(quotaExpiryBucket).Put(expiryKey(latest, 'r', entry.RequestID), []byte{})
}
func pruneQuota(tx *bolt.Tx, instant int64) error {
	entries := tx.Bucket(quotaEntryBucket)
	cursor := tx.Bucket(quotaExpiryBucket).Cursor()
	for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
		if len(key) < 10 {
			return ErrInvalid
		}
		if int64(binary.BigEndian.Uint64(key[:8])) > instant {
			break
		}
		switch key[8] {
		case 'c':
			parts := strings.Split(string(key[9:]), "\x00")
			if len(parts) != 3 {
				return ErrInvalid
			}
			entry, err := readQuotaReceipt(tx, parts[2])
			if err != nil {
				return err
			}
			if err := applyQuotaCounter(tx, parts[0], parts[1], entry, -1); err != nil {
				return err
			}
		case 'r':
			id := key[9:]
			if tx.Bucket(readyBucket).Get(id) != nil || tx.Bucket(pendingBucket).Get(id) != nil {
				continue
			}
			if entries.Get(id) == nil || entries.Sequence() == 0 {
				return ErrInvalid
			}
			if err := entries.Delete(id); err != nil {
				return err
			}
			if err := entries.SetSequence(entries.Sequence() - 1); err != nil {
				return err
			}
		default:
			return ErrInvalid
		}
		if err := cursor.Delete(); err != nil {
			return err
		}
	}
	return nil
}
