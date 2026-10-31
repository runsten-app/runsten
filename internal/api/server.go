// Package api is the HTTP interface of runsten-api:
//
//   - a few server-rendered pages, without JavaScript: sign in (/login), sign out, a
//     home page, and the Volvo ID connection (/auth/volvo/start and its callback);
//   - the JSON API under /api/v1, built with huma (huma.go): the session, the vehicles,
//     their current state, their trips and charges with their costs, the account's
//     settings, the costs entered for charges, the user's access tokens, and the
//     account's MQTT broker (mqtt.go). Every route but the login requires a session, or
//     an access token to read (tokens.go).
//
// A session is a server-side record whose random token is kept in an HttpOnly cookie.
// The account comes from the session only, never from the request: the store then
// restricts every read to it. Cross-site requests that change state are refused by
// http.CrossOriginProtection (Fetch metadata and Origin headers), on top of
// SameSite=Lax cookies.
//
// The package knows neither the database nor the vendor: it declares its interfaces,
// and the binary wires them.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"runsten/internal/auth"
	"runsten/internal/catalog"
	"runsten/internal/core"
	"runsten/internal/flags"
	"runsten/internal/oauth"
	"runsten/internal/platform/clock"
)

// Sessions logs users in and authenticates their sessions (auth.Service).
type Sessions interface {
	Login(ctx context.Context, username, password, client string) (string, auth.Session, error)
	Authenticate(ctx context.Context, token string) (auth.Session, error)
	Logout(ctx context.Context, s auth.Session) error
}

// Reader reads the account's vehicles and their events. Every method is restricted to
// the account: another account's vehicle is not found.
type Reader interface {
	Vehicles(ctx context.Context, accountID string) ([]Vehicle, error)
	Vehicle(ctx context.Context, accountID, vehicleID string) (Vehicle, bool, error)
	ListTrips(ctx context.Context, accountID, vehicleID string, q EventQuery) ([]core.Trip, error)
	FindTrip(ctx context.Context, accountID, vehicleID string, ref EventRef) (core.Trip, bool, error)
	// TripTrack returns the track of an imported trip, when its source gave one; a
	// derived trip has none.
	TripTrack(ctx context.Context, accountID, vehicleID string, ref EventRef) (Track, bool, error)
	// ListCharges and FindCharge read the charges with what their costs depend on.
	ListCharges(ctx context.Context, accountID, vehicleID string, q EventQuery) (PricedCharges, error)
	FindCharge(ctx context.Context, accountID, vehicleID string, ref EventRef) (PricedCharges, bool, error)
	// PeriodEvents returns the events that started in [from, to), and the latest one that
	// started before from, with what the costs of the charges depend on.
	PeriodEvents(ctx context.Context, accountID, vehicleID string, from, to time.Time) (Period, error)
	// ChargeHistory returns every charge of the account, by vehicle, each vehicle's with
	// all its entered costs (Deciding empty), from one snapshot.
	ChargeHistory(ctx context.Context, accountID string) ([]PricedCharges, error)
	// ListSoftwareVersions returns the versions of the vehicle's software, newest first,
	// that may have been installed within the query's period.
	ListSoftwareVersions(ctx context.Context, accountID, vehicleID string, q EventQuery) ([]core.SoftwareVersion, error)
}

// States returns the latest known values of a vehicle, as of at.
type States interface {
	Current(ctx context.Context, accountID, vehicleID string, at time.Time) (core.Current, error)
}

// Limits limits what an account reads: an extension's, such as the hosted offer's free
// plan. Without one, every account reads all it has.
type Limits interface {
	// Limits returns the account's limits as of at.
	Limits(ctx context.Context, accountID string, at time.Time) (AccountLimits, error)
}

// Flags tell the feature flags (ADR 0039): flags.All declares them, with their
// defaults. Without them, each flag has its default.
type Flags interface {
	// Enabled is the flag's value for the account.
	Enabled(ctx context.Context, name flags.Name, accountID string) bool
}

