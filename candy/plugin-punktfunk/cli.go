package punktfunk

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk/params"
)

// cli.go is the CLIENT half of the verb. methods.go maps a method to a management-API
// call on a punktfunk HOST; this file maps a method to the headless `punktfunk` CLI that
// every Linux client package ships.
//
// Why the verb runs the CLI rather than a bed authoring `command: punktfunk …`: a bed
// should assert in charly verbs, so the exit-code contract below is interpreted ONCE here
// instead of being re-derived (and mis-derived) in every bed that streams. It also keeps
// the PIN off argv — see pairStdin.

// defaultClientBin is the headless CLI shipped by punktfunk-client. Overridable per step
// because the Flatpak build is reached as
// `flatpak run --command=punktfunk io.unom.Punktfunk`, which is not on PATH.
const defaultClientBin = "punktfunk"

// defaultNativePort is the punktfunk/1 control port; the CLI documents it as the default.
const defaultNativePort = "9777"

// cliCall is a resolved client-CLI invocation.
type cliCall struct {
	// Args are appended to the client binary, already shell-quoted by the caller.
	Args []string
	// Stdin, when non-empty, is piped in. Used ONLY for the PIN: upstream documents
	// `--pin <value>` as visible in the process list and `--pin -` as the scripting
	// form, so the value never becomes an argument.
	Stdin string
	// ResolveHost, when set, names a peer whose ADDRESS must be looked up in the venue and
	// substituted into Args before the call. `pair` rejects a hostname outright.
	ResolveHost string
	// StdinFile pipes a file's contents in instead of a literal. Used for the PIN when it
	// arrives on disk (a fleet bed's shared path), so the value never passes through the
	// manifest, the plugin's memory, or argv — `cat` feeds the pipe directly.
	StdinFile string
	// Mutating marks a call that changes client or host state (enrolling, launching,
	// forgetting). Same contract as apiCall.Mutating: these belong in `run:` steps, so
	// a `check:` can never silently pair or tear down a session.
	Mutating bool
	// TimeoutSecs bounds the call with `timeout`. A streaming session never ends by
	// itself, so a probe MUST be bounded or it hangs the bed rather than asserting.
	TimeoutSecs int
	// IgnoreExit judges the call on its OUTPUT rather than its exit code. A bounded
	// stream is killed by `timeout` (124) exactly when it is working, so the exit code
	// is the wrong signal — the frame count is the assertion.
	IgnoreExit bool
	// NeedsDisplay marks a call that starts the RENDERER. `punktfunk` itself is a control
	// binary that links no video libraries at all (libgcc/libm/libc and nothing else); the
	// decoder lives in the separate `punktfunk-client`, which links libvulkan, libwayland-*,
	// libdrm and libgbm. `launch` and `open` spawn it, so they need a Wayland display to
	// render into — without one the renderer cannot start (the CLI's documented exit 4).
	NeedsDisplay bool
}

// cliRequiredField names the input field a client method needs, so a missing one is an
// authoring error rather than a confusing CLI usage dump.
var cliRequiredField = map[string]string{
	"pair":           "host",
	"launch":         "host",
	"stream-probe":   "host",
	"client-library": "host",
	"speed-test":     "host",
	"open":           "host",
	"reachable":      "host",
	"hosts-add":      "host",
	"hosts-forget":   "host",
	"wake":           "host",
}

// isCLIMethod reports whether a method is served by the client CLI rather than the
// management API. Kept as one predicate so Invoke has a single branch point.
func isCLIMethod(m string) bool {
	_, ok := cliMethods[m]
	return ok
}

// cliMethods is the set of client-CLI methods, mirroring the documented verb list.
var cliMethods = map[string]struct{}{
	"hosts-list": {}, "hosts-add": {}, "hosts-forget": {},
	"pair": {}, "launch": {}, "client-library": {}, "speed-test": {}, "stream-probe": {},
	"open": {}, "wake": {}, "reachable": {}, "profiles-list": {}, "client-reset": {},
}

