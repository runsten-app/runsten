package renault

import (
	"context"
	"errors"
	"net/url"
)

// The one-time code Renault emails to some accounts since 2026-06-01 (Gigya's
// two-factor authentication by email), as renault-api follows it: the login answers
// 403101 with a registration token; the code is sent, entered, and the registration
// finalized; then the same device signs in again, and is not asked for a code for a
// while (tempDevice false: the device is remembered).

// Challenge is a one-time code sent, waiting to be entered: what Verify needs, and the
// address it went to, masked as Gigya tells it (e***@example.com). It holds no password.
type Challenge struct {
	Device     Device
	RegToken   string
	Assertion  string // gigyaAssertion
	PHVToken   string // the code's own token
	MaskedMail string
}

// SendCode starts the one-time code of a login that asked for it (regToken, its
// *APIError's): the assertion of the registration, the account's first address, and
// the code emailed to it in the language lang (fr, en…).
func (c *Client) SendCode(ctx context.Context, d Device, regToken, lang string) (Challenge, error) {
	var init struct {
		GigyaAssertion string `json:"gigyaAssertion"`
	}
	q := url.Values{"provider": {"gigyaEmail"}, "mode": {"verify"}, "regToken": {regToken}, "gmid": {d.GMID}, "ucid": {d.UCID}}
	if err := c.gigyaGet(ctx, "accounts.tfa.initTFA", "init tfa", q, d, &init); err != nil {
		return Challenge{}, err
	}
	if init.GigyaAssertion == "" {
		return Challenge{}, errors.New("init tfa: no assertion in the response")
	}
	var emails struct {
		Emails []struct {
			ID         string `json:"id"`
			Obfuscated string `json:"obfuscated"`
		} `json:"emails"`
	}
	if err := c.gigyaGet(ctx, "accounts.tfa.email.getEmails", "tfa emails", url.Values{"gigyaAssertion": {init.GigyaAssertion}}, d, &emails); err != nil {
		return Challenge{}, err
	}
	if len(emails.Emails) == 0 || emails.Emails[0].ID == "" {
		return Challenge{}, errors.New("tfa emails: no address to send the code to")
	}
	var sent struct {
		PHVToken string `json:"phvToken"`
	}
	q = url.Values{"gigyaAssertion": {init.GigyaAssertion}, "emailID": {emails.Emails[0].ID}, "lang": {lang}}
	if err := c.gigyaGet(ctx, "accounts.tfa.email.sendVerificationCode", "send code", q, d, &sent); err != nil {
		return Challenge{}, err
	}
	if sent.PHVToken == "" {
		return Challenge{}, errors.New("send code: no token in the response")
	}
	return Challenge{
		Device: d, RegToken: regToken, Assertion: init.GigyaAssertion, PHVToken: sent.PHVToken,
		MaskedMail: emails.Emails[0].Obfuscated,
	}, nil
}

// Verify enters the code of a challenge and finalizes the registration: the device may
// then sign in. A wrong code answers an *APIError of KindWrongCode, the challenge still
// open.
func (c *Client) Verify(ctx context.Context, ch Challenge, code string) error {
	var done struct {
		ProviderAssertion string `json:"providerAssertion"`
	}
	q := url.Values{"gigyaAssertion": {ch.Assertion}, "phvToken": {ch.PHVToken}, "code": {code}}
	if err := c.gigyaGet(ctx, "accounts.tfa.email.completeVerification", "verify code", q, ch.Device, &done); err != nil {
		return err
	}
	if done.ProviderAssertion == "" {
		return errors.New("verify code: no assertion in the response")
	}
	var fin struct{}
	q = url.Values{
		"gigyaAssertion": {ch.Assertion}, "providerAssertion": {done.ProviderAssertion},
		"regToken": {ch.RegToken}, "tempDevice": {"false"},
	}
	return c.gigyaGet(ctx, "accounts.tfa.finalizeTFA", "finalize tfa", q, ch.Device, &fin)
}
