package punktfunk

import (
	"context"
	"strings"
	"testing"
)

// TestBodylessWriteSendsJSON is the regression guard for the mutating half of the verb never
// having run against a real host.
//
// Observed live against punktfunk 0.33.0-1, POST /api/v1/native/pair/arm:
//
//	no Content-Type          -> {"error":"Expected request with `Content-Type: application/json`"}
//	Content-Type, no body    -> {"error":"Failed to parse the request body as JSON: EOF …"}
//	Content-Type and `{}`    -> {"enabled":true,"armed":true,"pin":"…",…}
//
// curlRequest only emits Content-Type when a body is present, so a bodyless write sent
// neither — and every mutating method (pair-arm, approve, deny, end-game, diagnostics-refresh,
// action-invoke, scanner-toggle, display-release) was rejected. Every check shipped before
// this called a READ method, which is why it stayed invisible.
func TestBodylessWriteSendsJSON(t *testing.T) {
	fe := &fakeExec{stdout: "{}\n200"}
	if _, err := curlRequest(context.Background(), fe, "https://127.0.0.1:47990/api/v1/native/pair/arm",
		"POST", "tok", emptyJSONBody, false, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fe.script, "Content-Type: application/json") {
		t.Errorf("a write must carry Content-Type; script:\n%s", fe.script)
	}
	if !strings.Contains(fe.script, emptyJSONBody) {
		t.Errorf("a write must carry a parseable JSON body; script:\n%s", fe.script)
	}
}

// A read must NOT acquire a body or a Content-Type it never had — the fix is scoped to writes.
func TestReadStaysBodyless(t *testing.T) {
	fe := &fakeExec{stdout: "{}\n200"}
	if _, err := curlRequest(context.Background(), fe, "https://127.0.0.1:47990/api/v1/health",
		"GET", "tok", "", false, 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fe.script, "Content-Type") {
		t.Errorf("a read must not send Content-Type; script:\n%s", fe.script)
	}
}

// The provider's DECISION, not just the transport's obedience: a mutating call authored with
// no body must acquire one. Without this the earlier guard passed while production still sent
// nothing, because the test handed curlRequest a body the provider never supplied.
func TestApplyDefaultWriteBody(t *testing.T) {
	cases := []struct {
		name string
		in   apiCall
		want string
	}{
		{"bodyless POST gets {}", apiCall{HTTPMethod: "POST", Mutating: true}, emptyJSONBody},
		{"authored body is kept", apiCall{HTTPMethod: "POST", Mutating: true, Body: `{"pin":"1"}`}, `{"pin":"1"}`},
		{"DELETE stays bodyless", apiCall{HTTPMethod: "DELETE", Mutating: true}, ""},
		{"read stays bodyless", apiCall{HTTPMethod: "GET"}, ""},
	}
	for _, c := range cases {
		got := c.in
		applyDefaultWriteBody(&got)
		if got.Body != c.want {
			t.Errorf("%s: body = %q, want %q", c.name, got.Body, c.want)
		}
	}
}
