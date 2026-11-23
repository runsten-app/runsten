package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/core"
	"runsten/internal/notify"
)

// memNotifications keeps the devices, the preferences and the deliveries in memory, by
// account, as the store does.
type memNotifications struct {
	mu        sync.Mutex
	devices   map[string][]notify.Device
	prefs     map[string]notify.Preferences
	delivered map[string]bool
	// live holds the activities by account, then device and vehicle.
	live map[string]map[[2]string]notify.LiveActivity
	fail error
}

func newMemNotifications() *memNotifications {
	return &memNotifications{
		devices: map[string][]notify.Device{}, prefs: map[string]notify.Preferences{}, delivered: map[string]bool{},
		live: map[string]map[[2]string]notify.LiveActivity{},
	}
}

func (m *memNotifications) SetLiveActivityToken(_ context.Context, accountID, deviceID, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for i, d := range m.devices[accountID] {
		if d.ID == deviceID {
			m.devices[accountID][i].LiveActivityToken = token
			return nil
		}
	}
	return ErrNoDevice
}

func (m *memNotifications) PutLiveActivity(_ context.Context, accountID string, a notify.LiveActivity, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if !slices.ContainsFunc(m.devices[accountID], func(d notify.Device) bool { return d.ID == a.DeviceID }) {
		return ErrNoDevice
	}
	if m.live[accountID] == nil {
		m.live[accountID] = map[[2]string]notify.LiveActivity{}
	}
	key := [2]string{a.DeviceID, a.VehicleID}
	if o, ok := m.live[accountID][key]; ok && o.ChargeID > a.ChargeID {
		return nil
	} else if ok && o.ChargeID == a.ChargeID {
		a.State, a.PushedAt = o.State, o.PushedAt
	}
	m.live[accountID][key] = a
	return nil
}

func (m *memNotifications) PutDevice(_ context.Context, accountID string, d notify.Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for i, o := range m.devices[accountID] {
		if o.ID == d.ID {
			d.CreatedAt, d.LiveActivityToken = o.CreatedAt, o.LiveActivityToken
			m.devices[accountID][i] = d
			return nil
		}
	}
	if len(m.devices[accountID]) >= MaxDevices {
		return ErrTooManyDevices
	}
	m.devices[accountID] = append(m.devices[accountID], d)
	return nil
}

func (m *memNotifications) DeleteDevice(_ context.Context, accountID, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	var kept []notify.Device
	for _, d := range m.devices[accountID] {
		if d.ID != deviceID {
			kept = append(kept, d)
		}
	}
	m.devices[accountID] = kept
	for key := range m.live[accountID] {
		if key[0] == deviceID {
			delete(m.live[accountID], key)
		}
	}
	return nil
}

func (m *memNotifications) Devices(_ context.Context, accountID string) ([]notify.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]notify.Device(nil), m.devices[accountID]...), m.fail
}

func (m *memNotifications) NotificationPreferences(_ context.Context, accountID string) (notify.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.prefs[accountID]
	out := notify.Preferences{
		Account: maps.Clone(p.Account), VehiclesDefault: maps.Clone(p.VehiclesDefault), Vehicles: map[string]map[notify.Kind]bool{},
		VehiclesDefaultThreshold: maps.Clone(p.VehiclesDefaultThreshold), VehicleThresholds: map[string]map[notify.Kind]int{},
	}
	for v, kinds := range p.Vehicles {
		out.Vehicles[v] = maps.Clone(kinds)
	}
	for v, kinds := range p.VehicleThresholds {
		out.VehicleThresholds[v] = maps.Clone(kinds)
	}
	return out, m.fail
}

