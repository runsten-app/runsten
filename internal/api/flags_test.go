package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"runsten/internal/flags"
)

// accountFlags turns the flags off for one account, and records whom it was asked about.
type accountFlags struct {
	off   string
	asked []string
}

func (f *accountFlags) Enabled(_ context.Context, _ flags.Name, accountID string) bool {
	f.asked = append(f.asked, accountID)
	return accountID != f.off
}

// TestFlags: the session gives the flags the front end reads, evaluated for its own
// account; without Flags, each has its default.
func TestFlags(t *testing.T) {
	e, c := readerEnv(t)
	base := e.api.URL + "/api/v1/"
	tesla := func(t *testing.T, c *http.Cookie) bool {
		t.Helper()
		resp, body := get(t, noFollow(), base+"session", c)
		var sess struct {
			Flags map[string]bool `json:"flags"`
		}
		if err := json.Unmarshal([]byte(body), &sess); err != nil || resp.status != http.StatusOK || len(sess.Flags) != 1 {
			t.Fatalf("session: %d %s", resp.status, body)
		}
		return sess.Flags[string(flags.SourceTesla)]
	}
	if !tesla(t, c) {
		t.Error("without Flags: source.tesla's default, on")
	}

	f := &accountFlags{off: account}
	e.server.Flags = f
	if tesla(t, c) {
		t.Error("source.tesla on for the account it is off for")
	}
	if !tesla(t, e.session(t, "other")) {
		t.Error("source.tesla off for another account")
	}
	if len(f.asked) != 2 || f.asked[0] != account || f.asked[1] == account {
		t.Errorf("asked about %v", f.asked)
	}
}
