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
	web := 0
	for _, f := range flags.All() {
		if f.Web {
			web++
		}
	}
	session := func(t *testing.T, c *http.Cookie) map[string]bool {
		t.Helper()
		resp, body := get(t, noFollow(), base+"session", c)
		var sess struct {
			Flags map[string]bool `json:"flags"`
		}
		if err := json.Unmarshal([]byte(body), &sess); err != nil || resp.status != http.StatusOK || len(sess.Flags) != web {
			t.Fatalf("session: %d %s", resp.status, body)
		}
		return sess.Flags
	}
	tesla := func(t *testing.T, c *http.Cookie) bool {
		t.Helper()
		return session(t, c)[string(flags.SourceTesla)]
	}
	if !tesla(t, c) {
		t.Error("without Flags: source.tesla's default, on")
	}
	if session(t, c)[string(flags.SourcePolestarID)] {
		t.Error("without Flags: source.polestar-id's default, off")
	}

	f := &accountFlags{off: account}
	e.server.Flags = f
	if tesla(t, c) {
		t.Error("source.tesla on for the account it is off for")
	}
	if !tesla(t, e.session(t, "other")) {
		t.Error("source.tesla off for another account")
	}
	// Each flag the front end reads, for the session's account only.
	if n := web; len(f.asked) != 2*n || f.asked[0] != account || f.asked[n-1] != account || f.asked[n] == account {
		t.Errorf("asked about %v", f.asked)
	}
}
