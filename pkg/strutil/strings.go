// Package strutil collects pure string helpers that are not tied to any
// business domain.
package strutil

import "strings"

// FirstNonEmpty returns the first string whose trimmed form is non-empty.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
