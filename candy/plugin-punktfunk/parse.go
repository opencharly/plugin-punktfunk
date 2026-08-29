package punktfunk

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// parse.go holds the two pure helpers the provider needs — kept separate from the
// reverse-channel code so they are unit-testable with no venue at all.

// parseSSE extracts the `data:` payloads from a Server-Sent Events stream. punktfunk's
// /api/v1/events emits one JSON object per event; a payload may span several `data:`
// lines, which the SSE spec says to join with newlines, and a blank line ends the event.
func parseSSE(body string) []string {
	var (
		events []string
		cur    []string
	)
	flush := func() {
		if len(cur) > 0 {
			events = append(events, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case strings.HasPrefix(line, "data:"):
			cur = append(cur, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		default:
			// id:, event:, retry: and comments carry no payload for our purposes.
		}
	}
	flush()
	return events
}

// extractJSONPath walks a dotted path into a decoded JSON document and returns the
// value there as text, so a `stdout:` matcher can assert one field instead of
// pattern-matching the whole document.
//
// Deliberately a small dotted walk rather than a JSONPath dependency: it covers
// object keys and numeric array indices ("clients.0.name"), which is the entire shape
// of punktfunk's responses, and adds no module to the plugin's dependency surface.
func extractJSONPath(body []byte, path string) (string, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("punktfunk: json_path %q: response is not JSON: %v", path, err)
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", fmt.Errorf("punktfunk: json_path %q: no key %q", path, seg)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return "", fmt.Errorf("punktfunk: json_path %q: %q is not an array index", path, seg)
			}
			if i < 0 || i >= len(node) {
				return "", fmt.Errorf("punktfunk: json_path %q: index %d out of range (len %d)", path, i, len(node))
			}
			cur = node[i]
		default:
			return "", fmt.Errorf("punktfunk: json_path %q: %q is not traversable", path, seg)
		}
	}
	return renderScalar(cur), nil
}

// renderScalar prints a JSON value as the text a matcher compares against. Strings are
// returned bare (not re-quoted) so `contains: ok` matches a "ok" value; composites are
// re-marshalled.
func renderScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
