package service

import (
	"encoding/json"
	"slices"
	"strings"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const registrationDomainCount = 32
const registrationDomainBytes = 2048

// All-numeric dotted names are excluded lexically, including IP shorthand and
// leading-zero forms; no DNS lookup or IP normalization is performed.
func validRegistrationDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") || strings.Trim(domain, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range []byte(label) {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// Only boundary ASCII spaces and ASCII letter case are normalized. Unicode,
// suffix expansion, wildcard matching and mailbox-provider aliases are absent.
func canonicalRegistrationDomains(input []string) ([]string, error) {
	if input == nil || len(input) > registrationDomainCount {
		return nil, apperrors.ErrBadRequest
	}
	domains := make([]string, len(input))
	for i, value := range input {
		value = strings.Trim(value, " ")
		for _, c := range []byte(value) {
			if c >= 128 {
				return nil, apperrors.ErrBadRequest
			}
		}
		value = strings.ToLower(value)
		if !validRegistrationDomain(value) {
			return nil, apperrors.ErrBadRequest
		}
		domains[i] = value
	}
	slices.Sort(domains)
	if len(slices.Compact(slices.Clone(domains))) != len(domains) {
		return nil, apperrors.ErrBadRequest
	}
	raw, err := json.Marshal(domains)
	if err != nil || len(raw) > registrationDomainBytes {
		return nil, apperrors.ErrBadRequest
	}
	return domains, nil
}
func registrationStoredDomains(raw string) ([]string, error) {
	if len(raw) > registrationDomainBytes {
		return nil, registrationApprovalUnavailable
	}
	var input []string
	if json.Unmarshal([]byte(raw), &input) != nil {
		return nil, registrationApprovalUnavailable
	}
	domains, err := canonicalRegistrationDomains(input)
	if err != nil {
		return nil, registrationApprovalUnavailable
	}
	canonical, _ := json.Marshal(domains)
	if string(canonical) != raw {
		return nil, registrationApprovalUnavailable
	}
	return domains, nil
}
func registrationEmailAllowed(normalizedEmail string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	at := strings.LastIndexByte(normalizedEmail, '@')
	if at < 0 {
		return false
	}
	domain := normalizedEmail[at+1:]
	return validRegistrationDomain(domain) && slices.Contains(domains, domain)
}
func requireRegistrationEmail(settingsRaw, normalizedEmail string) error {
	domains, err := registrationStoredDomains(settingsRaw)
	if err != nil {
		return err
	}
	if !registrationEmailAllowed(normalizedEmail, domains) {
		return apperrors.ErrForbidden
	}
	return nil
}

func registrationDomainsJSON(domains []string) string {
	raw, _ := json.Marshal(domains)
	return string(raw)
}
