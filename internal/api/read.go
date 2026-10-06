package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// idFormat formats the ID of an event: its detection time, to the microsecond (the
// precision of the database). Unlike a row ID, it survives a rebuild of the derived
// tables, as long as the same reading reveals the event.
const idFormat = "2006-01-02T15:04:05.000000Z"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// body is an output made of a JSON body only.
type body[T any] struct {
	Body T
}

func respond[T any](v T) *body[T] { return &body[T]{Body: v} }

// vehicleInput is the {vehicle} of the path. Its format is not declared: an invalid ID
// is not found, like another account's vehicle, rather than a 400. The inputs repeat
// the field: huma ignores the fields of an unexported embedded struct.
type vehicleInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
}

// accountVehicle resolves the {vehicle} of the path within the session's account:
// another account's vehicle is not found, like a missing one.
func (s *Server) accountVehicle(ctx context.Context, id string) (Vehicle, error) {
	if !uuidPattern.MatchString(id) {
		return Vehicle{}, apiError(http.StatusNotFound, codeNotFound, "no such vehicle")
	}
	v, found, err := s.Reader.Vehicle(ctx, sessionFrom(ctx).AccountID, id)
	switch {
	case err != nil:
		return Vehicle{}, s.internal("vehicle not read", err)
	case !found:
		return Vehicle{}, apiError(http.StatusNotFound, codeNotFound, "no such vehicle")
	}
	return v, nil
}

type connectionStatus string

// Schema lists the statuses.
func (connectionStatus) Schema(huma.Registry) *huma.Schema {
	return enumSchema(connectionActive, connectionReauth)
}

const (
	connectionActive connectionStatus = "active"
	connectionReauth connectionStatus = "reauth_required"
)

// apiKeyStatus is the application key a vehicle is read with.
type apiKeyStatus string

// Schema lists the statuses.
func (apiKeyStatus) Schema(huma.Registry) *huma.Schema {
	return enumSchema(apiKeyInstance, apiKeyOwn, apiKeyRefused, apiKeyMissing)
}

const (
	apiKeyInstance apiKeyStatus = "instance"
	apiKeyOwn      apiKeyStatus = "own"
	apiKeyRefused  apiKeyStatus = "refused"
	apiKeyMissing  apiKeyStatus = "missing"
)

type connectionJSON struct {
	Status       connectionStatus `json:"status" doc:"reauth_required once the grant is lost: the vehicle is no longer read until the user connects the Volvo ID again."`
	APIKey       apiKeyStatus     `json:"api_key" doc:"The application key the vehicle is read with: instance, the instance's, which reads every vehicle when the instance has one; own, the account's. refused: the account's, refused by Volvo; missing: none, the instance has none either. Neither refused nor missing is read, until the user gives a key (PUT /connection/api-key)."`
	AuthorizedAt *time.Time       `json:"authorized_at" pattern:"Z$" doc:"When the user authorized the connection; null for a token pasted by hand."`
	RefreshedAt  *time.Time       `json:"refreshed_at" pattern:"Z$" doc:"When the current refresh token was issued: the collector renews an idle one daily, before it lapses. null without one (a token pasted by hand)."`
	ReauthAt     *time.Time       `json:"reauth_at" pattern:"Z$" doc:"When the grant was found lost; null while active."`
	ReauthReason *string          `json:"reauth_reason" doc:"Why the grant was lost; null while active."`
}

type vehicleJSON struct {
	ID         string               `json:"id" format:"uuid"`
	VIN        string               `json:"vin"`
	Model      modelJSON            `json:"model" doc:"What the vehicle is, from its latest details."`
	Connection connectionJSON       `json:"connection" doc:"The state of the provider connection (Volvo ID) that reads the vehicle."`
	Collection null[collectionJSON] `json:"collection" doc:"What the collector wrote of the vehicle after its latest pass; null before the first one."`
}

// The collector's statuses, the same strings as its own.
type (
	collectionModeJSON string
	failureKindJSON    string
)

// Schema lists the polling modes.
func (collectionModeJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema("parked", "driving", "charging")
}

// Schema lists the kinds of a failed call.
func (failureKindJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema("quota", "rate_limited", "unauthorized", "not_found", "unavailable", "token", "key_refused", "other")
}

