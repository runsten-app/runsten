package oauth

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"runsten/internal/platform/clock"
)

func TestFlows(t *testing.T) {
	clk := clock.NewManual(t0)
	f := NewFlows(clk, 10*time.Minute, 2)

	state, challenge, err := f.Start("acc")
	if err != nil {
		t.Fatal(err)
	}
	verifier, owner, err := f.Finish(state)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "acc" {
		t.Errorf("owner %q", owner)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(verifier) || oauth2.S256ChallengeFromVerifier(verifier) != challenge {
		t.Errorf("verifier %q, challenge %q", verifier, challenge)
	}
	if _, _, err := f.Finish(state); !errors.Is(err, ErrUnknownState) {
		t.Errorf("state reused: %v", err)
	}
	if _, _, err := f.Finish("forged"); !errors.Is(err, ErrUnknownState) {
		t.Errorf("unknown state: %v", err)
	}

	expiring, _, _ := f.Start("acc")
	clk.Advance(10 * time.Minute)
	if _, _, err := f.Finish(expiring); !errors.Is(err, ErrUnknownState) {
		t.Errorf("expired state: %v", err)
	}

	for range 2 {
		if _, _, err := f.Start("acc"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.Start("acc"); !errors.Is(err, ErrTooManyFlows) {
		t.Errorf("third pending flow: %v", err)
	}
	clk.Advance(10 * time.Minute) // expired flows are purged
	if _, _, err := f.Start("acc"); err != nil {
		t.Errorf("after expiry: %v", err)
	}
}

func TestSameState(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{{"x", "x", true}, {"x", "y", false}, {"", "", false}, {"x", "", false}} {
		if got := SameState(tt.a, tt.b); got != tt.want {
			t.Errorf("SameState(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}

type enrollStore struct {
	creds    Credentials
	vehicles []string
	failAt   string
	refused  map[time.Time]bool // by the key's setAt
}

func (e *enrollStore) SetKeyRefused(_ context.Context, _, _ string, setAt time.Time, refused bool) error {
	if e.failAt == "key" {
		return errors.New("boom")
	}
	if e.refused == nil {
		e.refused = map[time.Time]bool{}
	}
	e.refused[setAt] = refused
	return nil
}

func (e *enrollStore) SaveConnection(_ context.Context, _ string, c Credentials) (string, error) {
	if e.failAt == "connection" {
		return "", errors.New("boom")
	}
	e.creds = c
	return "conn", nil
}

func (e *enrollStore) AddVehicle(_ context.Context, _, _, vin string) (string, error) {
	if e.failAt == "vehicle" {
		return "", errors.New("boom")
	}
	e.vehicles = append(e.vehicles, vin)
	return "veh-" + vin, nil
}

type lister struct {
	vins []string
	err  error
	key  *string // the key the vehicles were listed with
}

func (l lister) Vehicles(_ context.Context, key, _ string) ([]string, error) {
	if l.key != nil {
		*l.key = key
	}
	return l.vins, l.err
}

// refusedKey is the provider's refusal of an application key.
type refusedKey struct{}

func (refusedKey) Error() string    { return "key refused" }
func (refusedKey) KeyRefused() bool { return true }

func TestEnroll(t *testing.T) {
	creds := Credentials{AccessToken: "a", RefreshToken: "r"}
	st := &enrollStore{}
	vins, err := Enroll(context.Background(), st, lister{vins: []string{"VIN1", "VIN2"}}, "acc", APIKey{}, creds, 0)
	if err != nil || len(vins) != 2 || st.creds != creds || len(st.vehicles) != 2 || st.refused != nil {
		t.Fatalf("Enroll = %v, %v; store %+v", vins, err, st)
	}

	for name, tt := range map[string]struct {
		lister lister
		creds  Credentials
		failAt string
	}{
		"empty token":      {lister{vins: []string{"VIN1"}}, Credentials{}, ""},
		"listing fails":    {lister{err: errors.New("403")}, creds, ""},
		"connection fails": {lister{vins: []string{"VIN1"}}, creds, "connection"},
		"vehicle fails":    {lister{vins: []string{"VIN1"}}, creds, "vehicle"},
	} {
		t.Run(name, func(t *testing.T) {
			st := &enrollStore{failAt: tt.failAt}
			if _, err := Enroll(context.Background(), st, tt.lister, "acc", APIKey{}, tt.creds, 0); err == nil {
				t.Error("error expected")
			}
			if tt.failAt == "" && st.creds.AccessToken != "" {
				t.Error("connection stored although enrollment failed")
			}
		})
	}
}

// TestEnrollNoVehicle: credentials that give access to no vehicle store nothing.
func TestEnrollNoVehicle(t *testing.T) {
	st := &enrollStore{}
	if _, err := Enroll(context.Background(), st, lister{}, "acc", APIKey{}, Credentials{AccessToken: "a"}, 0); !errors.Is(err, ErrNoVehicle) ||
		st.creds.AccessToken != "" || len(st.vehicles) != 0 {
		t.Errorf("no vehicle: %v; store %+v", err, st)
	}
}

// TestEnrollCapped: more vehicles than the cap store nothing; as many are enrolled.
func TestEnrollCapped(t *testing.T) {
	creds := Credentials{AccessToken: "a", RefreshToken: "r"}
	st := &enrollStore{}
	if _, err := Enroll(context.Background(), st, lister{vins: []string{"VIN1", "VIN2", "VIN3"}}, "acc", APIKey{}, creds, 2); !errors.Is(err, ErrTooManyVehicles) ||
		st.creds.AccessToken != "" || len(st.vehicles) != 0 {
		t.Errorf("over the cap: %v; store %+v", err, st)
	}
	st = &enrollStore{}
	if vins, err := Enroll(context.Background(), st, lister{vins: []string{"VIN1", "VIN2"}}, "acc", APIKey{}, creds, 2); err != nil || len(vins) != 2 || len(st.vehicles) != 2 {
		t.Errorf("at the cap: %v, %v; store %+v", vins, err, st)
	}
}

// TestEnrollWithKey: the vehicles are listed with the account's key, whose refusal
// keeps the connection, marks the key and lists nothing; the instance's key refused is
// an error as any other, and stores nothing.
func TestEnrollWithKey(t *testing.T) {
	creds := Credentials{AccessToken: "a", RefreshToken: "r"}
	setAt := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	key := APIKey{Value: "own-key", SetAt: setAt}

	var used string
	st := &enrollStore{}
	if _, err := Enroll(context.Background(), st, lister{vins: []string{"VIN1"}, key: &used}, "acc", key, creds, 0); err != nil {
		t.Fatal(err)
	}
	if used != "own-key" || st.refused[setAt] || len(st.vehicles) != 1 {
		t.Errorf("listed with %q, refused %v, vehicles %v", used, st.refused, st.vehicles)
	}
	if r, ok := st.refused[setAt]; !ok || r {
		t.Error("an accepted key is not marked accepted")
	}

	st = &enrollStore{}
	_, err := Enroll(context.Background(), st, lister{err: refusedKey{}}, "acc", key, creds, 0)
	if !errors.Is(err, ErrKeyRefused) || !errors.Is(err, refusedKey{}) || st.creds != creds || !st.refused[setAt] || len(st.vehicles) != 0 {
		t.Errorf("own key refused: %v (the provider's answer kept); store %+v", err, st)
	}

	st = &enrollStore{}
	_, err = Enroll(context.Background(), st, lister{err: refusedKey{}}, "acc", APIKey{}, creds, 0)
	if err == nil || errors.Is(err, ErrKeyRefused) || st.creds.AccessToken != "" || st.refused != nil {
		t.Errorf("instance key refused: %v; store %+v", err, st)
	}

	for _, failAt := range []string{"connection", "key"} {
		st = &enrollStore{failAt: failAt}
		if _, err := Enroll(context.Background(), st, lister{err: refusedKey{}}, "acc", key, creds, 0); err == nil || errors.Is(err, ErrKeyRefused) {
			t.Errorf("%s fails: %v", failAt, err)
		}
		st = &enrollStore{failAt: failAt}
		if _, err := Enroll(context.Background(), st, lister{vins: []string{"VIN1"}}, "acc", key, creds, 0); err == nil {
			t.Errorf("%s fails after listing: no error", failAt)
		}
	}
}