func (m *memNotifications) SetNotificationPreferences(_ context.Context, accountID string, c NotificationChoices) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	p := m.prefs[accountID]
	if p.Account == nil {
		p = notify.Preferences{
			Account: map[notify.Kind]bool{}, VehiclesDefault: map[notify.Kind]bool{}, Vehicles: map[string]map[notify.Kind]bool{},
			VehiclesDefaultThreshold: map[notify.Kind]int{}, VehicleThresholds: map[string]map[notify.Kind]int{},
		}
	}
	for k, v := range c.VehiclesDefaultThreshold {
		if v == nil {
			delete(p.VehiclesDefaultThreshold, k)
		} else {
			p.VehiclesDefaultThreshold[k] = *v
		}
	}
	for vehicle, kinds := range c.VehicleThresholds {
		if p.VehicleThresholds[vehicle] == nil {
			p.VehicleThresholds[vehicle] = map[notify.Kind]int{}
		}
		for k, v := range kinds {
			if v == nil {
				delete(p.VehicleThresholds[vehicle], k)
			} else {
				p.VehicleThresholds[vehicle][k] = *v
			}
		}
	}
	maps.Copy(p.Account, c.Account)
	maps.Copy(p.VehiclesDefault, c.VehiclesDefault)
	for v, kinds := range c.Vehicles {
		if p.Vehicles[v] == nil {
			p.Vehicles[v] = map[notify.Kind]bool{}
		}
		for k, on := range kinds {
			if on == nil {
				delete(p.Vehicles[v], k)
			} else {
				p.Vehicles[v][k] = *on
			}
		}
	}
	m.prefs[accountID] = p
	return nil
}

func (m *memNotifications) RecordDelivery(_ context.Context, e notify.Event, _ time.Time, _ int) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := e.AccountID + "|" + string(e.Kind) + "|" + e.VehicleID + "|" + e.Key
	if m.delivered[key] {
		return "", false, nil
	}
	m.delivered[key] = true
	return fmt.Sprint(len(m.delivered)), true, nil
}

// pushes records the deliveries a PushSender is given.
type pushes struct {
	mu   sync.Mutex
	sent []notify.Delivery
}

func (p *pushes) Send(_ context.Context, d notify.Delivery) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, d)
	return nil
}

func (p *pushes) kinds() []notify.Kind {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []notify.Kind
	for _, d := range p.sent {
		out = append(out, d.Event.Kind)
	}
	return out
}

// sending gives the server a Notifier with a PushSender, as an extension would.
func sending(e *env) *pushes {
	p := &pushes{}
	n := notify.New(notify.NewRegistry(), e.notifications, e.clk, quiet)
	n.SetSender(p)
	e.server.Notifier = n
	return p
}

const phoneID = "6f1c2a8e-0d5b-4c3a-9e7f-1a2b3c4d5e6f"

const phoneBody = `{"platform":"apns","token":"a1b2c3","locale":"fr-FR","app_version":"1.0.0"}`

// tokenWith issues an access token of the session's user with scopes, through the API.
func tokenWith(t *testing.T, e *env, c *http.Cookie, scopes ...string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"name": "phone", "expiry": "30d", "scopes": append([]string{}, scopes...)})
	resp, body := do(t, noFollow(), http.MethodPost, e.api.URL+"/api/v1/tokens", jsonType, string(b), c)
	if resp.status != http.StatusCreated {
		t.Fatalf("token: %d %s", resp.status, body)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	return "Bearer " + created.Token
}

