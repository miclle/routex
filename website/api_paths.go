package website

import "strings"

// Native API paths must never fall back to the SPA or development proxy.
func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api") ||
		path == "/v1" || strings.HasPrefix(path, "/v1/") ||
		path == "/v1beta" || strings.HasPrefix(path, "/v1beta/")
}
