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
	// The punktfunk invocation is what follows the LAST pipe — the address lookup adds an
	// earlier `getent … | awk` pipeline, so the first pipe is not the interesting one.
	cmd := fe.script[strings.LastIndex(fe.script, "|")+1:]
	if strings.Contains(cmd, "4271") {
		t.Errorf("PIN reached the punktfunk argv in the emitted script: %s", fe.script)
	}
	// Args are shell-quoted individually, so the documented `--pin -` appears as
	// '--pin' '-' in the emitted script.
	if !strings.Contains(fe.script, "'--pin' '-'") {
		t.Errorf("expected the documented stdin form `--pin -`: %s", fe.script)
	}
	if !strings.Contains(fe.script, "printf %s '4271' | ") {
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
	// Every method whose argv names a host must be listed, or it emits a malformed
	// command instead of the authoring error the map exists to produce. `wake` was
	// missing exactly that way.
	for _, m := range []string{"pair", "launch", "speed-test", "client-library",
		"reachable", "open", "wake", "hosts-add", "hosts-forget"} {
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

// pin_file must reach the CLI on stdin, read by the venue's own shell — the PIN never enters
// argv, and never passes through this process at all.
func TestPinFileNeverReachesArgv(t *testing.T) {
	in := &params.PunktfunkInput{Method: "pair", Host: "h", PinFile: "/pfshare/pin"}
	call, err := resolveCLICall(in)
	if err != nil {
		t.Fatal(err)
	}
	if call.StdinFile != "/pfshare/pin" {
		t.Errorf("StdinFile = %q, want the pin_file path", call.StdinFile)
	}
	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fe.script, "cat '/pfshare/pin' |") {
		t.Errorf("the venue shell must pipe the file in: %s", fe.script)
	}
	if !strings.Contains(fe.script, "is missing or empty") {
		t.Errorf("a missing or empty pin file must fail with a NAMED error, not a silent "+
			"exit 1 indistinguishable from the client failing: %s", fe.script)
	}
	if !strings.Contains(fe.script, "'--pin' '-'") {
		t.Errorf("expected the documented stdin form: %s", fe.script)
	}
}

// An inline pin still works, and pin_file wins when both are given — the file is the one a
// fleet bed uses, and silently preferring the manifest value would be the wrong default.
func TestPinFileWinsOverInlinePin(t *testing.T) {
	call, err := resolveCLICall(&params.PunktfunkInput{
		Method: "pair", Host: "h", Pin: "1234", PinFile: "/pfshare/pin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if call.Stdin != "" {
		t.Errorf("inline pin must not be used when pin_file is set, got Stdin=%q", call.Stdin)
	}
	if call.StdinFile != "/pfshare/pin" {
		t.Errorf("StdinFile = %q", call.StdinFile)
	}
}

// `pair` rejects a hostname with InvalidArg("host:port") even where `reachable` accepts the
// same name (verified against 0.33.0-1). The verb therefore resolves the peer in the venue so
// a bed can keep naming its peer by member name.
func TestPairResolvesHostToAnAddress(t *testing.T) {
	in := &params.PunktfunkInput{Method: "pair", Host: "charly-punktfunk-fleet-host", PinFile: "/pfshare/pin"}
	call, err := resolveCLICall(in)
	if err != nil {
		t.Fatal(err)
	}
	if call.ResolveHost != "charly-punktfunk-fleet-host" {
		t.Fatalf("ResolveHost = %q", call.ResolveHost)
	}
	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fe.script, "getent hosts 'charly-punktfunk-fleet-host'") {
		t.Errorf("the peer must be resolved in the venue: %s", fe.script)
	}
	if !strings.Contains(fe.script, `"$PF_IP:9777"`) {
		t.Errorf("the resolved address must carry the default native port: %s", fe.script)
	}
	if strings.Contains(fe.script, "pair' 'charly-punktfunk-fleet-host'") {
		t.Errorf("the bare name must not reach the CLI: %s", fe.script)
	}
	if !strings.Contains(fe.script, "cannot resolve") {
		t.Errorf("an unresolvable peer must fail with the name, not the CLI's opaque error: %s", fe.script)
	}
}

// EVERY host-taking method resolves the peer, not just `pair`. `pair` saves the host under
// the ADDRESS it paired with, so a later `speed-test <name>` is told the name "isn't a saved
// host" — the saved key and every later reference have to agree. An earlier version of this
// test asserted the opposite (only pair resolves), and the bed failed exactly that way.
func TestEveryHostTakingMethodResolvesThePeer(t *testing.T) {
	for _, m := range []string{"pair", "speed-test", "reachable", "launch", "client-library", "open", "wake"} {
		call, err := resolveCLICall(&params.PunktfunkInput{Method: m, Host: "peer"})
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if call.ResolveHost != "peer" {
			t.Errorf("%s must resolve the peer, got %q", m, call.ResolveHost)
		}
	}
}

// Methods that name no host must not acquire a lookup.
func TestHostlessMethodsDoNotResolve(t *testing.T) {
	for _, m := range []string{"hosts-list", "profiles-list", "client-reset"} {
		call, err := resolveCLICall(&params.PunktfunkInput{Method: m})
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if call.ResolveHost != "" {
			t.Errorf("%s takes no host, got ResolveHost=%q", m, call.ResolveHost)
		}
	}
}

// The PIN must be piped into punktfunk itself, not into the address lookup. Folding the
// lookup into the command put it on the right-hand side of `cat pin | …`, so the PIN fed
// `getent` and `pair` ran with no stdin — which the host reported as a failed connection,
// several layers away from the actual mistake.
func TestPinFileIsPipedIntoPunktfunkNotTheLookup(t *testing.T) {
	in := &params.PunktfunkInput{Method: "pair", Host: "peer-host", PinFile: "/pfshare/pin"}
	call, err := resolveCLICall(in)
	if err != nil {
		t.Fatal(err)
	}
	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatal(err)
	}
	// Anchor on the client invocation itself, not on pipe positions: the HOME fallback and
	// the address lookup each contain their own `|`, so "the first pipe" is not the one that
	// feeds punktfunk. (An earlier version of this assertion keyed on the first pipe and
	// broke the moment a second pipeline appeared ahead of it.)
	invocation := strings.Index(fe.script, "'punktfunk'")
	lookup := strings.Index(fe.script, "getent hosts")
	catPipe := strings.Index(fe.script, "cat '/pfshare/pin' |")
	if invocation < 0 || lookup < 0 || catPipe < 0 {
		t.Fatalf("expected a lookup, a cat pipe and the invocation: %s", fe.script)
	}
	if lookup > catPipe {
		t.Errorf("the address lookup must run BEFORE the PIN pipeline, or the PIN feeds it "+
			"instead of punktfunk: %s", fe.script)
	}
	if catPipe > invocation {
		t.Errorf("the PIN pipe must immediately feed the punktfunk invocation: %s", fe.script)
	}
}

