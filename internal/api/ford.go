package api

// A Ford connection is the owner's application on Ford's developer portal (FordConnect
// Query) and the sign-in they made with it. Nothing of it is returned, logged nor
// exported: only the client ID's last four characters, when it was given, when its grant
// ends, and whether it was refused.

// FordCredential is what a Ford connection holds: the application's client ID and
// secret, the redirect address the sign-in was asked with (the token endpoint is given
// it again at every refresh), and the refresh token.
type FordCredential struct {
	ClientID, ClientSecret, RedirectURI, Refresh string
}
