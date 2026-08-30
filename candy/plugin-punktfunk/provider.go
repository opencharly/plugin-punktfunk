package punktfunk

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk/params"
	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/kit"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// provider.go is the out-of-process punktfunk verb provider. charly's host dispatches a
// `punktfunk:` step to it through the registry (ResolveVerb("punktfunk") → grpcProvider →
// Provider.Invoke) with the FULL #Op marshaled as params_json and a CheckEnv snapshot as
// env_json. The out-of-process path runs no host-side matcher pipeline, so this Invoke
// OWNS the whole verdict: resolve the endpoint and token, issue the call, then evaluate
// the stdout/stderr/exit_status matchers through the shared sdk implementation (R3).

// defaultMgmtPort is punktfunk's management REST API port (PUNKTFUNK_MGMT_BIND).
const defaultMgmtPort = 47990

// defaultTokenFile is where the host writes its generated bearer token at first start.
const defaultTokenFile = "~/.config/punktfunk/mgmt-token"

// punktfunkEnv is the plugin-side decode of the CheckEnv the host ships as the
// Operation env for a `punktfunk:` step.
type punktfunkEnv struct {
	Box  string `json:"box"`
	Mode string `json:"mode"` // "live" | "box"
}

type provider struct{ pb.UnimplementedProviderServer }

// Invoke runs one `punktfunk:` operation.
func (provider) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeReply, error) {
	var op spec.Op
	if len(req.GetParamsJson()) > 0 {
		if err := json.Unmarshal(req.GetParamsJson(), &op); err != nil {
			return sdk.ResultJSON("fail", "punktfunk: decode op: "+err.Error())
		}
	}
	var in params.PunktfunkInput
	kit.DecodeInput(op.PluginInput, &in)
	var env punktfunkEnv
	if len(req.GetEnvJson()) > 0 {
		_ = json.Unmarshal(req.GetEnvJson(), &env)
	}
	method := in.Method

	// Live-deployment verb: there is no running host during `charly check box` (a
	// disposable build container), so skip rather than fail — the same contract the
	// other live probe verbs follow.
	if env.Mode == "box" {
		return sdk.ResultJSON("skip", fmt.Sprintf(
			"punktfunk: %s requires a running host (skip under charly check box)", method))
	}

	// Client methods are served by the headless `punktfunk` CLI in the venue, not by a
	// host management API, so they resolve and dispatch on their own path: no bearer
	// token, no loopback address, no 47990. Branching here rather than inside
	// resolveCall keeps the two contracts (HTTP call vs argv + exit code) from bleeding
	// into each other.
	if isCLIMethod(method) {
		return invokeCLI(ctx, req, &op, &in)
	}

	call, err := resolveCall(&in)
	if err != nil {
		return sdk.ResultJSON("fail", err.Error())
	}
	// A mutating method in a `check:` step would make a probe change host state — a
	// check that unpairs a device or reboots the machine is not a check. The intent is
	// carried by Op.IntentDo, which the runner stamps from the step's keyword
	// (run→act, check→assert); it is runtime-derived and cannot be authored.
	//
	// Refused ONLY on an explicit "assert" rather than "anything that is not act": an
	// unstamped (empty) IntentDo would otherwise reject every legitimate `run:` step
	// on any path that does not stamp it, trading a real bug for a false positive.
	if call.Mutating && op.IntentDo == string(spec.DoAssert) {
		return sdk.ResultJSON("fail", fmt.Sprintf(
			"punktfunk: %s mutates host state and must be authored as a `run:` step, not a `check:` step", method))
	}

	// ONE broker dial for the whole Invoke. NewCheckContext and ExecutorFromInvoke both
	// dial the same broker id and a second dial hangs, so everything goes through cc.
	cc, err := sdk.NewCheckContext(req.GetExecutorBrokerId(), req.GetEnvJson())
	if err != nil {
		return sdk.ResultJSON("fail", fmt.Sprintf("punktfunk: %s: %v", method, err))
	}

	port := defaultMgmtPort
	if in.Port > 0 {
		port = int(in.Port)
	}
	// The request is issued from INSIDE the venue (see transport.go), so the address is
	// the venue's own loopback — deliberately NOT a host-resolved endpoint. punktfunk
	// binds the management API to 127.0.0.1 by design; reaching it from the host would
	// mean republishing the port or relaying it, i.e. weakening the posture upstream
	// chose. Probing in-venue also puts the request on the same side as the token.
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// /api/v1/health is the one unauthenticated route, so a liveness probe works on a
	// host whose token has not been created yet. Every other route needs the bearer.
	var token string
	if method != "health" {
		token, err = resolveToken(ctx, cc, &in)
		if err != nil {
			return sdk.ResultJSON("fail", fmt.Sprintf("punktfunk: %s: %v", method, err))
		}
	}

	applyDefaultWriteBody(&call)

	out, runErr := doCall(ctx, cc, addr, token, &in, call)
	if runErr == nil && in.PinOut != "" {
		runErr = writePinOut(ctx, cc.Exec(), out, in.PinOut)
	}
	return sdk.VerbVerdict("punktfunk", method, out, runErr, &op, false)
}

