package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func (m *memReader) DeleteVehicle(_ context.Context, acc, vehicle string) error {
	if m.err != nil {
		return m.err
	}
	n := len(m.vehicles[acc])
	m.vehicles[acc] = slices.DeleteFunc(m.vehicles[acc], func(v Vehicle) bool { return v.ID == vehicle })
	if len(m.vehicles[acc]) == n {
		return ErrNotFound
	}
	delete(m.trips, vehicle)
	delete(m.charges, vehicle)
	return nil
}

func TestDeleteVehicle(t *testing.T) {
	e, c := readerEnv(t)
	logs := logged(e)
	u := e.api.URL + apiPrefix + "/vehicles/"
	del := func(id string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodDelete, u+id, "", "", c)
	}
	for _, tt := range []struct {
		id     string
		status int
	}{
		{foreign, http.StatusNotFound}, // another account's
		{"not-a-uuid", http.StatusNotFound},
		{car, http.StatusNoContent},
		{car, http.StatusNotFound}, // deleted already
	} {
		resp, body := del(tt.id)
		if resp.status != tt.status {
			t.Errorf("delete %s: %d %s", tt.id, resp.status, body)
		}
		if tt.status == http.StatusNotFound {
			wantError(t, body, codeNotFound)
		}
	}
	if resp, _ := get(t, noFollow(), u+car, c); resp.status != http.StatusNotFound {
		t.Errorf("the deleted vehicle read: %d", resp.status)
	}
	if resp, _ := get(t, noFollow(), u+parked, c); resp.status != http.StatusOK {
		t.Errorf("the account's other vehicle: %d", resp.status)
	}
	if e.reader.accountOf(foreign) == "" {
		t.Error("another account's vehicle deleted")
	}
	if !strings.Contains(logs.String(), "vehicle deleted") {
		t.Errorf("the deletion is not logged:\n%s", logs)
	}

	e.reader.err = errors.New("boom")
	e.server.Reader = foundReader{e.reader}
	resp, body := del(parked)
	if resp.status != http.StatusInternalServerError || strings.Contains(body, "boom") {
		t.Errorf("deletion failed: %d %s", resp.status, body)
	}
	wantError(t, body, codeInternal)
}

// foundReader finds the vehicles whatever its memReader's error: the deletion fails after.
type foundReader struct{ *memReader }

func (r foundReader) Vehicle(ctx context.Context, acc, id string) (Vehicle, bool, error) {
	err := r.err
	r.err = nil
	defer func() { r.err = err }()
	return r.memReader.Vehicle(ctx, acc, id)
}