// The client stores its identity and saved hosts under $HOME, and charly's reverse channel
// does not set it. A bare exec fails with "client identity: HOME unset: environment variable
// not found" — reported as a plain exit 1, outside the CLI's documented code set, so it reads
// as a generic failure rather than a missing variable. Every client invocation therefore
// derives HOME when the venue did not supply one.
func TestEveryClientCallEnsuresHome(t *testing.T) {
	for _, m := range []string{"hosts-list", "profiles-list", "speed-test", "pair", "launch"} {
		in := &params.PunktfunkInput{Method: m, Host: "peer", PinFile: "/pfshare/pin"}
		call, err := resolveCLICall(in)
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		fe := &fakeExec{}
		if _, err := runCLI(context.Background(), fe, in, call); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		if !strings.HasPrefix(fe.script, `[ -n "$HOME" ] || export HOME=`) {
			t.Errorf("%s: script must ensure HOME first: %s", m, fe.script)
		}
	}
}

// The client reports its failures on STDOUT ("Pairing failed: InvalidArg(…)", "client
// identity: HOME unset…"). Reporting only stderr discarded the one line that says what went
// wrong and left a bare "exit 1" to be re-diagnosed by hand — which is exactly what happened,
// repeatedly, while debugging this bed.
func TestClientFailureSurfacesItsOwnMessage(t *testing.T) {
	in := &params.PunktfunkInput{Method: "pair", Host: "peer", Pin: "1234"}
	call, _ := resolveCLICall(in)
	fe := &fakeExec{exit: 1, stdout: "Pairing failed: InvalidArg(\"host:port\")\n"}
	_, err := runCLI(context.Background(), fe, in, call)
	if err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	if !strings.Contains(err.Error(), "InvalidArg") {
		t.Errorf("the client's own message must reach the verdict, got: %v", err)
	}
}

// stderr still wins when both are present — it is the conventional channel.
func TestStderrPreferredOverStdout(t *testing.T) {
	in := &params.PunktfunkInput{Method: "speed-test", Host: "peer"}
	call, _ := resolveCLICall(in)
	fe := &fakeExec{exit: 2, stdout: "noise", stderr: "the real error"}
	_, err := runCLI(context.Background(), fe, in, call)
	if err == nil || !strings.Contains(err.Error(), "the real error") {
		t.Errorf("stderr should be preferred, got: %v", err)
	}
}