// resolveCLICall builds the client invocation for a method from the typed input.
// resolveCLICall builds the client invocation, then resolves the peer for every method that
// takes a host-ref.
//
// Resolution is uniform for one reason: `pair` stores the host under the ADDRESS it paired
// with, so `hosts list` reports
//
//	10.89.0.160  10.89.0.160:9777  paired  -
//
// and a later `speed-test <name>` is told the name "isn't a saved host". Resolving in exactly
// one place keeps the saved key and every later reference identical, and lets a bed keep
// naming its peer by member name throughout.
func resolveCLICall(in *params.PunktfunkInput) (cliCall, error) {
	c, err := buildCLICall(in)
	if err != nil {
		return c, err
	}
	if cliRequiredField[in.Method] == "host" {
		c.ResolveHost = in.Host
	}
	return c, nil
}

func buildCLICall(in *params.PunktfunkInput) (cliCall, error) {
	m := in.Method
	if want, ok := cliRequiredField[m]; ok {
		if err := requireField(in, want); err != nil {
			return cliCall{}, err
		}
	}
	// hostRef is the documented `<host-ref>` argument: a saved name, or host[:port].
	hostRef := in.Host

	switch m {
	case "hosts-list":
		args := []string{"hosts", "list"}
		if in.Probe {
			args = append(args, "--probe")
		}
		return cliCall{Args: args}, nil
	case "hosts-add":
		return cliCall{Args: []string{"hosts", "add", hostRef}, Mutating: true}, nil
	case "hosts-forget":
		return cliCall{Args: []string{"hosts", "forget", hostRef}, Mutating: true}, nil

	case "pair":
		// `--pin -` reads the PIN from stdin. When no PIN is supplied the CLI refuses
		// unattended ("no --pin and no terminal to ask on", exit 6) — console approval is
		// a GUI flow, not a scriptable one, so an automated bed must supply a PIN.
		//
		// hostRef is resolved to a literal ADDRESS below: `pair` rejects a hostname with
		// InvalidArg("host:port") even when the same name works for `reachable`. Verified
		// against 0.33.0-1 — `pair charly-punktfunk-fleet-host:9777` fails, `pair
		// 10.89.0.122:9777` pairs. Doing the lookup here means a bed can keep naming its
		// peer by member name, which is what makes the manifest readable and portable.
		args := []string{"pair", hostRef}
		c := cliCall{Args: args, Mutating: true}
		// pin_file is preferred over an inline pin for the same reason token_file is
		// preferred over an inline token: the value never appears in the manifest. Both
		// end up on stdin — never argv.
		if in.PinFile != "" {
			c.Args = append(c.Args, "--pin", "-")
			c.StdinFile = in.PinFile
			return c, nil
		}
		if in.Pin != "" {
			c.Args = append(c.Args, "--pin", "-")
			c.Stdin = in.Pin
		}
		return c, nil

	case "launch":
		args := []string{"launch", hostRef}
		if in.Game != "" {
			args = append(args, "--game", in.Game)
		}
		return cliCall{Args: args, Mutating: true, NeedsDisplay: true}, nil

	case "client-library":
		// --json because a library listing is data a step will want to match on, and
		// json_path extraction only means anything against machine-readable output.
		return cliCall{Args: []string{"library", hostRef, "--json"}}, nil

	case "speed-test":
		// Reports measured and recommended bitrate. NOTE this runs on the CONTROL binary,
		// which carries no decoder, so it advertises codec 0x00 and a host that requires a
		// video codec refuses the session outright:
		//   no shared video codec: client advertised 0x00, host can emit 0x01
		// That is the client accurately reporting what it is, not a host defect. `launch`
		// is the method that proves streaming, because it spawns the renderer.
		return cliCall{Args: []string{"speed-test", hostRef}}, nil

	case "open":
		args := []string{"open", "punktfunk://connect/" + hostRef}
		if in.AutoApprove {
			args = append(args, "--yes")
		}
		return cliCall{Args: args, Mutating: true, NeedsDisplay: true}, nil

	case "stream-probe":
		// THE streaming assertion. `speed-test` cannot be it: that runs on the control
		// binary, which links no video libraries, so it advertises codec 0x00 and the host
		// refuses. This runs a real bounded session through the RENDERER and counts frames.
		//
		// Non-mutating on purpose, so it can be authored as a `check:`: it opens a session
		// and lets it close, leaving no enrolment or state behind — same contract as
		// speed-test.
		secs := streamForSeconds(in.StreamFor)
		return cliCall{
			Args:         []string{"launch", hostRef, "--fullscreen"},
			TimeoutSecs:  secs,
			IgnoreExit:   true,
			NeedsDisplay: true,
		}, nil

	case "wake":
		return cliCall{Args: []string{"wake", hostRef}, Mutating: true}, nil
	case "reachable":
		return cliCall{Args: []string{"reachable", hostRef}}, nil
	case "profiles-list":
		return cliCall{Args: []string{"profiles", "list"}}, nil
	case "client-reset":
		return cliCall{Args: []string{"reset"}, Mutating: true}, nil
	}
	return cliCall{}, fmt.Errorf("punktfunk: %q is not a client method", m)
}

