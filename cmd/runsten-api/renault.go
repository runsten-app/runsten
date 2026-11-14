package main

import (
	"context"
	"errors"
	"fmt"

	"runsten/internal/api"
	"runsten/internal/renault"
)

// renaultAccounts signs an owner in to My Renault for the API (ADR 0050 §1): a device's
// identity, the login, the one-time code when the account asks for it, then the person,
// their accounts and the vehicles of each.
type renaultAccounts struct{ c *renault.Client }

// renaultChallenge is what a code completes: the challenge Gigya opened, without the
// password, which the API keeps beside it.
type renaultChallenge struct{ ch renault.Challenge }

func (r renaultAccounts) RenaultSignIn(ctx context.Context, email, password, lang string) (api.RenaultSignIn, error) {
	d, err := r.c.Bootstrap(ctx)
	if err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	s, err := r.c.Login(ctx, d, email, password)
	var e *renault.APIError
	if errors.As(err, &e) && e.Kind == renault.KindTFARequired {
		ch, err := r.c.SendCode(ctx, d, e.RegToken, lang)
		if err != nil {
			return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
		}
		return api.RenaultSignIn{Pending: &api.RenaultPending{MaskedMail: ch.MaskedMail, State: renaultChallenge{ch}}}, nil
	}
	if err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	return r.vehicles(ctx, s)
}

func (r renaultAccounts) RenaultVerify(ctx context.Context, state any, email, password, code string) (api.RenaultSignIn, error) {
	st, ok := state.(renaultChallenge)
	if !ok {
		return api.RenaultSignIn{}, errors.New("renault: no challenge to verify")
	}
	if err := r.c.Verify(ctx, st.ch, code); err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	s, err := r.c.Login(ctx, st.ch.Device, email, password)
	if err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	return r.vehicles(ctx, s)
}

// defaultCountry is the country the person is first read with: Kamereon tells their own
// in the response, which every later call names.
const defaultCountry = "FR"

// vehicles reads the person of a sign-in, and the vehicles of their MYRENAULT accounts.
func (r renaultAccounts) vehicles(ctx context.Context, s renault.Session) (api.RenaultSignIn, error) {
	person, err := r.c.PersonID(ctx, s)
	if err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	jwt, err := r.c.JWT(ctx, s)
	if err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	p, err := r.c.Person(ctx, jwt.Value, person, defaultCountry)
	if err != nil {
		return api.RenaultSignIn{}, err //nolint:wrapcheck // the client's, with its context
	}
	country := p.Country
	if country == "" {
		country = defaultCountry
	}
	out := api.RenaultSignIn{Credential: api.RenaultCredential{
		LoginToken: s.LoginToken, GMID: s.Device.GMID, UCID: s.Device.UCID, PersonID: person, Country: country,
	}}
	seen := map[string]bool{}
	for _, a := range p.Accounts {
		if a.Type != renault.AccountMyRenault {
			continue
		}
		vs, err := r.c.Vehicles(ctx, jwt.Value, a.ID, country)
		if err != nil {
			return api.RenaultSignIn{}, fmt.Errorf("renault account: %w", err)
		}
		for _, v := range vs {
			if seen[v.VIN] {
				continue
			}
			seen[v.VIN] = true
			if v.Electric() {
				out.VINs = append(out.VINs, v.VIN)
			} else {
				out.Others++
			}
		}
	}
	return out, nil
}
