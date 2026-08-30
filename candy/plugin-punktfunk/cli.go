package punktfunk

import (
	"context"
	"fmt"
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
}

// cliRequiredField names the input field a client method needs, so a missing one is an
// authoring error rather than a confusing CLI usage dump.
var cliRequiredField = map[string]string{
	"pair":           "host",
	"launch":         "host",
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
	"pair": {}, "launch": {}, "client-library": {}, "speed-test": {},
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
		return cliCall{Args: args, Mutating: true}, nil

	case "client-library":
		// --json because a library listing is data a step will want to match on, and
		// json_path extraction only means anything against machine-readable output.
		return cliCall{Args: []string{"library", hostRef, "--json"}}, nil

	case "speed-test":
		// The assertion that the transport actually carries: it reports measured and
		// recommended bitrate, which a host that installed but cannot stream cannot
		// produce.
		return cliCall{Args: []string{"speed-test", hostRef}}, nil

	case "open":
		args := []string{"open", "punktfunk://connect/" + hostRef}
		if in.AutoApprove {
			args = append(args, "--yes")
		}
		return cliCall{Args: args, Mutating: true}, nil

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