// cliExitMeaning renders the documented exit-code contract. Mapping these to distinct
// messages rather than collapsing them to pass/fail is the point: "trust rejected,
// re-pair needed" and "renderer startup failed" are different defects, and a bed that
// says which one it hit is a diagnosis instead of a shrug.
func cliExitMeaning(code int) string {
	switch code {
	case 0:
		return ""
	case 2:
		return "connection failed — the host is unreachable or not listening"
	case 3:
		return "trust rejected — the client is not paired with this host (re-pair needed)"
	case 4:
		return "renderer startup failed — the client could not start decode/present"
	case 5:
		return "no match for the given host reference"
	case 6:
		return "interactive action required — the flow cannot complete unattended"
	default:
		return fmt.Sprintf("exit %d", code)
	}
}

// clientBin resolves the client binary, honouring an explicit override.
func clientBin(in *params.PunktfunkInput) string {
	if b := strings.TrimSpace(in.ClientBin); b != "" {
		return b
	}
	return defaultClientBin
}

// waylandPrologue points a renderer call at the venue's own compositor. It runs BEFORE the
// pipeline, like the HOME fallback, and exits 4 — the CLI's own "renderer startup failed"
// code — when the venue has no display, so the verdict keeps the documented meaning.
const waylandPrologue = `if [ -z "$WAYLAND_DISPLAY" ]; then
  for d in "$XDG_RUNTIME_DIR" "/run/user/$(id -u)" /tmp; do
    [ -n "$d" ] || continue
    for s in "$d"/wayland-[0-9]*; do
      [ -S "$s" ] || continue
      XDG_RUNTIME_DIR="$d"; export XDG_RUNTIME_DIR
      WAYLAND_DISPLAY="${s##*/}"; export WAYLAND_DISPLAY
      break 2
    done
  done
fi
[ -n "$WAYLAND_DISPLAY" ] || { echo "punktfunk: no wayland display in this venue (searched \"$XDG_RUNTIME_DIR\", /run/user/$(id -u), /tmp) - the renderer cannot start" >&2; exit 4; }
`

// defaultStreamForSecs bounds a stream probe when the author names no duration. Long
// enough for the handshake, the decoder to come up and the adaptive-bitrate probe to run
// (that alone takes ~800ms and only starts after the first frame), short enough that a
// broken host fails the bed quickly.
const defaultStreamForSecs = 20

// streamForSeconds parses the schema-constrained "<n>s" form. The schema already rejects
// anything else, so a bad value here can only mean the field was bypassed — fall back
// rather than fail, since the probe's verdict comes from the frame count either way.
func streamForSeconds(v string) int {
	if v == "" {
		return defaultStreamForSecs
	}
	n, err := strconv.Atoi(strings.TrimSuffix(v, "s"))
	if err != nil || n <= 0 {
		return defaultStreamForSecs
	}
	return n
}