// TestDevices: a device is registered and removed with the session, or with a token
// given the scope notifications, never with one without it.
func TestDevices(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/devices/" + phoneID
	if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, phoneBody, c); resp.status != http.StatusNoContent {
		t.Fatalf("with the session: %d %s", resp.status, body)
	}
	devices, _ := e.notifications.Devices(t.Context(), account)
	if len(devices) != 1 || devices[0].ID != phoneID || devices[0].Platform != notify.APNs || devices[0].Token != "a1b2c3" ||
		devices[0].Locale != "fr-FR" || devices[0].AppVersion != "1.0.0" || devices[0].UserID == "" || !devices[0].CreatedAt.Equal(t0) {
		t.Errorf("registered %+v", devices)
	}

	t.Run("a token without the scope", func(t *testing.T) {
		bearer := tokenWith(t, e, c, "trips:mark")
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			resp, body := withBearer(t, m, u, bearer, jsonType, phoneBody)
			if resp.status != http.StatusForbidden || resp.header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope", scope="notifications"` {
				t.Errorf("%s: %d %v", m, resp.status, resp.header)
			}
			wantError(t, body, "insufficient_scope")
		}
		resp, body := withBearer(t, http.MethodPut, e.api.URL+"/api/v1/notification-preferences", bearer, jsonType, `{"account":[],"vehicles_default":[],"vehicles":[]}`)
		if resp.status != http.StatusForbidden {
			t.Errorf("preferences: %d %s", resp.status, body)
		}
		// It reads them all the same, as every route that reads.
		if resp, body := withBearer(t, http.MethodGet, e.api.URL+"/api/v1/notification-preferences", bearer, "", ""); resp.status != http.StatusOK {
			t.Errorf("read: %d %s", resp.status, body)
		}
	})

	t.Run("a token with it", func(t *testing.T) {
		bearer := tokenWith(t, e, c, "notifications")
		if resp, body := withBearer(t, http.MethodPut, u, bearer, jsonType, `{"platform":"fcm","token":"new","locale":"nb","app_version":"1.1.0+7"}`); resp.status != http.StatusNoContent {
			t.Fatalf("put: %d %s", resp.status, body)
		}
		devices, _ := e.notifications.Devices(t.Context(), account)
		if len(devices) != 1 || devices[0].Platform != notify.FCM || devices[0].Token != "new" || devices[0].AppVersion != "1.1.0+7" {
			t.Errorf("updated %+v", devices)
		}
		// Not a write that scope allows.
		if resp, _ := withBearer(t, http.MethodPost, e.api.URL+"/api/v1/places", bearer, jsonType, workBody); resp.status != http.StatusForbidden {
			t.Errorf("another write: %d", resp.status)
		}
		if resp, body := withBearer(t, http.MethodDelete, u, bearer, "", ""); resp.status != http.StatusNoContent {
			t.Fatalf("delete: %d %s", resp.status, body)
		}
		if devices, _ := e.notifications.Devices(t.Context(), account); len(devices) != 0 {
			t.Errorf("left %+v", devices)
		}
		// Removing it again does nothing.
		if resp, body := withBearer(t, http.MethodDelete, u, bearer, "", ""); resp.status != http.StatusNoContent {
			t.Errorf("delete again: %d %s", resp.status, body)
		}
	})

	t.Run("refused", func(t *testing.T) {
		for _, bad := range []string{
			`{"platform":"webpush","token":"x","locale":"fr","app_version":"1"}`,
			`{"platform":"apns","token":"","locale":"fr","app_version":"1"}`,
			`{"platform":"apns","token":"a b","locale":"fr","app_version":"1"}`,
			`{"platform":"apns","token":"x","locale":"français","app_version":"1"}`,
			`{"platform":"apns","token":"x","locale":"fr","app_version":""}`,
			`{"platform":"apns","token":"x","locale":"fr"}`,
		} {
			resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, bad, c)
			if resp.status != http.StatusBadRequest {
				t.Errorf("%s: %d", bad, resp.status)
			}
			wantError(t, body, "invalid_body")
		}
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			resp, body := do(t, noFollow(), m, e.api.URL+"/api/v1/devices/not-a-uuid", jsonType, phoneBody, c)
			if resp.status != http.StatusBadRequest {
				t.Errorf("%s an ID not a UUID: %d", m, resp.status)
			}
			wantError(t, body, "invalid_parameter")
		}
		for _, target := range []string{u, e.api.URL + "/api/v1/notification-preferences"} {
			resp, body := do(t, noFollow(), http.MethodPut, target, "text/plain", phoneBody, c)
			if resp.status != http.StatusUnsupportedMediaType {
				t.Errorf("%s not JSON: %d", target, resp.status)
			}
			wantError(t, body, "unsupported_media_type")
		}
		resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, phoneBody)
		if resp.status != http.StatusUnauthorized {
			t.Errorf("without a session: %d", resp.status)
		}
		wantError(t, body, "unauthorized")
	})

	t.Run("too many", func(t *testing.T) {
		for i := range MaxDevices {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
			if resp, body := do(t, noFollow(), http.MethodPut, e.api.URL+"/api/v1/devices/"+id, jsonType, phoneBody, c); resp.status != http.StatusNoContent {
				t.Fatalf("#%d: %d %s", i, resp.status, body)
			}
		}
		resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, phoneBody, c)
		if resp.status != http.StatusConflict {
			t.Errorf("one too many: %d %s", resp.status, body)
		}
		wantError(t, body, "too_many_devices")
	})

	t.Run("a store down", func(t *testing.T) {
		e.notifications.fail = errors.New("down")
		defer func() { e.notifications.fail = nil }()
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			resp, body := do(t, noFollow(), m, u, jsonType, phoneBody, c)
			if resp.status != http.StatusInternalServerError {
				t.Errorf("%s: %d", m, resp.status)
			}
			wantError(t, body, "internal")
		}
		for _, m := range []string{http.MethodGet, http.MethodPut} {
			resp, body := do(t, noFollow(), m, e.api.URL+"/api/v1/notification-preferences", jsonType, `{"account":[],"vehicles_default":[],"vehicles":[]}`, c)
			if resp.status != http.StatusInternalServerError {
				t.Errorf("preferences %s: %d", m, resp.status)
			}
			wantError(t, body, "internal")
		}
	})
}

