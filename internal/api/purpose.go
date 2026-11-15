package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// TripMarks reads and writes the purposes the user marked the account's trips with. Every
// method is restricted to the account: another account's trip is not found.
type TripMarks interface {
	// TripMarks returns the marks of the vehicle's trips: marks[i] is trips[i]'s.
	TripMarks(ctx context.Context, accountID, vehicleID string, trips []core.Trip) ([]core.Value[core.TripMark], error)
	// SetTripMarks marks the vehicle's trips by hand, or, mark absent, deletes their marks:
	// the places' rules apply again. All or none: ErrNotFound if a ref names no trip.
	SetTripMarks(ctx context.Context, accountID, vehicleID string, refs []EventRef, mark core.Value[core.TripMark]) error
}

type purposeJSON string

// Schema lists the purposes.
func (purposeJSON) Schema(huma.Registry) *huma.Schema { return enumSchema(core.Purposes...) }

type purposeOriginJSON string

// Schema lists the origins of a purpose.
func (purposeOriginJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.PurposeByHand, core.PurposeByRule)
}

type purposeScopeJSON string

// Schema lists the scopes of a place's rule.
func (purposeScopeJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.ScopeEnd, core.ScopeEither)
}

// TripPurposeFields are a trip's purpose, in tripJSON.
type TripPurposeFields struct {
	Purpose       null[purposeJSON]       `json:"purpose" doc:"What the trip was made for: business, commute (the way between home and work, which each country counts its own way: as business in some, apart in others) or personal. Marked by hand, else given by the rule of the place the trip ends at, or starts at when that place's rule takes both ends; two rules that disagree give none. null without one."`
	PurposeOrigin null[purposeOriginJSON] `json:"purpose_origin" doc:"hand: marked by the user, which no rule overrides; purpose may then be null, the user said the trip has none. rule: given by purpose_place's rule. null when the trip has neither."`
	PurposePlace  null[placeRefJSON]      `json:"purpose_place" doc:"The place whose rule gave the purpose; null otherwise."`
}

func purposeFieldsOf(p core.Value[core.TripPurpose]) TripPurposeFields {
	if !p.OK {
		return TripPurposeFields{}
	}
	return TripPurposeFields{
		Purpose:       nullOf(p.V.Purpose, text[core.Purpose, purposeJSON]),
		PurposeOrigin: known(purposeOriginJSON(p.V.Origin)),
		PurposePlace:  nullOf(p.V.Place, placeRefOf),
	}
}

// tripPurposes gives the purpose of each of the vehicle's trips: its mark, else the
// rules of places.
func (s *Server) tripPurposes(ctx context.Context, vehicleID string, trips []core.Trip, places []core.Place) ([]core.Value[core.TripPurpose], error) {
	marks, err := s.Marks.TripMarks(ctx, sessionFrom(ctx).AccountID, vehicleID, trips)
	if err != nil {
		return nil, err //nolint:wrapcheck // logged by the caller, never shown
	}
	out := make([]core.Value[core.TripPurpose], len(trips))
	for i, t := range trips {
		out[i] = core.PurposeOf(t, marks[i], places)
	}
	return out, nil
}

// purposeFilterJSON selects the trips by purpose.
type purposeFilterJSON string

// none: the trips without a purpose.
const purposeNone = "none"

// Schema lists the purposes, and none.
func (purposeFilterJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(append([]string{}, string(core.PurposeBusiness), string(core.PurposeCommute), string(core.PurposePersonal), purposeNone)...)
}

// matches tells whether a trip of purpose p is kept; an empty filter keeps all.
func (f purposeFilterJSON) matches(p core.Value[core.TripPurpose]) bool {
	switch f {
	case "":
		return true
	case purposeNone:
		return !p.OK || !p.V.Purpose.OK
	}
	return p.OK && p.V.Purpose.OK && string(p.V.Purpose.V) == string(f)
}

// markJSON is what a write marks trips with: a purpose, none (by hand: no rule applies),
// or rule (the mark is deleted: the rules apply again).
type markJSON string

const markRule = "rule"

// Schema lists the marks.
func (markJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(string(core.PurposeBusiness), string(core.PurposeCommute), string(core.PurposePersonal), purposeNone, markRule)
}

type tripMarksBody struct {
	Trips   []string `json:"trips" minItems:"1" maxItems:"200" uniqueItems:"true" nullable:"false" doc:"The IDs of the vehicle's trips, derived or imported." example:"[\"2026-09-28T07:01:00.000000Z\"]"`
	Purpose markJSON `json:"purpose" doc:"business, commute or personal: the trips take it by hand. none: they have none, by hand, whatever the places' rules. rule: their marks are deleted, and the places' rules apply again."`
}

type tripMarksInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	Body    tripMarksBody
}

func (s *Server) putTripMarks(ctx context.Context, in *tripMarksInput) (*struct{}, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	acc := sessionFrom(ctx).AccountID
	l, err := s.limitsOf(ctx, acc)
	if err != nil {
		return nil, err
	}
	const notFound = "no such trip"
	refs := make([]EventRef, 0, len(in.Body.Trips))
	for _, id := range in.Body.Trips {
		ref, err := eventRef(id, "trip")
		if err != nil {
			return nil, err
		}
		// The trip as stored: its detection time in any RFC 3339 form, and the history
		// the account sees, which a trip it hides is not found in.
		t, ok, err := s.Reader.FindTrip(ctx, acc, v.ID, ref)
		switch {
		case err != nil:
			return nil, s.internal("trip not read", err)
		case !ok || l.hides(t.End):
			return nil, apiError(http.StatusNotFound, codeNotFound, notFound)
		}
		if !t.Import.OK {
			ref = EventRef{DetectedAt: t.DetectedAt}
		}
		refs = append(refs, ref)
	}
	var mark core.Value[core.TripMark]
	if in.Body.Purpose != markRule {
		mark.OK = true
		if in.Body.Purpose != purposeNone {
			mark.V.Purpose = core.Value[core.Purpose]{V: core.Purpose(in.Body.Purpose), OK: true}
		}
	}
	if err := s.Marks.SetTripMarks(ctx, acc, v.ID, refs, mark); err != nil {
		return nil, s.costError("trips not marked", notFound, err)
	}
	return &struct{}{}, nil
}

func (s *Server) registerMarks(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "putTripMarks", Method: http.MethodPut, Path: "/vehicles/{vehicle}/trip-marks", Tags: []string{"trips"},
		Summary: "Mark trips with their purpose",
		Description: "Marks one trip or several (a selection of the list) by hand: a mark by hand always wins over the " +
			"rules of the places (`trip_purpose` of a place). It is not derived data: a rebuild keeps it, and finds its " +
			"trip again by its detection time or else by its window. All or none: a trip not found writes nothing.",
		DefaultStatus: http.StatusNoContent, Middlewares: huma.Middlewares{s.requireJSON},
	}, "The trips are marked.", writeErrors(map[int]string{
		http.StatusNotFound: "`not_found`: no such vehicle in the account, or no such trip for it (or one the account's history hides).",
	})), s.putTripMarks)
}