type collectionJSON struct {
	PassedAt    time.Time          `json:"passed_at" pattern:"Z$" doc:"The collector's latest pass over the vehicle, written again at least every minute while it runs: an old one means that it stopped, or cannot reach the database."`
	Mode        collectionModeJSON `json:"mode" doc:"The collector's polling mode, from the latest readings: it reads more often while driving or charging."`
	ReadAt      *time.Time         `json:"read_at" pattern:"Z$" doc:"The latest successful call; null when unknown."`
	NextReadAt  *time.Time         `json:"next_read_at" pattern:"Z$" doc:"When the next call is due, as of passed_at, pauses and quotas included; null when none is, the connection having lost its grant, or having no application key it may read with."`
	PausedUntil *time.Time         `json:"paused_until" pattern:"Z$" doc:"No call before this, as of passed_at: the vendor limited the rate, or refused the token after a refresh. null when not paused."`
	Quota       []quotaJSON        `json:"quota" nullable:"false" doc:"The vendor's APIs whose quota is exhausted, as of passed_at, by name: what they give is not read until then. The quota is that of the application key the vehicle is read with: the instance's, which counts every vehicle of the instance, or on an instance without one, the account's own."`
	LastFailure null[failureJSON]  `json:"last_failure" doc:"The latest failed call; null when none is known."`
}

type quotaJSON struct {
	API   string    `json:"api" doc:"The vendor's API, by the vendor's name (Volvo: connected-vehicle, energy, location)."`
	Until time.Time `json:"until" pattern:"Z$"`
}

type failureJSON struct {
	At       time.Time       `json:"at" pattern:"Z$"`
	Endpoint *string         `json:"endpoint" doc:"The vendor's endpoint called; null when no call could be made, without an access token."`
	Status   *int            `json:"status" doc:"The HTTP status; null when the API did not respond."`
	Kind     failureKindJSON `json:"kind" doc:"quota: the API's quota is exhausted. rate_limited: too many calls, the account is paused. unauthorized: the token was refused again after a refresh. not_found: the vendor does not know the vehicle. unavailable: no response, or a server error. token: no access token could be had, the grant still held (Volvo ID unreachable). key_refused: the application key was refused, not the token. other: another refusal."`
}

func collectionOf(v Vehicle) null[collectionJSON] {
	c := v.Collection
	if c == nil {
		return null[collectionJSON]{}
	}
	j := collectionJSON{
		PassedAt: c.PassedAt, Mode: collectionModeJSON(c.Mode), ReadAt: timeOrNil(c.ReadAt),
		NextReadAt: timeOrNil(c.NextAt), PausedUntil: timeOrNil(c.PausedUntil), Quota: []quotaJSON{},
	}
	for _, api := range slices.Sorted(maps.Keys(c.Quota)) {
		j.Quota = append(j.Quota, quotaJSON{API: api, Until: c.Quota[api]})
	}
	if f := c.Failure; !f.At.IsZero() {
		fj := failureJSON{At: f.At, Kind: failureKindJSON(f.Kind)}
		if f.Endpoint != "" {
			fj.Endpoint = &f.Endpoint
		}
		if f.Status != 0 {
			fj.Status = &f.Status
		}
		j.LastFailure = known(fj)
	}
	return known(j)
}

type vehicleListJSON struct {
	Items []vehicleJSON `json:"items" nullable:"false"`
}

func (s *Server) connectionOf(v Vehicle) connectionJSON {
	c := connectionJSON{Status: connectionActive, APIKey: apiKeyInstance, AuthorizedAt: timeOrNil(v.AuthorizedAt), RefreshedAt: timeOrNil(v.RefreshedAt)}
	switch {
	case s.InstanceKey: // it reads every vehicle, whatever key an account left
	case v.OwnKey && v.KeyRefused:
		c.APIKey = apiKeyRefused
	case v.OwnKey:
		c.APIKey = apiKeyOwn
	case !s.InstanceKey:
		c.APIKey = apiKeyMissing
	}
	if v.ReauthReason != "" {
		c.Status, c.ReauthAt, c.ReauthReason = connectionReauth, timeOrNil(v.ReauthAt), &v.ReauthReason
	}
	return c
}

