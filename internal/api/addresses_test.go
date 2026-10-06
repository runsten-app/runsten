package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"sync"
	"testing"

	"runsten/internal/core"
)

// memAddresses knows the address of work, and records the cells asked for.
type memAddresses struct {
	mu    sync.Mutex
	asked [][]core.GeoCell
	err   error
}

func (m *memAddresses) Addresses(_ context.Context, accountID string, cells []core.GeoCell) (map[core.GeoCell]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if accountID != account {
		return nil, errors.New("another account")
	}
	m.asked = append(m.asked, cells)
	if m.err != nil {
		return nil, m.err
	}
	out := map[core.GeoCell]string{}
	if c := core.CellOf(work); slices.Contains(cells, c) {
		out[c] = "Avenue Roger Salengro, Villeurbanne"
	}
	return out, nil
}

func TestAddresses(t *testing.T) {
	e, c := readerEnv(t)
	book := &memAddresses{}
	e.server.Addresses = book
	base := e.api.URL + "/api/v1/vehicles/" + car

	var trip struct {
		StartAddress *string `json:"start_address"`
		EndAddress   *string `json:"end_address"`
	}
	resp, body := get(t, noFollow(), base+"/trips/2026-09-28T07:01:00Z", c)
	if resp.status != http.StatusOK || json.Unmarshal([]byte(body), &trip) != nil {
		t.Fatalf("trip: %d %s", resp.status, body)
	}
	// From home, a place: no address, not asked for. To work: its address.
	if trip.StartAddress != nil || trip.EndAddress == nil || *trip.EndAddress != "Avenue Roger Salengro, Villeurbanne" {
		t.Errorf("trip addresses %v, %v", trip.StartAddress, trip.EndAddress)
	}
	if want := [][]core.GeoCell{{core.CellOf(work)}}; !reflect.DeepEqual(book.asked, want) {
		t.Errorf("asked %v, want %v", book.asked, want)
	}

	// A list asks once for all its positions outside the places, each once.
	book.asked = nil
	get(t, noFollow(), base+"/trips", c)
	if len(book.asked) != 1 || !reflect.DeepEqual(book.asked[0], []core.GeoCell{core.CellOf(work)}) {
		t.Errorf("list asked %v", book.asked)
	}

	// Without the place, the charge at home is asked for: unknown yet, null.
	e.settings.places[account] = nil
	book.asked = nil
	resp, body = get(t, noFollow(), base+"/charges/2026-09-28T18:30:00Z", c)
	var charge struct {
		Address *string `json:"address"`
	}
	if resp.status != http.StatusOK || json.Unmarshal([]byte(body), &charge) != nil || charge.Address != nil {
		t.Errorf("charge: %d %s", resp.status, body)
	}
	if want := [][]core.GeoCell{{core.CellOf(home)}}; !reflect.DeepEqual(book.asked, want) {
		t.Errorf("charge asked %v, want %v", book.asked, want)
	}

	// The store fails: the read answers, without addresses.
	book.err = errors.New("database down")
	resp, body = get(t, noFollow(), base+"/trips/2026-09-28T07:01:00Z", c)
	if resp.status != http.StatusOK || json.Unmarshal([]byte(body), &trip) != nil || trip.EndAddress != nil {
		t.Errorf("failing store: %d %s", resp.status, body)
	}
}
