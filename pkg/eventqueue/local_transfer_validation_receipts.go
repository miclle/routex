package eventqueue

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type localTransferSemanticState struct {
	result   *LocalTransferValidation
	bounds   LocalTransferBounds
	buckets  map[string]LocalTransferBucket
	accounts map[string]struct{}
	births   map[string]int64
	receipts map[string]QuotaReceipt
	markers  map[string]string
}

func validateLocalTransferSemantics(ctx context.Context, result *LocalTransferValidation, bounds LocalTransferBounds) error {
	state := &localTransferSemanticState{
		result: result, bounds: bounds, buckets: map[string]LocalTransferBucket{},
		accounts: map[string]struct{}{}, births: map[string]int64{},
		receipts: map[string]QuotaReceipt{}, markers: map[string]string{},
	}
	for _, bucket := range result.Raw {
		state.buckets[string(bucket.Name)] = bucket
	}
	if err := state.validateMetadata(); err != nil {
		return err
	}
	for _, name := range []string{"pending-v1", "ready-v1", "leases-v1"} {
		if len(state.buckets[name].Entries) != 0 {
			return ErrLocalTransferNotDrained
		}
	}
	if err := state.validateBirths(ctx); err != nil {
		return err
	}
	if err := state.validateMarkers(ctx); err != nil {
		return err
	}
	if err := state.validateReceipts(ctx); err != nil {
		return err
	}
	if len(state.buckets["active-counts-v1"].Entries) != 0 {
		return ErrInvalid
	}
	if err := state.validateQuotaProjections(ctx); err != nil {
		return err
	}
	return state.validateRPMProjections(ctx)
}

func (state *localTransferSemanticState) reference() error {
	if state.result.ReferencesRead >= state.bounds.References {
		return ErrLocalTransferBound
	}
	state.result.ReferencesRead++
	return nil
}

func (state *localTransferSemanticState) account(name string) error {
	if !validKey.MatchString(name) {
		return ErrInvalid
	}
	if _, exists := state.accounts[name]; !exists {
		if len(state.accounts) >= state.bounds.Accounts {
			return ErrLocalTransferBound
		}
		state.accounts[name] = struct{}{}
	}
	return nil
}