// The renderer lives in a SEPARATE binary from the CLI: `punktfunk` links libgcc/libm/libc
// and nothing else, while `punktfunk-client` links libvulkan, libwayland-*, libdrm and
// libgbm. Only the methods that spawn it need a display, and a method that does not must
// not acquire an exit-4 guard it can never satisfy.
func TestOnlyRendererMethodsCarryTheDisplayPrologue(t *testing.T) {
	// stream-probe belongs here too: it IS a launch, bounded and counted.
	renderer := map[string]bool{"launch": true, "open": true, "stream-probe": true}
	for m := range cliMethods {
		in := &params.PunktfunkInput{Method: m, Host: "host-a"}
		call, err := resolveCLICall(in)
		if err != nil {
			t.Fatalf("%s: resolve: %v", m, err)
		}
		if got, want := call.NeedsDisplay, renderer[m]; got != want {
			t.Errorf("%s: NeedsDisplay = %v, want %v", m, got, want)
		}
		fe := &fakeExec{}
		if _, err := runCLI(context.Background(), fe, in, call); err != nil {
			t.Fatalf("%s: runCLI: %v", m, err)
		}
		hasPrologue := strings.Contains(fe.script, "WAYLAND_DISPLAY")
		if hasPrologue != renderer[m] {
			t.Errorf("%s: script carries the display prologue = %v, want %v\nscript:\n%s",
				m, hasPrologue, renderer[m], fe.script)
		}
	}
}

