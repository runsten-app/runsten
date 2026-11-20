-- Notifications: the devices the account's apps registered, the kinds its user turned on
-- or off, and the deliveries, recorded before they are sent. Without a PushSender (an
-- extension's), nothing is sent and nothing recorded.

-- An app installed on a phone, and the token its push service reaches it with. Its ID is
-- chosen by the app, once per installation: the same phone signed in to two accounts is
-- a device of each.
CREATE TABLE devices (
  id          uuid        NOT NULL,
  account_id  uuid        NOT NULL,
  user_id     uuid        NOT NULL,
  platform    text        NOT NULL CHECK (platform IN ('apns', 'fcm')),
  token       text        NOT NULL CHECK (char_length(token) BETWEEN 1 AND 4096),
  locale      text        NOT NULL CHECK (char_length(locale) BETWEEN 2 AND 35),
  app_version text        NOT NULL CHECK (char_length(app_version) BETWEEN 1 AND 32),
  created_at  timestamptz NOT NULL,
  updated_at  timestamptz NOT NULL,
  PRIMARY KEY (account_id, id),
  FOREIGN KEY (account_id, user_id) REFERENCES users (account_id, id) ON DELETE CASCADE
);

ALTER TABLE devices ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON devices TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON devices TO runsten_app;

-- The kinds the user turned on or off; a kind without a row takes its default. vehicle_id
-- NULL: an account's kind, or a vehicle's kind for every vehicle; set: that vehicle's own
-- choice. A kind no longer registered (an extension's, in a build without it) is kept
-- and ignored.
CREATE TABLE notification_preferences (
  account_id uuid    NOT NULL REFERENCES accounts ON DELETE CASCADE,
  vehicle_id uuid,
  kind       text    NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{0,63}$'),
  enabled    boolean NOT NULL,
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  UNIQUE NULLS NOT DISTINCT (account_id, vehicle_id, kind)
);

ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON notification_preferences TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON notification_preferences TO runsten_app;

-- An event told to the account's devices, recorded before it is handed to the
-- PushSender: once per kind, vehicle and key, a lost push being better than a duplicate.
-- held_back: beyond the vehicle's notifications of the hour, recorded and never sent.
CREATE TABLE notification_deliveries (
  id          uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id  uuid        NOT NULL REFERENCES accounts ON DELETE CASCADE,
  vehicle_id  uuid,
  kind        text        NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{0,63}$'),
  key         text        NOT NULL CHECK (char_length(key) BETWEEN 1 AND 256),
  happened_at timestamptz,
  args        jsonb       NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL,
  held_back   boolean     NOT NULL DEFAULT false,
  PRIMARY KEY (id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  UNIQUE NULLS NOT DISTINCT (account_id, kind, vehicle_id, key)
);
CREATE INDEX notification_deliveries_by_vehicle ON notification_deliveries (account_id, vehicle_id, created_at)
  WHERE vehicle_id IS NOT NULL;

ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON notification_deliveries TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON notification_deliveries TO runsten_app;

-- The deliveries recorded before an instant, across the accounts, deleted: their number.
-- A delivery is kept only as long as its key may come again (notify.Retention).
CREATE FUNCTION runsten_purge_notification_deliveries(before timestamptz)
  RETURNS integer
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    WITH gone AS (DELETE FROM notification_deliveries WHERE created_at < before RETURNING 1)
    SELECT count(*)::integer FROM gone
  $$;
REVOKE ALL ON FUNCTION runsten_purge_notification_deliveries(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_purge_notification_deliveries(timestamptz) TO runsten_app;

-- An access token may be given the scope notifications: it registers its device and sets
-- the account's preferences.
ALTER TABLE api_tokens
  DROP CONSTRAINT api_tokens_scopes_check,
  ADD CONSTRAINT api_tokens_scopes_check CHECK (scopes <@ ARRAY['trips:mark', 'notifications']);
