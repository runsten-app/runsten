-- What the collector knows of each vehicle, written after its passes, for the user: the
-- rest of its state (the calls, their responses) lives in its memory and on its debug
-- page. Nothing here is derived from the snapshots, and nothing is derived from it.
--
-- A row is replaced as a whole, but for read_at and the last failure: a restarted
-- collector knows neither, and keeps those it wrote before.
CREATE TABLE collector_status (
  account_id     uuid        NOT NULL,
  vehicle_id     uuid        NOT NULL,
  passed_at      timestamptz NOT NULL, -- the latest pass over the vehicle
  mode           text        NOT NULL CHECK (mode IN ('parked', 'driving', 'charging')),
  read_at        timestamptz,          -- the latest successful call
  next_at        timestamptz,          -- the earliest call due, as of passed_at
  paused_until   timestamptz,          -- rate limit, or a token refused after a refresh
  -- APIs whose quota is exhausted, as of passed_at: {"energy": "2026-09-28T14:00:00Z"}.
  quota          jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(quota) = 'object'),
  failed_at      timestamptz,          -- the latest failed call
  fail_endpoint  text,                 -- empty when no call was made (no access token)
  fail_status    integer,              -- HTTP status; NULL when the API did not respond
  fail_kind      text CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized', 'not_found',
                                           'unavailable', 'token', 'other')),
  PRIMARY KEY (vehicle_id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK ((failed_at IS NULL) = (fail_kind IS NULL) AND (failed_at IS NULL) = (fail_endpoint IS NULL))
);

ALTER TABLE collector_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON collector_status TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON collector_status TO runsten_app;
