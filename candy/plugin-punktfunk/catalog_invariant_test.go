package punktfunk

// catalog_invariant_test.go pins the invariant the jetkvm and cua siblings keep
// and this plugin needs for the same reason: every method the SCHEMA advertises
// must be reachable — dispatched by resolveCall (the management API) or
// resolveCLICall (the headless client CLI) — and every method either builder
// accepts must be in the schema. Without it a method can sit in the catalog as a
// phantom (advertised but unimplemented): the schema validates the authored step,
// and the step then fails only at dispatch with `unknown method`, so the catalog
// and the skill that documents it actively mislead the agent that dispatches on it.
//
// The tests read the REAL sources — the SERVED enum (through the CUE API,
// schema_served_test.go), methods.go's resolveCall switch, and cli.go's
// cliMethods set — rather than restating any of them, so they cannot drift
// alongside the code they guard.

import (
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk/params"
)

// schemaMethods returns the sorted method names the SERVED #PunktfunkInput enum
// accepts. It reads the schema through the CUE API rather than its text: the
// enum is now §10.1 S3 (a vocabulary table plus a derived word list), and a text
// parse of the union would find ZERO methods and leave every invariant below
// passing vacuously.
func schemaMethods(t *testing.T) []string {
	t.Helper()
	return schemaWordList(t, servedSchema(t), "#PunktfunkInput.method")
}

// resolveCallMethods returns the host methods resolveCall routes. It reads the
// switch's `case "…":` arms out of methods.go, scoped to the resolveCall
// function so the requireField switch is not mistaken for dispatch.
func resolveCallMethods(t *testing.T) []string {
	t.Helper()
	fn := funcSource(t, "methods.go", `(?s)func resolveCall\(.*?\n\}`)
	if fn == "" {
		t.Fatal("could not locate resolveCall in methods.go")
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([a-z][a-z0-9-]*)":`).FindAllStringSubmatch(fn, -1) {
		seen[m[1]] = true
	}
	return sortedKeys(seen)
}

// cliMethodsFromSource returns the client methods cli.go's cliMethods set
// carries. That map is the ONE branch point isCLIMethod reads.
func cliMethodsFromSource(t *testing.T) []string {
	t.Helper()
	block := funcSource(t, "cli.go", `(?s)var cliMethods = map\[string\]struct\{\}\{(.*?)\n\}`)
	if block == "" {
		t.Fatal("could not locate the cliMethods set in cli.go")
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z][a-z0-9-]*)"`).FindAllStringSubmatch(block, -1) {
		seen[m[1]] = true
	}
	return sortedKeys(seen)
}

func funcSource(t *testing.T, path, pattern string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return regexp.MustCompile(pattern).FindString(string(b))
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// minimalInput fills every field any builder requires, so a method's routing is
// exercised without its authoring preconditions masking a missing arm.
func minimalInput(method string) params.PunktfunkInput {
	return params.PunktfunkInput{
		Method:      method,
		Host:        "host-a",
		PendingID:   "dev-1",
		Fingerprint: "ab:cd",
		ActionID:    "power.sleep",
		ProviderID:  "steam",
		Name:        "couch",
		Access:      "view",
		Pin:         "1234",
		Enabled:     true,
	}
}

// TestServedCatalogCoversTheWholeEnum guards the READER and the served catalog in
// one step: the enum's first and last entries must be present, so neither a
// truncated read nor a table whose tail never reaches the enum can exempt the
// catalog. The old text reader stopped at the first blank line, which dropped the
// entire CLIENT half of the catalog from its count.
func TestServedCatalogCoversTheWholeEnum(t *testing.T) {
	got := schemaMethods(t)
	has := func(name string) bool {
		for _, m := range got {
			if m == name {
				return true
			}
		}
		return false
	}
	if !has("health") {
		t.Errorf("served enum is missing its HEAD (health) — got %d methods: %v", len(got), got)
	}
	if !has("stream-probe") {
		t.Errorf("served enum is missing its TAIL (stream-probe) — the derivation is losing rows; got %d methods: %v", len(got), got)
	}
	if len(got) < 43 {
		t.Errorf("read only %d methods — the served #PunktfunkInput.method derivation looks truncated", len(got))
	}
}

// TestCatalogHasNoPhantomMethods asserts every schema method is dispatched — no
// method is advertised that the plugin cannot act on.
func TestCatalogHasNoPhantomMethods(t *testing.T) {
	reachable := map[string]bool{}
	for _, m := range resolveCallMethods(t) {
		reachable[m] = true
	}
	for _, m := range cliMethodsFromSource(t) {
		reachable[m] = true
	}
	if len(reachable) == 0 {
		t.Fatal("the dispatch surface is empty — the invariant would pass over nothing")
	}
	for _, m := range schemaMethods(t) {
		if !reachable[m] {
			t.Errorf("schema method %q is a PHANTOM: neither resolveCall nor the client CLI routes it — add an implementation or remove it from the schema", m)
		}
	}
}

// TestDispatchHasNoPhantomArms is the other direction: an arm for a word the
// schema no longer accepts is dead code, and it hides a rename that only half
// landed — the arm would keep an unreachable path compiling.
func TestDispatchHasNoPhantomArms(t *testing.T) {
	advertised := map[string]bool{}
	for _, m := range schemaMethods(t) {
		advertised[m] = true
	}
	for _, m := range resolveCallMethods(t) {
		if !advertised[m] {
			t.Errorf("resolveCall routes %q, which the served #PunktfunkInput.method enum does NOT accept — the arm is unreachable", m)
		}
	}
	for _, m := range cliMethodsFromSource(t) {
		if !advertised[m] {
			t.Errorf("the client CLI routes %q, which the served #PunktfunkInput.method enum does NOT accept — the arm is unreachable", m)
		}
	}
}

// TestEverySchemaMethodBuilds is the strongest half: every advertised method must
// resolve through the builder that owns it. A word routed to a builder that
// rejects it is advertised and broken in one step — the exact split this test
// exists to make impossible.
func TestEverySchemaMethodBuilds(t *testing.T) {
	for _, m := range schemaMethods(t) {
		in := minimalInput(m)
		if isCLIMethod(m) {
			if _, err := resolveCLICall(&in); err != nil {
				t.Errorf("schema method %q is routed to the client CLI but resolveCLICall rejects it: %v", m, err)
			}
			continue
		}
		if _, err := resolveCall(&in); err != nil {
			t.Errorf("schema method %q is routed to the management API but resolveCall rejects it: %v", m, err)
		}
	}
}
