package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"runsten/internal/api"
	"runsten/internal/publish"
)

// The account's MQTT broker (mqtt_brokers). Its password is sealed as the tokens, bound
// to the account and to its column: copied into another account's row, or into
// another secret's column, it does not open.

// mqttPasswordData is what a broker's password is sealed with, beside the account.
func mqttPasswordData(accountID string) []byte { return []byte(accountID + "/mqtt_brokers.password") }

// MQTTBroker implements api.Brokers.
func (s *Store) MQTTBroker(ctx context.Context, accountID string) (api.MQTTBroker, bool, error) {
	var b api.MQTTBroker
	found := false
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		var connectedAt, failedAt *time.Time
		var failure *string
		err := tx.QueryRow(ctx, `
			SELECT b.url, b.username, b.password IS NOT NULL, b.client_id, b.topic_prefix, b.discovery, b.discovery_prefix,
				b.publish_location, b.updated_at, s.connected_at, s.failed_at, s.failure
			FROM mqtt_brokers b LEFT JOIN mqtt_status s ON s.account_id = b.account_id`).Scan(
			&b.URL, &b.Username, &b.PasswordSet, &b.ClientID, &b.TopicPrefix, &b.Discovery, &b.DiscoveryPrefix,
			&b.PublishLocation, &b.UpdatedAt, &connectedAt, &failedAt, &failure)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // wrapped by inAccount
		}
		found = true
		b.UpdatedAt = b.UpdatedAt.UTC()
		b.Status = api.MQTTStatus{ConnectedAt: utcOrZero(connectedAt), FailedAt: utcOrZero(failedAt)}
		if failure != nil {
			b.Status.Failure = *failure
		}
		return nil
	})
	return b, found, err
}

// SetMQTTBroker implements api.Brokers.
func (s *Store) SetMQTTBroker(ctx context.Context, accountID string, b api.MQTTBroker, at time.Time) error {
	var sealed []byte
	if b.SetPassword && b.Password != "" {
		sealed = s.box.Seal([]byte(b.Password), mqttPasswordData(accountID))
	}
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mqtt_brokers (account_id, url, username, password, client_id, topic_prefix, discovery, discovery_prefix,
				publish_location, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
			ON CONFLICT (account_id) DO UPDATE SET
				url = EXCLUDED.url, username = EXCLUDED.username,
				password = CASE WHEN $11::boolean THEN EXCLUDED.password ELSE mqtt_brokers.password END,
				client_id = EXCLUDED.client_id, topic_prefix = EXCLUDED.topic_prefix, discovery = EXCLUDED.discovery,
				discovery_prefix = EXCLUDED.discovery_prefix, publish_location = EXCLUDED.publish_location,
				updated_at = EXCLUDED.updated_at`,
			accountID, b.URL, b.Username, sealed, b.ClientID, b.TopicPrefix, b.Discovery, b.DiscoveryPrefix,
			b.PublishLocation, at.Truncate(time.Microsecond), b.SetPassword); err != nil {
			return fmt.Errorf("broker: %w", err)
		}
		// What the collector wrote was of the previous configuration.
		if _, err := tx.Exec(ctx, "DELETE FROM mqtt_status"); err != nil {
			return fmt.Errorf("status: %w", err)
		}
		return nil
	})
}

// DeleteMQTTBroker implements api.Brokers. Its status goes with it.
func (s *Store) DeleteMQTTBroker(ctx context.Context, accountID string) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM mqtt_brokers")
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// MQTTBrokers implements the publisher's Store: the accounts that have a broker, across
// the accounts (runsten_mqtt_brokers), identifiers only.
func (s *Store) MQTTBrokers(ctx context.Context) ([]publish.BrokerRef, error) {
	rows, err := s.pool.Query(ctx, "SELECT account_id, updated_at FROM runsten_mqtt_brokers()")
	if err != nil {
		return nil, fmt.Errorf("MQTT brokers: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (publish.BrokerRef, error) {
		var b publish.BrokerRef
		err := r.Scan(&b.AccountID, &b.UpdatedAt)
		b.UpdatedAt = b.UpdatedAt.UTC()
		return b, err //nolint:wrapcheck // wrapped below
	})
	if err != nil {
		return nil, fmt.Errorf("MQTT brokers: %w", err)
	}
	return refs, nil
}

// PublishBroker implements the publisher's Store: the account's broker, its password
// opened, read within the account; false when it has none.
func (s *Store) PublishBroker(ctx context.Context, accountID string) (publish.Broker, bool, error) {
	b := publish.Broker{AccountID: accountID}
	var sealed []byte
	found := false
	err := s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT url, username, password, client_id, topic_prefix, discovery, discovery_prefix, publish_location, updated_at
			FROM mqtt_brokers`).Scan(
			&b.URL, &b.Username, &sealed, &b.ClientID, &b.TopicPrefix, &b.Discovery, &b.DiscoveryPrefix,
			&b.PublishLocation, &b.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err //nolint:wrapcheck // wrapped by inAccount
	})
	if err != nil || !found {
		return publish.Broker{}, false, err
	}
	b.UpdatedAt = b.UpdatedAt.UTC()
	if sealed != nil {
		if b.Password, err = s.mqttPassword(accountID, sealed); err != nil {
			return publish.Broker{}, false, err
		}
	}
	return b, true, nil
}

// SaveMQTTStatus implements the publisher's Store: what it knows of its connection to
// the broker configured at updatedAt. A zero time keeps the one stored before; a
// configuration changed or removed since takes nothing: the status was of the previous
// one.
func (s *Store) SaveMQTTStatus(ctx context.Context, accountID string, updatedAt time.Time, st publish.Status) error {
	return s.inAccount(ctx, accountID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO mqtt_status (account_id, connected_at, failed_at, failure)
			SELECT account_id, $2, $3, $4 FROM mqtt_brokers WHERE updated_at = $1
			ON CONFLICT (account_id) DO UPDATE SET
				connected_at = coalesce(EXCLUDED.connected_at, mqtt_status.connected_at),
				failed_at = coalesce(EXCLUDED.failed_at, mqtt_status.failed_at),
				failure = CASE WHEN EXCLUDED.failed_at IS NULL THEN mqtt_status.failure ELSE EXCLUDED.failure END`,
			updatedAt, nullTime(st.ConnectedAt), nullTime(st.FailedAt), nullString(st.Failure))
		return err //nolint:wrapcheck // wrapped by inAccount
	})
}

// mqttPassword opens a broker's sealed password.
func (s *Store) mqttPassword(accountID string, sealed []byte) (string, error) {
	plain, err := s.box.Open(sealed, mqttPasswordData(accountID))
	if err != nil {
		return "", fmt.Errorf("MQTT password for account %s: %w", accountID, err)
	}
	return string(plain), nil
}
