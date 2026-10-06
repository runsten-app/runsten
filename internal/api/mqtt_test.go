package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// memBrokers is an in-memory Brokers.
type memBrokers struct {
	mu        sync.Mutex
	brokers   map[string]MQTTBroker // by account, without the password
	passwords map[string]string
	writes    int
	err       error // returned by every call when set
}

func newMemBrokers() *memBrokers {
	return &memBrokers{brokers: map[string]MQTTBroker{}, passwords: map[string]string{}}
}

func (m *memBrokers) MQTTBroker(_ context.Context, accountID string) (MQTTBroker, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.brokers[accountID]
	return b, ok, m.err
}

func (m *memBrokers) SetMQTTBroker(_ context.Context, accountID string, b MQTTBroker, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.writes++
	if b.SetPassword {
		m.passwords[accountID] = b.Password
	}
	b.PasswordSet = m.passwords[accountID] != ""
	b.Password, b.SetPassword = "", false
	b.UpdatedAt = at.UTC()
	b.Status = MQTTStatus{}
	m.brokers[accountID] = b
	return nil
}

func (m *memBrokers) DeleteMQTTBroker(_ context.Context, accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.writes++
	delete(m.brokers, accountID)
	delete(m.passwords, accountID)
	return nil
}

// brokerBody is a broker on the local network, with a password.
const brokerBody = `{"url":"mqtt://homeassistant.local:1883","username":"runsten","password":"s3cret-pass","client_id":"",` +
	`"topic_prefix":"runsten","discovery":true,"discovery_prefix":"homeassistant","publish_location":true}`

// brokerWith is brokerBody with its URL and password replaced; password is JSON.
func brokerWith(url, password string) string {
	return strings.NewReplacer(`"mqtt://homeassistant.local:1883"`, `"`+url+`"`, `"s3cret-pass"`, password).Replace(brokerBody)
}

// TestMQTTJSON: the broker is set, read without its password, kept for its host only,
// checked, and removed; another account has none.
func TestMQTTJSON(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/mqtt"

	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK {
		t.Fatalf("none: %d %s", resp.status, body)
	}
	golden(t, "mqtt-none.json", body)

	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, brokerBody, c)
	if resp.status != http.StatusOK || strings.Contains(body, "s3cret") || !strings.Contains(body, `"password_set":true`) ||
		!strings.Contains(body, `"status":{"connected_at":null,"failed_at":null,"failure":null}`) {
		t.Fatalf("set: %d %s", resp.status, body)
	}
	if e.brokers.passwords[account] != "s3cret-pass" {
		t.Errorf("password stored: %q", e.brokers.passwords[account])
	}

	// What the collector wrote.
	b := e.brokers.brokers[account]
	b.Status = MQTTStatus{ConnectedAt: t0, FailedAt: t0.Add(time.Hour), Failure: string(mqttNotAuthorized)}
	e.brokers.brokers[account] = b
	_, body = get(t, noFollow(), u, c)
	golden(t, "mqtt.json", body)
	if _, body := get(t, noFollow(), u, e.session(t, "other")); !strings.Contains(body, `"broker":null`) {
		t.Errorf("another account's broker: %s", body)
	}

	// The password is kept for the same host, whatever its port or scheme, and only then.
	for _, url := range []string{"mqtt://homeassistant.local:1884", "mqtts://HomeAssistant.local"} {
		if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, brokerWith(url, "null"), c); resp.status != http.StatusOK ||
			!strings.Contains(body, `"password_set":true`) {
			t.Errorf("%s keeping the password: %d %s", url, resp.status, body)
		}
	}
	resp, body = do(t, noFollow(), http.MethodPut, u, jsonType, brokerWith("mqtt://elsewhere.example:1883", "null"), c)
	if resp.status != http.StatusBadRequest || !strings.Contains(body, "body.password") {
		t.Errorf("another host keeping the password: %d %s", resp.status, body)
	}
	wantError(t, body, "invalid_body")
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, brokerWith("mqtt://elsewhere.example:1883", `""`), c); resp.status != http.StatusOK ||
		!strings.Contains(body, `"password_set":false`) {
		t.Errorf("without a password: %d %s", resp.status, body)
	}
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, brokerWith("mqtt://third.example", "null"), c); resp.status != http.StatusOK {
		t.Errorf("another host, no password to keep: %d %s", resp.status, body)
	}

	for _, bad := range []string{
		brokerWith("http://broker.example", "null"),
		brokerWith("mqtt://", "null"),
		brokerWith("mqtt://user:hunter2@broker.example", "null"),
		brokerWith("mqtt://broker.example/topic", "null"),
		brokerWith("mqtt://broker.example?x=1", "null"),
		brokerWith("mqtt://broker.example#x", "null"),
		brokerWith("mqtt://broker.example:0", "null"),
		brokerWith("mqtt://broker.example:65536", "null"),
		brokerWith("mqtt:broker.example", "null"),
		brokerWith("mqtt://%zz", "null"),
		brokerWith("", "null"),
		strings.Replace(brokerBody, `"topic_prefix":"runsten"`, `"topic_prefix":"runsten/+"`, 1),
		strings.Replace(brokerBody, `"topic_prefix":"runsten"`, `"topic_prefix":"/runsten"`, 1),
		strings.Replace(brokerBody, `"topic_prefix":"runsten"`, `"topic_prefix":"a//b"`, 1),
		strings.Replace(brokerBody, `"discovery_prefix":"homeassistant"`, `"discovery_prefix":"#"`, 1),
		strings.Replace(brokerBody, `"client_id":""`, `"client_id":"car one"`, 1),
		strings.Replace(brokerBody, `,"publish_location":true`, ``, 1),
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, bad, c)
		if resp.status != http.StatusBadRequest || strings.Contains(body, "hunter2") {
			t.Errorf("%s: %d %s", bad, resp.status, body)
		}
		wantError(t, body, "invalid_body")
	}
	if resp, body := do(t, noFollow(), http.MethodPut, u, "text/plain", brokerBody, c); resp.status != http.StatusUnsupportedMediaType {
		t.Errorf("not JSON: %d", resp.status)
	} else {
		wantError(t, body, "unsupported_media_type")
	}

	// Removed.
	if resp, _ := do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.status)
	}
	if _, body := get(t, noFollow(), u, c); !strings.Contains(body, `"broker":null`) {
		t.Errorf("after delete: %s", body)
	}

	// Failures: never a detail.
	e.brokers.err = errors.New("boom")
	for _, r := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPut, brokerBody},
		{http.MethodPut, brokerWith("mqtt://broker.example", "null")},
		{http.MethodDelete, ""},
	} {
		resp, body := do(t, noFollow(), r.method, u, jsonType, r.body, c)
		if resp.status != http.StatusInternalServerError {
			t.Errorf("%s failing: %d", r.method, resp.status)
		}
		wantError(t, body, "internal")
	}
}