// TestLiveActivities: a device registers its token to start an activity with, then an
// activity's own token, which a later charge replaces and an earlier one does not.
func TestLiveActivities(t *testing.T) {
	e, c := readerEnv(t)
	dev := e.api.URL + "/api/v1/devices/" + phoneID
	u := dev + "/live-activities/" + car
	const charge, later = "2026-09-28T18:30:00.000000Z", "2026-09-29T07:00:00.000000Z"
	activity := func() (notify.LiveActivity, bool) {
		e.notifications.mu.Lock()
		defer e.notifications.mu.Unlock()
		a, ok := e.notifications.live[account][[2]string{phoneID, car}]
		return a, ok
	}

	resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, `{"charge":"`+charge+`","token":"t1"}`, c)
	if resp.status != http.StatusNotFound {
		t.Errorf("before the device: %d %s", resp.status, body)
	}
	wantError(t, body, "not_found")

	startToken := dev + "/live-activity-token"
	if resp, body := do(t, noFollow(), http.MethodPut, startToken, jsonType, `{"token":"s1"}`, c); resp.status != http.StatusNotFound {
		t.Errorf("start token before the device: %d %s", resp.status, body)
	}
	if resp, body := do(t, noFollow(), http.MethodPut, dev, jsonType, phoneBody, c); resp.status != http.StatusNoContent {
		t.Fatalf("device: %d %s", resp.status, body)
	}
	startWith := func(body, want string) {
		t.Helper()
		if resp, body := do(t, noFollow(), http.MethodPut, startToken, jsonType, body, c); resp.status != http.StatusNoContent {
			t.Fatalf("start token: %d %s", resp.status, body)
		}
		if devices, _ := e.notifications.Devices(t.Context(), account); len(devices) != 1 || devices[0].LiveActivityToken != want {
			t.Errorf("start token: %+v, want %q", devices, want)
		}
	}
	startWith(`{"token":"s1"}`, "s1")
	// Registering the device again keeps it.
	if resp, body := do(t, noFollow(), http.MethodPut, dev, jsonType, phoneBody, c); resp.status != http.StatusNoContent {
		t.Fatalf("device again: %d %s", resp.status, body)
	}
	if devices, _ := e.notifications.Devices(t.Context(), account); devices[0].LiveActivityToken != "s1" {
		t.Errorf("start token lost: %+v", devices)
	}
	startWith(`{"token":null}`, "")
	startWith(`{"token":"s2"}`, "s2")
	for _, step := range []struct {
		body, charge, token string
	}{
		{`{"charge":"` + charge + `","token":"t1"}`, charge, "t1"},
		{`{"charge":"` + later + `","token":"t2"}`, later, "t2"},
		{`{"charge":"` + charge + `","token":"t3"}`, later, "t2"}, // an earlier charge changes nothing
		{`{"charge":"` + later + `","token":null}`, later, ""},    // dismissed
	} {
		if resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, step.body, c); resp.status != http.StatusNoContent {
			t.Fatalf("%s: %d %s", step.body, resp.status, body)
		}
		if a, ok := activity(); !ok || a.ChargeID != step.charge || a.Token != step.token || a.VehicleID != car {
			t.Errorf("%s: %+v", step.body, a)
		}
	}

	t.Run("refused", func(t *testing.T) {
		for _, bad := range []string{
			`{"charge":"yesterday","token":"t"}`,
			`{"charge":"` + charge + `","token":""}`,
			`{"charge":"` + charge + `","token":"a b"}`,
			`{"charge":"` + charge + `"}`,
		} {
			resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, bad, c)
			if resp.status != http.StatusBadRequest {
				t.Errorf("%s: %d", bad, resp.status)
			}
			wantError(t, body, "invalid_body")
		}
		for _, bad := range []string{`{"token":""}`, `{"token":"a b"}`, `{}`} {
			resp, body := do(t, noFollow(), http.MethodPut, startToken, jsonType, bad, c)
			if resp.status != http.StatusBadRequest {
				t.Errorf("start token %s: %d", bad, resp.status)
			}
			wantError(t, body, "invalid_body")
		}
		resp, body := do(t, noFollow(), http.MethodPut, dev+"/live-activities/"+foreign, jsonType, `{"charge":"`+charge+`","token":"t"}`, c)
		if resp.status != http.StatusNotFound {
			t.Errorf("another account's vehicle: %d %s", resp.status, body)
		}
		bearer := tokenWith(t, e, c, "trips:mark")
		if resp, _ := withBearer(t, http.MethodPut, u, bearer, jsonType, `{"charge":"`+charge+`","token":"t"}`); resp.status != http.StatusForbidden {
			t.Errorf("without the scope: %d", resp.status)
		}
	})

	t.Run("not JSON, or a store down", func(t *testing.T) {
		for _, target := range []string{u, startToken} {
			resp, body := do(t, noFollow(), http.MethodPut, target, "text/plain", `{"charge":"`+charge+`","token":"t"}`, c)
			if resp.status != http.StatusUnsupportedMediaType {
				t.Errorf("%s not JSON: %d", target, resp.status)
			}
			wantError(t, body, "unsupported_media_type")
		}
		e.notifications.fail = errors.New("down")
		defer func() { e.notifications.fail = nil }()
		bodies := map[string]string{u: `{"charge":"` + charge + `","token":"t"}`, startToken: `{"token":"t"}`} //nolint:gosec // a test's tokens
		for target, b := range bodies {
			resp, body := do(t, noFollow(), http.MethodPut, target, jsonType, b, c)
			if resp.status != http.StatusInternalServerError {
				t.Errorf("%s: %d", target, resp.status)
			}
			wantError(t, body, "internal")
		}
	})

	if resp, body := do(t, noFollow(), http.MethodDelete, dev, "", "", c); resp.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", resp.status, body)
	}
	if a, ok := activity(); ok {
		t.Errorf("kept with its device: %+v", a)
	}
}

