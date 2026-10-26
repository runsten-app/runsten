package publish

import "time"

// A vehicle read with the owner's credential (the Polestar Data Portal's) loses its
// reads when the credential expires, and what the car sends meanwhile is lost: the
// portal gives the latest values, not their history. Its state and its expiry are
// published beside the vehicle's values, so that an automation can warn the owner, who
// may never open the web interface.

// Credential is the credential a vehicle is read with: when it expires at the latest
// (counted from when it was given) and when the provider refused it. A zero ExpiresAt:
// none given.
type Credential struct {
	ExpiresAt time.Time
	RefusedAt time.Time
}

// RenewAhead is how long before its expiry a credential is to be renewed: the web
// interface's reminder starts then, and so does the problem it is published as.
const RenewAhead = 14 * 24 * time.Hour

// The states of a credential, the payloads of its credential_status topic.
const (
	CredentialValid    = "valid"
	CredentialExpiring = "expiring" // within RenewAhead of its expiry
	CredentialExpired  = "expired"  // past its expiry: it may still read, the date is counted from when it was given
	CredentialRefused  = "refused"  // the provider refused it: its vehicles are not read until another
	CredentialMissing  = "missing"  // none given, or removed
)

// Status is where the credential stands at now.
func (c Credential) Status(now time.Time) string {
	switch {
	case c.ExpiresAt.IsZero():
		return CredentialMissing
	case !c.RefusedAt.IsZero():
		return CredentialRefused
	case !now.Before(c.ExpiresAt):
		return CredentialExpired
	case !now.Before(c.ExpiresAt.Add(-RenewAhead)):
		return CredentialExpiring
	}
	return CredentialValid
}

// credentialTopics are the topics of a vehicle's credential, in the order of
// CredentialState.
var credentialTopics = []string{"credential_status", "credential_expires_at"}

// CredentialState is the state of the credential the vehicle is read with at now, one
// retained message per value; nil: the vehicle is read without one, and the messages are
// empty, which removes them.
func CredentialState(b Broker, vehicleID string, c *Credential, now time.Time) []Message {
	base := b.TopicPrefix + "/vehicles/" + vehicleID + "/"
	payloads := make([]string, len(credentialTopics))
	if c != nil {
		payloads[0] = c.Status(now)
		if !c.ExpiresAt.IsZero() {
			payloads[1] = c.ExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	msgs := make([]Message, len(credentialTopics))
	for i, topic := range credentialTopics {
		msgs[i] = Message{Topic: base + topic, Payload: payloads[i], Retained: true}
	}
	return msgs
}