func (s *Server) listVehicles(ctx context.Context, _ *struct{}) (*body[vehicleListJSON], error) {
	vs, err := s.Reader.Vehicles(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, s.internal("vehicles not read", err)
	}
	items := make([]vehicleJSON, len(vs))
	for i, v := range vs {
		if items[i], err = s.vehicleOf(ctx, v); err != nil {
			return nil, err
		}
	}
	return respond(vehicleListJSON{items}), nil
}

func (s *Server) getVehicle(ctx context.Context, in *vehicleInput) (*body[vehicleJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	j, err := s.vehicleOf(ctx, v)
	if err != nil {
		return nil, err
	}
	return respond(j), nil
}

// reading is a current value: what the vehicle reported, and when Runsten read it.
type reading[T any] struct {
	Value      T          `json:"value"`
	ReportedAt *time.Time `json:"reported_at" pattern:"Z$" doc:"The vehicle's own timestamp of the value; null when the vehicle gives none."`
	FetchedAt  time.Time  `json:"fetched_at" pattern:"Z$" doc:"The first reading of this value. The value held at fetched_at and again at checked_at; nothing is known in between, nor after."`
	CheckedAt  time.Time  `json:"checked_at" pattern:"Z$" doc:"The last reading of the same value, unchanged."`
}

// readingOf is null when the value is unknown: never read, or not reported.
func readingOf[T, U any](c core.Current, f core.Field, v core.Value[T], conv func(T) U) null[reading[U]] {
	if !v.OK {
		return null[reading[U]]{}
	}
	fetched, checked := c.Read(f)
	return known(reading[U]{Value: conv(v.V), ReportedAt: timeOrNil(v.At), FetchedAt: fetched, CheckedAt: checked})
}

func same[T any](v T) T                       { return v }
func text[T ~string, U ~string](v T) U        { return U(v) }
func positionOf(p core.Position) positionJSON { return positionJSON(p) }

type positionJSON struct {
	Lat float64 `json:"lat" minimum:"-90" maximum:"90"`
	Lon float64 `json:"lon" minimum:"-180" maximum:"180"`
}

// Enumerations of the state, from the domain's values.
type (
	engineJSON         string
	chargingStatusJSON string
	cableJSON          string
	chargeTypeJSON     string
)

// Schema lists the engine states.
func (engineJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.EngineRunning, core.EngineStopped)
}

// Schema lists the charging statuses.
func (chargingStatusJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.ChargingIdle, core.ChargingActive, core.ChargingDone, core.ChargingScheduled,
		core.ChargingDischarging, core.ChargingError)
}

// Schema lists the cable states.
func (cableJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.Connected, core.Disconnected, core.ConnectionFault)
}

// Schema lists the charge types.
func (chargeTypeJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.AC, core.DC)
}

type chargingJSON struct {
	Status       null[reading[chargingStatusJSON]] `json:"status" doc:"A charge is in progress when it is charging."`
	Cable        null[reading[cableJSON]]          `json:"cable"`
	Type         null[reading[chargeTypeJSON]]     `json:"type"`
	PowerW       null[reading[float64]]            `json:"power_w" doc:"Charging power, in W."`
	TargetSoCPct null[reading[float64]]            `json:"target_soc_pct" doc:"Target state of charge, in %."`
}

type stateJSON struct {
	VehicleID          string                      `json:"vehicle_id" format:"uuid"`
	Connection         connectionJSON              `json:"connection"`
	CheckedAt          *time.Time                  `json:"checked_at" pattern:"Z$" doc:"The latest reading of any value: whether the collection is up to date. null before the first one."`
	Engine             null[reading[engineJSON]]   `json:"engine"`
	SoCPct             null[reading[float64]]      `json:"soc_pct" doc:"State of charge, in %."`
	RangeKm            null[reading[float64]]      `json:"range_km" doc:"Remaining electric range."`
	OdometerKm         null[reading[float64]]      `json:"odometer_km"`
	Position           null[reading[positionJSON]] `json:"position"`
	BatteryCapacityKWh null[reading[float64]]      `json:"battery_capacity_kwh" doc:"The battery capacity the vendor reports. The energy estimates rest on it when Runsten's catalog does not know the vehicle's variant or its net capacity (capacity_source api), and on the variant's net capacity otherwise."`
	Charging           chargingJSON                `json:"charging"`
}

