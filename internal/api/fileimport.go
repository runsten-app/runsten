package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// FileImports imports the files a user uploads, at once: the history another
// application or the manufacturer gave them (ADR 0030).
type FileImports interface {
	// ImportVolvoDataPortal imports the trips' file of the Volvo Data Portal and, when
	// not nil, its waypoints' file, which makes the trips' tracks. An error wrapping
	// ErrImportFile is a file that is not the portal's: its message tells why, and
	// nothing is written.
	ImportVolvoDataPortal(ctx context.Context, accountID, vehicleID string, trips, waypoints io.Reader) (FileImport, error)
}

// ErrImportFile is a file that is not the one expected.
var ErrImportFile = errors.New("not the file expected")

// FileImport is what an import of files did.
type FileImport struct {
	Imported, Skipped, Invalid int
	Cutoff                     time.Time
}

// maxImportFiles bounds the files of an import, together: a year of the portal's
// waypoints is a few megabytes.
const maxImportFiles = 32 << 20

type fileImportJSON struct {
	Source   string    `json:"source" doc:"The source the events were imported from." example:"volvo_data_portal"`
	Imported int       `json:"imported" minimum:"0" doc:"Events written, new or replacing those of an earlier import of the same source."`
	Skipped  int       `json:"skipped" minimum:"0" doc:"Events left because they start at or after the cutoff."`
	Invalid  int       `json:"invalid" minimum:"0" doc:"Rows left because they are unreadable or out of bounds (a trip that ends before it starts, a position out of range)."`
	Cutoff   time.Time `json:"cutoff" pattern:"Z$" doc:"The vehicle's first reading, or the time of the import when it has none: only what starts before it is imported."`
}

type volvoDataPortalFiles struct {
	Trips     huma.FormFile `form:"trips" required:"true" doc:"The trips' file the portal gave (CSV): Start time, End time, positions, odometers, distance, energy."`
	Waypoints huma.FormFile `form:"waypoints" required:"false" doc:"The waypoints' file the portal gave (CSV), about one a minute while driving: they make the trips' tracks. Optional."`
}

type volvoDataPortalInput struct {
	Vehicle string `path:"vehicle" doc:"The vehicle ID."`
	RawBody huma.MultipartFormFiles[volvoDataPortalFiles]
}

// requireMultipart refuses a body that is not a multipart form, and bounds it: huma
// applies no MaxBodyBytes to a multipart body.
func (s *Server) requireMultipart(ctx huma.Context, next func(huma.Context)) {
	r, w := unwrap(ctx)
	if mt, _, err := mime.ParseMediaType(ctx.Header("Content-Type")); err != nil || mt != "multipart/form-data" {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType, "multipart/form-data expected")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportFiles)
	next(ctx)
}

// volvoDataPortalPath is the route of the import of the portal's files.
const volvoDataPortalPath = "/vehicles/{vehicle}/imports/volvo_data_portal"

func (s *Server) importVolvoDataPortal(ctx context.Context, in *volvoDataPortalInput) (*body[fileImportJSON], error) {
	files := in.RawBody.Data()
	defer func() {
		for _, f := range []huma.FormFile{files.Trips, files.Waypoints} {
			if f.IsSet {
				_ = f.Close()
			}
		}
	}()
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	// The portal's files are a Volvo's history: imported into a Polestar or a Tesla, they
	// would be another car's.
	if v.Provider != string(providerVolvo) && v.Provider != "" {
		return nil, apiError(http.StatusConflict, codeNotVolvo, "the Volvo Data Portal's files are a Volvo's: this vehicle is not a Volvo")
	}
	// The form is strict, as every body: huma decodes the known fields only.
	for name := range in.RawBody.Form.File {
		if name != "trips" && name != "waypoints" {
			return nil, apiError(http.StatusBadRequest, codeInvalidBody, "unknown field "+strconv.Quote(name))
		}
	}
	for name := range in.RawBody.Form.Value {
		return nil, apiError(http.StatusBadRequest, codeInvalidBody, "unknown field "+strconv.Quote(name))
	}
	var waypoints io.Reader
	if files.Waypoints.IsSet {
		waypoints = files.Waypoints
	}
	account := sessionFrom(ctx).AccountID
	res, err := s.FileImports.ImportVolvoDataPortal(ctx, account, v.ID, files.Trips, waypoints)
	switch {
	case errors.Is(err, ErrImportFile):
		return nil, apiError(http.StatusBadRequest, codeInvalidFile, err.Error())
	case err != nil:
		return nil, s.internal("import not written", err)
	}
	s.Log.Info("files imported", "account", account, "vehicle", v.ID, "source", "volvo_data_portal",
		"imported", res.Imported, "skipped", res.Skipped, "invalid", res.Invalid)
	return respond(fileImportJSON{
		Source: "volvo_data_portal", Imported: res.Imported, Skipped: res.Skipped, Invalid: res.Invalid, Cutoff: res.Cutoff.UTC(),
	}), nil
}

// registerFileImports registers the imports of files.
func (s *Server) registerFileImports(api huma.API) {
	huma.Register(api, s.sessionOperation(huma.Operation{
		OperationID: "importVolvoDataPortal", Method: http.MethodPost, Path: volvoDataPortalPath,
		Tags: []string{"events"}, Summary: "Import the files of the Volvo Data Portal",
		Description: "The trips the Volvo Data Portal (data.volvocars.com) gave an owner who asked for their " +
			"vehicle's data under the EU Data Act, with their tracks when the waypoints' file is given. Written at " +
			"once: only what starts before the vehicle's first reading; a trip imported before, the same start, is " +
			"replaced. At most 32 MiB of files.",
		Middlewares: huma.Middlewares{s.requireMultipart},
	}, "What was imported.", writeErrors(map[int]string{
		http.StatusBadRequest: "`invalid_body`: not a multipart form with the trips' file, or more than 32 MiB. " +
			"`invalid_file`: a file is not the portal's (not CSV, or a column missing); the message tells which and why.",
		http.StatusForbidden:            errCrossOrigin + " " + errSessionOnly,
		http.StatusNotFound:             errVehicle,
		http.StatusConflict:             "`not_volvo`: the vehicle is not a Volvo (a Polestar, a Tesla); nothing is imported.",
		http.StatusUnsupportedMediaType: "`unsupported_media_type`: the body is not `multipart/form-data`.",
	})), s.importVolvoDataPortal)
	// huma leaves a form open to other fields; the handler refuses them.
	form := api.OpenAPI().Paths[volvoDataPortalPath].Post.RequestBody.Content["multipart/form-data"].Schema
	form.AdditionalProperties = false
}
