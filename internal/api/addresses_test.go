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

// TestStatePlace: the state names its position as a trip's ends: by the place that holds
// it, else by its address, asked for once per read and never at a place.
func TestStatePlace(t *testing.T) {
	e, c := readerEnv(t)
	book := &memAddresses{}
	e.server.Addresses = book
	url := e.api.URL + "/api/v1/vehicles/" + car + "/state"
	at := func(p core.Position) {
		e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: core.Snapshot{
			Covers: core.FieldPosition, Position: core.Some(p, h(4, 0)),
		}}}
	}
	type state struct {
		Place   *placeRefJSON `json:"place"`
		Address *string       `json:"address"`
	}
	read := func() state {
		t.Helper()
		resp, body := get(t, noFollow(), url, c)
		var st state
		if resp.status != http.StatusOK || json.Unmarshal([]byte(body), &st) != nil {
			t.Fatalf("state: %d %s", resp.status, body)
		}
		return st
	}

	// At home, a place: its name, no address, the geocoder not asked.
	at(home)
	if st := read(); st.Place == nil || *st.Place != (placeRefJSON{ID: placeID(1), Name: homeName}) || st.Address != nil {
		t.Errorf("at home: %+v", st)
	}
	if len(book.asked) != 0 {
		t.Errorf("asked at a place: %v", book.asked)
	}

	// At work, outside every place: its address, its cell alone asked for at each read,
	// the store's cache resolving it once whatever the reads.
	at(work)
	for range 3 {
		if st := read(); st.Place != nil || st.Address == nil || *st.Address != "Avenue Roger Salengro, Villeurbanne" {
			t.Errorf("at work: %+v", st)
		}
	}
	cell := []core.GeoCell{core.CellOf(work)}
	if want := [][]core.GeoCell{cell, cell, cell}; !reflect.DeepEqual(book.asked, want) {
		t.Errorf("asked %v, want %v", book.asked, want)
	}

	// Outside every place, an address not known yet: null, asked for a later read.
	e.settings.places[account] = nil
	book.asked = nil
	at(home)
	if st := read(); st.Place != nil || st.Address != nil {
		t.Errorf("unknown address: %+v", st)
	}
	if want := [][]core.GeoCell{{core.CellOf(home)}}; !reflect.DeepEqual(book.asked, want) {
		t.Errorf("asked %v, want %v", book.asked, want)
	}

	// Without a position: both null, nothing asked.
	book.asked = nil
	e.reader.current[car] = []core.Record{{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: core.Snapshot{
		Covers: core.FieldOdometer, OdometerKm: core.Some(12400.0, h(4, 0)),
	}}}
	if st := read(); st.Place != nil || st.Address != nil || len(book.asked) != 0 {
		t.Errorf("without a position: %+v, asked %v", st, book.asked)
	}

	// Without a geocoder: no address.
	e.server.Addresses = nil
	at(work)
	if st := read(); st.Address != nil {
		t.Errorf("without a geocoder: %+v", st)
	}
}