// getState is Verdandi: the vehicle now.
func (s *Server) getState(ctx context.Context, in *vehicleInput) (*body[stateJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	c, err := s.States.Current(ctx, sessionFrom(ctx).AccountID, v.ID, s.Clock.Now())
	if err != nil {
		return nil, s.internal("state not read", err)
	}
	n := c.Snapshot
	return respond(stateJSON{
		VehicleID:          v.ID,
		Connection:         s.connectionOf(v),
		CheckedAt:          timeOrNil(c.CheckedAt()),
		Engine:             readingOf(c, core.FieldEngine, n.Engine, text[core.EngineState, engineJSON]),
		SoCPct:             readingOf(c, core.FieldSoC, n.SoC, same),
		RangeKm:            readingOf(c, core.FieldRange, n.RangeKm, same),
		OdometerKm:         readingOf(c, core.FieldOdometer, n.OdometerKm, same),
		Position:           readingOf(c, core.FieldPosition, n.Position, positionOf),
		BatteryCapacityKWh: readingOf(c, core.FieldCapacity, n.CapacityKWh, same),
		Charging: chargingJSON{
			Status:       readingOf(c, core.FieldCharging, n.Charging, text[core.ChargingStatus, chargingStatusJSON]),
			Cable:        readingOf(c, core.FieldConnection, n.Connection, text[core.Connection, cableJSON]),
			Type:         readingOf(c, core.FieldChargeType, n.ChargeType, text[core.ChargeType, chargeTypeJSON]),
			PowerW:       readingOf(c, core.FieldPower, n.PowerW, same),
			TargetSoCPct: readingOf(c, core.FieldTargetSoC, n.TargetSoC, same),
		},
	}), nil
}

// boundsJSON is the interval in which an instant lies: polling only brackets a
// transition.
type boundsJSON struct {
	After  time.Time `json:"after" pattern:"Z$"`
	Before time.Time `json:"before" pattern:"Z$"`
}

// EventFields are common to trips and charges. The type is exported because huma, unlike
// encoding/json, ignores the fields of an unexported embedded struct.
type EventFields struct {
	ID            string     `json:"id" doc:"Identifies the event for its vehicle: its detection time, to the microsecond. Unlike a row ID, it survives a rebuild of the derived data, as long as the same reading reveals the event. Treat it as opaque." example:"2026-09-28T07:01:00.000000Z"`
	DetectedAt    time.Time  `json:"detected_at" pattern:"Z$" doc:"The first reading that revealed the event."`
	Reconstructed bool       `json:"reconstructed" doc:"The event was never seen, only revealed by the odometer or the state of charge: it lies somewhere within [start.after, end.before], and start and end are that same interval."`
	Start         boundsJSON `json:"start" doc:"When the event started. The bounds are never collapsed into a single time."`
	End           boundsJSON `json:"end" doc:"When the event ended."`
}

func eventOf(detected time.Time, reconstructed bool, start, end core.Bounds) EventFields {
	return EventFields{
		ID: detected.UTC().Format(idFormat), DetectedAt: detected, Reconstructed: reconstructed,
		Start: boundsJSON(start), End: boundsJSON(end),
	}
}

type capacitySourceJSON string

// Schema lists the sources of a capacity.
func (capacitySourceJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.CapacityCatalogNet, core.CapacityAPI)
}

// CapacityFields tell which battery capacity an energy estimate rests on. Exported, like
// EventFields, for huma.
type CapacityFields struct {
	CapacityKWh    *float64                 `json:"capacity_kwh" exclusiveMinimum:"0" doc:"The battery capacity the energy estimate rests on, as of the readings that ended the event. null when none was known, and for an event derived before Runsten recorded it: a rebuild of the derived data gives it."`
	CapacitySource null[capacitySourceJSON] `json:"capacity_source" doc:"Where capacity_kwh comes from. catalog_net: the net capacity of the vehicle's variant in Runsten's catalog; an assumption, the state of charge running from 0 to 100 % over the usable capacity. api: the capacity the vendor reports, gross as far as the published values tell, when the catalog does not know the variant or its net capacity. null with capacity_kwh."`
}

