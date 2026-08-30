package punktfunk

import (
	"context"
	"strings"
	"testing"
)

// writePinOut carries the host-generated PIN to a path the client member can read. It is the
// only bridge between the two halves of a pairing, so its failure modes matter as much as its
// success: a silent empty write would make the client pair with an empty PIN.
func TestWritePinOut(t *testing.T) {
	armed := `{"enabled":true,"armed":true,"pin":"8155","expires_in_secs":119,"paired_clients":0}`

	t.Run("writes the pin 0600 at the requested path", func(t *testing.T) {
		fe := &fakeExec{}
		if err := writePinOut(context.Background(), fe, armed, "/pfshare/pin"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(fe.script, "umask 077") {
			t.Errorf("the pin must be written with a restrictive umask: %s", fe.script)
		}
		if !strings.Contains(fe.script, "'8155'") {
			t.Errorf("the pin must reach the file: %s", fe.script)
		}
		if !strings.Contains(fe.script, "> '/pfshare/pin'") {
			t.Errorf("the pin must land at the requested path: %s", fe.script)
		}
		if !strings.Contains(fe.script, "mkdir -p '/pfshare'") {
			t.Errorf("the directory must be created — a bind mount may be empty: %s", fe.script)
		}
	})

	// A host that is not armed returns no pin. Writing an empty file would make the client
	// pair with an empty PIN and fail confusingly on the far side; fail here instead.
	t.Run("a response with no pin is an error", func(t *testing.T) {
		fe := &fakeExec{}
		err := writePinOut(context.Background(), fe, `{"enabled":true,"armed":false}`, "/pfshare/pin")
		if err == nil {
			t.Fatal("a response without a pin must be an error")
		}
		if !strings.Contains(err.Error(), "no pin") {
			t.Errorf("the error should say the host returned no pin, got: %v", err)
		}
	})

	t.Run("a non-JSON response is an error", func(t *testing.T) {
		fe := &fakeExec{}
		if err := writePinOut(context.Background(), fe, "not json", "/pfshare/pin"); err == nil {
			t.Fatal("a non-JSON response must be an error")
		}
	})

	// A failed write must surface, not be swallowed — the client would otherwise read a
	// stale PIN from a previous run and fail trust verification for no visible reason.
	t.Run("a failed write surfaces", func(t *testing.T) {
		fe := &fakeExec{exit: 1, stderr: "read-only file system"}
		err := writePinOut(context.Background(), fe, armed, "/pfshare/pin")
		if err == nil {
			t.Fatal("a non-zero exit must be an error")
		}
		if !strings.Contains(err.Error(), "/pfshare/pin") {
			t.Errorf("the error should name the path, got: %v", err)
		}
	})
}
