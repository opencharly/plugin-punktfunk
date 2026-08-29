package punktfunk

import (
	"regexp"
	"strings"
)

// token.go parses punktfunk's management-token file.
//
// The file is NOT a bare token, which is what its documentation ("generated and
// persisted API token … path=~/.config/punktfunk/mgmt-token") suggests. Measured
// against punktfunk-host 0.33.0-1, it is an ENV-FILE LINE:
//
//	PUNKTFUNK_MGMT_TOKEN=<64-hex-token>
//
// Sending the whole line as the bearer yields a clean HTTP 401
// ("missing or invalid credentials"), which is exactly what a live bed run caught and
// no amount of schema validation would have.
//
// Both shapes are accepted so the plugin keeps working if upstream ever simplifies the
// file, and a KEY=VALUE line naming the management token is preferred over any other
// key in case the file grows companions (the host writes a sibling `plugin-token`).

// envKeyPattern is a shell-style environment variable name.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// preferredTokenKey is the key punktfunk writes the management token under.
const preferredTokenKey = "PUNKTFUNK_MGMT_TOKEN"

// parseTokenFile extracts the bearer token from the contents of a token file,
// returning "" when nothing usable is present.
func parseTokenFile(content string) string {
	var fallback string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, isPair := strings.Cut(line, "=")
		if isPair && envKeyPattern.MatchString(strings.TrimSpace(key)) {
			v := unquote(strings.TrimSpace(value))
			if v == "" {
				continue
			}
			if strings.TrimSpace(key) == preferredTokenKey {
				return v // an explicit match wins immediately
			}
			if fallback == "" {
				fallback = v
			}
			continue
		}
		// Not a KEY=VALUE line: treat it as a bare token.
		if fallback == "" {
			fallback = unquote(line)
		}
	}
	return fallback
}

// unquote strips one layer of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
