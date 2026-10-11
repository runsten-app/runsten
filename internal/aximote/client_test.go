package aximote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"runsten/internal/simulator/aximoteapi"
)

const vin = "YV1SMLT0000ER0001"

var base = time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC)

// newFake starts the simulator's Aximote: the contract test between the client and the
// fake backend.
func newFake(t *testing.T) (*Client, *aximoteapi.Handler) {
	t.Helper()
	h := aximoteapi.NewHandler(aximoteapi.DevToken, aximoteapi.Default(base, vin))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/", srv.Client()), h
}

func TestVehicles(t *testing.T) {
	c, _ := newFake(t)
	vs, err := c.Vehicles(context.Background(), aximoteapi.DevToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].ID != "veh-ex30" || vs[0].VIN == nil || *vs[0].VIN != vin || *vs[0].BatteryCapacityKWh != 64 ||
		vs[1].VIN != nil || vs[1].BatteryCapacityKWh != nil || *vs[1].Year != 2016 {
		t.Errorf("vehicles %+v", vs)
	}
	v, err := c.Vehicle(context.Background(), aximoteapi.DevToken, "veh-ex30")
	if err != nil || *v.Name != "My EX30" {
		t.Errorf("vehicle %+v, %v", v, err)
	}
	var e *APIError
	if _, err := c.Vehicle(context.Background(), aximoteapi.DevToken, "nope"); !errors.As(err, &e) || e.Status != http.StatusNotFound {
		t.Errorf("unknown vehicle: %v", err)
	}
}

func TestTripsByPage(t *testing.T) {
	c, h := newFake(t)
	ctx := context.Background()
	q := Query{VehicleID: "veh-ex30", Until: base, Limit: 2}
	var ids []string
	for range 3 {
		p, err := c.Trips(ctx, aximoteapi.DevToken, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, trip := range p.Items {
			ids = append(ids, trip.ID)
		}
		if q.Cursor = p.NextCursor; q.Cursor == "" {
			break
		}
	}
	if strings.Join(ids, ",") != "trip-1003,trip-1002,trip-1001" {
		t.Errorf("trips %v", ids)
	}
	calls := h.Calls()
	if len(calls) != 2 || !strings.Contains(calls[0], "until=2026-10-11T09%3A30%3A00Z") || !strings.Contains(calls[1], "cursor=2") {
		t.Errorf("calls %q", calls)
	}

	// Until bounds the starts.
	p, err := c.Trips(ctx, aximoteapi.DevToken, Query{VehicleID: "veh-ex30", Until: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)})
	if err != nil || len(p.Items) != 2 || p.NextCursor != "" {
		t.Errorf("until: %+v, %v", p, err)
	}
	trip := p.Items[1]
	if trip.ID != "trip-1001" || *trip.DistanceKm != 24.5 || *trip.EnergyConsumedKWh != 5.1 || trip.EndTime == nil ||
		*trip.StartLocation.Locality != "Lyon" || *trip.EndLocation.Country != "France" {
		t.Errorf("trip %+v", trip)
	}
}

func TestRefuels(t *testing.T) {
	c, _ := newFake(t)
	p, err := c.Refuels(context.Background(), aximoteapi.DevToken, Query{VehicleID: "veh-ex30"})
	if err != nil || len(p.Items) != 3 {
		t.Fatalf("%+v, %v", p, err)
	}
	r := p.Items[2]
	if r.ID != "charge-501" || r.Type != RefuelCharge || r.IsActive || *r.StartEnergyKWh != 41.6 || *r.EnergyChargedKWh != 16.8 ||
		*r.CostAmount != 4.37 || *r.CostCurrency != "EUR" || r.Location == nil {
		t.Errorf("charge %+v", r)
	}
	if p.Items[0].ID != "charge-502" || !p.Items[0].IsActive || p.Items[0].EndTime != nil || p.Items[1].Type != RefuelFuel {
		t.Errorf("refuels %+v", p.Items)
	}
}

