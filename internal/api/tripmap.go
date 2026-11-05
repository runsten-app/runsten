package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

type tripMapInput struct {
	Vehicle string    `path:"vehicle" doc:"The vehicle ID."`
	From    time.Time `query:"from" doc:"The start of the period [from, to). Without it, the period starts with the vehicle's first trip." example:"2026-09-01T00:00:00+02:00"`
	To      time.Time `query:"to" doc:"The end of the period, excluded; it must come after from. Without it, now." example:"2026-10-01T00:00:00+02:00"`
}

type tripMapJSON struct {
	From                 time.Time     `json:"from" pattern:"Z$" doc:"The start of the period: the requested one, or the start of the first trip. Equal to to when the vehicle has no trip."`
	To                   time.Time     `json:"to" pattern:"Z$"`
	Trips                int           `json:"trips" minimum:"0" doc:"The trips of the map: those that started in the period, both ends known."`
	TripsWithoutPosition int           `json:"trips_without_position" minimum:"0" doc:"The trips of the period left out, an end unknown (a reconstructed trip has none)."`
	Spots                []mapSpotJSON `json:"spots" nullable:"false" doc:"Where the trips started and ended, the most visited first: a place, the ends within its circle, or else the ends within 250 m of the first one it gathered, around the same address. The first 50 have their address."`
	Links                []mapLinkJSON `json:"links" nullable:"false" doc:"The trips between two spots, either way, the most driven first. A trip back to the spot it left is none."`
}

type mapSpotJSON struct {
	Position   positionJSON       `json:"position" doc:"The place's position, else the mean of the ends the spot gathers."`
	Place      null[placeRefJSON] `json:"place" doc:"The place, null outside every place."`
	Address    *string            `json:"address" doc:"Outside every place, the street and town of one of the ends the spot gathers, as start_address of a trip; null at a place, without a geocoder, and until it answered."`
	Departures int                `json:"departures" minimum:"0"`
	Arrivals   int                `json:"arrivals" minimum:"0"`
}

type mapLinkJSON struct {
	A          int     `json:"a" minimum:"0" doc:"The index of a spot in spots, before b."`
	B          int     `json:"b" minimum:"1"`
	Trips      int     `json:"trips" minimum:"1"`
	DistanceKm float64 `json:"distance_km" minimum:"0" doc:"The sum of the trips' known distances."`
	// DistanceUnknown as distance_unknown of the statistics' trips.
	DistanceUnknown int `json:"distance_unknown" minimum:"0" doc:"The trips whose distance is unknown, left out of distance_km."`
}

// maxSpotAddresses bounds the spots of a map whose addresses are read.
const maxSpotAddresses = 50

// getTripMap draws where a vehicle went: the ends of the trips that started in the
// period, gathered into spots, and the links between them.
func (s *Server) getTripMap(ctx context.Context, in *tripMapInput) (*body[tripMapJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	from, to := in.From.UTC(), in.To.UTC()
	if in.To.IsZero() {
		to = s.Clock.Now().UTC()
	}
	if !in.From.IsZero() && !from.Before(to) {
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "from must be before to")
	}
	// The history the account does not see is not drawn either.
	l, err := s.limitsOf(ctx, sessionFrom(ctx).AccountID)
	if err != nil {
		return nil, err
	}
	fromSet := !in.From.IsZero()
	if h := l.HistoryFrom; !h.IsZero() && (!fromSet || from.Before(h)) {
		from, fromSet = h, true
	}
	// A period wholly before the history stays empty, never reversed.
	if from.After(to) {
		from = to
	}
	var p Period
	if !fromSet || from.Before(to) {
		if p, err = s.Reader.PeriodEvents(ctx, sessionFrom(ctx).AccountID, v.ID, from, to); err != nil {
			return nil, s.internal("events not read", err)
		}
	}
	if !fromSet {
		from = firstStart(p.Trips, nil, to)
	}
	m := core.MapTrips(p.Trips, p.Places, from, to)
	out := tripMapJSON{
		From: from, To: to, Trips: m.Trips, TripsWithoutPosition: m.WithoutPosition,
		Spots: make([]mapSpotJSON, 0, len(m.Spots)), Links: make([]mapLinkJSON, 0, len(m.Links)),
	}
	// The addresses of the most visited spots only: the others are drawn, never named.
	firsts := make([]core.Value[core.Position], 0, min(len(m.Spots), maxSpotAddresses))
	for _, sp := range m.Spots[:min(len(m.Spots), maxSpotAddresses)] {
		firsts = append(firsts, core.Value[core.Position]{V: sp.First, OK: !sp.Place.OK})
	}
	book := s.addresses(ctx, p.Places, firsts...)
	for i, sp := range m.Spots {
		var address *string
		if i < len(firsts) {
			address = book.of(firsts[i])
		}
		out.Spots = append(out.Spots, mapSpotJSON{
			Position: positionOf(sp.Position), Place: nullOf(sp.Place, placeRefOf), Address: address,
			Departures: sp.Departures, Arrivals: sp.Arrivals,
		})
	}
	for _, lk := range m.Links {
		out.Links = append(out.Links, mapLinkJSON{
			A: lk.A, B: lk.B, Trips: lk.Trips, DistanceKm: lk.DistanceKm, DistanceUnknown: lk.DistanceUnknown,
		})
	}
	return respond(out), nil
}

// registerTripMap registers the map of a vehicle's trips.
func (s *Server) registerTripMap(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getTripMap", Method: http.MethodGet, Path: "/vehicles/{vehicle}/trip-map", Tags: []string{"events"},
		Summary: "Where a vehicle went over a period",
		Description: "The ends of the trips that started in the period, gathered into spots, and the links between them: " +
			"the same journey, driven day after day either way, is one link that counts its trips. A trip read by Runsten " +
			"has its two ends only, never its path: a link is a straight line between two spots.",
	}, "The map.", map[int]string{
		http.StatusBadRequest:   "`invalid_parameter`: `from` or `to`; the message names the parameter.",
		http.StatusUnauthorized: errUnauthorized, http.StatusNotFound: errVehicle, http.StatusInternalServerError: errInternal,
	}), s.getTripMap)
}
