package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Enrollment records a connection and its vehicles.
type Enrollment interface {
	// SaveConnection stores (or replaces) the account's connection, which becomes
	// usable again if it required re-authentication. Its application key, if any, is
	// kept.
	SaveConnection(ctx context.Context, accountID string, c Credentials) (string, error)
	AddVehicle(ctx context.Context, accountID, connectionID, vin string) (string, error)
	// SetKeyRefused records whether the provider refused the connection's application
	// key set at setAt; a key set since is left alone.
	SetKeyRefused(ctx context.Context, accountID, connectionID string, setAt time.Time, refused bool) error
}

// VehicleLister lists the VINs accessible with an access token, with an application
// key (empty: the instance's).
type VehicleLister interface {
	Vehicles(ctx context.Context, key, token string) ([]string, error)
}

// APIKey is the application key of an account's connection: the provider counts its
// quota on it. A zero APIKey is the instance's key.
type APIKey struct {
	Value string
	SetAt time.Time
}

// ErrKeyRefused is returned by Enroll when the provider refused the account's own
// application key: the connection is saved, its vehicles are listed once the user gives
// another key.
var ErrKeyRefused = errors.New("application key refused")

// ErrNoVehicle is returned by Enroll when the credentials give access to no vehicle (none
// attached to the account at the provider): nothing is stored.
var ErrNoVehicle = errors.New("no vehicle accessible with this token")

// ErrTooManyVehicles is returned by Enroll when the credentials give access to more
// vehicles than the account may have: nothing is stored.
var ErrTooManyVehicles = errors.New("more vehicles than the account may have")

// keyRefused reports whether err is the provider's refusal of the application key.
func keyRefused(err error) bool {
	var r interface{ KeyRefused() bool }
	return errors.As(err, &r) && r.KeyRefused()
}

// Enroll attaches credentials to the account and records the vehicles they give access
// to, listed with key. The vehicles are listed first: a token that gives access to
// nothing is not stored (ErrNoVehicle). A refusal of the account's own key is not the grant's: the
// connection is stored, the key marked refused, and ErrKeyRefused returned. More than
// maxVehicles vehicles (0: no cap) are refused with ErrTooManyVehicles.
func Enroll(ctx context.Context, st Enrollment, api VehicleLister, accountID string, key APIKey, c Credentials, maxVehicles int) ([]string, error) {
	if c.AccessToken == "" {
		return nil, errors.New("empty access token")
	}
	vins, err := api.Vehicles(ctx, key.Value, c.AccessToken)
	if key.Value != "" && keyRefused(err) {
		conn, err := st.SaveConnection(ctx, accountID, c)
		if err != nil {
			return nil, fmt.Errorf("connection: %w", err)
		}
		if err := st.SetKeyRefused(ctx, accountID, conn, key.SetAt, true); err != nil {
			return nil, fmt.Errorf("connection: %w", err)
		}
		return nil, ErrKeyRefused
	}
	if err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	if len(vins) == 0 {
		return nil, ErrNoVehicle
	}
	if maxVehicles > 0 && len(vins) > maxVehicles {
		return nil, ErrTooManyVehicles
	}
	conn, err := st.SaveConnection(ctx, accountID, c)
	if err != nil {
		return nil, fmt.Errorf("connection: %w", err)
	}
	if key.Value != "" {
		if err := st.SetKeyRefused(ctx, accountID, conn, key.SetAt, false); err != nil {
			return nil, fmt.Errorf("connection: %w", err)
		}
	}
	if err := AddVehicles(ctx, st, accountID, conn, vins); err != nil {
		return nil, err
	}
	return vins, nil
}

// AddVehicles records vehicles of a connection.
func AddVehicles(ctx context.Context, st Enrollment, accountID, connectionID string, vins []string) error {
	for _, vin := range vins {
		if _, err := st.AddVehicle(ctx, accountID, connectionID, vin); err != nil {
			return fmt.Errorf("vehicle: %w", err)
		}
	}
	return nil
}
