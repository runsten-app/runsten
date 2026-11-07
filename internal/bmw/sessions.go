package bmw

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"runsten/internal/core"
)

// SessionKinds returns the kinds of response NormalizeSessions reads.
func (Normalizer) SessionKinds() []core.Kind { return kinds(ChargingHistory) }

// NormalizeSessions reads a stored response of kind k into charging sessions.
func (Normalizer) NormalizeSessions(k core.Kind, raw []byte) ([]core.ChargeSession, error) {
	if k.Source != Source {
		return nil, fmt.Errorf("normalize sessions: source %q is not %s", k.Source, Source)
	}
	if Endpoint(k.Endpoint) != ChargingHistory {
		return nil, nil
	}
	return NormalizeSessions(raw)
}

// chargingHistory is what the normalization reads of a chargingHistory response. Its
// numbers are read as JSON numbers or strings: CarData's other responses give strings.
type chargingHistory struct {
	Data []struct {
		StartTime    json.RawMessage `json:"startTime"`
		EndTime      json.RawMessage `json:"endTime"`
		StartSoC     json.RawMessage `json:"displayedStartSoc"`
		EndSoC       json.RawMessage `json:"displayedSoc"`
		GridKWh      json.RawMessage `json:"energyConsumedFromPowerGridKwh"`
		Mileage      json.RawMessage `json:"mileage"`
		MileageUnits string          `json:"mileageUnits"`
		Location     struct {
			Lat json.RawMessage `json:"mapMatchedLatitude"`
			Lon json.RawMessage `json:"mapMatchedLongitude"`
		} `json:"chargingLocation"`
	} `json:"data"`
}

// kmPerMile converts a mileage in miles.
const kmPerMile = 1.609344

// NormalizeSessions reads the sessions of a chargingHistory response, in its order:
// their start and end, the energy drawn from the grid, the state of charge the car
// displayed at both ends, where it charged and its odometer. A session without a start,
// or ending before it starts, is left out. Assumed until a real car's response is seen
// (ADR 0045 §9): the times are Unix seconds (milliseconds read too), the mileage in the
// unit mileageUnits names, kilometres when it names none.
func NormalizeSessions(raw []byte) ([]core.ChargeSession, error) {
	var h chargingHistory
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("normalize %s: %w", ChargingHistory, err)
	}
	out := make([]core.ChargeSession, 0, len(h.Data))
	for _, d := range h.Data {
		start, ok1 := epoch(d.StartTime)
		end, ok2 := epoch(d.EndTime)
		if !ok1 || !ok2 || end.Before(start) {
			continue
		}
		s := core.ChargeSession{
			Start: start, End: end,
			EnergyGridKWh: nonNegative(d.GridKWh),
			StartSoC:      percent(d.StartSoC), EndSoC: percent(d.EndSoC),
		}
		if m := nonNegative(d.Mileage); m.OK {
			if miles(d.MileageUnits) {
				m.V *= kmPerMile
			}
			s.OdometerKm = m
		}
		lat, okLat := number(d.Location.Lat)
		lon, okLon := number(d.Location.Lon)
		if okLat && okLon && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 && (lat != 0 || lon != 0) {
			s.Position = core.Value[core.Position]{V: core.Position{Lat: lat, Lon: lon}, OK: true}
		}
		out = append(out, s)
	}
	return out, nil
}

// number reads a finite JSON number, or a string holding one.
func number(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var str string
	if json.Unmarshal(raw, &str) != nil {
		str = string(raw)
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(str), 64)
	return n, err == nil && !math.IsInf(n, 0) && !math.IsNaN(n)
}

// miles tells whether mileageUnits, MileageUnits.KM or MileageUnits.MILES (assumed),
// names miles.
func miles(units string) bool {
	u := strings.ToUpper(units)
	if i := strings.LastIndexByte(u, '.'); i >= 0 {
		u = u[i+1:]
	}
	return strings.HasPrefix(u, "MI")
}

func nonNegative(raw json.RawMessage) core.Value[float64] {
	if n, ok := number(raw); ok && n >= 0 {
		return core.Value[float64]{V: n, OK: true}
	}
	return core.Value[float64]{}
}

func percent(raw json.RawMessage) core.Value[float64] {
	if n, ok := number(raw); ok && n >= 0 && n <= 100 {
		return core.Value[float64]{V: n, OK: true}
	}
	return core.Value[float64]{}
}

// epoch reads a Unix time in seconds, or in milliseconds for a value beyond the year
// 33658 in seconds.
func epoch(raw json.RawMessage) (time.Time, bool) {
	n, ok := number(raw)
	if !ok || n <= 0 {
		return time.Time{}, false
	}
	if n >= 1e12 {
		return time.UnixMilli(int64(n)).UTC(), true
	}
	return time.Unix(int64(n), 0).UTC(), true
}
