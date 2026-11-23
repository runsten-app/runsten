package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/auth"
	"runsten/internal/core"
	"runsten/internal/notify"
)

// An app registers the device it runs on (PUT /devices/{device}), and its user chooses
// what to be told of (/notification-preferences): the account's kinds, a default for
// every vehicle, and each vehicle's own choice. The instance sends them only with an
// extension's PushSender (notify.Notifier.Sends), which /instance tells.

// Notifications keeps the account's devices and notification preferences.
type Notifications interface {
	// PutDevice registers the device, or updates it if the account has it already;
	// ErrTooManyDevices when the account has MaxDevices others.
	PutDevice(ctx context.Context, accountID string, d notify.Device) error
	// DeleteDevice removes the device, if the account has it, and its live activities.
	DeleteDevice(ctx context.Context, accountID, deviceID string) error
	// SetLiveActivityToken sets the token the device's Live Activities are started with;
	// empty: none is. ErrNoDevice when the account does not have the device.
	SetLiveActivityToken(ctx context.Context, accountID, deviceID, token string) error
	// PutLiveActivity records the device's activity of a vehicle's charge, with its token
	// (empty: the person dismissed it), unless the device's activity of the vehicle is of a
	// later charge; ErrNoDevice when the account does not have the device.
	PutLiveActivity(ctx context.Context, accountID string, a notify.LiveActivity, now time.Time) error
	NotificationPreferences(ctx context.Context, accountID string) (notify.Preferences, error)
	// SetNotificationPreferences sets the choices c lists, leaving the others as they are.
	SetNotificationPreferences(ctx context.Context, accountID string, c NotificationChoices) error
}

// NotificationChoices are the preferences a user sets at once: the kinds they list,
// each on or off; a vehicle's kind nil follows the vehicles' default again. A kind with a
// threshold (notify.Spec.Threshold) has it set apart from its choice, which a threshold
// alone leaves as it is; a vehicle's threshold nil follows the vehicles' default again,
// and the default's nil, the kind's.
type NotificationChoices struct {
	Account                  map[notify.Kind]bool
	VehiclesDefault          map[notify.Kind]bool
	Vehicles                 map[string]map[notify.Kind]*bool
	VehiclesDefaultThreshold map[notify.Kind]*int
	VehicleThresholds        map[string]map[notify.Kind]*int
}

// MaxDevices is how many devices an account registers at most: a phone or two, a tablet,
// and those reinstalled before their app could remove them.
const MaxDevices = 20

// ErrTooManyDevices is returned when the account has MaxDevices devices already.
var ErrTooManyDevices = errors.New("too many devices")

// ErrAccessRevoked is returned when the access token a device registers with was revoked
// since it authenticated the request.
var ErrAccessRevoked = errors.New("access revoked")

// ErrNoDevice is returned when the account does not have the device.
var ErrNoDevice = errors.New("no such device")

type platformJSON string

// Schema lists the push services.
func (platformJSON) Schema(huma.Registry) *huma.Schema { return enumSchema(notify.Platforms...) }

type deviceBody struct {
	Platform   platformJSON `json:"platform" doc:"The push service that reaches the device: apns (Apple's, iOS) or fcm (Firebase Cloud Messaging, Android)."`
	Token      string       `json:"token" minLength:"1" maxLength:"4096" pattern:"^[\\x21-\\x7e]+$" doc:"The token the push service gave the app: the device's address there."`
	Locale     string       `json:"locale" minLength:"2" maxLength:"35" pattern:"^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$" doc:"The language the device writes the notifications in, BCP 47." example:"fr-FR"`
	AppVersion string       `json:"app_version" minLength:"1" maxLength:"32" pattern:"^[0-9A-Za-z.+-]+$" doc:"The app's version, as 1.0.0." example:"1.0.0"`
}

type deviceInput struct {
	Device string `path:"device" format:"uuid" doc:"The device's ID, drawn by the app once per installation."`
	Body   deviceBody
}

type deviceIDInput struct {
	Device string `path:"device" format:"uuid" doc:"The device's ID, drawn by the app once per installation."`
}

