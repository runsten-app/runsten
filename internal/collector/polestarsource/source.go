// Package polestarsource reads the Polestar Data Portal for the collector: it adapts the
// client of internal/polestar to collector.Source, which knows neither its transport nor
// its formats.
package polestarsource

import (
	"context"
	"errors"
	"time"

	"runsten/internal/collector"
	"runsten/internal/core"
	"runsten/internal/polestar"
)

// Source is the Data Portal as a collector.Source.
type Source struct {
	client     *polestar.Client
	credential func(ctx context.Context, accountID, connectionID string) (polestar.Credential, error)
}

// New returns the Source reading through client, with the credential of each connection
// read by credential (the store's).
func New(client *polestar.Client, credential func(ctx context.Context, accountID, connectionID string) (polestar.Credential, error)) Source {
	return Source{client: client, credential: credential}
}

// Daily is the Data Portal's quota: 10,000 calls a day per credential, over every domain
// (according to the community, not documented).
const Daily = 10_000

// Spec implements collector.Source. The paces are set for a credential's 10,000 calls a
// day: parked, the battery and the availability every parked interval, the rest every
// rare one; driving or charging, the four telemetries every active interval; the
// target level every rare interval.
func (Source) Spec() collector.SourceSpec {
	telemetry := func(d polestar.Domain, parked collector.Pace) collector.EndpointSpec {
		return collector.EndpointSpec{
			Endpoint: collector.Endpoint(d), API: polestar.Source,
			Parked: parked, Driving: collector.PaceActive, Charging: collector.PaceActive,
		}
	}
	return collector.SourceSpec{
		Name: polestar.Source,
		Endpoints: []collector.EndpointSpec{
			telemetry(polestar.Battery, collector.PaceParked),
			telemetry(polestar.Availability, collector.PaceParked),
			telemetry(polestar.Odometer, collector.PaceRare),
			telemetry(polestar.Location, collector.PaceRare),
			{
				Endpoint: collector.Endpoint(polestar.TargetSoC), API: polestar.Source,
				Parked: collector.PaceRare, Driving: collector.PaceRare, Charging: collector.PaceRare,
			},
		},
		Settled: []collector.Endpoint{collector.Endpoint(polestar.Odometer), collector.Endpoint(polestar.Location)},
		Daily:   Daily,
	}
}

// Login implements collector.Source: a token of the connection's credential.
func (s Source) Login(ctx context.Context, accountID, connectionID string) (collector.Session, time.Duration, error) {
	cred, err := s.credential(ctx, accountID, connectionID)
	if err != nil {
		return nil, 0, err
	}
	tok, err := s.client.Token(ctx, cred)
	if err != nil {
		return nil, 0, sourceError(err)
	}
	return session{client: s.client, token: tok.Value, accountID: cred.AccountID}, tok.ExpiresIn, nil
}

// Observe implements collector.Source: the usage mode tells whether the car drives, the
// battery whether it charges.
func (Source) Observe(ep collector.Endpoint, raw []byte, a *collector.Activity) error {
	s, err := polestar.Normalize(polestar.Domain(ep), raw)
	if err != nil {
		return err //nolint:wrapcheck // polestar's, which names the domain
	}
	if s.Covers&core.FieldEngine != 0 && s.Engine.OK {
		a.Engine, a.Driving = string(s.Engine.V), s.Engine.V == core.EngineRunning
	}
	if s.Covers&core.FieldCharging != 0 && s.Charging.OK {
		a.Charging, a.Charges = string(s.Charging.V), s.Charging.V == core.ChargingActive
	}
	return nil
}

// Fingerprint implements collector.Source.
func (Source) Fingerprint(raw []byte) ([]byte, error) {
	return polestar.Fingerprint(raw) //nolint:wrapcheck // polestar's, which says what failed
}

// session reads with a token of a credential.
type session struct {
	client    *polestar.Client
	token     string
	accountID string // the credential's, which every call names
}

// Fetch implements collector.Session.
func (s session) Fetch(ctx context.Context, vin string, ep collector.Endpoint) ([]byte, error) {
	raw, err := s.client.Fetch(ctx, s.token, s.accountID, vin, polestar.Domain(ep))
	if err != nil {
		return nil, sourceError(err)
	}
	return raw, nil
}

// sourceError tells the collector what a refusal of the Data Portal means.
func sourceError(err error) error {
	var e *polestar.APIError
	if !errors.As(err, &e) {
		return err
	}
	se := &collector.SourceError{Status: e.Status, RetryIn: e.RetryIn, Err: err}
	switch e.Kind {
	case polestar.KindCredentialRefused:
		se.Kind = collector.KindCredentialRefused
	case polestar.KindUnauthorized:
		se.Kind = collector.KindUnauthorized
	case polestar.KindVINUnauthorized:
		se.Kind = collector.KindUnshared
	case polestar.KindDomainNotGranted:
		se.Kind = collector.KindNotGranted
	case polestar.KindNotAvailable:
		se.Kind = collector.KindNotAvailable
	case polestar.KindRateLimited:
		se.Kind = collector.KindRateLimited
	case polestar.KindOther:
		se.Kind = collector.KindOther
	}
	return se
}
