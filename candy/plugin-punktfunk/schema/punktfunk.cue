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
	method: "health" | "status" | "diagnostics" | "diagnostics-refresh" |
		"compositors" | "gpus" | "plugins" | "hooks" |
		"pair-status" | "pair-arm" | "pair-disarm" | "pending" | "approve" | "deny" | "pin" |
		"clients" | "rename" | "access" | "unpair" |
		"library" | "scanners" | "scanner-toggle" |
		"display-state" | "display-monitors" | "display-settings" | "display-release" |
		"actions" | "action-invoke" | "end-game" | "events"

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
	access?: "full" | "controller" | "view"
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
