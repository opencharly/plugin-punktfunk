package punktfunk

import (
	"context"
	"strings"
	"testing"
)

// fakeExec records the script it was handed so the tests can assert on exactly what
// would run inside the venue.
type fakeExec struct {
	script string
	stdout string
	stderr string
	exit   int
}

func (f *fakeExec) RunCapture(_ context.Context, script string) (string, string, int, error) {
	f.script = script
	return f.stdout, f.stderr, f.exit, nil
}

// TestTokenNeverReachesArgv is a security property, not a formatting preference: argv
// is world-readable through /proc, so a management token passed as a curl flag would
// leak to every user on a shared host. It must travel via --config on stdin.
func TestTokenNeverReachesArgv(t *testing.T) {
	const secret = "s3cr3t-mgmt-token"
	fe := &fakeExec{stdout: "{}\n200"}
	if _, err := curlRequest(context.Background(), fe,
		"https://127.0.0.1:47990/api/v1/host", "GET", secret, "", false, 30); err != nil {
		t.Fatalf("curlRequest: %v", err)
	}
	cmdLine := fe.script
	if i := strings.Index(cmdLine, "<<'CHARLY_PF_CFG'"); i >= 0 {
		cmdLine = cmdLine[:i] // the argv portion, before the heredoc payload
	}
	if strings.Contains(cmdLine, secret) {
		t.Fatalf("token leaked into the command line: %q", cmdLine)
	}
	if !strings.Contains(fe.script, "--config -") {
		t.Error("expected the token to be delivered via `--config -`")
	}
	if !strings.Contains(fe.script, "Authorization: Bearer "+secret) {
		t.Error("expected the bearer header in the stdin config")
	}
	// A quoted heredoc delimiter is what stops the shell expanding a token containing $.
	if !strings.Contains(fe.script, "<<'CHARLY_PF_CFG'") {
		t.Error("expected a QUOTED heredoc delimiter so the shell does not expand the token")
	}
}

// TestVerifyTLSDefaultsToInsecure — punktfunk serves a self-signed certificate, so the
// zero value must skip verification or every stock host fails.
func TestVerifyTLSDefaultsToInsecure(t *testing.T) {
	fe := &fakeExec{stdout: "{}\n200"}
	_, _ = curlRequest(context.Background(), fe, "https://127.0.0.1:47990/x", "GET", "", "", false, 30)
	if !strings.Contains(fe.script, "--insecure") {
		t.Error("verify_tls unset must produce --insecure (the API is self-signed)")
	}
	fe2 := &fakeExec{stdout: "{}\n200"}
	_, _ = curlRequest(context.Background(), fe2, "https://127.0.0.1:47990/x", "GET", "", "", true, 30)
	if strings.Contains(fe2.script, "--insecure") {
		t.Error("verify_tls: true must NOT pass --insecure")
	}
}

func TestSplitStatus(t *testing.T) {
	r, err := splitStatus("{\"status\":\"ok\"}\n200\n")
	if err != nil {
		t.Fatalf("splitStatus: %v", err)
	}
	if r.Status != 200 {
		t.Errorf("status = %d, want 200", r.Status)
	}
	if r.Body != `{"status":"ok"}` {
		t.Errorf("body = %q", r.Body)
	}
	// A multi-line body must keep its newlines and still yield the trailing status.
	r2, err := splitStatus("line1\nline2\n404\n")
	if err != nil {
		t.Fatalf("splitStatus: %v", err)
	}
	if r2.Status != 404 || r2.Body != "line1\nline2" {
		t.Errorf("multi-line split wrong: %+v", r2)
	}
	if _, err := splitStatus("no status here"); err == nil {
		t.Error("output with no numeric status must fail")
	}
}

// TestMissingCurlIsActionable — the in-venue design makes curl a hard dependency, so
// its absence must say so plainly instead of surfacing a bare exit code.
func TestMissingCurlIsActionable(t *testing.T) {
	fe := &fakeExec{exit: 127, stderr: "sh: line 1: curl: command not found"}
	_, err := curlRequest(context.Background(), fe, "https://127.0.0.1:47990/x", "GET", "", "", false, 30)
	if err == nil {
		t.Fatal("a missing curl must fail")
	}
	if !strings.Contains(err.Error(), "curl is not available") {
		t.Errorf("error should name the missing dependency, got: %v", err)
	}
}

func TestShellQuoteHandlesQuotes(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shellQuote(%q) = %q", "a'b", got)
	}
}

func TestCfgEscape(t *testing.T) {
	if got := cfgEscape(`a"b\c`); got != `a\"b\\c` {
		t.Errorf("cfgEscape = %q", got)
	}
}