// TestMQTTPublicOnly: an instance that publishes to the Internet only refuses a broker
// over plain MQTT, or on an address or a name of a local network.
func TestMQTTPublicOnly(t *testing.T) {
	e, c := readerEnv(t)
	e.server.PublicBrokersOnly = true
	u := e.api.URL + "/api/v1/mqtt"
	if _, body := get(t, noFollow(), u, c); !strings.Contains(body, `"public_only":true`) {
		t.Errorf("the rule is not told: %s", body)
	}
	for url, ok := range map[string]bool{
		"mqtts://broker.example.com:8883": true,
		"mqtts://1.1.1.1":                 true,
		"mqtts://[2606:4700::1111]:8883":  true,
		"mqtt://broker.example.com":       false,
		"mqtts://192.168.1.2":             false,
		"mqtts://10.0.0.1:8883":           false,
		"mqtts://169.254.169.254":         false,
		"mqtts://[::1]":                   false,
		"mqtts://[fe80::1%25eth0]":        false,
		"mqtts://localhost":               false,
		"mqtts://broker.localhost":        false,
		"mqtts://homeassistant":           false,
	} {
		resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, brokerWith(url, `""`), c)
		switch {
		case ok && resp.status != http.StatusOK:
			t.Errorf("%s: %d %s", url, resp.status, body)
		case !ok:
			if resp.status != http.StatusBadRequest {
				t.Errorf("%s: %d %s", url, resp.status, body)
			}
			wantError(t, body, "broker_refused")
		}
	}
}

// TestMQTTLimit: an account without MQTT may not set a broker; it reads and removes the
// one it set before.
func TestMQTTLimit(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/mqtt"
	if resp, _ := do(t, noFollow(), http.MethodPut, u, jsonType, brokerBody, c); resp.status != http.StatusOK {
		t.Fatalf("set: %d", resp.status)
	}
	e.server.Limits = &mqttLimit{}
	if _, body := get(t, noFollow(), e.api.URL+"/api/v1/session", c); !strings.Contains(body, `"unavailable":["mqtt"]`) {
		t.Errorf("session: %s", body)
	}
	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, brokerBody, c)
	if resp.status != http.StatusForbidden {
		t.Errorf("set without MQTT: %d %s", resp.status, body)
	}
	wantError(t, body, "feature_unavailable")
	if resp, body := get(t, noFollow(), u, c); resp.status != http.StatusOK || !strings.Contains(body, `"url":"mqtt://homeassistant.local:1883"`) {
		t.Errorf("read without MQTT: %d %s", resp.status, body)
	}
	if resp, _ := do(t, noFollow(), http.MethodDelete, u, "", "", c); resp.status != http.StatusNoContent {
		t.Errorf("delete without MQTT: %d", resp.status)
	}
}

type mqttLimit struct{}

func (mqttLimit) Limits(context.Context, string, time.Time) (AccountLimits, error) {
	return AccountLimits{NoMQTT: true}, nil
}
