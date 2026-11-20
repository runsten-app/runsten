package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/auth"
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
	// DeleteDevice removes the device, if the account has it.
	DeleteDevice(ctx context.Context, accountID, deviceID string) error
	NotificationPreferences(ctx context.Context, accountID string) (notify.Preferences, error)
	// SetNotificationPreferences sets the choices c lists, leaving the others as they are.
	SetNotificationPreferences(ctx context.Context, accountID string, c NotificationChoices) error
}

// NotificationChoices are the preferences a user sets at once: the kinds they list,
// each on or off; a vehicle's kind nil follows the vehicles' default again.
type NotificationChoices struct {
	Account         map[notify.Kind]bool
	VehiclesDefault map[notify.Kind]bool
	Vehicles        map[string]map[notify.Kind]*bool
}

// MaxDevices is how many devices an account registers at most: a phone or two, a tablet,
// and those reinstalled before their app could remove them.
const MaxDevices = 20

// ErrTooManyDevices is returned when the account has MaxDevices devices already.
var ErrTooManyDevices = errors.New("too many devices")

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
		Locale: in.Body.Locale, AppVersion: in.Body.AppVersion, CreatedAt: now, UpdatedAt: now,
	}
	switch err := s.Notifications.PutDevice(ctx, sess.AccountID, d); {
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

type kindPreferenceJSON struct {
	Kind    string `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"The kind of notification, as charge_finished. The instance may have kinds of its own beyond those the document names."`
	Enabled bool   `json:"enabled" doc:"Whether the user is told of it."`
	Default bool   `json:"default" doc:"Whether it is on when the user chose nothing."`
	Always  bool   `json:"always" doc:"The user cannot turn it off: enabled is always true."`
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
	Kind    string     `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"A vehicle's kind of notification."`
	Enabled bool       `json:"enabled" doc:"Whether the user is told of it for this vehicle."`
	Choice  choiceJSON `json:"choice" doc:"The vehicle's own choice: on, off, or default, as vehicles_default."`
}

type vehiclePreferencesJSON struct {
	Vehicle string            `json:"vehicle" doc:"The vehicle ID."`
	Kinds   []vehicleKindJSON `json:"kinds" nullable:"false" doc:"Every vehicle's kind, in the order of vehicles_default."`
}

type notificationPreferencesJSON struct {
	Account         []kindPreferenceJSON     `json:"account" nullable:"false" doc:"The account's kinds: credential_expiring, credential_refused (a connection to a maker that expires soon, or was refused), access_granted (a new access token), import_finished (an import of a vehicle's history done or failed)."`
	VehiclesDefault []kindPreferenceJSON     `json:"vehicles_default" nullable:"false" doc:"The vehicles' kinds, for every vehicle without a choice of its own: charge_interrupted (a charge stopped below its limit, the cable connected), charge_finished (it reached its limit), trip_to_classify (a trip ended without a purpose), charge_started, trip_ended."`
	Vehicles        []vehiclePreferencesJSON `json:"vehicles" nullable:"false" doc:"Each vehicle of the account, with its own choices."`
}

type kindChoiceJSON struct {
	Kind    string `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"A kind of the level."`
	Enabled bool   `json:"enabled" doc:"On or off; a kind always on cannot be turned off."`
}

type vehicleChoiceJSON struct {
	Kind   string     `json:"kind" pattern:"^[a-z][a-z0-9_]{0,63}$" doc:"A vehicle's kind."`
	Choice choiceJSON `json:"choice" doc:"On or off for this vehicle; default: it follows vehicles_default again."`
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
	reg := s.kinds()
	level := func(l notify.Level, vehicleID string) []kindPreferenceJSON {
		out := []kindPreferenceJSON{}
		for _, k := range reg.Kinds(l) {
			out = append(out, kindPreferenceJSON{
				Kind: string(k.Kind), Enabled: p.Enabled(k, vehicleID), Default: k.Default, Always: k.Always,
			})
		}
		return out
	}
	out := notificationPreferencesJSON{
		Account: level(notify.AccountLevel, ""), VehiclesDefault: level(notify.VehicleLevel, ""),
		Vehicles: make([]vehiclePreferencesJSON, 0, len(vehicles)),
	}
	for _, v := range vehicles {
		kinds := []vehicleKindJSON{}
		for _, k := range reg.Kinds(notify.VehicleLevel) {
			choice := choiceDefault
			if on, ok := p.Vehicles[v.ID][k.Kind]; ok && !k.Always {
				choice = choiceOff
				if on {
					choice = choiceOn
				}
			}
			kinds = append(kinds, vehicleKindJSON{Kind: string(k.Kind), Enabled: p.Enabled(k, v.ID), Choice: choice})
		}
		out.Vehicles = append(out.Vehicles, vehiclePreferencesJSON{Vehicle: v.ID, Kinds: kinds})
	}
	return out, nil
}

func (s *Server) putNotificationPreferences(ctx context.Context, in *notificationChoicesInput) (*body[notificationPreferencesJSON], error) {
	reg := s.kinds()
	// check refuses a kind the instance does not know, of another level, or always on and
	// turned off; field names it in the message.
	check := func(field, kind string, l notify.Level, off bool) (notify.Kind, error) {
		spec, ok := reg.Spec(notify.Kind(kind))
		switch {
		case !ok:
			return "", apiError(http.StatusBadRequest, codeInvalidBody, field+": unknown notification kind")
		case spec.Level != l:
			return "", apiError(http.StatusBadRequest, codeInvalidBody, field+": a kind of the "+string(spec.Level)+", not of the "+string(l))
		case spec.Always && off:
			return "", apiError(http.StatusBadRequest, codeInvalidBody, field+": this kind cannot be turned off")
		}
		return spec.Kind, nil
	}
	c := NotificationChoices{
		Account: map[notify.Kind]bool{}, VehiclesDefault: map[notify.Kind]bool{}, Vehicles: map[string]map[notify.Kind]*bool{},
	}
	for i, ch := range in.Body.Account {
		k, err := check("body.account["+strconv.Itoa(i)+"].kind", ch.Kind, notify.AccountLevel, !ch.Enabled)
		if err != nil {
			return nil, err
		}
		c.Account[k] = ch.Enabled
	}
	for i, ch := range in.Body.VehiclesDefault {
		k, err := check("body.vehicles_default["+strconv.Itoa(i)+"].kind", ch.Kind, notify.VehicleLevel, !ch.Enabled)
		if err != nil {
			return nil, err
		}
		c.VehiclesDefault[k] = ch.Enabled
	}
	for i, vc := range in.Body.Vehicles {
		v, err := s.accountVehicle(ctx, vc.Vehicle)
		if err != nil {
			return nil, err
		}
		if c.Vehicles[v.ID] == nil {
			c.Vehicles[v.ID] = map[notify.Kind]*bool{}
		}
		for j, ch := range vc.Kinds {
			field := "body.vehicles[" + strconv.Itoa(i) + "].kinds[" + strconv.Itoa(j) + "].kind"
			k, err := check(field, ch.Kind, notify.VehicleLevel, ch.Choice == choiceOff)
			if err != nil {
				return nil, err
			}
			var on *bool
			if ch.Choice != choiceDefault {
				b := ch.Choice == choiceOn
				on = &b
			}
			c.Vehicles[v.ID][k] = on
		}
	}
	if err := s.Notifications.SetNotificationPreferences(ctx, sessionFrom(ctx).AccountID, c); err != nil {
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
		Description:   "The device is told nothing more, such as after the app signed out. Removing a device the account does not have does nothing.",
		DefaultStatus: http.StatusNoContent,
	}, scope, "Removed.", map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: the device's ID is not a UUID.", http.StatusUnauthorized: errUnauthorized,
		http.StatusForbidden: errCrossOrigin, http.StatusInternalServerError: errInternal,
	}), s.deleteDevice)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getNotificationPreferences", Method: http.MethodGet, Path: "/notification-preferences", Tags: []string{"notifications"},
		Summary: "What the user is told of",
		Description: "Each kind of notification and whether the user is told of it: the account's, the vehicles' default, " +
			"and each vehicle's own choices. A value the vehicle does not tell triggers nothing: without a charge limit, " +
			"no charge is told interrupted.",
	}, "The preferences.", map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal,
	}), s.getNotificationPreferences)
	huma.Register(api, s.scopedOperation(huma.Operation{
		OperationID: "putNotificationPreferences", Method: http.MethodPut, Path: "/notification-preferences", Tags: []string{"notifications"},
		Summary: "Choose what the user is told of",
		Description: "Sets the kinds it lists, and leaves the others as they are: an app that does not know a kind of the " +
			"instance never changes it. All or none.",
		Middlewares: huma.Middlewares{s.requireJSON},
	}, scope, "The preferences, as they now stand.", writeErrors(map[int]string{
		http.StatusBadRequest: errBody + " Also a kind the instance does not know, of another level, or that cannot be turned off.",
		http.StatusNotFound:   errVehicle,
	})), s.putNotificationPreferences)
}