func (s *Server) putDevice(ctx context.Context, in *deviceInput) (*struct{}, error) {
	sess := sessionFrom(ctx)
	now := s.Clock.Now().UTC()
	d := notify.Device{
		ID: in.Device, UserID: sess.UserID, Platform: notify.Platform(in.Body.Platform), Token: in.Body.Token,
		AccessID: sess.TokenID,
		Locale:   in.Body.Locale, AppVersion: in.Body.AppVersion, CreatedAt: now, UpdatedAt: now,
	}
	switch err := s.Notifications.PutDevice(ctx, sess.AccountID, d); {
	case errors.Is(err, ErrAccessRevoked):
		return nil, apiError(http.StatusUnauthorized, codeUnauthorized, "the access token was revoked")
	case errors.Is(err, ErrTooManyDevices):
		return nil, apiError(http.StatusConflict, codeTooManyDevices, "the account has "+strconv.Itoa(MaxDevices)+" devices already: remove one first")
	case err != nil:
		return nil, s.internal("device not registered", err)
	}
	return &struct{}{}, nil
}

func (s *Server) deleteDevice(ctx context.Context, in *deviceIDInput) (*struct{}, error) {
	if err := s.Notifications.DeleteDevice(ctx, sessionFrom(ctx).AccountID, in.Device); err != nil {
		return nil, s.internal("device not removed", err)
	}
	return &struct{}{}, nil
}

type liveActivityTokenBody struct {
	Token *string `json:"token" nullable:"true" minLength:"1" maxLength:"4096" pattern:"^[\\x21-\\x7e]+$" doc:"The token the push service starts a charge's Live Activity on the device with (APNs: the push-to-start token of iOS 17.2 and later); null: the instance starts none."`
}

type liveActivityTokenInput struct {
	Device string `path:"device" format:"uuid" doc:"The device's ID, registered first (PUT /devices/{device})."`
	Body   liveActivityTokenBody
}

func (s *Server) putLiveActivityToken(ctx context.Context, in *liveActivityTokenInput) (*struct{}, error) {
	token := ""
	if in.Body.Token != nil {
		token = *in.Body.Token
	}
	switch err := s.Notifications.SetLiveActivityToken(ctx, sessionFrom(ctx).AccountID, in.Device, token); {
	case errors.Is(err, ErrNoDevice):
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such device: register it first")
	case err != nil:
		return nil, s.internal("live activity token not set", err)
	}
	return &struct{}{}, nil
}

type liveActivityBody struct {
	Charge string  `json:"charge" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\\.[0-9]{6}Z$" doc:"The ID of the charge the activity shows, as GET /vehicles/{vehicle}/charging gives it." example:"2026-09-28T18:30:00.000000Z"`
	Token  *string `json:"token" nullable:"true" minLength:"1" maxLength:"4096" pattern:"^[\\x21-\\x7e]+$" doc:"The activity's own push token, which the instance updates and ends it with; null: the person dismissed it, and the instance tells it nothing more of this charge."`
}

type liveActivityInput struct {
	Device  string `path:"device" format:"uuid" doc:"The device's ID, registered first (PUT /devices/{device})."`
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Body    liveActivityBody
}

func (s *Server) putLiveActivity(ctx context.Context, in *liveActivityInput) (*struct{}, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	a := notify.LiveActivity{DeviceID: in.Device, VehicleID: v.ID, ChargeID: in.Body.Charge}
	if in.Body.Token != nil {
		a.Token = *in.Body.Token
	}
	switch err := s.Notifications.PutLiveActivity(ctx, sessionFrom(ctx).AccountID, a, s.Clock.Now().UTC()); {
	case errors.Is(err, ErrNoDevice):
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such device: register it first")
	case err != nil:
		return nil, s.internal("live activity not registered", err)
	}
	return &struct{}{}, nil
}

// number is a whole number of a JSON item that may be absent, null or a value, which a
// pointer cannot tell apart: a kind's threshold. Absent when not Sent.
type number struct {
	Sent  bool
	Null  bool
	Value int
}

func (n *number) UnmarshalJSON(b []byte) error {
	n.Sent = true
	if string(b) == "null" {
		n.Null = true
		return nil
	}
	return json.Unmarshal(b, &n.Value) //nolint:wrapcheck // the decoder's message names the value
}

func (n number) MarshalJSON() ([]byte, error) {
	if n.Null {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value) //nolint:wrapcheck // an int always marshals
}

