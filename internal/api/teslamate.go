package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
	"runsten/internal/platform/httpserver"
)

// ErrNoMatchingCar is a TeslaMate backup without the vehicle's VIN: its message names the
// backup's.
var ErrNoMatchingCar = errors.New("no car of the backup has the vehicle's VIN")

const (
	// teslaMatePath is the route of the import of a TeslaMate backup.
	teslaMatePath = "/vehicles/{vehicle}/imports/teslamate"
	// maxTeslaMateBody bounds the bytes of a backup as sent: a few years of TeslaMate are
	// about a gigabyte plain, several times less gzipped.
	maxTeslaMateBody = 4 << 30
	// DefaultTeslaMateImports is how many backups an instance reads at once, unless
	// Config.TeslaMateImports says otherwise: each keeps its drives and their tracks in
	// memory until it is written: about 170 MB for 4.5 years of a car, up to about 800 MB
	// for the largest backup accepted (30,000 drives, their tracks both compact and as
	// GeoJSON while they are converted).
	DefaultTeslaMateImports = 2
	// teslaMateRetry is what a refusal for want of a slot asks to wait, in seconds.
	teslaMateRetry = 60
)

// teslaMateImports are the imports of TeslaMate backups running: by vehicle, at most max.
type teslaMateImports struct {
	mu       sync.Mutex
	vehicles []string
}

// start takes a slot for the vehicle: false and busy when the instance has none, false
// alone when the vehicle has one.
func (t *teslaMateImports) start(vehicle string, max int) (ok, busy bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case slices.Contains(t.vehicles, vehicle):
		return false, false
	case len(t.vehicles) >= max:
		return false, true
	}
	t.vehicles = append(t.vehicles, vehicle)
	return true, false
}

func (t *teslaMateImports) done(vehicle string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.vehicles = slices.DeleteFunc(t.vehicles, func(v string) bool { return v == vehicle })
}

type teslaMateInput struct {
	Vehicle  string `path:"vehicle" doc:"The vehicle ID."`
	Currency string `query:"currency" required:"false" pattern:"^[A-Z]{3}$" doc:"The ISO 4217 code of the costs TeslaMate recorded, which it does not tell: without it, the charges are imported without their cost." example:"EUR"`
}

// teslaMateBodyKey holds the request's body in the operation's context: huma would read a
// body whole, and a backup is read as it comes.
type teslaMateBodyKey struct{}

// teslaMateMediaTypes are the backup's: plain, or gzipped.
var teslaMateMediaTypes = []string{"application/octet-stream", "application/gzip"}

// teslaMateIdle is how long the sender of a backup may pause: the upload as a whole has
// no deadline, a gigabyte takes minutes, but a sender that stalls is cut off rather than
// holding one of the instance's few slots.
const teslaMateIdle = time.Minute

// teslaMateBody gives the handler the body as it comes, bounded, each read within
// teslaMateIdle of the previous one, and the response as long as the import takes. Only
// a session reaches it.
func (s *Server) teslaMateBody(ctx huma.Context, next func(huma.Context)) {
	r, w := unwrap(ctx)
	if mt, _, err := mime.ParseMediaType(ctx.Header("Content-Type")); err != nil || !slices.Contains(teslaMateMediaTypes, mt) {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType, "application/octet-stream or application/gzip expected")
		return
	}
	if err := httpserver.IdleReads(w, r, teslaMateIdle); err != nil {
		// The server's deadlines stand: a large backup is cut off by them.
		s.Log.Warn("the deadlines of a backup's upload are the server's", "err", err)
	}
	body := http.MaxBytesReader(w, r.Body, maxTeslaMateBody)
	next(huma.WithValue(ctx, teslaMateBodyKey{}, io.Reader(body)))
}

