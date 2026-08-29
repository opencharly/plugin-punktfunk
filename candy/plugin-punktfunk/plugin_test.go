package punktfunk

import (
	"strings"
	"testing"

	"github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk/params"
)

// TestResolveCallMapsEveryDeclaredMethod is the gate that keeps the CUE method enum and
// the Go dispatch table in step. The enum is the authoring contract — a method the
// schema accepts but resolveCall rejects would validate at load time and then fail at
// run time, which is the worst possible split. Every method named here must appear in
// schema/punktfunk.cue's `method:` disjunction and vice versa.
func TestResolveCallMapsEveryDeclaredMethod(t *testing.T) {
	// Minimal inputs that satisfy each method's required fields.
	cases := map[string]params.PunktfunkInput{
		"health":              {},
		"status":              {},
		"diagnostics":         {},
		"diagnostics-refresh": {},
		"compositors":         {},
		"gpus":                {},
		"plugins":             {},
		"hooks":               {},
		"pair-status":         {},
		"pair-arm":            {},
		"pair-disarm":         {},
		"pending":             {},
		"approve":             {PendingID: "dev-1"},
		"deny":                {PendingID: "dev-1"},
		"pin":                 {Pin: "1234"},
		"clients":             {},
		"unpair":              {Fingerprint: "ab:cd"},
		"rename":              {Fingerprint: "ab:cd", Name: "couch"},
		"access":              {Fingerprint: "ab:cd", Access: "view"},
		"library":             {},
		"scanners":            {},
		"scanner-toggle":      {ProviderID: "steam", Enabled: true},
		"display-state":       {},
		"display-monitors":    {},
		"display-settings":    {},
		"display-release":     {},
		"actions":             {},
		"action-invoke":       {ActionID: "power.sleep"},
		"end-game":            {},
		"events":              {},
	}
	for method, in := range cases {
		in.Method = method
		call, err := resolveCall(&in)
		if err != nil {
			t.Errorf("resolveCall(%q) returned error: %v", method, err)
			continue
		}
		if call.HTTPMethod == "" || call.Path == "" {
			t.Errorf("resolveCall(%q) produced an incomplete call: %+v", method, call)
		}
		if !strings.HasPrefix(call.Path, "/api/v1/") {
			t.Errorf("resolveCall(%q) path %q is not under /api/v1/", method, call.Path)
		}
	}
	if got, want := len(cases), countSchemaMethods(t); got != want {
		t.Errorf("test covers %d methods but schema/punktfunk.cue declares %d — "+
			"the enum and the dispatch table have drifted", got, want)
	}
}