// IsZero omits the field of an item that has none.
func (n number) IsZero() bool { return !n.Sent }

// Schema is a whole number or null, in the bounds of the field's tags.
func (number) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeInteger, Nullable: true}
}

// value returns the number as the pointer a store takes: nil for null.
func (n number) value() *int {
	if n.Null {
		return nil
	}
	return &n.Value
}

// thresholdOf is a threshold that is there: null when v is nil.
func thresholdOf(v *int) number {
	if v == nil {
		return number{Sent: true, Null: true}
	}
	return number{Sent: true, Value: *v}
}

type kindPreferenceJSON struct {
	Kind      string `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"The kind of notification, as charge_finished. The instance may have kinds of its own beyond those the document names."`
	Enabled   bool   `json:"enabled" doc:"Whether the user is told of it."`
	Default   bool   `json:"default" doc:"Whether it is on when the user chose nothing."`
	Always    bool   `json:"always" doc:"The user cannot turn it off: enabled is always true."`
	Threshold number `json:"threshold,omitzero" minimum:"1" maximum:"100" doc:"Only for a kind with a setting, left out for the others. battery_low: the state of charge, in %, under which a parked vehicle is told, from 5 to 50: the value for the vehicles that have none of their own, 20 unless the user set another."`
}

// choiceJSON is a vehicle's own choice of a kind: on, off, or default (it follows the
// vehicles' default).
type choiceJSON string

const (
	choiceOn      choiceJSON = "on"
	choiceOff     choiceJSON = "off"
	choiceDefault choiceJSON = "default"
)

// Schema lists the choices.
func (choiceJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(choiceOn, choiceOff, choiceDefault)
}

type vehicleKindJSON struct {
	Kind      string     `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"A vehicle's kind of notification."`
	Enabled   bool       `json:"enabled" doc:"Whether the user is told of it for this vehicle."`
	Choice    choiceJSON `json:"choice" doc:"The vehicle's own choice: on, off, or default, as vehicles_default."`
	Threshold number     `json:"threshold,omitzero" minimum:"1" maximum:"100" doc:"Only for a kind with a setting, left out for the others. battery_low: the vehicle's own threshold, in %; null when it follows the threshold of vehicles_default."`
}

type vehiclePreferencesJSON struct {
	Vehicle string            `json:"vehicle" doc:"The vehicle ID."`
	Kinds   []vehicleKindJSON `json:"kinds" nullable:"false" doc:"Every vehicle's kind, in the order of vehicles_default."`
}

type notificationPreferencesJSON struct {
	Account         []kindPreferenceJSON     `json:"account" nullable:"false" doc:"The account's kinds: credential_expiring, credential_refused (a connection to a maker that expires soon, or was refused), access_granted (a new access token), import_finished (an import of a vehicle's history done or failed)."`
	VehiclesDefault []kindPreferenceJSON     `json:"vehicles_default" nullable:"false" doc:"The vehicles' kinds, for every vehicle without a choice of its own: charge_interrupted (a charge stopped below its limit, the cable connected), charge_finished (it reached its limit), trip_to_classify (a trip ended without a purpose), charge_started, trip_ended, charge_live_activity (the charge in progress shown on the lock screen as it goes on, PUT /devices/{device}/live-activity-token), not_charged (plugged in at one of the account's places in the evening, and by the morning the state of charge has not risen: told once a night), battery_low (parked with the state of charge under a threshold, 20 % unless set: told once, again only after the state of charge went back to the threshold or above), left_open (parked and unlocked, or with a window open, for more than 15 minutes: told once; off by default). An extension may hide kinds from an account: they are then not listed."`
	Vehicles        []vehiclePreferencesJSON `json:"vehicles" nullable:"false" doc:"Each vehicle of the account, with its own choices. A vehicle's kinds are those that apply to it: left_open is not listed for a vehicle whose maker reports neither its locks nor its windows."`
}

type kindChoiceJSON struct {
	Kind      string `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"A kind of the level."`
	Enabled   *bool  `json:"enabled,omitempty" doc:"On or off; a kind always on cannot be turned off. Left out: unchanged, for an item that sets a threshold alone."`
	Threshold number `json:"threshold,omitzero" minimum:"1" maximum:"100" doc:"Only for a kind with a setting (battery_low: from 5 to 50, in % of the state of charge): the value for every vehicle without one of its own; null: the kind's default again. The kind's own choice stays as it is when the item has no enabled."`
}