func (state *localTransferSemanticState) validateMetadata() error {
	// Keep this historical namespace independent of future runtime additions.
	names := []string{"pending-v1", "ready-v1", "rpm-v1", "leases-v1", "rpm-counts-v1", "active-counts-v1", "limit-meta-v1", "quota-entries-v2", "quota-counters-v2", "quota-expiry-v2", "quota-accounts-v2", "quota-invalid-bounds-v2"}
	if len(state.buckets) != len(names) || len(state.result.Raw) != len(names) {
		return ErrInvalid
	}
	for _, name := range names {
		if _, exists := state.buckets[name]; !exists {
			return ErrInvalid
		}
	}
	metadata := state.buckets["limit-meta-v1"].Entries
	header := state.result.Header
	if len(metadata) != len(header.Metadata) || header.ClockHighWater == nil || header.Quota == nil {
		return ErrInvalid
	}
	for index, entry := range metadata {
		if !bytes.Equal(entry.Key, header.Metadata[index].Key) || !bytes.Equal(entry.Value, header.Metadata[index].Value) {
			return ErrInvalid
		}
		switch string(entry.Key) {
		case "version":
			if string(entry.Value) != "2" {
				return ErrInvalid
			}
		case "clock":
			if len(entry.Value) != 8 || binary.BigEndian.Uint64(entry.Value) != uint64(*header.ClockHighWater) {
				return ErrInvalid
			}
		case "installation":
			if !validKey.Match(entry.Value) || string(entry.Value) != header.InstallationID {
				return ErrInvalid
			}
		case "quota":
			canonical, err := json.Marshal(header.Quota)
			if err != nil || !bytes.Equal(canonical, entry.Value) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

func (state *localTransferSemanticState) validateBirths(ctx context.Context) error {
	for _, raw := range state.buckets["quota-accounts-v2"].Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := string(raw.Key)
		if err := state.account(name); err != nil {
			return err
		}
		birth, err := strconv.ParseInt(string(raw.Value), 10, 64)
		if err != nil || birth < 0 || birth > *state.result.Header.ClockHighWater || strconv.FormatInt(birth, 10) != string(raw.Value) {
			return ErrInvalid
		}
		state.births[name] = birth
		state.result.AccountBirths = append(state.result.AccountBirths, LocalTransferAccountBirth{Account: name, RecordedUnixNano: birth})
	}
	return nil
}

func (state *localTransferSemanticState) validateMarkers(ctx context.Context) error {
	for _, raw := range state.buckets["quota-invalid-bounds-v2"].Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validKey.Match(raw.Key) || !validKey.Match(raw.Value) {
			return ErrInvalid
		}
		if err := state.reference(); err != nil {
			return err
		}
		// A sticky invalidation survives pruning and request ID reuse. Its
		// request ID alone cannot identify a retained receipt's incarnation.
		state.markers[string(raw.Key)] = string(raw.Value)
	}
	return nil
}

func (state *localTransferSemanticState) validateReceipts(ctx context.Context) error {
	bucket := state.buckets["quota-entries-v2"]
	if bucket.Sequence != uint64(len(bucket.Entries)) || bucket.Sequence > quotaHistoryCapacity {
		return ErrInvalid
	}
	for _, raw := range bucket.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		var receipt QuotaReceipt
		if localTransferDecodeJSON(raw.Value, &receipt) != nil || string(raw.Key) != receipt.RequestID || !validKey.Match(raw.Key) || receipt.AdmittedAt < state.result.Header.Quota.CoverageStart || receipt.AdmittedAt > *state.result.Header.ClockHighWater || receipt.AdmittedAt > math.MaxInt64-int64(7*24*time.Hour) || len(receipt.Accounts) < 1 || len(receipt.Accounts) > 8 {
			return ErrInvalid
		}
		start, end, err := QuotaMonthBounds(receipt.AdmittedAt, state.result.Header.Quota.TimeZone)
		if err != nil || start != receipt.MonthStart || end != receipt.MonthEnd {
			return ErrInvalid
		}
		if validQuotaBound(receipt.Bound) != nil || validQuotaSettlement(receipt.Actual) != nil || validQuotaSettlement(receipt.Recovery) != nil || validQuotaRecovery(receipt.Bound, receipt.Recovery) != nil {
			return ErrInvalid
		}
		seen := map[string]struct{}{}
		for _, account := range receipt.Accounts {
			birth, exists := state.births[account.Account]
			_, repeated := seen[account.Account]
			if !exists || birth > receipt.AdmittedAt || repeated || !validKey.MatchString(account.Revision) {
				return ErrInvalid
			}
			if err := state.reference(); err != nil {
				return err
			}
			seen[account.Account] = struct{}{}
		}
		switch receipt.State {
		case "active":
			return ErrLocalTransferNotDrained
		case "complete", "interrupted":
			if receipt.Recovery.Tokens != nil || receipt.Recovery.Money != nil || receipt.Recovery.Currency != "" || !quotaDigest.MatchString(receipt.SettlementDigest) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if receipt.Actual.Money != nil && receipt.Bound.Money != nil && receipt.Actual.Currency != receipt.Bound.Currency || receipt.Overrun != localTransferOverrun(receipt) {
			return ErrInvalid
		}
		if receipt.Overrun && receipt.Bound.Revision != "" {
			if _, exists := state.markers[receipt.Bound.Revision]; !exists {
				return ErrInvalid
			}
		}
		state.receipts[receipt.RequestID] = receipt
		state.result.UnverifiedSettlementDigests++
	}
	return nil
}

func localTransferOverrun(receipt QuotaReceipt) bool {
	if receipt.Bound.Tokens != nil && receipt.Actual.Tokens != nil && *receipt.Actual.Tokens > *receipt.Bound.Tokens {
		return true
	}
	if receipt.Bound.Money != nil && receipt.Actual.Money != nil {
		bound, _ := quotaUnits(*receipt.Bound.Money)
		actual, _ := quotaUnits(*receipt.Actual.Money)
		return actual.Cmp(bound) > 0
	}
	return false
}

// Preserve original receipt encoding, including omitted legacy optional fields.
// Reject duplicate/case-alias fields, unknown fields, extra JSON and deep input;
// never re-encode a receipt to manufacture canonical historical bytes.
func localTransferDecodeJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := localTransferJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalid
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

func localTransferJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			token, err := decoder.Token()
			key, valid := token.(string)
			if err != nil || !valid {
				return ErrInvalid
			}
			folded := strings.ToLower(key)
			if _, duplicate := seen[folded]; duplicate {
				return ErrInvalid
			}
			seen[folded] = struct{}{}
			if err := localTransferJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := localTransferJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	end, err := decoder.Token()
	if err != nil || delimiter == '{' && end != json.Delim('}') || delimiter == '[' && end != json.Delim(']') {
		return ErrInvalid
	}
	return nil
}

func localTransferSortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