// TestNotificationPreferences: the kinds and their defaults, then the choices a PUT
// sets, the others left as they are.
func TestNotificationPreferences(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/notification-preferences"
	resp, body := get(t, noFollow(), u, c)
	if resp.status != http.StatusOK {
		t.Fatalf("%d %s", resp.status, body)
	}
	golden(t, "notification-preferences.json", body)

	put := func(t *testing.T, choices string) (reply, string) {
		t.Helper()
		return do(t, noFollow(), http.MethodPut, u, jsonType, choices, c)
	}
	resp, body = put(t, `{"account":[{"kind":"access_granted","enabled":false}],`+
		`"vehicles_default":[{"kind":"trip_ended","enabled":true}],`+
		`"vehicles":[{"vehicle":"`+car+`","kinds":[{"kind":"charge_finished","choice":"off"},{"kind":"trip_ended","choice":"off"}]}]}`)
	if resp.status != http.StatusOK {
		t.Fatalf("put: %d %s", resp.status, body)
	}
	golden(t, "notification-preferences-set.json", body)
	// The vehicle's trips follow the default again; nothing else changes.
	resp, body = put(t, `{"account":[],"vehicles_default":[],"vehicles":[{"vehicle":"`+car+`","kinds":[{"kind":"trip_ended","choice":"default"}]}]}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `{"kind":"access_granted","enabled":false,"default":true,"always":false}`) ||
		!strings.Contains(body, `"vehicle":"`+car+`","kinds":[{"kind":"charge_interrupted","enabled":true,"choice":"default"},{"kind":"charge_finished","enabled":false,"choice":"off"},{"kind":"trip_to_classify","enabled":false,"choice":"default"},{"kind":"charge_started","enabled":false,"choice":"default"},{"kind":"trip_ended","enabled":true,"choice":"default"},{"kind":"charge_live_activity","enabled":true,"choice":"default"},{"kind":"not_charged","enabled":true,"choice":"default"},{"kind":"battery_low","enabled":true,"choice":"default","threshold":null}]`) {
		t.Errorf("back to the default: %d %s", resp.status, body)
	}

	t.Run("an extension's kinds", func(t *testing.T) {
		sending(e)
		reg := e.server.Notifier.Registry()
		for _, s := range []notify.Spec{
			{Kind: "service_message", Level: notify.AccountLevel, Always: true},
			{Kind: "news", Level: notify.AccountLevel},
		} {
			if err := reg.Register(s); err != nil {
				t.Fatal(err)
			}
		}
		defer func() { e.server.Notifier = nil }()
		_, body := get(t, noFollow(), u, c)
		if !strings.Contains(body, `{"kind":"service_message","enabled":true,"default":true,"always":true},{"kind":"news","enabled":false,"default":false,"always":false}]`) {
			t.Errorf("listed: %s", body)
		}
		if resp, body := put(t, `{"account":[{"kind":"news","enabled":true}],"vehicles_default":[],"vehicles":[]}`); resp.status != http.StatusOK ||
			!strings.Contains(body, `{"kind":"news","enabled":true,"default":false,"always":false}`) {
			t.Errorf("opted in: %d %s", resp.status, body)
		}
		resp, body := put(t, `{"account":[{"kind":"service_message","enabled":false}],"vehicles_default":[],"vehicles":[]}`)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, "body.account[0].kind: this kind cannot be turned off") {
			t.Errorf("always turned off: %d %s", resp.status, body)
		}
	})

	t.Run("refused", func(t *testing.T) {
		for choices, message := range map[string]string{
			`{"account":[{"kind":"news","enabled":true}],"vehicles_default":[],"vehicles":[]}`:                                                       "body.account[0].kind: unknown notification kind",
			`{"account":[{"kind":"trip_ended","enabled":true}],"vehicles_default":[],"vehicles":[]}`:                                                 "body.account[0].kind: a kind of the vehicle, not of the account",
			`{"account":[],"vehicles_default":[{"kind":"access_granted","enabled":true}],"vehicles":[]}`:                                             "body.vehicles_default[0].kind: a kind of the account",
			`{"account":[],"vehicles_default":[],"vehicles":[{"vehicle":"` + car + `","kinds":[{"kind":"credential_refused","choice":"default"}]}]}`: "body.vehicles[0].kinds[0].kind",
			`{"account":[],"vehicles_default":[],"vehicles":[{"vehicle":"` + car + `","kinds":[{"kind":"trip_ended","choice":"maybe"}]}]}`:           "body.vehicles[0].kinds[0].choice",
			`{"account":[{"kind":"Access-Granted","enabled":true}],"vehicles_default":[],"vehicles":[]}`:                                             "body.account[0].kind",
			`{"account":[]}`: "",
		} {
			resp, body := put(t, choices)
			if resp.status != http.StatusBadRequest || !strings.Contains(body, message) {
				t.Errorf("%s: %d %s", choices, resp.status, body)
			}
			wantError(t, body, "invalid_body")
		}
		for _, id := range []string{foreign, "not-a-uuid"} {
			resp, body := put(t, `{"account":[],"vehicles_default":[],"vehicles":[{"vehicle":"`+id+`","kinds":[]}]}`)
			if resp.status != http.StatusNotFound {
				t.Errorf("vehicle %s: %d %s", id, resp.status, body)
			}
			wantError(t, body, "not_found")
		}
	})
}

// TestBatteryLowThreshold: the threshold of battery_low, for every vehicle and for each,
// set apart from the on or off choice, and its limits.
func TestBatteryLowThreshold(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/notification-preferences"
	put := func(choices string) (reply, string) {
		return do(t, noFollow(), http.MethodPut, u, jsonType, choices, c)
	}
	vehicle := `"vehicle":"` + car + `"`
	// The default for every vehicle, alone: the kind's choice is left as it is.
	resp, body := put(`{"account":[],"vehicles_default":[{"kind":"battery_low","threshold":30}],"vehicles":[]}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `{"kind":"battery_low","enabled":true,"default":true,"always":false,"threshold":30}`) ||
		!strings.Contains(body, `{"kind":"battery_low","enabled":true,"choice":"default","threshold":null}`) {
		t.Fatalf("default threshold: %d %s", resp.status, body)
	}
	// A vehicle's own, with its choice turned off, then the choice alone, then the threshold
	// alone: each leaves the other.
	resp, body = put(`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low","choice":"off","threshold":10}]}]}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `{"kind":"battery_low","enabled":false,"choice":"off","threshold":10}`) {
		t.Fatalf("vehicle's threshold: %d %s", resp.status, body)
	}
	resp, body = put(`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low","choice":"on"}]}]}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `{"kind":"battery_low","enabled":true,"choice":"on","threshold":10}`) {
		t.Fatalf("choice alone: %d %s", resp.status, body)
	}
	resp, body = put(`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low","threshold":45}]}]}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `{"kind":"battery_low","enabled":true,"choice":"on","threshold":45}`) {
		t.Fatalf("threshold alone: %d %s", resp.status, body)
	}
	// Null: it follows the default again, whose own stays.
	resp, body = put(`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low","threshold":null}]}]}`)
	if resp.status != http.StatusOK || !strings.Contains(body, `{"kind":"battery_low","enabled":true,"choice":"on","threshold":null}`) ||
		!strings.Contains(body, `"always":false,"threshold":30}`) {
		t.Fatalf("reset: %d %s", resp.status, body)
	}
	// The default's null is the kind's.
	if resp, body = put(`{"account":[],"vehicles_default":[{"kind":"battery_low","threshold":null}],"vehicles":[]}`); resp.status != http.StatusOK ||
		!strings.Contains(body, `"always":false,"threshold":20}`) {
		t.Fatalf("default reset: %d %s", resp.status, body)
	}
	for choices, message := range map[string]string{
		`{"account":[],"vehicles_default":[{"kind":"battery_low","threshold":4}],"vehicles":[]}`:                                          "body.vehicles_default[0].threshold: from 5 to 50",
		`{"account":[],"vehicles_default":[{"kind":"battery_low","enabled":true,"threshold":51}],"vehicles":[]}`:                          "body.vehicles_default[0].threshold: from 5 to 50",
		`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low","threshold":0}]}]}`:              "body.vehicles[0].kinds[0].threshold",
		`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low","threshold":90}]}]}`:             "body.vehicles[0].kinds[0].threshold: from 5 to 50",
		`{"account":[],"vehicles_default":[{"kind":"trip_ended","threshold":20}],"vehicles":[]}`:                                          "body.vehicles_default[0].threshold: this kind has no threshold",
		`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"left_open","choice":"on","threshold":20}]}]}`: "body.vehicles[0].kinds[0].threshold: this kind has no threshold",
		`{"account":[{"kind":"access_granted","threshold":20}],"vehicles_default":[],"vehicles":[]}`:                                      "body.account[0].threshold: this kind has no threshold",
		`{"account":[],"vehicles_default":[{"kind":"battery_low"}],"vehicles":[]}`:                                                        "body.vehicles_default[0]: enabled or threshold is required",
		`{"account":[],"vehicles_default":[],"vehicles":[{` + vehicle + `,"kinds":[{"kind":"battery_low"}]}]}`:                            "body.vehicles[0].kinds[0]: choice or threshold is required",
		`{"account":[],"vehicles_default":[{"kind":"battery_low","threshold":"low"}],"vehicles":[]}`:                                      "",
		`{"account":[],"vehicles_default":[{"kind":"battery_low","threshold":20.5}],"vehicles":[]}`:                                       "",
	} {
		resp, body := put(choices)
		if resp.status != http.StatusBadRequest || !strings.Contains(body, message) {
			t.Errorf("%s: %d %s", choices, resp.status, body)
		}
		wantError(t, body, "invalid_body")
	}
	// Refused all or none: nothing of a refused request was set.
	if _, body := get(t, noFollow(), u, c); !strings.Contains(body, `"always":false,"threshold":20}`) {
		t.Errorf("after the refusals: %s", body)
	}
}