type vehicleChoiceJSON struct {
	Kind      string     `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"A vehicle's kind."`
	Choice    choiceJSON `json:"choice,omitempty" doc:"On or off for this vehicle; default: it follows vehicles_default again. Left out: unchanged, for an item that sets a threshold alone."`
	Threshold number     `json:"threshold,omitzero" minimum:"1" maximum:"100" doc:"Only for a kind with a setting (battery_low: from 5 to 50, in % of the state of charge): this vehicle's own value; null: it follows the threshold of vehicles_default again. The vehicle's choice stays as it is when the item has no choice."`
}

type vehicleChoicesJSON struct {
	Vehicle string              `json:"vehicle" doc:"The vehicle ID."`
	Kinds   []vehicleChoiceJSON `json:"kinds" nullable:"false" maxItems:"64"`
}

type notificationChoicesJSON struct {
	Account         []kindChoiceJSON     `json:"account" nullable:"false" maxItems:"64" doc:"The account's kinds to set; empty: none."`
	VehiclesDefault []kindChoiceJSON     `json:"vehicles_default" nullable:"false" maxItems:"64" doc:"The vehicles' kinds to set for every vehicle without a choice of its own."`
	Vehicles        []vehicleChoicesJSON `json:"vehicles" nullable:"false" maxItems:"200" doc:"The vehicles' own choices to set; empty: none."`
}

type notificationChoicesInput struct {
	Body notificationChoicesJSON
}

// kinds are the notification kinds the instance knows.
func (s *Server) kinds() *notify.Registry {
	if s.Notifier == nil {
		return notify.NewRegistry()
	}
	return s.Notifier.Registry()
}