func TestTracks(t *testing.T) {
	c, _ := newFake(t)
	tracks, err := c.Tracks(context.Background(), aximoteapi.DevToken, []string{"trip-1001", "trip-1002"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 ||
		string(tracks["trip-1001"]) != `{"type":"LineString","coordinates":[[4.8357,45.764],[4.86,45.77],[4.9,45.776],[4.927,45.7797]]}` {
		t.Errorf("tracks %q", tracks)
	}
}

func TestParseTracks(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       map[string]string // nil: unexpected format
	}{
		{"not JSON", `<gpx/>`, nil},
		{"a single feature", `{"type":"Feature","geometry":{"type":"LineString","coordinates":[[1,2],[3,4]]}}`, nil},
		{
			"named otherwise", `{"type":"FeatureCollection","features":[
			{"type":"Feature","id":"a","geometry":{"type":"LineString","coordinates":[[1,2,300],[3,4,310]]}},
			{"type":"Feature","properties":{"id":"b"},"geometry":{"type":"LineString","coordinates":[[1,2],[3,4]]}}]}`,
			map[string]string{
				"a": `{"type":"LineString","coordinates":[[1,2],[3,4]]}`,
				"b": `{"type":"LineString","coordinates":[[1,2],[3,4]]}`,
			},
		},
		{
			"features left out", `{"type":"FeatureCollection","features":[
			{"type":"Feature","geometry":{"type":"LineString","coordinates":[[1,2],[3,4]]}},
			{"type":"Feature","properties":{"tripId":"point"},"geometry":{"type":"Point","coordinates":[1,2]}},
			{"type":"Feature","properties":{"tripId":"one"},"geometry":{"type":"LineString","coordinates":[[1,2]]}},
			{"type":"Feature","properties":{"tripId":"off"},"geometry":{"type":"LineString","coordinates":[[1,95],[3,4]]}},
			{"type":"Feature","properties":{"tripId":"none"},"geometry":null}]}`,
			map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTracks([]byte(tc.body))
			if (got == nil) != (tc.want == nil) || len(got) != len(tc.want) {
				t.Fatalf("got %q", got)
			}
			for id, track := range tc.want {
				if string(got[id]) != track {
					t.Errorf("%s: %s", id, got[id])
				}
			}
		})
	}

	// An export the client does not understand.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<gpx/>")) }))
	defer srv.Close()
	if _, err := NewClient(srv.URL, srv.Client()).Tracks(context.Background(), "t", []string{"a"}); !errors.Is(err, ErrUnexpectedFormat) {
		t.Errorf("err %v", err)
	}
}

func TestErrors(t *testing.T) {
	c, h := newFake(t)
	ctx := context.Background()
	var e *APIError

	_, err := c.Vehicles(ctx, "axm_pat_BBBBBBBBBBBBBBBBBBBBBBBBBB")
	if !errors.As(err, &e) || !e.TokenRefused() || e.Status != http.StatusUnauthorized {
		t.Errorf("another token: %v", err)
	}
	if strings.Contains(err.Error(), "axm_pat") {
		t.Errorf("the token in the error: %v", err)
	}

	h.LimitAfter(1, 30*time.Second)
	if _, err := c.Vehicles(ctx, aximoteapi.DevToken); err != nil {
		t.Fatal(err)
	}
	_, err = c.Trips(ctx, aximoteapi.DevToken, Query{VehicleID: "veh-ex30"})
	if !errors.As(err, &e) || e.Kind != KindRateLimited || e.RetryIn != 30*time.Second || e.TokenRefused() {
		t.Errorf("limited: %+v", err)
	}
	if _, err := c.Trips(ctx, aximoteapi.DevToken, Query{VehicleID: "veh-ex30"}); err != nil {
		t.Errorf("after the limit: %v", err)
	}

	h.Revoke()
	if _, err := c.Tracks(ctx, aximoteapi.DevToken, []string{"trip-1001"}); !errors.As(err, &e) || !e.TokenRefused() {
		t.Errorf("revoked: %v", err)
	}

	for header, want := range map[string]time.Duration{"": time.Minute, "x": time.Minute, "0": time.Minute, "120": 2 * time.Minute} {
		if got := retryAfter(header); got != want {
			t.Errorf("Retry-After %q: %v", header, got)
		}
	}

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[` + strings.Repeat(" ", maxBody) + `]`))
	}))
	defer big.Close()
	if _, err := NewClient(big.URL, big.Client()).Vehicles(ctx, "t"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	if _, err := NewClient("http://127.0.0.1:1", http.DefaultClient).Vehicles(ctx, "t"); err == nil || errors.As(err, &e) {
		t.Errorf("unreachable: %v", err)
	}
}
