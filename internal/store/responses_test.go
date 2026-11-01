package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"runsten/internal/api"
	"runsten/internal/collector"
)

// TestEachResponse: the responses stored of the period, oldest first, as the provider
// gave them, read within the account only.
func TestEachResponse(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, v := newVehicle(t, s, "token", "YV1AAAAAAAAAAAAA1")
	b, w := newVehicle(t, s, "token-b", "YV1AAAAAAAAAAAAA2")
	t0 := time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)
	save := func(account, vehicle, source, endpoint string, at time.Time, payload string) {
		t.Helper()
		_, err := s.SaveSnapshot(ctx, collector.Snapshot{
			AccountID: account, VehicleID: vehicle, Source: source, Endpoint: collector.Endpoint(endpoint),
			FetchedAt: at, Payload: []byte(payload), Hash: []byte(source + endpoint + payload),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	save(a, v, "volvo", "odometer", t0.Add(-time.Second), `{"data":{"odometer":{"value":1}}}`) // before the period
	save(a, v, "volvo", "odometer", t0, `{"data":{"odometer":{"value":2}}}`)
	save(a, v, "volvo", "energy-state", t0, `{"data":{"batteryChargeLevel":{"value":80.50}}}`)
	save(a, v, "polestar", "battery", t0.Add(time.Minute), `{"data":{}}`)
	save(a, v, "volvo", "odometer", t0.Add(time.Hour), `{"data":{"odometer":{"value":3}}}`) // at the period's end
	save(b, w, "volvo", "odometer", t0, `{"data":{"odometer":{"value":9}}}`)

	var got []api.Response
	collect := func(r api.Response) error { got = append(got, r); return nil }
	if err := s.EachResponse(ctx, a, v, t0, t0.Add(time.Hour), collect); err != nil {
		t.Fatal(err)
	}
	want := []api.Response{
		{Source: "volvo", Endpoint: "energy-state", FetchedAt: t0, Payload: []byte(`{"data": {"batteryChargeLevel": {"value": 80.50}}}`)},
		{Source: "volvo", Endpoint: "odometer", FetchedAt: t0, Payload: []byte(`{"data": {"odometer": {"value": 2}}}`)},
		{Source: "polestar", Endpoint: "battery", FetchedAt: t0.Add(time.Minute), Payload: []byte(`{"data": {}}`)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d responses: %+v", len(got), got)
	}
	for i, r := range got {
		if r.Source != want[i].Source || r.Endpoint != want[i].Endpoint || !r.FetchedAt.Equal(want[i].FetchedAt) ||
			string(r.Payload) != string(want[i].Payload) {
			t.Errorf("%d: %s %s %v %s", i, r.Source, r.Endpoint, r.FetchedAt, r.Payload)
		}
	}

	// Another account's vehicle: nothing, whatever its ID.
	got = nil
	if err := s.EachResponse(ctx, a, w, t0, t0.Add(time.Hour), collect); err != nil || len(got) != 0 {
		t.Errorf("another account's vehicle: %v, %v", got, err)
	}
	// An error of fn stops the read.
	boom, n := errors.New("boom"), 0
	err := s.EachResponse(ctx, a, v, t0, t0.Add(time.Hour), func(api.Response) error { n++; return boom })
	if !errors.Is(err, boom) || n != 1 {
		t.Errorf("stopped: %v after %d", err, n)
	}
}