// streamProbeFrames reports how many frames the session decoded. The client prints
//
//	INFO pf_client_core::session: session ended total_frames=1192 reason="..."
//
// on a session that ends, and `first frame decoded` as soon as one arrives — which is the
// signal that matters when `timeout` kills a HEALTHY session before it can report a total.
// ansiRE matches the SGR escapes punktfunk's tracing wraps field names in. The renderer
// prints `first frame decoded width=1920 …` as
//
//	first frame decoded \x1b[3mwidth\x1b[0m\x1b[2m=\x1b[0m1920
//
// so `width=1920` is not contiguous in the bytes and a naive regex silently finds nothing —
// which is how a real success first reported "geometry not reported". Stripping also makes
// the client's own words readable when they are carried into a FAILURE verdict.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

var totalFramesRE = regexp.MustCompile(`total_frames=(\d+)`)

// A bounded probe usually sees only this line, because the client reports a TOTAL only when
// the HOST ends the session — neither SIGTERM nor SIGINT makes it print one. The line is
// strong evidence in its own right, so the geometry and decode path are carried into the
// verdict rather than thrown away:
//
//	first frame decoded width=1920 height=1080 path="native-vulkan"
var firstFrameRE = regexp.MustCompile(`first frame decoded (width=\d+ height=\d+(?: path="[^"]*")?)`)

func streamProbeFrames(out string) (frames int, firstFrame string) {
	out = stripANSI(out)
	if m := totalFramesRE.FindStringSubmatch(out); m != nil {
		frames, _ = strconv.Atoi(m[1])
	}
	if m := firstFrameRE.FindStringSubmatch(out); m != nil {
		firstFrame = m[1]
	} else if strings.Contains(out, "first frame decoded") {
		firstFrame = "geometry not reported"
	}
	return frames, firstFrame
}