// resolveToken finds the management bearer token, in the order an author would expect:
// an explicit inline token, then an explicit file, then the default file the host
// generates. The file is read INSIDE the venue over the reverse channel, which is the
// whole reason this verb exists rather than an `http:` step — a per-deploy secret is
// not something an authored URL can reach.
func resolveToken(ctx context.Context, cc kit.CheckContext, in *params.PunktfunkInput) (string, error) {
	if strings.TrimSpace(in.Token) != "" {
		return strings.TrimSpace(in.Token), nil
	}
	path := in.TokenFile
	if strings.TrimSpace(path) == "" {
		path = defaultTokenFile
	}
	// `cat` through a shell so a leading ~ expands in the venue's own HOME rather than
	// the host's — the token belongs to the user running the host, not to charly.
	stdout, stderr, exit, err := cc.Exec().RunCapture(ctx, "cat "+path)
	if err != nil {
		return "", fmt.Errorf("read token %s: %v", path, err)
	}
	if exit != 0 {
		return "", fmt.Errorf("read token %s: exit %d%s", path, exit, trailer(stderr))
	}
	// The file is an env-file line (PUNKTFUNK_MGMT_TOKEN=…), not a bare token — see
	// token.go. Sending the raw contents as the bearer produces a 401.
	tok := parseTokenFile(stdout)
	if tok == "" {
		return "", fmt.Errorf("no token found in %s — has the host started at least once?", path)
	}
	return tok, nil
}

// doCall issues the resolved API call inside the venue and returns the text the
// matchers see.
func doCall(ctx context.Context, cc kit.CheckContext, addr, token string,
	in *params.PunktfunkInput, call apiCall) (string, error) {

	timeoutSecs := 30
	if call.Stream {
		d, err := eventWindow(in)
		if err != nil {
			return "", err
		}
		timeoutSecs = int(d.Seconds())
	}
	url := "https://" + addr + call.Path
	resp, err := curlRequest(ctx, cc.Exec(), url, call.HTTPMethod, token, call.Body,
		in.VerifyTLS, timeoutSecs)
	if err != nil {
		return "", fmt.Errorf("punktfunk: %s: %v", in.Method, err)
	}
	if call.Stream {
		return sliceEvents(resp.Body, in)
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return resp.Body, fmt.Errorf("punktfunk: %s: HTTP %d%s", in.Method, resp.Status, trailer(resp.Body))
	}
	if in.JSONPath != "" {
		v, err := extractJSONPath([]byte(resp.Body), in.JSONPath)
		if err != nil {
			return resp.Body, err
		}
		return v, nil
	}
	return resp.Body, nil
}

// eventWindow is how long `events` waits for the stream before giving up.
func eventWindow(in *params.PunktfunkInput) (time.Duration, error) {
	timeout := in.EventTimeout
	if strings.TrimSpace(timeout) == "" {
		timeout = "30s"
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return 0, fmt.Errorf("punktfunk: events: bad event_timeout %q: %v", timeout, err)
	}
	return d, nil
}

// sliceEvents turns the SSE payload into the first `count` events. This is the only
// way to assert a TRANSITION — polling /api/v1/host observes a state, never that a
// stream started.
func sliceEvents(body string, in *params.PunktfunkInput) (string, error) {
	want := int(in.Count)
	if want <= 0 {
		want = 1
	}
	events := parseSSE(body)
	if len(events) < want {
		return strings.Join(events, "\n"), fmt.Errorf(
			"punktfunk: events: wanted %d event(s), saw %d", want, len(events))
	}
	return strings.Join(events[:want], "\n"), nil
}

// trailer renders a short, single-line excerpt of an error body for a failure message.
func trailer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return ": " + s
}

