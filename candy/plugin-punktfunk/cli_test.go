package punktfunk

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk/params"
)

// TestPinNeverReachesArgv is a security property, not a style preference — the same one
// TestTokenNeverReachesArgv asserts for the management token. Upstream documents
// `--pin <value>` as visible in the process list and `--pin -` as the scripting form, so
// the PIN must arrive on stdin. argv is world-readable through /proc.
func TestPinNeverReachesArgv(t *testing.T) {
	in := &params.PunktfunkInput{Method: "pair", Host: "host-a:47989", Pin: "4271"}
	call, err := resolveCLICall(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range call.Args {
		if strings.Contains(a, "4271") {
			t.Fatalf("PIN leaked into argv: %v", call.Args)
		}
	}
	if call.Stdin != "4271" {
		t.Errorf("PIN must travel on stdin, got Stdin=%q", call.Stdin)
	}

	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatal(err)
	}
	cmd := fe.script[strings.Index(fe.script, "|")+1:] // the punktfunk invocation itself
	if strings.Contains(cmd, "4271") {
		t.Errorf("PIN reached the punktfunk argv in the emitted script: %s", fe.script)
	}
	// Args are shell-quoted individually, so the documented `--pin -` appears as
	// '--pin' '-' in the emitted script.
	if !strings.Contains(fe.script, "'--pin' '-'") {
		t.Errorf("expected the documented stdin form `--pin -`: %s", fe.script)
	}
	if !strings.HasPrefix(fe.script, "printf %s ") {
		t.Errorf("PIN must be piped in, not echoed or inlined: %s", fe.script)
	}
}

// A pair with no PIN is the console-approval flow and must NOT invent a --pin flag.
func TestPairWithoutPinIsConsoleApproval(t *testing.T) {
	call, err := resolveCLICall(&params.PunktfunkInput{Method: "pair", Host: "host-a"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(call.Args, " "), "--pin") {
		t.Errorf("no PIN supplied must mean no --pin flag; got %v", call.Args)
	}
	if call.Stdin != "" {
		t.Errorf("no PIN supplied must mean empty stdin; got %q", call.Stdin)
	}
}

func TestResolveCLICall_ArgvShapes(t *testing.T) {
	cases := []struct {
		in   params.PunktfunkInput
		want string
	}{
		{params.PunktfunkInput{Method: "hosts-list"}, "hosts list"},
		{params.PunktfunkInput{Method: "hosts-list", Probe: true}, "hosts list --probe"},
		{params.PunktfunkInput{Method: "speed-test", Host: "h"}, "speed-test h"},
		{params.PunktfunkInput{Method: "launch", Host: "h"}, "launch h"},
		{params.PunktfunkInput{Method: "launch", Host: "h", Game: "42"}, "launch h --game 42"},
		{params.PunktfunkInput{Method: "client-library", Host: "h"}, "library h --json"},
		{params.PunktfunkInput{Method: "open", Host: "h"}, "open punktfunk://connect/h"},
		{params.PunktfunkInput{Method: "open", Host: "h", AutoApprove: true}, "open punktfunk://connect/h --yes"},
		{params.PunktfunkInput{Method: "reachable", Host: "h"}, "reachable h"},
		{params.PunktfunkInput{Method: "profiles-list"}, "profiles list"},
		{params.PunktfunkInput{Method: "client-reset"}, "reset"},
	}
	for _, c := range cases {
		call, err := resolveCLICall(&c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in.Method, err)
			continue
		}
		if got := strings.Join(call.Args, " "); got != c.want {
			t.Errorf("%s: argv = %q, want %q", c.in.Method, got, c.want)
		}
	}
}

// A method that names a host must say so precisely, rather than letting the CLI emit a
// usage dump the bed author then has to decode.
func TestResolveCLICall_RequiresHost(t *testing.T) {
	for _, m := range []string{"pair", "launch", "speed-test", "client-library", "reachable", "open"} {
		if _, err := resolveCLICall(&params.PunktfunkInput{Method: m}); err == nil {
			t.Errorf("%s without host: expected an authoring error, got nil", m)
		}
	}
}

// The documented exit-code contract. Collapsing these to pass/fail would throw away the
// difference between "the host is down" and "we are not paired" — which is the entire
// diagnostic value of the client half.
func TestCLIExitMeaning(t *testing.T) {
	cases := map[int]string{
		2: "connection failed",
		3: "trust rejected",
		4: "renderer startup failed",
		5: "no match",
		6: "interactive action required",
	}
	for code, want := range cases {
		if got := cliExitMeaning(code); !strings.Contains(got, want) {
			t.Errorf("exit %d: %q does not mention %q", code, got, want)
		}
	}
	if cliExitMeaning(0) != "" {
		t.Errorf("exit 0 must carry no failure text, got %q", cliExitMeaning(0))
	}
}

// An exit code must surface as an error whose message names the failure mode.
func TestRunCLI_ExitCodeBecomesNamedError(t *testing.T) {
	in := &params.PunktfunkInput{Method: "speed-test", Host: "h"}
	call, _ := resolveCLICall(in)
	fe := &fakeExec{exit: 3, stderr: "not paired"}
	_, err := runCLI(context.Background(), fe, in, call)
	if err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	if !strings.Contains(err.Error(), "trust rejected") {
		t.Errorf("error should name the exit-3 meaning, got %v", err)
	}
}

// isCLIMethod is the single branch point in Invoke; a host method must never be routed
// to the CLI, and vice versa.
func TestCLIMethodPartitionIsDisjoint(t *testing.T) {
	for _, hostMethod := range []string{"health", "status", "clients", "pair-arm", "approve", "events"} {
		if isCLIMethod(hostMethod) {
			t.Errorf("%q is a host management-API method but routed to the CLI", hostMethod)
		}
	}
	for _, clientMethod := range []string{"pair", "launch", "speed-test", "hosts-list"} {
		if !isCLIMethod(clientMethod) {
			t.Errorf("%q is a client CLI method but not routed to the CLI", clientMethod)
		}
	}
}

// The client binary is overridable because the Flatpak build is not on PATH.
func TestClientBinOverride(t *testing.T) {
	if got := clientBin(&params.PunktfunkInput{}); got != "punktfunk" {
		t.Errorf("default client bin = %q", got)
	}
	custom := "flatpak run --command=punktfunk io.unom.Punktfunk"
	if got := clientBin(&params.PunktfunkInput{ClientBin: custom}); got != custom {
		t.Errorf("override ignored: %q", got)
	}
}