// AccountLimits are the limits of an account's reads; zero: none. They never delete
// anything: what they hide comes back once they are lifted.
type AccountLimits struct {
	// HistoryFrom hides the trips and charges that ended before it; zero: none is.
	HistoryFrom time.Time
	// NoCosts: the charges' costs are neither shown nor entered; the tariffs of the
	// places stay, and apply again once it is lifted.
	NoCosts bool
	// NoStats: the statistics give a period's totals only, not split into intervals.
	NoStats bool
	// NoCSV: the trips and charges are not given as CSV files; the account's export
	// stays, whatever its offer.
	NoCSV bool
	// NoMQTT: no MQTT broker is set; one set before stays, and is published to again
	// once it is lifted.
	NoMQTT bool
	// UnreadVehicles are the account's vehicles the collector does not read, by ID:
	// what was read of them stays, and is shown.
	UnreadVehicles []string
}

// Vehicle is a vehicle of the account, with the state of its provider connection.
type Vehicle struct {
	ID  string
	VIN string
	// Provider is the connection's: volvo (a Volvo ID, the fields below up to
	// KeyRefused), polestar (a credential of the Data Portal), or tesla (a refresh
	// token of the Owner API).
	Provider     string
	AuthorizedAt time.Time // zero: unknown (a token pasted by hand)
	RefreshedAt  time.Time // issue of the current refresh token; zero: none
	// ReauthReason is set once the grant is lost: the user must connect again.
	ReauthReason string
	ReauthAt     time.Time
	// OwnKey: the connection reads with the account's own application key, rather
	// than the instance's; KeyRefused: Volvo refused it.
	OwnKey, KeyRefused bool
	// MarketRefusedAt is when the collector found that the vendor's API does not serve
	// the vehicle's market: it is no longer read. Zero: served.
	MarketRefusedAt time.Time
	// CredentialMissing: the Polestar or Tesla connection's credential was removed;
	// CredentialRefused: the provider refused it. Neither is read until another.
	CredentialMissing, CredentialRefused bool
	// UnsharedAt is when the collector found that the owner no longer shares the
	// vehicle with the credential: it is no longer read. Zero: shared.
	UnsharedAt time.Time
	// Collection is what the collector last wrote of the vehicle; nil: it never did.
	Collection *Collection
	// VariantID is the variant of the catalog the user chose; empty: none, the
	// recognition applies. ACMaxKW is the onboard charger the user stated. Both are
	// what was written: effectiveModel decides whether they still apply.
	VariantID string
	ACMaxKW   core.Value[float64]
}

// Collection is what the collector wrote of a vehicle after its latest pass: whether
// it runs, and why nothing is read, if so.
type Collection struct {
	PassedAt time.Time
	Mode     string    // parked, driving or charging
	ReadAt   time.Time // the latest successful call; zero: unknown
	// NextAt is the earliest call due; zero: none, the connection lost its grant.
	NextAt      time.Time
	PausedUntil time.Time            // zero: not paused
	Quota       map[string]time.Time // the vendor's APIs whose quota is exhausted, until when
	Failure     CollectionFailure    // the latest failed call; zero At: none known
}

// CollectionFailure is a failed call of the collector.
type CollectionFailure struct {
	At       time.Time
	Endpoint string // empty: no call made (no access token)
	Status   int    // HTTP status; 0: no response
	Kind     string
}

// EventQuery selects a page of events, newest first (by Start.After, then DetectedAt,
// then the import).
type EventQuery struct {
	// From and To keep the events that may have happened, at least partly, within
	// [From, To): Start.After < To and End.Before > From. Zero: unbounded.
	From, To time.Time
	After    *EventKey // resume after this event
	Limit    int       // zero: every event
	// Place keeps the charges of one place, the Limit of them read past the others;
	// the trips ignore it.
	Place core.ChargesAt
}

