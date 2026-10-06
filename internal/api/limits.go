package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"runsten/internal/core"
)

// accountLimitsJSON is what the account's limits hide, for the interface to tell
// rather than show lists cut short without a reason.
type accountLimitsJSON struct {
	HistoryFrom    *time.Time    `json:"history_from" pattern:"Z$" doc:"The trips and charges that ended before it are neither listed nor counted in the statistics: they are kept, and shown again once the limit is lifted. null: none is hidden."`
	Unavailable    []featureJSON `json:"unavailable" nullable:"false" doc:"The functions the account does not have. costs: the charges' costs, null in the charges and the statistics, and their entry refused (feature_unavailable). stats: the statistics split into intervals, refused (feature_unavailable); a period's totals stay. csv: the trips and charges as CSV files, refused (feature_unavailable); the account's full export stays. mqtt: an MQTT broker, whose setting is refused (feature_unavailable); one set before stays, and nothing is published to it."`
	UnreadVehicles []string      `json:"unread_vehicles" nullable:"false" format:"uuid" doc:"The account's vehicles the collector does not read: what was read of them stays, and is shown, but nothing new is, until the limit is lifted."`
}

// featureJSON is a function the limits may leave out.
type featureJSON string

const (
	featureCosts featureJSON = "costs"
	featureStats featureJSON = "stats"
	featureCSV   featureJSON = "csv"
	featureMQTT  featureJSON = "mqtt"
)

// Schema lists the functions.
func (featureJSON) Schema(huma.Registry) *huma.Schema {
	return enumSchema(featureCosts, featureStats, featureCSV, featureMQTT)
}

// limitsOf returns the account's limits as of now; none without an extension.
func (s *Server) limitsOf(ctx context.Context, accountID string) (AccountLimits, error) {
	if s.Limits == nil {
		return AccountLimits{}, nil
	}
	l, err := s.Limits.Limits(ctx, accountID, s.Clock.Now())
	if err != nil {
		return AccountLimits{}, s.internal("limits not read", err)
	}
	if !l.HistoryFrom.IsZero() {
		l.HistoryFrom = l.HistoryFrom.UTC()
	}
	return l, nil
}

// limitsOfAccount is null without a limit.
func limitsOfAccount(l AccountLimits) null[accountLimitsJSON] {
	if l.none() {
		return null[accountLimitsJSON]{}
	}
	j := accountLimitsJSON{HistoryFrom: timeOrNil(l.HistoryFrom), Unavailable: []featureJSON{}, UnreadVehicles: []string{}}
	j.UnreadVehicles = append(j.UnreadVehicles, l.UnreadVehicles...)
	if l.NoCosts {
		j.Unavailable = append(j.Unavailable, featureCosts)
	}
	if l.NoStats {
		j.Unavailable = append(j.Unavailable, featureStats)
	}
	if l.NoCSV {
		j.Unavailable = append(j.Unavailable, featureCSV)
	}
	if l.NoMQTT {
		j.Unavailable = append(j.Unavailable, featureMQTT)
	}
	return known(j)
}

// none reports whether the limits limit nothing.
func (l AccountLimits) none() bool {
	return l.HistoryFrom.IsZero() && !l.NoCosts && !l.NoStats && !l.NoCSV && !l.NoMQTT && len(l.UnreadVehicles) == 0
}

// errFeature is the refusal of a function the account's limits leave out.
const errFeature = "`feature_unavailable`: the account's offer does not include this function (`limits.unavailable` of the session)."

// requireCosts refuses the operations of the costs to an account without them.
func (s *Server) requireCosts(ctx huma.Context, next func(huma.Context)) {
	s.requireFeature(ctx, next, func(l AccountLimits) bool { return l.NoCosts }, "the costs")
}

// requireCSV refuses the CSV files to an account without them.
func (s *Server) requireCSV(ctx huma.Context, next func(huma.Context)) {
	s.requireFeature(ctx, next, func(l AccountLimits) bool { return l.NoCSV }, "the CSV files")
}

// requireMQTT refuses the setting of a broker to an account without MQTT.
func (s *Server) requireMQTT(ctx huma.Context, next func(huma.Context)) {
	s.requireFeature(ctx, next, func(l AccountLimits) bool { return l.NoMQTT }, "MQTT")
}

// requireFeature runs next unless the account's limits lack the function.
func (s *Server) requireFeature(ctx huma.Context, next func(huma.Context), lacks func(AccountLimits) bool, what string) {
	l, err := s.limitsOf(ctx.Context(), sessionFrom(ctx.Context()).AccountID)
	_, w := unwrap(ctx)
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error, see the logs")
	case lacks(l):
		writeError(w, http.StatusForbidden, codeFeatureUnavailable, "the account's offer does not include "+what)
	default:
		next(ctx)
	}
}

// hides reports whether an event that ended within end is before the history the
// account sees, as the lists leave it out: a link to it, kept from before the limit,
// finds nothing.
func (l AccountLimits) hides(end core.Bounds) bool {
	return !l.HistoryFrom.IsZero() && !end.Before.After(l.HistoryFrom)
}
