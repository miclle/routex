package eventqueue

// QuotaCounterUsage converts validated integer-unit counters to detached public
// decimal values. The caller owns coherent sampling, account identity, window
// expiry and coverage; conversion establishes none of those facts.
func QuotaCounterUsage(counter QuotaCounter) (QuotaUsage, error) {
	if err := ValidateQuotaCounter(counter); err != nil {
		return QuotaUsage{}, err
	}
	return quotaCounterView(counter), nil
}