// countSchemaMethods reads the method disjunction straight out of the embedded CUE so
// the test fails when someone adds a method to one side only.
func countSchemaMethods(t *testing.T) int {
	t.Helper()
	b, err := schemaFS.ReadFile("schema/punktfunk.cue")
	if err != nil {
		t.Fatalf("read embedded schema: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "\tmethod: ")
	if start < 0 {
		t.Fatal("schema has no `method:` field")
	}
	// The disjunction runs to the first blank line after it.
	rest := src[start:]
	end := strings.Index(rest, "\n\n")
	if end < 0 {
		t.Fatal("could not delimit the method disjunction")
	}
	return strings.Count(rest[:end], "\"") / 2
}

// TestMutatingMethodsAreMarked pins the read/write split. A method wrongly marked
// read-only could be authored as a `check:` step, which would let a probe unpair a
// device or reboot the host — the provider refuses mutating methods in check steps,
// but only if this classification is right.
func TestMutatingMethodsAreMarked(t *testing.T) {
	mutating := []string{
		"diagnostics-refresh", "pair-arm", "pair-disarm", "approve", "deny", "pin",
		"unpair", "rename", "access", "scanner-toggle", "display-release",
		"action-invoke", "end-game",
	}
	readOnly := []string{
		"health", "status", "diagnostics", "compositors", "gpus", "plugins", "hooks",
		"pair-status", "pending", "clients", "library", "scanners",
		"display-state", "display-monitors", "display-settings", "actions", "events",
	}
	for _, m := range mutating {
		in := params.PunktfunkInput{Method: m, PendingID: "d", Fingerprint: "f",
			Name: "n", Access: "view", ActionID: "a", ProviderID: "p", Pin: "1"}
		call, err := resolveCall(&in)
		if err != nil {
			t.Fatalf("resolveCall(%q): %v", m, err)
		}
		if !call.Mutating {
			t.Errorf("method %q changes host state but is not marked Mutating", m)
		}
	}
	for _, m := range readOnly {
		in := params.PunktfunkInput{Method: m}
		call, err := resolveCall(&in)
		if err != nil {
			t.Fatalf("resolveCall(%q): %v", m, err)
		}
		if call.Mutating {
			t.Errorf("method %q is read-only but is marked Mutating", m)
		}
	}
}

// TestRequiredFieldsAreEnforced — a missing id must fail with an authoring error, not
// sail through and produce a confusing 404 from the host.
func TestRequiredFieldsAreEnforced(t *testing.T) {
	for method, want := range map[string]string{
		"approve":        "pending_id",
		"deny":           "pending_id",
		"unpair":         "fingerprint",
		"action-invoke":  "action_id",
		"scanner-toggle": "provider_id",
		"pin":            "pin",
	} {
		in := params.PunktfunkInput{Method: method}
		if _, err := resolveCall(&in); err == nil {
			t.Errorf("resolveCall(%q) with no %s should fail", method, want)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("resolveCall(%q) error %q should name the missing field %q", method, err, want)
		}
	}
	// rename and access need a second field beyond the fingerprint.
	if _, err := resolveCall(&params.PunktfunkInput{Method: "rename", Fingerprint: "f"}); err == nil {
		t.Error("rename without `name` should fail")
	}
	if _, err := resolveCall(&params.PunktfunkInput{Method: "access", Fingerprint: "f"}); err == nil {
		t.Error("access without `access` should fail")
	}
}

// TestUnknownMethodFails guards the default branch.
func TestUnknownMethodFails(t *testing.T) {
	if _, err := resolveCall(&params.PunktfunkInput{Method: "reboot-the-planet"}); err == nil {
		t.Fatal("an unknown method must fail")
	}
}

// TestIdentifiersArePathEscaped — a fingerprint is attacker-influenced text from a
// pairing request, so it must never be able to walk out of its path segment.
func TestIdentifiersArePathEscaped(t *testing.T) {
	in := params.PunktfunkInput{Method: "unpair", Fingerprint: "../../api/v1/clients"}
	call, err := resolveCall(&in)
	if err != nil {
		t.Fatalf("resolveCall: %v", err)
	}
	if strings.Contains(call.Path, "../") {
		t.Errorf("fingerprint was not path-escaped: %q", call.Path)
	}
}

func TestParseSSE(t *testing.T) {
	body := "" +
		": comment\n" +
		"event: stream.started\n" +
		"id: 7\n" +
		"data: {\"kind\":\"stream.started\"}\n" +
		"\n" +
		"data: {\"kind\":\n" +
		"data: \"stream.stopped\"}\n" +
		"\n"
	got := parseSSE(body)
	if len(got) != 2 {
		t.Fatalf("wanted 2 events, got %d: %q", len(got), got)
	}
	if got[0] != `{"kind":"stream.started"}` {
		t.Errorf("first event = %q", got[0])
	}
	// A payload split across data: lines rejoins with a newline, per the SSE spec.
	if got[1] != "{\"kind\":\n\"stream.stopped\"}" {
		t.Errorf("multi-line event = %q", got[1])
	}
}

func TestExtractJSONPath(t *testing.T) {
	body := []byte(`{"host":{"name":"deck","codecs":["h264","av1"]},"sessions":2,"paired":true}`)
	for path, want := range map[string]string{
		"host.name":     "deck",
		"host.codecs.1": "av1",
		"sessions":      "2",
		"paired":        "true",
	} {
		got, err := extractJSONPath(body, path)
		if err != nil {
			t.Errorf("extractJSONPath(%q): %v", path, err)
			continue
		}
		if got != want {
			t.Errorf("extractJSONPath(%q) = %q, want %q", path, got, want)
		}
	}
	for _, bad := range []string{"host.missing", "host.codecs.9", "sessions.name"} {
		if _, err := extractJSONPath(body, bad); err == nil {
			t.Errorf("extractJSONPath(%q) should fail", bad)
		}
	}
	if _, err := extractJSONPath([]byte("not json"), "a"); err == nil {
		t.Error("a non-JSON body should fail")
	}
}
