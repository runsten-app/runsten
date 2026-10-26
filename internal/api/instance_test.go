package api

import (
	"net/http"
	"testing"
)

// fakeReleases is a Releases with a newer release, or none.
type fakeReleases struct {
	update Release
	ok     bool
}

func (f fakeReleases) Update() (Release, bool) { return f.update, f.ok }

// TestInstanceJSON: the release running, without a check, with one and nothing newer,
// and with a newer release out.
func TestInstanceJSON(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/instance"
	e.server.Version = "0.11.0"

	if _, body := get(t, noFollow(), u, c); body != `{"version":"0.11.0","update_check":false,"update":null}`+"\n" {
		t.Errorf("no check: %s", body)
	}
	e.server.Releases = fakeReleases{}
	_, body := get(t, noFollow(), u, c)
	golden(t, "instance.json", body)
	e.server.Releases = fakeReleases{
		update: Release{Version: "0.12.0", URL: "https://github.com/runsten-app/runsten/releases/tag/v0.12.0"}, ok: true,
	}
	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "instance-update.json", body)

	resp, body = get(t, noFollow(), u)
	if resp.status != http.StatusUnauthorized {
		t.Errorf("without a session: %d", resp.status)
	}
	wantError(t, body, "unauthorized")
}
