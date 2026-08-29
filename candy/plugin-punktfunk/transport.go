package punktfunk

import (
	"context"
	"fmt"
	"strings"
)

// transport.go issues the management-API request FROM INSIDE THE VENUE, over the
// reverse channel, rather than from the charly host's network namespace.
//
// That is a deliberate design decision, not a convenience. punktfunk binds its
// management API to loopback by default (PUNKTFUNK_MGMT_BIND=127.0.0.1:47990) and its
// own documentation is explicit that the API "stays loopback-only" while the web
// console is the network-facing surface. A host-vantage request (cc.HTTPDo) cannot
// reach a loopback-bound listener inside a container, and the only ways to make it
// reachable — republishing the port, or a socat eth0→loopback relay — would weaken
// exactly the posture upstream chose. Probing from inside the venue keeps the host's
// security model intact and puts the request on the SAME side as the bearer token,
// which is already read in-venue because that is where it is generated.
//
// The cost is a `curl` dependency in the venue, which is why a missing curl is
// reported as a precise, actionable message rather than a generic transport error.

// venueResponse is the outcome of an in-venue request: the body plus the HTTP status
// curl reported, kept separate so the caller can distinguish a transport failure from
// a non-2xx answer.
type venueResponse struct {
	Status int
	Body   string
}

// curlRequest runs one management-API call inside the venue.
//
// The bearer token is passed through curl's --config on STDIN, never on the command
// line: argv is world-readable via /proc, and a management token in `ps` output would
// be a real leak on a shared host.
func curlRequest(ctx context.Context, exec venueExec, url, httpMethod, token, body string,
	verifyTLS bool, timeoutSecs int) (venueResponse, error) {

	var cfg strings.Builder
	if token != "" {
		// curl --config quoting: the value is a double-quoted string, so a quote or
		// backslash in the token has to be escaped. Tokens are hex in practice, but
		// escaping keeps a rotated format from silently corrupting the header.
		cfg.WriteString(fmt.Sprintf("header = \"Authorization: Bearer %s\"\n", cfgEscape(token)))
	}
	cfg.WriteString("header = \"Accept: application/json\"\n")
	if body != "" {
		cfg.WriteString("header = \"Content-Type: application/json\"\n")
	}

	args := []string{"curl", "--silent", "--show-error"}
	if !verifyTLS {
		// The management API serves a self-signed certificate; see #PunktfunkInput.verify_tls.
		args = append(args, "--insecure")
	}
	args = append(args,
		"--request", shellQuote(httpMethod),
		"--max-time", fmt.Sprintf("%d", timeoutSecs),
		// Emit the status on its own trailing line so it can be split off the body
		// without needing curl's --write-out to be parsed out of the payload.
		"--write-out", shellQuote("\\n%{http_code}"),
		"--config", "-",
	)
	if body != "" {
		args = append(args, "--data-binary", shellQuote(body))
	}
	args = append(args, shellQuote(url))

	// The config arrives on stdin via a heredoc with a QUOTED delimiter, so the shell
	// performs no expansion on the token.
	script := fmt.Sprintf("%s <<'CHARLY_PF_CFG'\n%sCHARLY_PF_CFG\n",
		strings.Join(args, " "), cfg.String())

	stdout, stderr, exitCode, err := exec.RunCapture(ctx, script)
	if err != nil {
		return venueResponse{}, fmt.Errorf("in-venue request failed: %v", err)
	}
	if exitCode != 0 {
		if strings.Contains(stderr, "not found") || strings.Contains(stderr, "command not found") {
			return venueResponse{}, fmt.Errorf(
				"curl is not available in the venue, and the punktfunk verb probes from "+
					"inside it because the management API is loopback-only: %s", strings.TrimSpace(stderr))
		}
		return venueResponse{}, fmt.Errorf("curl exit %d%s", exitCode, trailer(stderr))
	}
	return splitStatus(stdout)
}

// splitStatus peels the trailing status line --write-out appended.
func splitStatus(out string) (venueResponse, error) {
	trimmed := strings.TrimRight(out, "\n")
	idx := strings.LastIndex(trimmed, "\n")
	statusPart := trimmed
	bodyPart := ""
	if idx >= 0 {
		statusPart = trimmed[idx+1:]
		bodyPart = trimmed[:idx]
	}
	var status int
	if _, err := fmt.Sscanf(strings.TrimSpace(statusPart), "%d", &status); err != nil {
		return venueResponse{}, fmt.Errorf("could not read HTTP status from curl output %q", statusPart)
	}
	return venueResponse{Status: status, Body: bodyPart}, nil
}

// cfgEscape escapes a value for curl's --config double-quoted string syntax.
func cfgEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// shellQuote single-quotes a value for the venue shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// venueExec is the slice of the executor this file needs, declared as its own
// interface so the transport is unit-testable with a fake.
type venueExec interface {
	RunCapture(ctx context.Context, script string) (stdout, stderr string, exit int, err error)
}
