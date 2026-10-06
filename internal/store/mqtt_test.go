package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/publish"
)

func testBroker(url string) api.MQTTBroker {
	return api.MQTTBroker{
		URL: url, Username: "runsten", Password: "s3cret-pass", SetPassword: true, TopicPrefix: "runsten",
		Discovery: true, DiscoveryPrefix: "homeassistant", PublishLocation: true,
	}
}

// TestMQTTBrokerLifecycle: a broker is stored, read back without its password, which is
// sealed in the database, kept or replaced as asked; a new configuration forgets what
// the collector wrote of the previous one, and the broker's deletion takes it.
func TestMQTTBrokerLifecycle(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 4, 8, 0, 0, 123456789, time.UTC)
	a, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.MQTTBroker(ctx, a); err != nil || ok {
		t.Fatalf("no broker: %v, %v", ok, err)
	}

	if err := s.SetMQTTBroker(ctx, a, testBroker("mqtt://homeassistant.local:1883"), t0); err != nil {
		t.Fatal(err)
	}
	b, ok, err := s.MQTTBroker(ctx, a)
	if err != nil || !ok || b.URL != "mqtt://homeassistant.local:1883" || b.Username != "runsten" || !b.PasswordSet ||
		b.Password != "" || b.TopicPrefix != "runsten" || !b.Discovery || b.DiscoveryPrefix != "homeassistant" ||
		!b.PublishLocation || b.ClientID != "" || !b.UpdatedAt.Equal(t0.Truncate(time.Microsecond)) || b.Status != (api.MQTTStatus{}) {
		t.Fatalf("broker = %+v, %v, %v", b, ok, err)
	}

	db, err := pgx.Connect(ctx, dbURL) // the owner, outside RLS
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	sealed := func() []byte {
		t.Helper()
		var p []byte
		if err := db.QueryRow(ctx, "SELECT password FROM mqtt_brokers WHERE account_id = $1", a).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := sealed()
	if pw, err := s.mqttPassword(a, first); err != nil || pw != "s3cret-pass" || string(first) == "s3cret-pass" {
		t.Errorf("sealed password opens to %q, %v", pw, err)
	}

	// What the collector writes, then a new configuration that keeps the password.
	if _, err := db.Exec(ctx, "INSERT INTO mqtt_status (account_id, connected_at) VALUES ($1, $2)", a, t0); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.MQTTBroker(ctx, a); !b.Status.ConnectedAt.Equal(t0.Truncate(time.Microsecond)) {
		t.Errorf("status = %+v", b.Status)
	}
	kept := testBroker("mqtts://homeassistant.local")
	kept.Password, kept.SetPassword, kept.PublishLocation, kept.ClientID = "", false, false, "car-1"
	if err := s.SetMQTTBroker(ctx, a, kept, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	b, _, _ = s.MQTTBroker(ctx, a)
	if !b.PasswordSet || b.PublishLocation || b.ClientID != "car-1" || b.Status != (api.MQTTStatus{}) ||
		!b.UpdatedAt.Equal(t0.Add(time.Hour).Truncate(time.Microsecond)) {
		t.Errorf("after an update keeping the password: %+v", b)
	}
	if string(sealed()) != string(first) {
		t.Error("the password kept was sealed again")
	}

	// Removed by an empty one.
	none := testBroker("mqtt://elsewhere.example")
	none.Password = ""
	if err := s.SetMQTTBroker(ctx, a, none, t0); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.MQTTBroker(ctx, a); b.PasswordSet {
		t.Error("an empty password is stored")
	}

	if _, err := db.Exec(ctx, "INSERT INTO mqtt_status (account_id, failed_at, failure) VALUES ($1, $2, 'tls')", a, t0); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := s.MQTTBroker(ctx, a); b.Status.Failure != "tls" || !b.Status.FailedAt.Equal(t0.Truncate(time.Microsecond)) {
		t.Errorf("failure = %+v", b.Status)
	}
	if err := s.DeleteMQTTBroker(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.MQTTBroker(ctx, a); err != nil || ok {
		t.Errorf("after delete: %v, %v", ok, err)
	}
	var n int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM mqtt_status").Scan(&n); err != nil || n != 0 {
		t.Errorf("status left: %d, %v", n, err)
	}
}

// TestMQTTPasswordBound: the password is sealed with its account and its column: copied
// to another account's broker, or taken for an application key, it does not open.
func TestMQTTPasswordBound(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")
	if err := s.SetMQTTBroker(ctx, a, testBroker("mqtt://a.example"), t0); err != nil {
		t.Fatal(err)
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	var sealed []byte
	if err := db.QueryRow(ctx, "SELECT password FROM mqtt_brokers WHERE account_id = $1", a).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if pw, err := s.mqttPassword(b, sealed); err == nil {
		t.Errorf("A's password opened in B's account: %q", pw)
	}
	if _, err := db.Exec(ctx, "UPDATE connections SET api_key = $2, api_key_set_at = $3 WHERE account_id = $1", a, sealed, t0); err != nil {
		t.Fatal(err)
	}
	if k, err := s.AccountKey(ctx, a); err == nil {
		t.Errorf("the password opened as an application key: %q", k.Value)
	}
}

// TestMQTTBrokerIsolation: an account neither reads, nor replaces, nor deletes another's
// broker, nor its status.
func TestMQTTBrokerIsolation(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	a, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMQTTBroker(ctx, b, testBroker("mqtt://b.example"), t0); err != nil {
		t.Fatal(err)
	}
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	if _, err := db.Exec(ctx, "INSERT INTO mqtt_status (account_id, connected_at) VALUES ($1, $2)", b, t0); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := s.MQTTBroker(ctx, a); err != nil || ok {
		t.Errorf("A reads a broker: %v, %v", ok, err)
	}
	if err := s.DeleteMQTTBroker(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMQTTBroker(ctx, a, testBroker("mqtt://a.example"), t0); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.MQTTBroker(ctx, b)
	if err != nil || !ok || got.URL != "mqtt://b.example" || !got.Status.ConnectedAt.Equal(t0) {
		t.Errorf("B's broker after A's writes: %+v, %v, %v", got, ok, err)
	}
	// RLS itself, whatever the store's queries.
	if err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
		for _, q := range []string{
			"UPDATE mqtt_brokers SET url = 'mqtt://evil.example' WHERE account_id = '" + b + "'",
			"DELETE FROM mqtt_status WHERE account_id = '" + b + "'",
		} {
			tag, err := tx.Exec(ctx, q)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 0 {
				t.Errorf("%s: %d rows", q, tag.RowsAffected())
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.inAccount(ctx, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO mqtt_status (account_id) VALUES ($1)", b)
		return err //nolint:wrapcheck // test
	}); err == nil {
		t.Error("A wrote B's status")
	}
}

// TestPublishBrokers: the collector lists the accounts that have a broker, across the
// accounts, reads each one's configuration with its password within the account, and
// writes the status of the configuration it connected with only.
func TestPublishBrokers(t *testing.T) {
	s, dbURL := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 4, 8, 0, 0, 123456789, time.UTC)
	a, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx); err != nil { // without a broker
		t.Fatal(err)
	}
	if err := s.SetMQTTBroker(ctx, a, testBroker("mqtts://a.example"), t0); err != nil {
		t.Fatal(err)
	}
	none := testBroker("mqtt://b.example")
	none.Password, none.ClientID, none.PublishLocation = "", "car-1", false
	if err := s.SetMQTTBroker(ctx, b, none, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	refs, err := s.MQTTBrokers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Time{a: t0.Truncate(time.Microsecond), b: t0.Add(time.Hour).Truncate(time.Microsecond)}
	if len(refs) != len(want) {
		t.Fatalf("brokers = %+v", refs)
	}
	for _, r := range refs {
		if !r.UpdatedAt.Equal(want[r.AccountID]) || r.UpdatedAt.Location() != time.UTC {
			t.Errorf("%s updated at %v, want %v", r.AccountID, r.UpdatedAt, want[r.AccountID])
		}
	}

	got, ok, err := s.PublishBroker(ctx, a)
	if err != nil || !ok || got.AccountID != a || got.URL != "mqtts://a.example" || got.Username != "runsten" ||
		got.Password != "s3cret-pass" || got.TopicPrefix != "runsten" || !got.Discovery || got.DiscoveryPrefix != "homeassistant" ||
		!got.PublishLocation || !got.UpdatedAt.Equal(want[a]) {
		t.Errorf("A's broker = %+v, %v, %v", got, ok, err)
	}
	if got, ok, err := s.PublishBroker(ctx, b); err != nil || !ok || got.Password != "" || got.ClientID != "car-1" || got.PublishLocation {
		t.Errorf("B's broker = %+v, %v, %v", got, ok, err)
	}

	// The status of A's configuration; a zero time keeps the one stored.
	if err := s.SaveMQTTStatus(ctx, a, want[a], publish.Status{FailedAt: t0, Failure: publish.FailTLS}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMQTTStatus(ctx, a, want[a], publish.Status{ConnectedAt: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	status := func(account string) api.MQTTStatus {
		t.Helper()
		br, _, err := s.MQTTBroker(ctx, account)
		if err != nil {
			t.Fatal(err)
		}
		return br.Status
	}
	if st := status(a); !st.ConnectedAt.Equal(t0.Add(time.Minute).Truncate(time.Microsecond)) ||
		!st.FailedAt.Equal(t0.Truncate(time.Microsecond)) || st.Failure != "tls" {
		t.Errorf("A's status = %+v", st)
	}
	// Of a configuration replaced since: not written.
	if err := s.SaveMQTTStatus(ctx, b, t0, publish.Status{FailedAt: t0, Failure: publish.FailUnreachable}); err != nil {
		t.Fatal(err)
	}
	if st := status(b); st != (api.MQTTStatus{}) {
		t.Errorf("B's status written for a previous configuration: %+v", st)
	}

	// A sealed password that no longer opens (the key changed) is an error, never empty.
	db, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	if _, err := db.Exec(ctx, "UPDATE mqtt_brokers SET password = 'garbage' WHERE account_id = $1", a); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.PublishBroker(ctx, a); err == nil || ok || got.Password != "" {
		t.Errorf("unopenable password: %+v, %v, %v", got, ok, err)
	}
	if err := s.DeleteMQTTBroker(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.PublishBroker(ctx, a); err != nil || ok {
		t.Errorf("after delete: %v, %v", ok, err)
	}
}

// TestPublishBrokerIsolation: the configuration and the status the collector reads and
// writes are each account's own.
func TestPublishBrokerIsolation(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	a, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMQTTBroker(ctx, b, testBroker("mqtt://b.example"), t0); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.PublishBroker(ctx, a); err != nil || ok {
		t.Errorf("A reads a broker: %v, %v", ok, err)
	}
	// A's write, with B's configuration time, finds no broker of A's.
	if err := s.SaveMQTTStatus(ctx, a, t0, publish.Status{ConnectedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if br, _, _ := s.MQTTBroker(ctx, b); br.Status != (api.MQTTStatus{}) {
		t.Errorf("B's status after A's write: %+v", br.Status)
	}
}
