package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// SetVehicleAlias writes the alias like the store, without checking it.
func (m *memReader) SetVehicleAlias(_ context.Context, acc, vehicle, alias string) error {
	m.writes++
	if m.modelErr != nil {
		return m.modelErr
	}
	for i, v := range m.vehicles[acc] {
		if v.ID == vehicle {
			m.vehicles[acc][i].Alias = alias
			return nil
		}
	}
	return ErrNotFound
}

func TestPutVehicleAlias(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/vehicles/"
	put := func(vehicle, body string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodPut, u+vehicle+"/alias", jsonType, body, c)
	}
	alias := func(body string) *string {
		t.Helper()
		var v struct {
			Alias *string `json:"alias"`
		}
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatal(err)
		}
		return v.Alias
	}
	str := func(p *string) string {
		if p == nil {
			return "<null>"
		}
		return *p
	}

	if _, body := get(t, noFollow(), u+car, c); alias(body) != nil {
		t.Errorf("a vehicle without an alias: %s", str(alias(body)))
	}
	resp, body := put(car, `{"alias":"Fleet car 12"}`)
	if resp.status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.status, body)
	}
	golden(t, "vehicle-alias.json", body)

	for _, tt := range []struct{ name, body, want string }{
		{"trimmed", `{"alias":"  Long-range car  "}`, "Long-range car"},
		{"forty characters, accented", `{"alias":"` + strings.Repeat("é", MaxAlias) + `"}`, strings.Repeat("é", MaxAlias)},
		{"removed", `{"alias":null}`, "<null>"},
	} {
		resp, body := put(car, tt.body)
		if resp.status != http.StatusOK || str(alias(body)) != tt.want {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		// What was written shows on every read, the list's included.
		if _, read := get(t, noFollow(), u+car, c); str(alias(read)) != tt.want {
			t.Errorf("%s, read again: %s", tt.name, read)
		}
		if _, list := get(t, noFollow(), e.api.URL+"/api/v1/vehicles", c); tt.want != "<null>" && !strings.Contains(list, `"alias":"`+tt.want+`"`) {
			t.Errorf("%s, in the list: %s", tt.name, list)
		}
	}

	writes := e.reader.writes
	for _, tt := range []struct{ name, body string }{
		{"empty", `{"alias":""}`},
		{"blank", `{"alias":"   "}`},
		{"too long", `{"alias":"` + strings.Repeat("a", MaxAlias+1) + `"}`},
		{"a control character", `{"alias":"Car\u0007"}`},
		{"a new line", `{"alias":"Car\nTwo"}`},
		{"a key missing", `{}`},
		{"an unknown key", `{"alias":null,"name":"x"}`},
		{"not a string", `{"alias":12}`},
	} {
		resp, body := put(car, tt.body)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, "alias") && tt.name != "an unknown key" {
			t.Errorf("%s: %d %s", tt.name, resp.status, body)
		}
		wantError(t, body, codeInvalidBody)
	}
	for _, id := range []string{foreign, "0b5c6c3e-3f0e-4a57-9d3b-2f4f9e8d1a09", "not-a-uuid"} {
		resp, body := put(id, `{"alias":"Mine"}`)
		if resp.status != http.StatusNotFound {
			t.Errorf("%s: %d", id, resp.status)
		}
		wantError(t, body, codeNotFound)
	}
	if e.reader.writes != writes {
		t.Errorf("%d refused writes reached the store", e.reader.writes-writes)
	}
	if v := e.reader.vehicles["other"][0]; v.Alias != "" {
		t.Errorf("another account's vehicle written: %+v", v)
	}

	// The vehicle gone between the read and the write.
	e.reader.modelErr = ErrNotFound
	if resp, body := put(car, `{"alias":"Gone"}`); resp.status != http.StatusNotFound {
		t.Errorf("gone: %d %s", resp.status, body)
	}
}

func TestVehicleAliasErrors(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/vehicles/" + car + "/alias"
	e.reader.modelErr = errors.New("boom")
	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, `{"alias":"Mine"}`, c)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
		t.Errorf("write failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)

	// Written, but the vehicle's details not read for the response.
	e.reader.modelErr = nil
	e.server.States = failingStates{}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, `{"alias":"Mine"}`, c)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("details not read: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)

	for _, contentType := range []string{"", "text/plain"} {
		resp, body := do(t, noFollow(), http.MethodPut, u, contentType, `{"alias":null}`, c)
		if resp.status != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: %d", contentType, resp.status)
		}
		wantError(t, body, codeUnsupportedMediaType)
	}
}