// invokeCLI serves the client half of the verb: resolve the method to a `punktfunk` CLI
// invocation, run it inside the venue, and turn its exit code into a verdict.
//
// It mirrors the API path's guarantees deliberately — the same mutating-in-a-check
// refusal, the same single broker dial, the same json_path extraction — so an author does
// not have to remember which half of the verb they are using.
func invokeCLI(ctx context.Context, req *pb.InvokeRequest, op *spec.Op, in *params.PunktfunkInput) (*pb.InvokeReply, error) {
	method := in.Method
	call, err := resolveCLICall(in)
	if err != nil {
		return sdk.ResultJSON("fail", err.Error())
	}
	if call.Mutating && op.IntentDo == string(spec.DoAssert) {
		return sdk.ResultJSON("fail", fmt.Sprintf(
			"punktfunk: %s changes client or host state and must be authored as a `run:` step, not a `check:` step", method))
	}
	cc, err := sdk.NewCheckContext(req.GetExecutorBrokerId(), req.GetEnvJson())
	if err != nil {
		return sdk.ResultJSON("fail", fmt.Sprintf("punktfunk: %s: %v", method, err))
	}
	out, runErr := runCLI(ctx, cc.Exec(), in, call)
	if method == "stream-probe" {
		out, runErr = streamProbeVerdict(out, runErr)
	}
	if runErr == nil && in.JSONPath != "" {
		out, runErr = extractJSONPath([]byte(out), in.JSONPath)
	}
	return sdk.VerbVerdict("punktfunk", method, out, runErr, op, false)
}

// writePinOut extracts the host-generated pairing PIN from a pair-arm response and writes it
// into the venue at path, 0600.
//
// It exists because the two halves of a pairing live in different places and charly has no
// way to carry a value between steps: the HOST generates the PIN (it ignores a caller-supplied
// one) and only the CLIENT can use it. A fleet bed mounts one path into both members, so this
// is the automation of a human reading the PIN off one screen and typing it into the other.
//
// Unlike the management token, the PIN is written through the venue shell rather than over
// stdin. That is a deliberate, bounded trade: a PIN is single-use and expires in ~2 minutes
// (expires_in_secs: 119 on 0.33.0-1), where a token is long-lived and high-value — which is
// why the token still never touches argv (see transport.go).
func writePinOut(ctx context.Context, exec venueExec, body, path string) error {
	pin, err := extractJSONPath([]byte(body), "pin")
	if err != nil {
		// A pair-arm response that parses but carries no `pin` is the interesting case:
		// the host answered and simply is not armed. Say that, rather than surfacing a
		// json_path miss the bed author then has to translate.
		if json.Valid([]byte(body)) {
			return fmt.Errorf("punktfunk: pin_out: the host returned no pin — is pairing armed? (%s)", trailer(body))
		}
		return fmt.Errorf("punktfunk: pin_out: %w", err)
	}
	if strings.TrimSpace(pin) == "" {
		return fmt.Errorf("punktfunk: pin_out: the host returned no pin — is pairing armed?")
	}
	script := "umask 077 && mkdir -p " + shellQuote(filepath.Dir(path)) +
		" && printf %s " + shellQuote(pin) + " > " + shellQuote(path)
	if _, stderr, exit, rerr := exec.RunCapture(ctx, script); rerr != nil || exit != 0 {
		if rerr != nil {
			return fmt.Errorf("punktfunk: pin_out %s: %w", path, rerr)
		}
		return fmt.Errorf("punktfunk: pin_out %s: exit %d: %s", path, exit, trailer(stderr))
	}
	return nil
}

// streamProbeVerdict turns a bounded session's tracing into the one thing the bed is
// asserting: did video actually decode. A session that carried frames is a PASS even
// though `timeout` killed it, and a session that connected but decoded nothing is a
// FAILURE even though every control-plane check around it passes — which is exactly the
// state that let a fleet bed report green while punktfunk could not stream at all.
func streamProbeVerdict(out string, runErr error) (string, error) {
	if runErr != nil {
		return out, runErr
	}
	frames, sawFirst := streamProbeFrames(out)
	switch {
	case frames > 0:
		return fmt.Sprintf("streamed: %d frames decoded", frames), nil
	case sawFirst:
		// `timeout` can kill a healthy session before it prints a total. "First frame
		// decoded" is still proof that video arrived and the decoder ran.
		return "streamed: first frame decoded (session bounded before it reported a total)", nil
	}
	// Carry the client's own words: they distinguish a refused codec from an unreachable
	// host from a renderer that never started.
	return "", fmt.Errorf("punktfunk: stream-probe: no frame decoded — the session carried no video.\nclient said:\n%s", out)
}