// Same rule the PIN pipeline and the host lookup follow: the prologue is SETUP, so it runs
// before the first pipe. Inside the pipeline it would run in a subshell and its exports
// would not reach the client.
func TestDisplayPrologueRunsBeforeThePipeline(t *testing.T) {
	in := &params.PunktfunkInput{Method: "launch", Host: "host-a"}
	call, err := resolveCLICall(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	client := strings.Index(fe.script, "punktfunk")
	display := strings.Index(fe.script, "WAYLAND_DISPLAY")
	if display < 0 || client < 0 {
		t.Fatalf("expected both the prologue and the client call:\n%s", fe.script)
	}
	if display > client {
		t.Errorf("display prologue must precede the client invocation:\n%s", fe.script)
	}
}

// A venue with no compositor must fail NAMING that, with the CLI's own documented
// "renderer startup failed" code, rather than surfacing as an opaque exit.
func TestDisplayPrologueFailsWithTheDocumentedRendererCode(t *testing.T) {
	in := &params.PunktfunkInput{Method: "launch", Host: "host-a"}
	call, _ := resolveCLICall(in)
	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !strings.Contains(fe.script, "exit 4") {
		t.Errorf("missing the exit-4 guard:\n%s", fe.script)
	}
	if !strings.Contains(fe.script, "no wayland display") {
		t.Errorf("guard must name the condition:\n%s", fe.script)
	}
}

// The probe must be BOUNDED. An unbounded launch never returns, so a bed authored with it
// hangs instead of asserting — the failure mode that turns a check into a timeout.
func TestStreamProbeIsBounded(t *testing.T) {
	in := &params.PunktfunkInput{Method: "stream-probe", Host: "host-a"}
	call, err := resolveCLICall(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if call.TimeoutSecs != defaultStreamForSecs {
		t.Errorf("TimeoutSecs = %d, want %d", call.TimeoutSecs, defaultStreamForSecs)
	}
	fe := &fakeExec{}
	if _, err := runCLI(context.Background(), fe, in, call); err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !strings.Contains(fe.script, "timeout 20 ") {
		t.Errorf("probe is not bounded by timeout:\n%s", fe.script)
	}
	in2 := &params.PunktfunkInput{Method: "stream-probe", Host: "host-a", StreamFor: "45s"}
	call2, _ := resolveCLICall(in2)
	if call2.TimeoutSecs != 45 {
		t.Errorf("stream_for=45s -> TimeoutSecs = %d, want 45", call2.TimeoutSecs)
	}
}

// `timeout` kills the session with 124 exactly WHEN it is healthy, so judging on the exit
// code would invert the verdict: every working stream would report failure.
func TestStreamProbeJudgesFramesNotExitCode(t *testing.T) {
	in := &params.PunktfunkInput{Method: "stream-probe", Host: "host-a"}
	call, _ := resolveCLICall(in)
	fe := &fakeExec{exit: 124, stderr: `INFO pf_client_core::session: first frame decoded width=1920 height=1080
INFO pf_client_core::session: session ended total_frames=1192 reason="bounded"`}
	out, err := runCLI(context.Background(), fe, in, call)
	if err != nil {
		t.Fatalf("exit 124 with frames must not be an error: %v", err)
	}
	verdict, err := streamProbeVerdict(out, nil)
	if err != nil {
		t.Fatalf("verdict: %v", err)
	}
	if !strings.Contains(verdict, "1192 frames") || !strings.Contains(verdict, "1920") {
		t.Errorf("verdict must report the frame count, got %q", verdict)
	}
}

// The state this whole bed exists to catch: the session connects, every control-plane check
// passes, and not one frame is decoded.
func TestStreamProbeFailsWhenNoFrameDecodes(t *testing.T) {
	out := `INFO punktfunk_core::client::pump::handshake: host resolved compositor compositor="wlroots"
connect: Rejected(SetupFailed)
no shared video codec: client advertised 0x00, host can emit 0x01`
	_, err := streamProbeVerdict(out, nil)
	if err == nil {
		t.Fatal("a session that decoded nothing must FAIL")
	}
	if !strings.Contains(err.Error(), "no frame decoded") {
		t.Errorf("verdict must name the condition, got %q", err)
	}
	if !strings.Contains(err.Error(), "advertised 0x00") {
		t.Errorf("verdict must carry the client's own words, got %q", err)
	}
}

// A healthy session killed by the bound may never print a total. "first frame decoded" is
// still proof that video arrived.
func TestStreamProbeAcceptsFirstFrameWithoutATotal(t *testing.T) {
	out := "INFO pf_client_core::session: first frame decoded width=1920 height=1080 path=\"native-vulkan\""
	verdict, err := streamProbeVerdict(out, nil)
	if err != nil {
		t.Fatalf("first-frame-only must pass: %v", err)
	}
	if !strings.Contains(verdict, "width=1920 height=1080") {
		t.Errorf("verdict must carry the geometry, got %q", verdict)
	}
	if !strings.Contains(verdict, "native-vulkan") {
		t.Errorf("verdict must carry the decode rung, got %q", verdict)
	}
	if !strings.HasPrefix(verdict, "streamed:") {
		t.Errorf("both success shapes must share the streamed: prefix, got %q", verdict)
	}
}

// The bed matches on a prefix, so BOTH success shapes must carry it. Asserting on
// "frames decoded" instead cost a full bed run: the probe reported a real success as
// "first frame decoded" — singular — and the check failed on a working stream.
func TestBothStreamSuccessShapesSharePrefix(t *testing.T) {
	withTotal := "session ended total_frames=1192\nfirst frame decoded width=1920 height=1080 path=\"native-vulkan\""
	firstOnly := "first frame decoded width=1920 height=1080 path=\"native-vulkan\""
	for _, out := range []string{withTotal, firstOnly} {
		verdict, err := streamProbeVerdict(out, nil)
		if err != nil {
			t.Fatalf("unexpected failure: %v", err)
		}
		if !strings.HasPrefix(verdict, "streamed:") {
			t.Errorf("verdict %q lacks the shared prefix", verdict)
		}
	}
}

// The renderer's tracing wraps field names in SGR escapes, so `width=1920` is NOT
// contiguous in the bytes. A naive regex finds nothing and a real success degrades to
// "geometry not reported" — which is exactly what a live bed run reported before this.
func TestStreamProbeParsesANSIWrappedFields(t *testing.T) {
	esc := "\x1b"
	out := "INFO pf_client_core::session: first frame decoded " +
		esc + "[3mwidth" + esc + "[0m" + esc + "[2m=" + esc + "[0m1920 " +
		esc + "[3mheight" + esc + "[0m" + esc + "[2m=" + esc + "[0m1080 " +
		esc + "[3mpath" + esc + "[0m" + esc + "[2m=" + esc + "[0m\"native-vulkan\""
	verdict, err := streamProbeVerdict(out, nil)
	if err != nil {
		t.Fatalf("unexpected failure: %v", err)
	}
	if strings.Contains(verdict, "geometry not reported") {
		t.Fatalf("ANSI-wrapped fields were not parsed: %q", verdict)
	}
	if !strings.Contains(verdict, "width=1920 height=1080") {
		t.Errorf("verdict must carry the geometry, got %q", verdict)
	}
	if !strings.Contains(verdict, "native-vulkan") {
		t.Errorf("verdict must carry the decode rung, got %q", verdict)
	}
}

// A failure carries the client's own words to the reader, so they must not arrive as
// escape-sequence noise.
func TestStreamProbeFailureMessageIsReadable(t *testing.T) {
	esc := "\x1b"
	out := esc + "[33mconnect: Rejected(SetupFailed)" + esc + "[0m"
	_, err := streamProbeVerdict(out, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(err.Error(), esc) {
		t.Errorf("failure message still carries ANSI escapes: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "Rejected(SetupFailed)") {
		t.Errorf("failure message lost the client's words: %q", err.Error())
	}
}
