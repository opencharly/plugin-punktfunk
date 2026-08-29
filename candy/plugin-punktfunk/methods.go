package punktfunk

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/opencharly/plugin-punktfunk/candy/plugin-punktfunk/params"
)

// methods.go is the single table mapping a `punktfunk:` method to one management-API
// call. Keeping it declarative (rather than a switch that also builds requests) means
// the mapping is unit-testable without a live host, and adding a method is one entry.

// apiCall is a resolved management-API request: the HTTP method, the path under the
// API root, and an optional JSON body.
type apiCall struct {
	HTTPMethod string
	Path       string
	Body       string
	// Mutating marks a method that changes host state. Such methods belong in `run:`
	// steps; the provider refuses them in a `check:` step, so a probe can never
	// silently unpair a device or reboot a machine.
	Mutating bool
	// Stream marks the SSE subscription, which the provider handles separately
	// because it reads until N events or a timeout rather than reading one response.
	Stream bool
}

// requiredField names the input field a method needs, so a missing one fails with a
// precise authoring error instead of a confusing 404 from the host.
var requiredField = map[string]string{
	"approve":        "pending_id",
	"deny":           "pending_id",
	"unpair":         "fingerprint",
	"rename":         "fingerprint",
	"access":         "fingerprint",
	"action-invoke":  "action_id",
	"scanner-toggle": "provider_id",
	"pin":            "pin",
}

// resolveCall builds the API call for a method from the typed input.
func resolveCall(in *params.PunktfunkInput) (apiCall, error) {
	m := in.Method
	if want, ok := requiredField[m]; ok {
		if err := requireField(in, want); err != nil {
			return apiCall{}, err
		}
	}
	switch m {
	// ---- host / health -------------------------------------------------------
	case "health":
		// The ONLY unauthenticated route — usable as a liveness probe before a token
		// exists, which is what makes it the right first check on a fresh host.
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/health"}, nil
	case "status":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/host"}, nil
	case "diagnostics":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/diagnostics"}, nil
	case "diagnostics-refresh":
		return apiCall{HTTPMethod: "POST", Path: "/api/v1/diagnostics/refresh", Mutating: true}, nil
	case "compositors":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/compositors"}, nil
	case "gpus":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/gpus"}, nil
	case "plugins":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/plugins"}, nil
	case "hooks":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/hooks"}, nil

	// ---- native pairing ------------------------------------------------------
	// The native (punktfunk/1) pairing plane, NOT the GameStream one: a punktfunk
	// host is native-only unless PUNKTFUNK_GAMESTREAM is set, so /api/v1/native/*
	// is the surface that exists on a default host.
	case "pair-status":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/native/pair"}, nil
	case "pair-arm":
		return apiCall{HTTPMethod: "POST", Path: "/api/v1/native/pair/arm", Mutating: true}, nil
	case "pair-disarm":
		return apiCall{HTTPMethod: "DELETE", Path: "/api/v1/native/pair", Mutating: true}, nil
	case "pending":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/native/pending"}, nil
	case "approve":
		return apiCall{HTTPMethod: "POST", Mutating: true,
			Path: "/api/v1/native/pending/" + url.PathEscape(in.PendingID) + "/approve"}, nil
	case "deny":
		return apiCall{HTTPMethod: "POST", Mutating: true,
			Path: "/api/v1/native/pending/" + url.PathEscape(in.PendingID) + "/deny"}, nil
	case "pin":
		return apiCall{HTTPMethod: "POST", Path: "/api/v1/pair/pin", Mutating: true,
			Body: jsonObject("pin", in.Pin)}, nil

	// ---- paired clients ------------------------------------------------------
	case "clients":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/native/clients"}, nil
	case "unpair":
		return apiCall{HTTPMethod: "DELETE", Mutating: true,
			Path: "/api/v1/native/clients/" + url.PathEscape(in.Fingerprint)}, nil
	case "rename":
		if in.Name == "" {
			return apiCall{}, fmt.Errorf("punktfunk: rename needs `name`")
		}
		return apiCall{HTTPMethod: "PATCH", Mutating: true,
			Path: "/api/v1/native/clients/" + url.PathEscape(in.Fingerprint),
			Body: jsonObject("name", in.Name)}, nil
	case "access":
		if in.Access == "" {
			return apiCall{}, fmt.Errorf("punktfunk: access needs `access` (full|controller|view)")
		}
		return apiCall{HTTPMethod: "PATCH", Mutating: true,
			Path: "/api/v1/native/clients/" + url.PathEscape(in.Fingerprint),
			Body: jsonObject("access", in.Access)}, nil

	// ---- library -------------------------------------------------------------
	case "library":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/library"}, nil
	case "scanners":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/library/scanners"}, nil
	case "scanner-toggle":
		return apiCall{HTTPMethod: "PUT", Mutating: true,
			Path: "/api/v1/library/scanners/" + url.PathEscape(in.ProviderID),
			Body: jsonBool("enabled", in.Enabled)}, nil

	// ---- virtual displays ----------------------------------------------------
	case "display-state":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/display/state"}, nil
	case "display-monitors":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/display/monitors"}, nil
	case "display-settings":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/display/settings"}, nil
	case "display-release":
		return apiCall{HTTPMethod: "POST", Path: "/api/v1/display/release", Mutating: true}, nil

	// ---- lifecycle -----------------------------------------------------------
	case "actions":
		return apiCall{HTTPMethod: "GET", Path: "/api/v1/actions"}, nil
	case "action-invoke":
		return apiCall{HTTPMethod: "POST", Mutating: true,
			Path: "/api/v1/actions/" + url.PathEscape(in.ActionID)}, nil
	case "end-game":
		return apiCall{HTTPMethod: "POST", Path: "/api/v1/game/end", Mutating: true}, nil

	// ---- events --------------------------------------------------------------
	case "events":
		p := "/api/v1/events"
		if in.Kinds != "" {
			p += "?kinds=" + url.QueryEscape(in.Kinds)
		}
		return apiCall{HTTPMethod: "GET", Path: p, Stream: true}, nil
	}
	return apiCall{}, fmt.Errorf("punktfunk: unknown method %q", m)
}

// requireField reports a missing required input field by name.
func requireField(in *params.PunktfunkInput, field string) error {
	var got string
	switch field {
	case "pending_id":
		got = in.PendingID
	case "fingerprint":
		got = in.Fingerprint
	case "action_id":
		got = in.ActionID
	case "provider_id":
		got = in.ProviderID
	case "pin":
		got = in.Pin
	}
	if strings.TrimSpace(got) == "" {
		return fmt.Errorf("punktfunk: %s needs `%s`", in.Method, field)
	}
	return nil
}

// jsonObject builds a one-field JSON body. Hand-built rather than marshalled from a
// map so key order is stable and the body is trivially readable in a failure message.
func jsonObject(key, val string) string {
	return fmt.Sprintf("{%q:%q}", key, val)
}

func jsonBool(key string, val bool) string {
	return fmt.Sprintf("{%q:%t}", key, val)
}
