package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// Pictures relays the vendors' pictures of the vehicles, so that the browser calls no
// third party.
type Pictures interface {
	// Serves tells whether a picture is relayed from u.
	Serves(u string) bool
	// Picture fetches the picture at u, or gives the one kept.
	Picture(ctx context.Context, u string) (Picture, error)
}

// Picture is an image and its media type.
type Picture struct {
	Type string
	Data []byte
}

// pictureMaxAge is how long the browser keeps a picture: a vehicle's picture changes
// with its details, hardly ever.
const pictureMaxAge = "private, max-age=86400"

// Enumerations of the sheet, from the domain's values.
type (
	gearboxJSON  string
	driveJSON    string
	steeringJSON string
)

// Schema lists the gearboxes.
func (gearboxJSON) Schema(huma.Registry) *huma.Schema { return enumSchema(core.Automatic, core.Manual) }

// Schema lists the drives.
func (driveJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.FrontWheelDrive, core.RearWheelDrive, core.AllWheelDrive)
}

// Schema lists the sides of the steering wheel.
func (steeringJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(core.LeftHand, core.RightHand)
}

type sheetJSON struct {
	Name              *string            `json:"name" minLength:"1" doc:"The name the owner gave the vehicle in its vendor's app (Tesla); null for the others."`
	Colour            *string            `json:"colour" minLength:"1" doc:"The exterior's colour, as the vendor names it (Powder Blue, Deep Blue)."`
	RoofColour        *string            `json:"roof_colour" minLength:"1" doc:"The roof's, as the vendor names it (Tesla: Glass, Black)."`
	Wheels            *string            `json:"wheels" minLength:"1" doc:"The rims, as the vendor names them, a code (Tesla: HelixV220Dark)."`
	Upholstery        *string            `json:"upholstery" minLength:"1" doc:"The seats' upholstery, as the vendor names it, most often a code (Volvo: R310, CHARCOAL/LEAA/CHARCOAL)."`
	Gearbox           null[gearboxJSON]  `json:"gearbox"`
	Drive             null[driveJSON]    `json:"drive" doc:"The driven wheels: fwd, the front ones; rwd, the rear ones; awd, all four."`
	Steering          null[steeringJSON] `json:"steering" doc:"The side of the steering wheel."`
	PowerKW           *float64           `json:"power_kw" exclusiveMinimum:"0" doc:"The maximum power of the motors, or of the engine and motors together."`
	EngineCC          *int               `json:"engine_cc" minimum:"1" doc:"The combustion engine's displacement, in cm³; null for an electric vehicle."`
	GrossWeightKg     *int               `json:"gross_weight_kg" minimum:"1" doc:"The most the vehicle may weigh, loaded (gross vehicle weight)."`
	Seats             *int               `json:"seats" minimum:"1"`
	ThirdRow          bool               `json:"third_row" doc:"A third row of seats (Tesla); false: none, or not told."`
	Performance       bool               `json:"performance" doc:"A performance package (Tesla); false: none, or not told."`
	FirstRegistration *string            `json:"first_registration" format:"date" doc:"The day the vehicle was first registered."`
	Picture           bool               `json:"picture" doc:"Whether GET /vehicles/{vehicle}/picture gives a picture of the vehicle: its vendor links to one (Volvo)."`
}

// sheetOf gives the sheet a source told, nil when it told none. picture is whether its
// picture is relayed.
func sheetOf(sh core.Sheet, picture bool) sheetJSON {
	str := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	pos := func(n int) *int {
		if n <= 0 {
			return nil
		}
		return &n
	}
	out := sheetJSON{
		Name: str(sh.Name), Colour: str(sh.Colour), RoofColour: str(sh.RoofColour), Wheels: str(sh.Wheels),
		Upholstery: str(sh.Upholstery),
		EngineCC:   pos(sh.EngineCC), GrossWeightKg: pos(sh.GrossWeightKg), Seats: pos(sh.Seats),
		ThirdRow: sh.ThirdRow, Performance: sh.Performance, FirstRegistration: str(sh.FirstRegistration),
		Picture: picture,
	}
	if sh.Gearbox != "" {
		out.Gearbox = known(gearboxJSON(sh.Gearbox))
	}
	if sh.Drive != "" {
		out.Drive = known(driveJSON(sh.Drive))
	}
	if sh.Steering != "" {
		out.Steering = known(steeringJSON(sh.Steering))
	}
	if sh.PowerKW > 0 {
		out.PowerKW = &sh.PowerKW
	}
	return out
}

// sheetFor gives the vehicle's sheet from its latest details; null when its source tells
// none of it.
func (s *Server) sheetFor(n core.Snapshot) null[sheetJSON] {
	if !n.Sheet.OK {
		return null[sheetJSON]{}
	}
	return known(sheetOf(n.Sheet.V, s.servesPicture(n.Sheet.V.Picture)))
}

func (s *Server) servesPicture(u string) bool {
	return u != "" && s.Pictures != nil && s.Pictures.Serves(u)
}

type pictureOutput struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

func (s *Server) getPicture(ctx context.Context, in *vehicleInput) (*pictureOutput, error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	n, err := s.detailsOf(ctx, v)
	if err != nil {
		return nil, err
	}
	// The URL is the vehicle's latest details', never the request's.
	u := n.Sheet.V.Picture
	if !n.Sheet.OK || !s.servesPicture(u) {
		return nil, apiError(http.StatusNotFound, codeNotFound, "no picture of the vehicle")
	}
	p, err := s.Pictures.Picture(ctx, u)
	if err != nil {
		s.Log.WarnContext(ctx, "vehicle picture not fetched", "vehicle", v.ID, "error", err.Error())
		return nil, apiError(http.StatusBadGateway, codeSourceUnavailable, "the vendor did not give the picture, try again later")
	}
	return &pictureOutput{ContentType: p.Type, CacheControl: pictureMaxAge, Body: p.Data}, nil
}

// registerSheet registers the vehicle's picture; its sheet comes with the vehicle.
func (s *Server) registerSheet(api huma.API) {
	o := s.operation(huma.Operation{
		OperationID: "getVehiclePicture", Method: http.MethodGet, Path: "/vehicles/{vehicle}/picture", Tags: []string{"vehicles"},
		Summary: "A vehicle's picture",
		Description: "The vendor's picture of the vehicle from the outside, relayed by Runsten so that the browser calls no " +
			"third party: fetched from the vendor's CDN, at most 640 pixels wide, kept a day. Only for a vehicle whose " +
			"`sheet.picture` is true.",
	}, "The picture.", map[int]string{
		http.StatusUnauthorized:        errUnauthorized,
		http.StatusNotFound:            "`not_found`: no such vehicle in the account, or no picture of it.",
		http.StatusBadGateway:          "`source_unavailable`: the vendor's CDN did not give the picture.",
		http.StatusInternalServerError: errInternal,
	})
	o.Responses["200"].Content = map[string]*huma.MediaType{
		"image/png":  {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		"image/jpeg": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		"image/webp": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
	}
	huma.Register(api, o, s.getPicture)
	api.OpenAPI().Paths[o.Path].Get.Responses["200"].Headers = map[string]*huma.Param{
		"Cache-Control": {
			Description: "private, max-age=86400: the browser keeps it a day.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeString},
		},
	}
}
