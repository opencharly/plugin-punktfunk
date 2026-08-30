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

// cliCall is a resolved client-CLI invocation.
type cliCall struct {
	// Args are appended to the client binary, already shell-quoted by the caller.
	Args []string
	// Stdin, when non-empty, is piped in. Used ONLY for the PIN: upstream documents
	// `--pin <value>` as visible in the process list and `--pin -` as the scripting
	// form, so the value never becomes an argument.
	Stdin string
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
func resolveCLICall(in *params.PunktfunkInput) (cliCall, error) {
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
		// `--pin -` reads the PIN from stdin. When no PIN is supplied the call is the
		// console-approval flow: the client requests, an operator (or a `punktfunk:
		// approve` step on the host) approves.
		args := []string{"pair", hostRef}
		c := cliCall{Args: args, Mutating: true}
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
	script := cmd
	if call.Stdin != "" {
		// printf, not echo: no trailing newline surprises, and the value stays out of
		// the process list because it is an argument to printf in the same shell rather
		// than to punktfunk.
		script = "printf %s " + shellQuote(call.Stdin) + " | " + cmd
	}
	stdout, stderr, exit, err := exec.RunCapture(ctx, script)
	if err != nil {
		return stdout, fmt.Errorf("punktfunk: %s: %w", in.Method, err)
	}
	if exit != 0 {
		msg := cliExitMeaning(exit)
		if t := trailer(stderr); t != "" {
			return stdout, fmt.Errorf("punktfunk: %s: %s: %s", in.Method, msg, t)
		}
		return stdout, fmt.Errorf("punktfunk: %s: %s", in.Method, msg)
	}
	return stdout, nil
}