// TestLeftOpenApplies: left_open is listed for the vehicles whose state covers the doors or
// the windows only; an extension hides kinds from an account.
func TestLeftOpenApplies(t *testing.T) {
	e, c := readerEnv(t)
	u := e.api.URL + "/api/v1/notification-preferences"
	doors := core.Snapshot{Covers: core.FieldDoors, Doors: core.Some(core.Doors{Locked: core.Some(true, time.Time{})}, time.Time{})}
	e.reader.current[car] = append(e.reader.current[car], core.Record{FetchedAt: h(5, 0), CheckedAt: h(5, 30), Snapshot: doors})
	left := `{"kind":"left_open","enabled":false,"choice":"default"}`
	var listing struct {
		Default  []struct{ Kind string } `json:"vehicles_default"`
		Vehicles []struct {
			Vehicle string
			Kinds   []struct{ Kind string }
		}
	}
	read := func() {
		t.Helper()
		_, body := get(t, noFollow(), u, c)
		listing.Default, listing.Vehicles = nil, nil
		if err := json.Unmarshal([]byte(body), &listing); err != nil {
			t.Fatal(err)
		}
	}
	has := func(kinds []struct{ Kind string }, k string) bool {
		return slices.ContainsFunc(kinds, func(o struct{ Kind string }) bool { return o.Kind == k })
	}
	read()
	if !has(listing.Default, "left_open") {
		t.Error("vehicles_default lacks left_open")
	}
	for _, v := range listing.Vehicles {
		if got, want := has(v.Kinds, "left_open"), v.Vehicle == car; got != want {
			t.Errorf("vehicle %s: left_open listed %v, want %v", v.Vehicle, got, want)
		}
	}
	if _, body := get(t, noFollow(), u, c); !strings.Contains(body, left) {
		t.Errorf("the car's left_open: %s", body)
	}

	t.Run("hidden by an extension", func(t *testing.T) {
		sending(e)
		defer func() { e.server.Notifier = nil }()
		e.server.Notifier.SetHiddenKinds(func(context.Context, string) []notify.Kind { return []notify.Kind{notify.LeftOpen, notify.TripEnded} })
		read()
		if has(listing.Default, "left_open") || has(listing.Default, "trip_ended") || !has(listing.Default, "battery_low") {
			t.Errorf("vehicles_default: %+v", listing.Default)
		}
		for _, v := range listing.Vehicles {
			if has(v.Kinds, "left_open") || has(v.Kinds, "trip_ended") {
				t.Errorf("vehicle %s: %+v", v.Vehicle, v.Kinds)
			}
		}
		for _, choices := range []string{
			`{"account":[],"vehicles_default":[{"kind":"left_open","enabled":true}],"vehicles":[]}`,
			`{"account":[],"vehicles_default":[],"vehicles":[{"vehicle":"` + car + `","kinds":[{"kind":"left_open","choice":"on"}]}]}`,
		} {
			resp, body := do(t, noFollow(), http.MethodPut, u, jsonType, choices, c)
			if resp.status != http.StatusBadRequest || !strings.Contains(body, "unknown notification kind") {
				t.Errorf("%s: %d %s", choices, resp.status, body)
			}
		}
		// A kind not hidden is still set.
		if resp, _ := do(t, noFollow(), http.MethodPut, u, jsonType, `{"account":[],"vehicles_default":[{"kind":"battery_low","enabled":false}],"vehicles":[]}`, c); resp.status != http.StatusOK {
			t.Errorf("another kind: %d", resp.status)
		}
	})
}