func (s *Server) getNotificationPreferences(ctx context.Context, _ *struct{}) (*body[notificationPreferencesJSON], error) {
	out, err := s.notificationPreferences(ctx)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

func (s *Server) notificationPreferences(ctx context.Context) (notificationPreferencesJSON, error) {
	acc := sessionFrom(ctx).AccountID
	p, err := s.Notifications.NotificationPreferences(ctx, acc)
	if err != nil {
		return notificationPreferencesJSON{}, s.internal("notification preferences not read", err)
	}
	vehicles, err := s.Reader.Vehicles(ctx, acc)
	if err != nil {
		return notificationPreferencesJSON{}, s.internal("vehicles not read", err)
	}
	hidden := s.Notifier.Hidden(ctx, acc)
	reg := s.kinds()
	// listed are the kinds of a level the account may see, an extension's hidden ones left
	// out.
	listed := func(l notify.Level) []notify.Spec {
		return slices.DeleteFunc(reg.Kinds(l), func(k notify.Spec) bool { return slices.Contains(hidden, k.Kind) })
	}
	level := func(l notify.Level, vehicleID string) []kindPreferenceJSON {
		out := []kindPreferenceJSON{}
		for _, k := range listed(l) {
			item := kindPreferenceJSON{Kind: string(k.Kind), Enabled: p.Enabled(k, vehicleID), Default: k.Default, Always: k.Always}
			if k.Threshold != nil {
				v := p.DefaultThreshold(k)
				item.Threshold = thresholdOf(&v)
			}
			out = append(out, item)
		}
		return out
	}
	out := notificationPreferencesJSON{
		Account: level(notify.AccountLevel, ""), VehiclesDefault: level(notify.VehicleLevel, ""),
		Vehicles: make([]vehiclePreferencesJSON, 0, len(vehicles)),
	}
	vehicleKinds := listed(notify.VehicleLevel)
	for _, v := range vehicles {
		applies, err := s.kindsApplying(ctx, acc, v.ID, vehicleKinds)
		if err != nil {
			return notificationPreferencesJSON{}, err
		}
		kinds := []vehicleKindJSON{}
		for _, k := range vehicleKinds {
			if !applies(k.Kind) {
				continue
			}
			choice := choiceDefault
			if on, ok := p.Vehicles[v.ID][k.Kind]; ok && !k.Always {
				choice = choiceOff
				if on {
					choice = choiceOn
				}
			}
			item := vehicleKindJSON{Kind: string(k.Kind), Enabled: p.Enabled(k, v.ID), Choice: choice}
			if k.Threshold != nil {
				var own *int
				if v, ok := p.VehicleThresholds[v.ID][k.Kind]; ok {
					own = &v
				}
				item.Threshold = thresholdOf(own)
			}
			kinds = append(kinds, item)
		}
		out.Vehicles = append(out.Vehicles, vehiclePreferencesJSON{Vehicle: v.ID, Kinds: kinds})
	}
	return out, nil
}

// kindsApplying tells which of the vehicle's kinds apply to it: all, but left_open, which
// only a vehicle whose maker reports its locks or its windows can tell (the state covers
// the doors or the windows, as the left open sensor of MQTT states it). The state is read
// only when such a kind is among those listed.
func (s *Server) kindsApplying(ctx context.Context, accountID, vehicleID string, listed []notify.Spec) (func(notify.Kind) bool, error) {
	if !slices.ContainsFunc(listed, func(k notify.Spec) bool { return k.Kind == notify.LeftOpen }) || s.States == nil {
		return func(notify.Kind) bool { return true }, nil
	}
	cur, err := s.States.Current(ctx, accountID, vehicleID, s.Clock.Now().UTC())
	if err != nil {
		return nil, s.internal("vehicle's state not read", err)
	}
	tells := cur.Snapshot.Covers&(core.FieldDoors|core.FieldWindows) != 0
	return func(k notify.Kind) bool { return k != notify.LeftOpen || tells }, nil
}

func (s *Server) putNotificationPreferences(ctx context.Context, in *notificationChoicesInput) (*body[notificationPreferencesJSON], error) {
	reg := s.kinds()
	acc := sessionFrom(ctx).AccountID
	hidden := s.Notifier.Hidden(ctx, acc)
	// check refuses a kind the instance does not know or hides from the account, of another
	// level, or always on and turned off; field names it in the message.
	check := func(field, kind string, l notify.Level, off bool) (notify.Spec, error) {
		spec, ok := reg.Spec(notify.Kind(kind))
		switch {
		case !ok || slices.Contains(hidden, spec.Kind):
			return notify.Spec{}, apiError(http.StatusBadRequest, codeInvalidBody, field+": unknown notification kind")
		case spec.Level != l:
			return notify.Spec{}, apiError(http.StatusBadRequest, codeInvalidBody, field+": a kind of the "+string(spec.Level)+", not of the "+string(l))
		case spec.Always && off:
			return notify.Spec{}, apiError(http.StatusBadRequest, codeInvalidBody, field+": this kind cannot be turned off")
		}
		return spec, nil
	}
	// checkThreshold refuses a threshold on a kind without one, or out of the kind's range.
	// A null one resets the threshold.
	checkThreshold := func(field string, spec notify.Spec, n number) error {
		switch {
		case spec.Threshold == nil:
			return apiError(http.StatusBadRequest, codeInvalidBody, field+".threshold: this kind has no threshold")
		case !n.Null && (n.Value < spec.Threshold.Min || n.Value > spec.Threshold.Max):
			return apiError(http.StatusBadRequest, codeInvalidBody, field+".threshold: from "+strconv.Itoa(spec.Threshold.Min)+" to "+strconv.Itoa(spec.Threshold.Max))
		}
		return nil
	}
	c := NotificationChoices{
		Account: map[notify.Kind]bool{}, VehiclesDefault: map[notify.Kind]bool{}, Vehicles: map[string]map[notify.Kind]*bool{},
		VehiclesDefaultThreshold: map[notify.Kind]*int{}, VehicleThresholds: map[string]map[notify.Kind]*int{},
	}
	for i, ch := range in.Body.Account {
		field := "body.account[" + strconv.Itoa(i) + "]"
		spec, err := check(field+".kind", ch.Kind, notify.AccountLevel, ch.Enabled != nil && !*ch.Enabled)
		if err != nil {
			return nil, err
		}
		if ch.Threshold.Sent {
			return nil, checkThreshold(field, spec, ch.Threshold) // an account's kind has none
		}
		if ch.Enabled == nil {
			return nil, invalidBody(field[len("body."):], "enabled is required")
		}
		c.Account[spec.Kind] = *ch.Enabled
	}
	for i, ch := range in.Body.VehiclesDefault {
		field := "body.vehicles_default[" + strconv.Itoa(i) + "]"
		spec, err := check(field+".kind", ch.Kind, notify.VehicleLevel, ch.Enabled != nil && !*ch.Enabled)
		if err != nil {
			return nil, err
		}
		if ch.Threshold.Sent {
			if err := checkThreshold(field, spec, ch.Threshold); err != nil {
				return nil, err
			}
			c.VehiclesDefaultThreshold[spec.Kind] = ch.Threshold.value()
		}
		switch {
		case ch.Enabled != nil:
			c.VehiclesDefault[spec.Kind] = *ch.Enabled
		case !ch.Threshold.Sent:
			return nil, invalidBody(field[len("body."):], "enabled or threshold is required")
		}
	}
	for i, vc := range in.Body.Vehicles {
		v, err := s.accountVehicle(ctx, vc.Vehicle)
		if err != nil {
			return nil, err
		}
		if c.Vehicles[v.ID] == nil {
			c.Vehicles[v.ID] = map[notify.Kind]*bool{}
			c.VehicleThresholds[v.ID] = map[notify.Kind]*int{}
		}
		for j, ch := range vc.Kinds {
			field := "body.vehicles[" + strconv.Itoa(i) + "].kinds[" + strconv.Itoa(j) + "]"
			spec, err := check(field+".kind", ch.Kind, notify.VehicleLevel, ch.Choice == choiceOff)
			if err != nil {
				return nil, err
			}
			if ch.Threshold.Sent {
				if err := checkThreshold(field, spec, ch.Threshold); err != nil {
					return nil, err
				}
				c.VehicleThresholds[v.ID][spec.Kind] = ch.Threshold.value()
			}
			switch ch.Choice {
			case "":
				if !ch.Threshold.Sent {
					return nil, invalidBody(field[len("body."):], "choice or threshold is required")
				}
			case choiceDefault:
				c.Vehicles[v.ID][spec.Kind] = nil
			default:
				b := ch.Choice == choiceOn
				c.Vehicles[v.ID][spec.Kind] = &b
			}
		}
	}
	if err := s.Notifications.SetNotificationPreferences(ctx, acc, c); err != nil {
		return nil, s.internal("notification preferences not set", err)
	}
	out, err := s.notificationPreferences(ctx)
	if err != nil {
		return nil, err
	}
	return respond(out), nil
}

// notify tells the account's devices of e, if the instance sends notifications; a
// failure is logged, never the request's.
func (s *Server) notify(ctx context.Context, e notify.Event) {
	if !s.Notifier.Sends() {
		return
	}
	if err := s.Notifier.Notify(context.WithoutCancel(ctx), e); err != nil {
		s.Log.Warn("notification not recorded", "kind", string(e.Kind), "account", e.AccountID, "err", err)
	}
}

func (s *Server) registerNotifications(api huma.API) {
	const scope = auth.ScopeNotifications
	huma.Register(api, s.scopedOperation(huma.Operation{
		OperationID: "putDevice", Method: http.MethodPut, Path: "/devices/{device}", Tags: []string{"notifications"},
		Summary: "Register a device",
		Description: "Registers the device an app runs on, with the token its push service reaches it with, or updates it " +
			"(a new token, another language, a new version of the app). The instance sends it the notifications its user " +
			"wants (/notification-preferences) when it sends any (`notifications` of /instance). The device is removed " +
			"with the account, and is in its export.",
		DefaultStatus: http.StatusNoContent, Middlewares: huma.Middlewares{s.requireJSON},
	}, scope, "Registered.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `invalid_parameter`: the device's ID is not a UUID.",
		http.StatusConflict:   "`too_many_devices`: the account has " + strconv.Itoa(MaxDevices) + " devices already.",
	})), s.putDevice)
	huma.Register(api, s.scopedOperation(huma.Operation{
		OperationID: "deleteDevice", Method: http.MethodDelete, Path: "/devices/{device}", Tags: []string{"notifications"},
		Summary:       "Remove a device",
		Description:   "The device is told nothing more, such as after the app signed out, and its Live Activities are forgotten. Removing a device the account does not have does nothing.",
		DefaultStatus: http.StatusNoContent,
	}, scope, "Removed.", map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: the device's ID is not a UUID.", http.StatusUnauthorized: errUnauthorized,
		http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteDevice)
	huma.Register(api, s.scopedOperation(huma.Operation{
		OperationID: "putLiveActivityToken", Method: http.MethodPut, Path: "/devices/{device}/live-activity-token", Tags: []string{"notifications"},
		Summary: "Let the instance start Live Activities on a device",
		Description: "Sets the token the device's push service starts a Live Activity with: when a charge starts, the " +
			"instance starts one on the device for the vehicle, which shows the charge as it goes on; the app then registers " +
			"the activity's own token (PUT /devices/{device}/live-activities/{vehicle}). Once per charge and vehicle: an " +
			"activity the person dismissed is not started again. Kept until the device is removed; null: none is started.",
		DefaultStatus: http.StatusNoContent, Middlewares: huma.Middlewares{s.requireJSON},
	}, scope, "Set.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `invalid_parameter`: the device's ID is not a UUID.",
		http.StatusNotFound:   "`not_found`: the device is not registered.",
	})), s.putLiveActivityToken)
	huma.Register(api, s.scopedOperation(huma.Operation{
		OperationID: "putLiveActivity", Method: http.MethodPut, Path: "/devices/{device}/live-activities/{vehicle}", Tags: []string{"notifications"},
		Summary: "Register a charge's Live Activity",
		Description: "Registers the Live Activity the device shows of the vehicle's charge in progress, with the token its " +
			"push service updates it with: the instance updates it at each new reading of the vehicle, at least 50 " +
			"seconds apart, and ends it once the charge is over, with its final figures. Only figures go out: the state of " +
			"charge, its limit, the power, the energy added, an estimated end. A device shows one activity per vehicle: " +
			"a later charge takes the place of the former, an earlier one changes nothing. When the instance started the " +
			"activity itself (PUT /devices/{device}/live-activity-token), the app registers its token the same way. " +
			"The activity is forgotten once the charge is over, and with the device. Nothing is sent when the instance " +
			"sends no notifications (`notifications` of /instance).",
		DefaultStatus: http.StatusNoContent, Middlewares: huma.Middlewares{s.requireJSON},
	}, scope, "Registered.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " `invalid_parameter`: the device's ID is not a UUID.",
		http.StatusNotFound:   "`not_found`: no such vehicle in the account, or the device is not registered.",
	})), s.putLiveActivity)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getNotificationPreferences", Method: http.MethodGet, Path: "/notification-preferences", Tags: []string{"notifications"},
		Summary: "What the user is told of",
		Description: "Each kind of notification and whether the user is told of it: the account's, the vehicles' default, " +
			"and each vehicle's own choices. A vehicle's kinds are those that apply to it, and an instance may hide kinds " +
			"from an account. battery_low carries its threshold, in % of the state of charge: the vehicles' default in " +
			"vehicles_default, the vehicle's own (null: it follows the default) in each vehicle. A value the vehicle does not " +
			"tell triggers nothing: without a charge limit, no charge is told interrupted, without a state of charge no " +
			"battery is told low, without a lock or a window no vehicle is told left open.",
	}, "The preferences.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal,
	}), s.getNotificationPreferences)
	huma.Register(api, s.scopedOperation(huma.Operation{
		OperationID: "putNotificationPreferences", Method: http.MethodPut, Path: "/notification-preferences", Tags: []string{"notifications"},
		Summary: "Choose what the user is told of",
		Description: "Sets the kinds it lists, and leaves the others as they are: an app that does not know a kind of the " +
			"instance never changes it. An item may carry the threshold of battery_low (from 5 to 50, in % of the state of " +
			"charge) with or without its enabled or choice: a threshold alone leaves the choice as it is, and the choice " +
			"alone leaves the threshold. A null threshold resets it: the vehicle's follows the vehicles' default again, " +
			"the default's the kind's own, 20. All or none.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, scope, "The preferences, as they now stand.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " Also a kind the instance does not know or hides from the account, of another level, or that cannot be turned off, a threshold out of its range or on a kind that has none, an item with neither a choice nor a threshold.",
		http.StatusNotFound:   errVehicle,
	})), s.putNotificationPreferences)
}