// EventKey is the position of an event in the newest-first order.
type EventKey struct {
	StartedAfter time.Time
	DetectedAt   time.Time
	// Import is an imported event's source and ID there, as source:id; empty for a
	// derived one. Two sources may start events at the same time.
	Import string
}

// EventRef names an event of a vehicle: a derived one by its detection time, an imported
// one by its source and its ID there.
type EventRef struct {
	DetectedAt       time.Time
	Source, SourceID string // empty for a derived event
}

// Track is the track of an imported trip: its points in order, each a longitude and a
// latitude, and the end of the trip, which the history limit applies to.
type Track struct {
	EndedAt time.Time
	Line    [][2]float64
}

// Config wires a Server.
type Config struct {
	Sessions Sessions
	// AccessTokens are the personal access tokens; nil: none is accepted.
	AccessTokens AccessTokens
	Reader       Reader
	States       States
	Readings     Readings
	Health       VehicleHealth
	// Charging gives the vehicles' charges in progress.
	Charging Charging
	Settings Settings
	Models   VehicleModels
	Deletion VehicleDeletion
	Costs    ChargeCosts
	// Imports are the vehicles' events imported from other applications; ImportJobs
	// the imports asked for, run by the import worker; Aximote lists the vehicles of an
	// Aximote account.
	Imports    Imports
	ImportJobs ImportJobs
	Aximote    AximoteVehicles
	// FileImports imports the files a user uploads.
	FileImports FileImports
	// Addresses name the positions outside the places; nil without a geocoder: no
	// address, and nothing asked of a third party.
	Addresses Addresses
	// Params are the derivation's: the statistics take their noise threshold of the
	// state of charge.
	Params core.Params
	// CostParams are the assumptions of the costs of the charges.
	CostParams core.CostParams
	// Limits are the accounts' limits; nil: none.
	Limits Limits
	// Flags are the feature flags; nil: each its default.
	Flags Flags
	// Admission may hold an account back from connecting a vehicle; nil: none does.
	Admission Admission
	// Accounts exports the accounts' data, and ExportTables are the tables an extension
	// adds to it; nil: none.
	Accounts     Accounts
	ExportTables []ExportTable
	// CapacityParams are the assumptions of the battery capacity estimates.
	CapacityParams core.CapacityParams
	// Catalog recognizes the vehicles' variants.
	Catalog *catalog.Catalog

	// Volvo ID connection.
	Authorizer Authorizer
	Flows      *oauth.Flows
	Enrollment oauth.Enrollment
	Vehicles   oauth.VehicleLister
	// The account's application key: Keys keeps it, Tokens gives the access token to
	// list the vehicles with a new one. InstanceKey: the instance has its own
	// (RUNSTEN_VOLVO_API_KEY), which reads the connections without theirs; without it,
	// an account must give its key before connecting a Volvo ID.
	Keys        Keys
	Tokens      Tokens
	InstanceKey bool
	// Polestar keeps the account's credential of the Polestar Data Portal, and
	// PolestarVehicles checks a new one by listing its vehicles.
	Polestar         PolestarCredentials
	PolestarVehicles PolestarVehicles
	// Tesla keeps the account's refresh token of Tesla's Owner API, and TeslaVehicles
	// checks a new one by listing its vehicles.
	Tesla         TeslaTokens
	TeslaVehicles TeslaVehicles
	// MaxVehicles caps the vehicles of an account, set by an extension; 0: no cap. A
	// Volvo ID, a key or a Polestar credential that gives access to more is refused,
	// nothing recorded.
	MaxVehicles int
	// InstanceKeyLast4 and ClientID tell the user which application of the developer
	// portal the instance uses: the last four characters of its key, and its client ID,
	// which is no secret (every authorization URL carries it). ClientID is empty on an
	// instance without a Volvo application: no Volvo ID connects, no key is taken.
	InstanceKeyLast4 string
	ClientID         string

	// Version is the release the instance runs, X.Y.Z, or dev for a build from the
	// sources. Releases tells whether a newer one is out; nil: the instance does not
	// check (RUNSTEN_UPDATE_CHECK).
	Version  string
	Releases Releases

	// Brokers keeps the account's MQTT broker. PublicBrokersOnly: the instance
	// publishes to brokers on the Internet only, over TLS (the hosted offer's rule).
	Brokers           Brokers
	PublicBrokersOnly bool

	Clock clock.Clock
	// SecureCookies marks the cookies Secure (and names the session cookie with the
	// __Host- prefix): set it when the instance is served over https.
	SecureCookies bool
	// AppURL is the web interface, ending with a slash, where the Volvo ID flow sends
	// the browser back: empty when it is on the host of the callback (runsten-web),
	// set when it is elsewhere (Vite in development).
	AppURL string
	Log    *slog.Logger
}