// TestNotified: /instance tells whether the instance sends notifications; a new access
// token is told to the account's devices once.
func TestNotified(t *testing.T) {
	e, c := readerEnv(t)
	inst := e.api.URL + "/api/v1/instance"
	if _, body := get(t, noFollow(), inst, c); !strings.Contains(body, `"notifications":false`) {
		t.Errorf("without a sender: %s", body)
	}
	tokenWith(t, e, c) // without a sender: nothing told
	p := sending(e)
	if _, body := get(t, noFollow(), inst, c); !strings.Contains(body, `"notifications":true`) {
		t.Errorf("with a sender: %s", body)
	}
	tokenWith(t, e, c) // without a device: nothing told
	if resp, body := do(t, noFollow(), http.MethodPut, e.api.URL+"/api/v1/devices/"+phoneID, jsonType, phoneBody, c); resp.status != http.StatusNoContent {
		t.Fatalf("device: %d %s", resp.status, body)
	}
	tokenWith(t, e, c)
	if got := p.kinds(); len(got) != 1 || got[0] != notify.AccessGranted {
		t.Fatalf("told %v", got)
	}
	d := p.sent[0]
	if d.Event.AccountID != account || d.Event.Args["access"] != "token" || len(d.Event.Args) != 1 ||
		!strings.HasPrefix(d.Event.Key, "token:") || len(d.Devices) != 1 {
		t.Errorf("delivery %+v", d)
	}
	// Turned off: the next one is not told.
	if resp, body := do(t, noFollow(), http.MethodPut, e.api.URL+"/api/v1/notification-preferences", jsonType,
		`{"account":[{"kind":"access_granted","enabled":false}],"vehicles_default":[],"vehicles":[]}`, c); resp.status != http.StatusOK {
		t.Fatalf("off: %d %s", resp.status, body)
	}
	tokenWith(t, e, c)
	if got := p.kinds(); len(got) != 1 {
		t.Errorf("told while off: %v", got)
	}
}