func capacityOf(c core.Value[core.Capacity]) CapacityFields {
	if !c.OK {
		return CapacityFields{}
	}
	return CapacityFields{CapacityKWh: &c.V.KWh, CapacitySource: known(capacitySourceJSON(c.V.Source))}
}

// vehicleTripJSON holds the vehicle's own trip figures.
type vehicleTripJSON struct {
	TripMeterKm            *float64 `json:"trip_meter_km"`
	ConsumptionKWhPer100km *float64 `json:"consumption_kwh_per_100km"`
}

type tripJSON struct {
	EventFields
	DistanceKm      *float64 `json:"distance_km" doc:"Odometer difference."`
	StartOdometerKm *float64 `json:"start_odometer_km"`
	EndOdometerKm   *float64 `json:"end_odometer_km"`
	StartSoCPct     *float64 `json:"start_soc_pct"`
	EndSoCPct       *float64 `json:"end_soc_pct"`
	StartRangeKm    *float64 `json:"start_range_km"`
	EndRangeKm      *float64 `json:"end_range_km"`
	EnergyKWh       *float64 `json:"energy_kwh" doc:"An estimate, not a measurement: ΔSoC × capacity_kwh."`
	CapacityFields
	StartPosition   null[positionJSON] `json:"start_position"`
	EndPosition     null[positionJSON] `json:"end_position"`
	StartPlace      null[placeRefJSON] `json:"start_place" doc:"The place of start_position: the nearest one whose circle holds it. null without a position, and outside every place."`
	EndPlace        null[placeRefJSON] `json:"end_place" doc:"The place of end_position, as for start_place."`
	StartAddress    *string            `json:"start_address" doc:"The street and town of start_position, from the instance's reverse geocoder, outside every place. null at a place, without a geocoder, when it knows none, and until it answered: a request made by this read is resolved for a later one."`
	EndAddress      *string            `json:"end_address" doc:"The address of end_position, as for start_address."`
	VehicleReported vehicleTripJSON    `json:"vehicle_reported" doc:"The vehicle's own trip figures, for comparison only: their exact semantics are unknown, and a reconstructed trip has none."`
}

// tripOf converts a trip, its positions at the account's places or at their addresses.
func tripOf(t core.Trip, places []core.Place, book addressBook) tripJSON {
	return tripJSON{
		EventFields:     eventOf(t.DetectedAt, t.Reconstructed, t.Start, t.End),
		DistanceKm:      opt(t.DistanceKm, same),
		StartOdometerKm: opt(t.StartOdometerKm, same), EndOdometerKm: opt(t.EndOdometerKm, same),
		StartSoCPct: opt(t.StartSoC, same), EndSoCPct: opt(t.EndSoC, same),
		StartRangeKm: opt(t.StartRangeKm, same), EndRangeKm: opt(t.EndRangeKm, same),
		EnergyKWh:      opt(t.EnergyKWh, same),
		CapacityFields: capacityOf(t.Capacity),
		StartPosition:  nullOf(t.From, positionOf), EndPosition: nullOf(t.To, positionOf),
		StartPlace: placeAt(t.From, places), EndPlace: placeAt(t.To, places),
		StartAddress: book.of(t.From), EndAddress: book.of(t.To),
		VehicleReported: vehicleTripJSON{
			TripMeterKm: opt(t.TripMeterKm, same), ConsumptionKWhPer100km: opt(t.ConsumptionKWhPer100km, same),
		},
	}
}

type chargeJSON struct {
	EventFields
	Type           null[chargeTypeJSON] `json:"type"`
	StartSoCPct    *float64             `json:"start_soc_pct"`
	EndSoCPct      *float64             `json:"end_soc_pct"`
	TargetSoCPct   *float64             `json:"target_soc_pct"`
	EnergySoCKWh   *float64             `json:"energy_soc_kwh" doc:"One of two separate estimates, neither a measurement (the vehicle exposes no energy meter): ΔSoC × capacity_kwh."`
	EnergyPowerKWh *float64             `json:"energy_power_kwh" doc:"The other estimate: the charging power integrated over the observed readings. null without observed power readings, as for a reconstructed charge."`
	CapacityFields
	Position null[positionJSON] `json:"position"`
	Place    null[placeRefJSON] `json:"place" doc:"The place of the charge: the nearest one whose circle holds its position; without a position, the place that takes such charges, unless the charge is DC. null elsewhere."`
	Address  *string            `json:"address" doc:"The street and town of position, from the instance's reverse geocoder, outside every place. null at a place, without a geocoder, when it knows none, and until it answered: a request made by this read is resolved for a later one."`
	Cost     null[costJSON]     `json:"cost" doc:"What the charge cost: entered by the user, or else estimated from the tariff of its place. null when unknown: without the account's currency, outside every place without an entered cost, without energy_soc_kwh, or when the tariff has no version for a part of the charge's window."`
}