func (s *Server) importTeslaMate(ctx context.Context, in *teslaMateInput) (*body[fileImportJSON], error) {
	v, err := s.accountVehicle(ctx, in.Vehicle)
	if err != nil {
		return nil, err
	}
	// TeslaMate's history is a Tesla's: the VIN would tell another car's apart, but a
	// Volvo's is never in it.
	if v.Provider != string(providerTesla) {
		return nil, apiError(http.StatusConflict, codeNotTesla, "TeslaMate's backup is a Tesla's: this vehicle is not a Tesla")
	}
	if _, ok := core.CurrencyOf(in.Currency); in.Currency != "" && !ok {
		return nil, apiError(http.StatusBadRequest, codeInvalidParameter, "currency: not a currency Runsten accepts")
	}
	limit := s.TeslaMateImports
	if limit <= 0 {
		limit = DefaultTeslaMateImports
	}
	ok, busy := s.teslaMate.start(v.ID, limit)
	switch {
	case busy:
		return nil, huma.ErrorWithHeaders( //nolint:wrapcheck // the error of the contract, with its header
			apiError(http.StatusServiceUnavailable, codeSourceUnavailable, "the instance is reading other backups: try again in a minute"),
			http.Header{"Retry-After": {strconv.Itoa(teslaMateRetry)}})
	case !ok:
		return nil, apiError(http.StatusConflict, codeImportActive, "an import of this vehicle is already active")
	}
	defer s.teslaMate.done(v.ID)
	backup, _ := ctx.Value(teslaMateBodyKey{}).(io.Reader)
	account := sessionFrom(ctx).AccountID
	res, err := s.FileImports.ImportTeslaMate(ctx, account, v.ID, v.VIN, in.Currency, backup)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return nil, apiError(http.StatusBadRequest, codeInvalidBody, "the backup is larger than 4 GiB: gzip it")
	case errors.Is(err, ErrImportFile):
		return nil, apiError(http.StatusBadRequest, codeInvalidFile, err.Error())
	case errors.Is(err, ErrNoMatchingCar):
		return nil, apiError(http.StatusConflict, codeNoMatchingCar, err.Error())
	case err != nil:
		return nil, s.internal("import not written", err)
	}
	s.Log.Info("backup imported", "account", account, "vehicle", v.ID, "source", "teslamate",
		"imported", res.Imported, "skipped", res.Skipped, "invalid", res.Invalid)
	return respond(fileImportJSON{
		Source: "teslamate", Imported: res.Imported, Skipped: res.Skipped, Invalid: res.Invalid, Cutoff: res.Cutoff.UTC(),
	}), nil
}

// registerTeslaMate registers the import of a TeslaMate backup.
func (s *Server) registerTeslaMate(api huma.API) {
	content := map[string]*huma.MediaType{}
	for _, mt := range teslaMateMediaTypes {
		content[mt] = &huma.MediaType{Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}
	}
	op := s.sessionOperation(huma.Operation{
		OperationID: "importTeslaMate", Method: http.MethodPost, Path: teslaMatePath,
		Tags: []string{"events"}, Summary: "Import the history of a TeslaMate instance",
		Description: "The drives, with their tracks, and the charging processes of the car of the vehicle's VIN in " +
			"TeslaMate's backup: the plain file `pg_dump` writes (`docker compose exec -T database pg_dump -U " +
			"teslamate teslamate > teslamate.bck`), as the body, maybe gzipped. Read as it comes, nothing kept of " +
			"it: written at once at its end, only what starts before the vehicle's first reading; an event imported " +
			"before replaced. A charge keeps its ranges, usable level and measured energy, which the battery's " +
			"rated estimates rest on. At most 4 GiB as sent; one import per vehicle, and a few per instance, at once.",
		RequestBody: &huma.RequestBody{
			Required: true, Content: content,
			Description: "TeslaMate's backup, plain (`application/octet-stream`) or gzipped (`application/gzip`).",
		},
		Middlewares: huma.Middlewares{s.teslaMateBody},
	}, "What was imported.", writeErrors(map[int]string{
		http.StatusBadRequest: "`invalid_parameter`: `currency`. `invalid_body`: more than 4 GiB. `invalid_file`: " +
			"not TeslaMate's backup (a custom-format archive, a table or a column missing, a file cut short or corrupt), " +
			"or larger than an import reads (8 GiB decompressed, 30,000 drives or charges of the car); the message tells " +
			"why and how to make the backup.",
		http.StatusForbidden: errCrossOrigin + " " + errSessionOnly,
		http.StatusNotFound:  errVehicle,
		http.StatusConflict: "`not_tesla`: the vehicle is not a Tesla. `no_matching_car`: no car of the backup has the " +
			"vehicle's VIN; the message names theirs. `import_active`: a backup of the vehicle is being read. Nothing is " +
			"imported.",
		http.StatusUnsupportedMediaType: "`unsupported_media_type`: neither `application/octet-stream` nor " +
			"`application/gzip`.",
		http.StatusServiceUnavailable: "`source_unavailable`: the instance is reading as many backups as it reads at once.",
	}))
	one := 1.0
	op.Responses[strconv.Itoa(http.StatusServiceUnavailable)].Headers = map[string]*huma.Param{
		"Retry-After": {
			Description: "Seconds to wait before sending the backup again.", Required: true,
			Schema: &huma.Schema{Type: huma.TypeInteger, Minimum: &one},
		},
	}
	huma.Register(api, op, s.importTeslaMate)
}
