// Package id provides helpers for application identifiers.
package id

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewPrefixed returns a lower-case ULID with the given prefix, such as
// "usr_01hz...".
func NewPrefixed(prefix string) (string, error) {
	value, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + strings.ToLower(value.String()), nil
}
