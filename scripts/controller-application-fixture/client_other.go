//go:build !linux

package main

import (
	"encoding/json"
	"io"
)

func launchClaude(out io.Writer) int {
	_ = json.NewEncoder(out).Encode(claudeResult{Result: "client_binding_invalid", Native: "unknown"})
	return 1
}
