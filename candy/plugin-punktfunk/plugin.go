// Package punktfunk is the charly plugin serving the `punktfunk` check verb — an
// importable root package plus its own go.mod. It drives a running punktfunk
// streaming host through the host's management REST API (bearer token over HTTPS on
// 47990) so a candy or box plan can PROBE a host (health, diagnostics, compositors,
// paired clients, library, virtual displays, plugins) and MANAGE one (arm pairing,
// approve or deny a pending device, unpair, set an access preset, end a game,
// invoke a power action) declaratively.
//
// Why this is not the generic `http:` verb. `http:` already does URL + status +
// body/header matchers + allow_insecure, and duplicating that would be pointless.
// This verb exists for the three things `http:` structurally cannot do:
//
//   - TOKEN DISCOVERY. The management API's bearer token is generated per host at
//     first start and lands at ~/.config/punktfunk/mgmt-token INSIDE the venue. An
//     authored `http:` step cannot read a per-deploy secret out of the container or
//     guest; this plugin reads it over the reverse channel (cc.Exec()).
//   - ENDPOINT RESOLUTION. cc.ResolveEndpoint turns the in-venue port into a
//     host-reachable address, so ONE authored step works unchanged against a pod's
//     published port and a VM's forwarded one.
//   - DOMAIN SEMANTICS. Methods map to endpoints and return real verdicts, instead
//     of pattern-matching a JSON blob in a stdout matcher.
//
// Dual-placement by construction: the SAME NewProvider()/NewMeta() compile INTO
// charly in-process when listed in compiled_plugins, or cmd/serve serves them
// OUT-OF-PROCESS over go-plugin gRPC when they are not — placement is invisible
// above the provider registry.
package punktfunk

import (
	"embed"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
)

//go:embed schema/*.cue
var schemaFS embed.FS

// pluginCalVer is this candy's CalVer, advertised over Describe. It must match the
// `version:` in charly.yml — the host reports it when the verb resolves.
const pluginCalVer = "2026.241.1845"

// NewProvider returns the punktfunk provider.
func NewProvider() pb.ProviderServer { return &provider{} }

// NewMeta advertises verb:punktfunk plus the plugin's self-contained CUE schema (via
// sdk.NewMeta → BuildCapabilities). The verb's whole authoring contract — the method
// enum and every punktfunk-exclusive modifier — lives in the served #PunktfunkInput
// (schema/punktfunk.cue), which the host splices onto the base and validates every
// authored `punktfunk:` step's plugin_input against.
//
// Primary is "method", so the scalar sugar `punktfunk: status` desugars to
// {method: "status"} the same way the other live probe verbs do.
func NewMeta() pb.PluginMetaServer {
	return sdk.NewMeta(pluginCalVer,
		[]sdk.ProvidedCapability{{
			Class:    "verb",
			Word:     "punktfunk",
			InputDef: "#PunktfunkInput",
			Primary:  "method",
		}},
		schemaFS)
}