func chargeOf(c core.Charge, places []core.Place, cost core.Value[core.Cost], book addressBook) chargeJSON {
	place, _ := core.PlaceOf(c, places)
	return chargeJSON{
		EventFields: eventOf(c.DetectedAt, c.Reconstructed, c.Start, c.End),
		Type:        nullOf(c.Type, text[core.ChargeType, chargeTypeJSON]),
		StartSoCPct: opt(c.StartSoC, same), EndSoCPct: opt(c.EndSoC, same), TargetSoCPct: opt(c.TargetSoC, same),
		EnergySoCKWh: opt(c.EnergySoCKWh, same), EnergyPowerKWh: opt(c.EnergyPowerKWh, same),
		CapacityFields: capacityOf(c.Capacity),
		Position:       nullOf(c.Position, positionOf),
		Address:        book.of(c.Position),
		Place:          nullOf(core.Value[core.Place]{V: place, OK: place.ID != ""}, placeRefOf),
		Cost:           nullOf(cost, costOf),
	}
}

// page is a page of a list, newest first (by start.after, then detected_at).
type page[T any] struct {
	Items      []T     `json:"items" nullable:"false"`
	NextCursor *string `json:"next_cursor" doc:"The cursor of the next page; null on the last one."`
}

// listInput selects a page of events.
type listInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" doc:"Keeps the events that may have happened, at least partly, at or after this time: those with end.before after it. An event whose bounds straddle it is kept." example:"2026-09-28T00:00:00Z"`
	To      time.Time `query:"to" doc:"Keeps the events that may have happened, at least partly, before this time: those with start.after before it. With from, it must come after from." example:"2026-09-29T00:00:00Z"`
	// A cursor is opaque to clients: see encodeCursor.
	Cursor string `query:"cursor" doc:"The next_cursor of the previous page. It stays valid when from or to change."`
	Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"50" doc:"The maximum number of events of the page."`
}

// eventQuery checks what the schema cannot: the period and the cursor.
func eventQuery(in *listInput) (EventQuery, error) {
	q := EventQuery{From: in.From.UTC(), To: in.To.UTC(), Limit: in.Limit}
	if in.From.IsZero() {
		q.From = time.Time{}
	}
	if in.To.IsZero() {
		q.To = time.Time{}
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return q, apiError(http.StatusBadRequest, codeInvalidParameter, "from must be before to")
	}
	if in.Cursor != "" {
		k, err := decodeCursor(in.Cursor)
		if err != nil {
			return q, apiError(http.StatusBadRequest, codeInvalidParameter, "cursor: not a cursor returned by this API")
		}
		q.After = &k
	}
	return q, nil
}

// listEvents is Urd: a page of events of the vehicle.
func listEvents[E, J any](ctx context.Context, s *Server, in *listInput,
	list func(accountID string, v Vehicle, q EventQuery) ([]E, error), key func(E) EventKey,
	conv func([]E, AccountLimits) []J,
) (*body[page[J]], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	q, err := eventQuery(in)
	if err != nil {
		return nil, err
	}
	// A period before the history the account sees gets the events from its start on:
	// never an error, the session's limits tell why.
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	if l.HistoryFrom.After(q.From) {
		q.From = l.HistoryFrom
		if !q.To.IsZero() && !q.From.Before(q.To) {
			return respond(page[J]{Items: []J{}}), nil
		}
	}
	want := q.Limit
	q.Limit++ // one more tells whether there is a next page
	events, err := list(sessionFrom(ctx).AccountID, v, q)
	if err != nil {
		return nil, s.internal("events not read", err)
	}
	p := page[J]{Items: []J{}}
	if len(events) > want {
		events = events[:want]
		next := encodeCursor(key(events[want-1]))
		p.NextCursor = &next
	}
	p.Items = append(p.Items, conv(events, l)...)
	return respond(p), nil
}

// each converts the events one by one.
func each[E, J any](conv func(E) J) func([]E) []J {
	return func(events []E) []J {
		out := make([]J, len(events))
		for i, e := range events {
			out[i] = conv(e)
		}
		return out
	}
}

// tripPositions are the starts and ends of the trips.
func tripPositions(trips ...core.Trip) []core.Value[core.Position] {
	out := make([]core.Value[core.Position], 0, 2*len(trips))
	for _, t := range trips {
		out = append(out, t.From, t.To)
	}
	return out
}

func (s *Server) listTrips(ctx context.Context, in *listInput) (*body[page[tripJSON]], error) {
	var places []core.Place
	return listEvents(ctx, s, in, func(acc string, v Vehicle, q EventQuery) ([]core.Trip, error) {
		var err error
		if places, err = s.Settings.Places(ctx, acc); err != nil {
			return nil, err //nolint:wrapcheck // logged by listEvents, never shown
		}
		return s.Reader.ListTrips(ctx, acc, v.ID, q)
	}, func(t core.Trip) EventKey { return EventKey{t.Start.After, t.DetectedAt} }, func(trips []core.Trip, _ AccountLimits) []tripJSON {
		book := s.addresses(ctx, places, tripPositions(trips...)...)
		return each(func(t core.Trip) tripJSON { return tripOf(t, places, book) })(trips)
	})
}

func (s *Server) listCharges(ctx context.Context, in *listInput) (*body[page[chargeJSON]], error) {
	var priced PricedCharges
	var charger core.Value[float64]
	return listEvents(ctx, s, in, func(acc string, v Vehicle, q EventQuery) ([]core.Charge, error) {
		var err error
		if charger, err = s.chargerOf(ctx, v); err != nil {
			return nil, err
		}
		priced, err = s.Reader.ListCharges(ctx, acc, v.ID, q)
		return priced.Charges, err //nolint:wrapcheck // logged by listEvents, never shown
	}, func(c core.Charge) EventKey { return EventKey{c.Start.After, c.DetectedAt} }, func(page []core.Charge, l AccountLimits) []chargeJSON {
		// The costs of the whole read: its extra charge, left out of the page, is one of
		// the vehicle's like the others.
		return s.chargesOf(ctx, priced, charger, l)[:len(page)]
	})
}

// eventInput is an event of a vehicle.
type eventInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	ID      string `path:"id" doc:"The event ID. Any RFC 3339 form of the detection time is accepted too, such as 2026-09-28T07:01:00Z." example:"2026-09-28T07:01:00.000000Z"`
}

func (s *Server) getTrip(ctx context.Context, in *eventInput) (*body[tripJSON], error) {
	var places []core.Place
	return getEvent(ctx, s, in, "trip", func(acc, vehicle string, at time.Time) (core.Trip, bool, error) {
		var err error
		if places, err = s.Settings.Places(ctx, acc); err != nil {
			return core.Trip{}, false, err //nolint:wrapcheck // logged by getEvent, never shown
		}
		return s.Reader.FindTrip(ctx, acc, vehicle, at)
	}, func(t core.Trip) core.Bounds { return t.End }, func(t core.Trip) tripJSON { return tripOf(t, places, s.addresses(ctx, places, tripPositions(t)...)) })
}

func (s *Server) getCharge(ctx context.Context, in *eventInput) (*body[chargeJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	at, err := eventTime(in.ID, "charge")
	if err != nil {
		return nil, err
	}
	return s.chargeAt(ctx, v, at)
}

// eventTime reads the detection time of an event ID; a malformed one is not found.
func eventTime(id, kind string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, id)
	if err != nil {
		return time.Time{}, apiError(http.StatusNotFound, codeNotFound, "no such "+kind)
	}
	return at, nil
}

func getEvent[E, J any](ctx context.Context, s *Server, in *eventInput, kind string,
	find func(accountID, vehicleID string, at time.Time) (E, bool, error), end func(E) core.Bounds, conv func(E) J,
) (*body[J], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	at, err := eventTime(in.ID, kind)
	if err != nil {
		return nil, err
	}
	e, found, err := find(sessionFrom(ctx).AccountID, v.ID, at)
	switch {
	case err != nil:
		return nil, s.internal(kind+" not read", err)
	case !found:
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such "+kind)
	}
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	if l.hides(end(e)) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no such "+kind)
	}
	return respond(conv(e)), nil
}

// registerReads registers Verdandi and Urd.
func (s *Server) registerReads(api huma.API) {
	vehicleErrs := map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}
	listErrs := map[int]string{
		http.StatusBadRequest: errParameter, http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle,
		http.StatusInternalServerError: errInternal,
	}
	eventErrs := map[int]string{
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errEvent, http.StatusInternalServerError: errInternal,
	}
	const listDescription = "Newest first, by `start.after` then `detected_at`. Figures are `null` when the readings do not give them."

	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listVehicles", Method: http.MethodGet, Path: "/vehicles", Tags: []string{"vehicles"},
		Summary: "The account's vehicles",
	}, "Every vehicle of the account, in no particular order.",
		map[int]string{http.StatusUnauthorized: errUnauthorized, http.StatusInternalServerError: errInternal}), s.listVehicles)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicle", Method: http.MethodGet, Path: "/vehicles/{vehicle}", Tags: []string{"vehicles"},
		Summary: "A vehicle",
	}, "The vehicle.", vehicleErrs), s.getVehicle)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getVehicleState", Method: http.MethodGet, Path: "/vehicles/{vehicle}/state", Tags: []string{"vehicles"},
		Summary: "The current state of a vehicle (Verdandi)",
		Description: "The latest known value of each field, computed from the latest recorded response of each endpoint. " +
			"Each is a reading, or `null` when it was never read or not reported. The values stay those of the latest " +
			"readings when the connection is lost: `checked_at` tells how fresh they are. There is no \"mode\": parked, " +
			"driving or charging follow from `engine` and `charging.status`.",
	}, "The state.", vehicleErrs), s.getState)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listTrips", Method: http.MethodGet, Path: "/vehicles/{vehicle}/trips", Tags: []string{"events"},
		Summary: "The trips of a vehicle (Urd)", Description: listDescription,
	}, "A page of trips, newest first.", listErrs), s.listTrips)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getTrip", Method: http.MethodGet, Path: "/vehicles/{vehicle}/trips/{id}", Tags: []string{"events"},
		Summary: "A trip", Description: "The same object as the item of the list.",
	}, "The trip.", eventErrs), s.getTrip)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listCharges", Method: http.MethodGet, Path: "/vehicles/{vehicle}/charges", Tags: []string{"events"},
		Summary: "The charges of a vehicle (Urd)", Description: listDescription,
	}, "A page of charges, newest first.", listErrs), s.listCharges)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getCharge", Method: http.MethodGet, Path: "/vehicles/{vehicle}/charges/{id}", Tags: []string{"events"},
		Summary: "A charge", Description: "The same object as the item of the list.",
	}, "The charge.", eventErrs), s.getCharge)
}

// A cursor is opaque to clients: the position of the last event of a page, in
// microseconds since the epoch.
func encodeCursor(k EventKey) string {
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%d.%d", k.StartedAfter.UnixMicro(), k.DetectedAt.UnixMicro()))
}

func decodeCursor(s string) (EventKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return EventKey{}, fmt.Errorf("cursor: %w", err)
	}
	a, b, found := strings.Cut(string(raw), ".")
	started, err1 := strconv.ParseInt(a, 10, 64)
	detected, err2 := strconv.ParseInt(b, 10, 64)
	if !found || err1 != nil || err2 != nil {
		return EventKey{}, fmt.Errorf("cursor %q: malformed", raw)
	}
	return EventKey{StartedAfter: time.UnixMicro(started).UTC(), DetectedAt: time.UnixMicro(detected).UTC()}, nil
}

func opt[T, U any](v core.Value[T], conv func(T) U) *U {
	if !v.OK {
		return nil
	}
	u := conv(v.V)
	return &u
}

func nullOf[T, U any](v core.Value[T], conv func(T) U) null[U] {
	if !v.OK {
		return null[U]{}
	}
	return known(conv(v.V))
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
