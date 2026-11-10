// The `punktfunk` plugin's OWN CUE schema — the typed plugin_input for the
// `punktfunk` check verb, which drives a punktfunk streaming host's management
// REST API (bearer token over HTTPS on 47990).
//
// SELF-CONTAINED by contract: it references NO base def, so it compiles standalone
// (both `cue exp gengotypes` and the host's load-gate compile) AND splices onto the
// base as `base ++ plugin`.
//
// Single source, used two ways:
//  1. GENERATE ../params/cue_types_gen.go via `cue exp gengotypes`, so the provider
//     decodes plugin_input into a TYPED struct rather than a hand-parsed map.
//  2. VALIDATE authored input AT RUNTIME — served over Describe, spliced by the host,
//     and every authored `punktfunk:` step's plugin_input checked against #PunktfunkInput.
#PunktfunkInput: {
	// method — the operation (also the scalar-sugar primary, so `punktfunk: status`
	// desugars to {method: "status"}).
	//
	// Read methods are safe on any host; the mutating ones (pair-arm, approve, deny,
	// unpair, rename, access, end-game, action-invoke, scanner-toggle) change host
	// state and belong in `run:` steps, not `check:` steps.
	//
	// S3: the words are rows of the `_method` table at the foot of this file and
	// the enum below is derived from its word list, so the catalog has ONE home —
	// a new method is one row there, and the table, the derived list and this enum
	// cannot disagree.
	method: or(_methodWord)

	// host — the client-side `<host-ref>`: a saved host name, or host[:port]. Required
	// by every client method that names a host.
	host?: string

	// probe — hosts-list only: add the live reachability check (`--probe`).
	probe?: bool

	// game — launch only: stream directly into a specific game id (`--game`).
	game?: string

	// auto_approve — open only: `--yes`, auto-approving an unknown host. Documented
	// for automation; it trades a trust prompt for unattended operation, so it is opt-in
	// per step rather than implied.
	auto_approve?: bool @go(AutoApprove)

	// pin_out — pair-arm only: write the PIN the host generates to this path INSIDE the
	// venue. The host GENERATES the pairing PIN and ignores a caller-supplied one (verified
	// against 0.33.0-1: POST pair/arm with {"pin":"424242"} returns a different, host-chosen
	// pin), and charly has no mechanism to carry a step's output into another step's input.
	// So a fleet bed that pairs two members needs the PIN on disk, on a path both members
	// mount — the faithful automation of a human reading the PIN off one screen and typing
	// it into the other. Written with 0600.
	pin_out?: string & =~"^/" @go(PinOut)

	// pin_file — pair only: read the PIN from this path inside the venue instead of `pin:`.
	// Pairs with pin_out. Preferred over an inline `pin:` for the same reason token_file is
	// preferred over an inline token: the value never appears in the manifest.
	pin_file?: string & =~"^/" @go(PinFile)

	// stream_for — stream-probe only: how long to hold the session open before counting
	// frames. The probe is BOUNDED on purpose: a real session never ends by itself, so an
	// unbounded launch would hang the bed instead of asserting anything.
	stream_for?: string & =~"^[0-9]+s$" @go(StreamFor)

	// client_bin — override the client binary. The Flatpak build is not on PATH and is
	// reached as `flatpak run --command=punktfunk io.unom.Punktfunk`.
	client_bin?: string @go(ClientBin)

	// port — the in-venue management API port. Default 47990. Resolved to a
	// host-reachable address by the host's generic endpoint reverse-leg, so the same
	// authored step works against a pod's published port and a VM's forwarded one.
	port?: int & >0 & <65536

	// token — the management bearer token, inline. Prefer token_file or the default
	// discovery (see the plugin's token resolution order); an inline token in
	// charly.yml is a committed secret.
	token?: string
	// token_file — read the bearer token from this path INSIDE the venue. Defaults to
	// ~/.config/punktfunk/mgmt-token, which is where the host writes it at first start.
	token_file?: string @go(TokenFile)

	// verify_tls — require a verifiable TLS certificate chain. Deliberately named so the
	// ZERO VALUE is the right default: punktfunk's management API and web console both
	// serve a SELF-SIGNED certificate, so verification fails on every stock host. A
	// bool cannot distinguish "unset" from "false" once gengotypes drops omitempty
	// semantics, so an `insecure?: bool` field could never default to true — this
	// inversion is how the correct default survives the round trip.
	verify_tls?: bool @go(VerifyTLS)

	// fingerprint — the paired client this operation targets (rename / access / unpair).
	fingerprint?: string
	// pending_id — the pending device this operation targets (approve / deny).
	pending_id?: string @go(PendingID)
	// action_id — the host action to invoke (power.sleep / power.reboot / power.shutdown).
	action_id?: string @go(ActionID)
	// provider_id — the library provider or scanner this operation targets.
	provider_id?: string @go(ProviderID)
	// name — the new display name for `rename`.
	name?: string
	// access — the access preset for `access`.
	access?: or(_accessWord)
	// pin — the pairing PIN submitted by `pin`.
	pin?: string
	// enabled — the desired state for scanner-toggle.
	enabled?: bool

	// kinds — an event-kind filter for `events` (e.g. "pairing.*,stream.started").
	kinds?: string
	// count — how many events `events` waits for before returning. Default 1.
	count?: int & >0
	// event_timeout — how long `events` waits before giving up. Default 30s.
	event_timeout?: string @go(EventTimeout)

	// json_path — a dotted path into the JSON response; when set, the verb's stdout is
	// the value at that path rather than the whole document, so a `stdout:` matcher can
	// assert one field without pattern-matching a blob.
	json_path?: string @go(JSONPath)
}

// --- vocabulary tables (S3) --------------------------------------------------
// Every closed set of words is a table keyed by word, and its word list is
// derived from the table and used as `or()` of that list (§10.1 S3, D-PAT-3).
// The table and the enum it feeds therefore cannot disagree, and a new word is
// ONE row here. `@vocab` names the owner (§3.1): this set is compiled into THIS
// plugin, so the owner is the plugin's own word `punktfunk`.
_method: {
	// host / health
	health: {}
	status: {}
	diagnostics: {}
	"diagnostics-refresh": {}
	compositors: {}
	gpus: {}
	plugins: {}
	hooks: {}
	// native pairing
	"pair-status": {}
	"pair-arm": {}
	"pair-disarm": {}
	pending: {}
	approve: {}
	deny: {}
	pin: {}
	// paired clients
	clients: {}
	rename: {}
	access: {}
	unpair: {}
	// library
	library: {}
	scanners: {}
	"scanner-toggle": {}
	// virtual displays
	"display-state": {}
	"display-monitors": {}
	"display-settings": {}
	"display-release": {}
	// lifecycle
	actions: {}
	"action-invoke": {}
	"end-game": {}
	// stream
	events: {}
	// CLIENT methods — served by the headless `punktfunk` CLI that every Linux
	// client package ships, not by the host management API. They run inside the
	// venue where punktfunk-client is installed, so a fleet bed drives BOTH sides
	// of a stream in charly verbs instead of shelling out.
	"hosts-list": {}
	"hosts-add": {}
	"hosts-forget": {}
	pair: {}
	launch: {}
	"client-library": {}
	"speed-test": {}
	open: {}
	wake: {}
	reachable: {}
	"profiles-list": {}
	"client-reset": {}
	"stream-probe": {}
} @vocab(punktfunk)
_methodWord: [for w, _ in _method {w}]

_access: {full: {}, controller: {}, view: {}} @vocab(punktfunk)
_accessWord: [for w, _ in _access {w}]