// Server serves runsten-api.
type Server struct {
	Config
	handler http.Handler
	api     huma.API // the JSON API, and its OpenAPI document
	// tokenLimiter bounds the rate of each access token's requests.
	tokenLimiter *limiter
	errorSchema  *huma.Schema // the body of the errors
}

// New creates the server.
func New(cfg Config) *Server {
	s := &Server{Config: cfg, tokenLimiter: newLimiter(tokenBurst, tokenRate)}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.page(s.home))
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginForm)
	mux.HandleFunc("POST /logout", s.logoutForm)
	mux.HandleFunc("GET /auth/volvo/start", s.page(s.start))
	mux.HandleFunc("GET /auth/volvo/callback", s.page(s.callback))
	// runsten-api alone, without the web interface: where the flow comes back to.
	mux.HandleFunc("GET /connection", s.page(s.connectionPage))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		render(w, http.StatusNotFound, view{Title: "Not found", Message: "There is nothing here."})
	})

	setErrorFormat()
	s.api = humago.NewWithPrefix(mux, apiPrefix, humaConfig())
	s.errorSchema = s.api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[errorJSON](), true, "Error")
	s.registerSession(s.api)
	s.registerReads(s.api)
	s.registerCSV(s.api)
	s.registerImports(s.api)
	s.registerImportJobs(s.api)
	s.registerFileImports(s.api)
	s.registerModel(s.api)
	s.registerDeletion(s.api)
	s.registerStats(s.api)
	s.registerBattery(s.api)
	s.registerSoftware(s.api)
	s.registerSeries(s.api)
	s.registerHealth(s.api)
	s.registerCharging(s.api)
	s.registerSettings(s.api)
	s.registerCosts(s.api)
	s.registerConnection(s.api)
	s.registerPolestar(s.api)
	s.registerTesla(s.api)
	s.registerAccount(s.api)
	s.registerTokens(s.api)
	s.registerMQTT(s.api)
	s.registerInstance(s.api)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if allowed := allowedMethods(mux, r); allowed != "" {
			w.Header().Set("Allow", allowed)
			writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method not allowed on this route")
			return
		}
		writeError(w, http.StatusNotFound, codeNotFound, "no such route")
	})

	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPI(r) {
			writeError(w, http.StatusForbidden, codeCrossOrigin, "cross-origin request refused")
			return
		}
		render(w, http.StatusForbidden, view{Title: "Request refused", Message: "This request came from another site."})
	}))
	s.handler = cop.Handler(mux)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store") // personal data, sessions
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer") // the callback URL carries the code
	s.handler.ServeHTTP(w, r)
}

func isAPI(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/") }

// allowedMethods lists the methods another route accepts on r's path.
func allowedMethods(mux *http.ServeMux, r *http.Request) string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := mux.Handler(probe); pattern != "/api/" {
			allowed = append(allowed, m)
		}
	}
	return strings.Join(allowed, ", ")
}
