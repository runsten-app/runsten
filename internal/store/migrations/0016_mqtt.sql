-- The account's MQTT broker, which the collector publishes the vehicles' state to: one
-- per account, given in the settings, never by the environment.
--
-- The password is encrypted by the application, bound to the account, as the tokens
-- and the application key. NULL: the broker takes no password.
CREATE TABLE mqtt_brokers (
  account_id       uuid        NOT NULL REFERENCES accounts ON DELETE CASCADE,
  url              text        NOT NULL CHECK (url ~ '^mqtts?://' AND char_length(url) <= 255),
  username         text        NOT NULL CHECK (char_length(username) <= 256),
  password         bytea,
  client_id        text        NOT NULL CHECK (client_id ~ '^[0-9A-Za-z_-]{0,64}$'), -- empty: the collector's own
  topic_prefix     text        NOT NULL CHECK (topic_prefix <> '' AND char_length(topic_prefix) <= 64),
  discovery        boolean     NOT NULL, -- Home Assistant's discovery messages
  discovery_prefix text        NOT NULL CHECK (discovery_prefix <> '' AND char_length(discovery_prefix) <= 64),
  publish_location boolean     NOT NULL,
  created_at       timestamptz NOT NULL,
  -- Any change: the collector connects again with the new configuration.
  updated_at       timestamptz NOT NULL,
  PRIMARY KEY (account_id)
);

ALTER TABLE mqtt_brokers ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON mqtt_brokers TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON mqtt_brokers TO runsten_app;

-- What the collector knows of its connection to the broker, for the user: as
-- collector_status, a kind of failure, never the broker's message. Gone with the
-- configuration it was about.
CREATE TABLE mqtt_status (
  account_id   uuid        NOT NULL REFERENCES mqtt_brokers ON DELETE CASCADE,
  connected_at timestamptz,          -- the latest connection
  failed_at    timestamptz,          -- the latest failure, to connect or to publish
  failure      text CHECK (failure IN ('unreachable', 'refused_address', 'tls', 'not_authorized', 'rejected')),
  PRIMARY KEY (account_id),
  CHECK ((failed_at IS NULL) = (failure IS NULL))
);

ALTER TABLE mqtt_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON mqtt_status TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON mqtt_status TO runsten_app;