// runCLI executes a client call INSIDE the venue and returns its stdout.
//
// An exit code is turned into an error carrying cliExitMeaning, so the step's verdict
// names the failure mode. Note exit 6 in particular: it means the CLI wanted a human,
// which in an automated bed is a broken automation contract rather than a flaky run.
func runCLI(ctx context.Context, exec venueExec, in *params.PunktfunkInput, call cliCall) (string, error) {
	quoted := make([]string, 0, len(call.Args))
	for _, a := range call.Args {
		quoted = append(quoted, shellQuote(a))
	}
	cmd := shellQuote(clientBin(in)) + " " + strings.Join(quoted, " ")
	if call.TimeoutSecs > 0 {
		// A streaming session runs until something stops it, so the probe supplies the
		// stop. `timeout` exits 124 here precisely WHEN the stream is healthy, which is
		// why such a call is judged on its frame count instead (IgnoreExit).
		cmd = "timeout " + strconv.Itoa(call.TimeoutSecs) + " " + cmd
	}
	// prefix runs BEFORE the pipeline, never inside it. Folding the lookup into cmd put it
	// on the right-hand side of `cat pin | …`, so the PIN was piped into the lookup instead
	// of into punktfunk and `pair` ran with no stdin at all.
	// The client keeps its identity and saved-hosts store under $HOME, and the reverse
	// channel does not set HOME — a bare exec fails with
	//   client identity: HOME unset: environment variable not found
	// which the CLI reports as a plain exit 1, outside its documented code set, so it
	// reads as "something went wrong" rather than "there is no HOME". Derive it from
	// passwd when the venue's environment did not supply one.
	prefix := `[ -n "$HOME" ] || export HOME="$(getent passwd "$(id -u)" | cut -d: -f6)"; `
	if call.NeedsDisplay {
		// The renderer is a Wayland client. charly's exec channel sets no display, so a
		// venue that HAS a compositor still fails as the CLI's opaque exit 4. Derive both
		// variables from the socket the compositor actually created, and only when the
		// venue did not supply them, so an explicit setting always wins.
		//
		// A venue with no compositor fails HERE, naming what was searched, instead of
		// surfacing as "renderer startup failed" with no indication of why.
		prefix += waylandPrologue
	}
	if call.ResolveHost != "" {
		// `pair` needs a literal address. Resolve the peer's name in the venue and
		// substitute it, so the author writes a member name and the CLI still gets what
		// it demands. A name that does not resolve fails here, with the name in the
		// message, rather than as the CLI's opaque InvalidArg("host:port").
		name, port, _ := strings.Cut(call.ResolveHost, ":")
		if port == "" {
			port = defaultNativePort
		}
		prefix += "PF_IP=$(getent hosts " + shellQuote(name) + " | awk '{print $1; exit}'); " +
			"[ -n \"$PF_IP\" ] || { echo \"punktfunk: cannot resolve " + name + "\" >&2; exit 2; }; "
		cmd = strings.Replace(cmd, shellQuote(call.ResolveHost), "\"$PF_IP:"+port+"\"", 1)
	}
	script := prefix + cmd
	if call.StdinFile != "" {
		// The PIN never reaches this process: the venue's own shell reads the file and
		// pipes it. A missing file fails loudly rather than pairing with an empty PIN.
		// The prefix stays OUTSIDE the pipeline — see above.
		// An `X && Y` guard exits 1 with NO output when X fails, which is
		// indistinguishable from the client itself failing — and the client's exit 1 is
		// already undocumented, so a bare "exit 1" says nothing at all. Name the branch.
		q := shellQuote(call.StdinFile)
		script = prefix +
			"if [ ! -s " + q + " ]; then echo \"punktfunk: pin file " + call.StdinFile +
			" is missing or empty — did the host's pair-arm step run, and does this member " +
			"mount the same path?\" >&2; exit 2; fi; " +
			"cat " + q + " | " + cmd
		stdout, stderr, exit, err := exec.RunCapture(ctx, script)
		return cliResult(in, stdout, stderr, exit, err)
	}
	if call.Stdin != "" {
		// printf, not echo: no trailing newline surprises, and the value stays out of
		// the process list because it is an argument to printf in the same shell rather
		// than to punktfunk.
		script = prefix + "printf %s " + shellQuote(call.Stdin) + " | " + cmd
	}
	stdout, stderr, exit, err := exec.RunCapture(ctx, script)
	if call.IgnoreExit && err == nil {
		// Judged on output, not status — see cliCall.IgnoreExit. The evidence is the
		// renderer's TRACING, which goes to stderr, while cliResult would return only
		// stdout on a zero exit — so hand back both or the frame count is invisible.
		exit = 0
		return strings.TrimSpace(stdout + "\n" + stderr), nil
	}
	return cliResult(in, stdout, stderr, exit, err)
}

// cliResult turns a client invocation's outcome into a verdict, mapping the documented exit
// codes to named failures. Shared by every runCLI path so the stdin-file variant cannot drift
// into reporting failures differently from the plain one.
func cliResult(in *params.PunktfunkInput, stdout, stderr string, exit int, err error) (string, error) {
	if err != nil {
		return stdout, fmt.Errorf("punktfunk: %s: %w", in.Method, err)
	}
	if exit != 0 {
		msg := cliExitMeaning(exit)
		// The client reports its own failures on STDOUT, not stderr — "Pairing failed:
		// InvalidArg(…)", "client identity: HOME unset…", "Pairing isn't armed on the
		// host…" all arrive there. Reporting only stderr threw the one line that says
		// what actually went wrong, leaving a bare "exit 1" to be re-diagnosed by hand
		// every time. Prefer stderr, fall back to stdout.
		detail := trailer(stderr)
		if detail == "" {
			detail = trailer(stdout)
		}
		if detail != "" {
			return stdout, fmt.Errorf("punktfunk: %s: %s: %s", in.Method, msg, detail)
		}
		return stdout, fmt.Errorf("punktfunk: %s: %s", in.Method, msg)
	}
	return stdout, nil
}
