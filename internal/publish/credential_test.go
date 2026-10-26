package publish

import (
	"testing"
	"time"
)

func TestCredentialStatus(t *testing.T) {
	expires := time.Date(2027, 1, 2, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		c    Credential
		now  time.Time
		want string
	}{
		{"none given", Credential{}, expires, CredentialMissing},
		{"valid", Credential{ExpiresAt: expires}, expires.Add(-RenewAhead - time.Second), CredentialValid},
		{"the reminder's start", Credential{ExpiresAt: expires}, expires.Add(-RenewAhead), CredentialExpiring},
		{"its expiry", Credential{ExpiresAt: expires}, expires, CredentialExpired},
		{"refused before its expiry", Credential{ExpiresAt: expires, RefusedAt: expires.Add(-time.Hour)}, expires.Add(-30 * 24 * time.Hour), CredentialRefused},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Status(tt.now); got != tt.want {
				t.Errorf("Status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCredentialState(t *testing.T) {
	expires := time.Date(2027, 1, 2, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	now := expires.Add(-24 * time.Hour)
	for _, tt := range []struct {
		name string
		c    *Credential
		want map[string]string
	}{
		{"read with one", &Credential{ExpiresAt: expires}, map[string]string{ //nolint:gosec // topics and states, no credential
			"credential_status": "expiring", "credential_expires_at": "2027-01-02T09:00:00Z",
		}},
		{"its connection has none", &Credential{}, map[string]string{"credential_status": "missing", "credential_expires_at": ""}},
		{"read without one", nil, map[string]string{"credential_status": "", "credential_expires_at": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := payloads(t, CredentialState(broker(false), vehicle, tt.c, now))
			if len(got) != len(tt.want) {
				t.Fatalf("payloads = %v", got)
			}
			for topic, want := range tt.want {
				if got[topic] != want {
					t.Errorf("%s = %q, want %q", topic, got[topic], want)
				}
			}
		})
	}
}
