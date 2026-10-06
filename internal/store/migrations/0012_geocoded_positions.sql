-- The addresses of the positions of an account, as a reverse geocoder gave them, per
-- cell of 10⁻⁴ degrees (core.GeoCell). A read of the API asks for the cells it does not
-- know (requested_at); runsten-api's geocoding worker claims them one at a time, across
-- the accounts, and writes the answer. Neither derived from the snapshots nor derived
-- from: a rebuild keeps them. Per account, never shared: a position is personal data.
CREATE TABLE geocoded_positions (
  account_id   uuid        NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  lat_e4       integer     NOT NULL CHECK (lat_e4 BETWEEN -900000 AND 900000),
  lon_e4       integer     NOT NULL CHECK (lon_e4 BETWEEN -1800000 AND 1800000),
  requested_at timestamptz NOT NULL,
  -- The latest claim of the worker: another may claim the cell once its lease is over,
  -- which retries a failed request.
  claimed_at   timestamptz,
  attempts     integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  resolved_at  timestamptz,
  address      text,       -- empty when the geocoder knows none; NULL until resolved
  PRIMARY KEY (account_id, lat_e4, lon_e4),
  CHECK ((resolved_at IS NULL) = (address IS NULL))
);

-- The worker takes the oldest requests first.
CREATE INDEX geocoded_positions_pending ON geocoded_positions (requested_at) WHERE resolved_at IS NULL;

ALTER TABLE geocoded_positions ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON geocoded_positions TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON geocoded_positions TO runsten_app;

-- Claims the oldest unresolved cell of any account whose lease is over, and under
-- max_attempts: the worker then writes its address within the account. Only the account
-- and the cell leave the function.
CREATE FUNCTION runsten_claim_geocoding(at timestamptz, lease interval, max_attempts integer)
  RETURNS TABLE (account_id uuid, lat_e4 integer, lon_e4 integer)
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    UPDATE geocoded_positions g SET claimed_at = at, attempts = g.attempts + 1
    FROM (
      SELECT p.account_id, p.lat_e4, p.lon_e4 FROM geocoded_positions p
      WHERE p.resolved_at IS NULL AND p.attempts < max_attempts
        AND (p.claimed_at IS NULL OR p.claimed_at <= at - lease)
      ORDER BY p.requested_at, p.account_id, p.lat_e4, p.lon_e4
      LIMIT 1
      FOR UPDATE SKIP LOCKED
    ) c
    WHERE (g.account_id, g.lat_e4, g.lon_e4) = (c.account_id, c.lat_e4, c.lon_e4)
    RETURNING g.account_id, g.lat_e4, g.lon_e4
  $$;

REVOKE ALL ON FUNCTION runsten_claim_geocoding(timestamptz, interval, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_claim_geocoding(timestamptz, interval, integer) TO runsten_app;
