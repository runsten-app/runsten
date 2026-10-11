-- The imports of a vehicle's history from another application (ADR 0030), asked for in
-- the interface and run by runsten-api's import worker, page by page: a job resumes at
-- its source's cursor after a restart. At most one active job per vehicle.
--
-- The token the source is read with is sealed by the application, bound to the account
-- and to its column, and erased as soon as the job ends, done or failed: it is never
-- returned, logged, nor exported.
CREATE TABLE import_jobs (
  id             uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id     uuid        NOT NULL,
  vehicle_id     uuid        NOT NULL,
  source         text        NOT NULL CHECK (source ~ '^[a-z][a-z0-9_]{0,31}$'),
  -- The vehicle at the source, whose history is imported.
  source_vehicle text        NOT NULL CHECK (source_vehicle ~ '^[A-Za-z0-9._~-]{1,128}$'),
  token          bytea,
  status         text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed')),
  -- The trips, then the charges; cursor is the source's, of the step's next page
  -- (NULL: its first).
  step           text        NOT NULL DEFAULT 'trips' CHECK (step IN ('trips', 'charges')),
  cursor         text        CHECK (char_length(cursor) BETWEEN 1 AND 1000),
  -- Events written, left after the cutoff (store.SaveImport's), and refused by the
  -- importer's checks.
  imported       integer     NOT NULL DEFAULT 0 CHECK (imported >= 0),
  skipped        integer     NOT NULL DEFAULT 0 CHECK (skipped >= 0),
  invalid        integer     NOT NULL DEFAULT 0 CHECK (invalid >= 0),
  -- The vehicle's first snapshot as of the latest page: only what starts before it is
  -- imported.
  cutoff         timestamptz,
  -- Why the job failed: a kind, never the source's message.
  error          text        CHECK (error IN ('token_refused', 'source_failed', 'internal')),
  created_at     timestamptz NOT NULL,
  -- Not claimed before: the source asked to wait (429), or a failure is retried later.
  not_before     timestamptz NOT NULL,
  -- The latest claim of the worker, renewed at each page: another may claim the job
  -- once its lease is over, which resumes it after a crash.
  claimed_at     timestamptz,
  -- Failures in a row, other than the source's limits: the job fails at the worker's
  -- maximum.
  failures       integer     NOT NULL DEFAULT 0 CHECK (failures >= 0),
  ended_at       timestamptz,
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK ((status IN ('done', 'failed')) = (ended_at IS NOT NULL)),
  CHECK ((status = 'failed') = (error IS NOT NULL)),
  CHECK ((status IN ('pending', 'running')) = (token IS NOT NULL))
);

CREATE UNIQUE INDEX import_jobs_active ON import_jobs (vehicle_id) WHERE status IN ('pending', 'running');
CREATE INDEX import_jobs_latest ON import_jobs (vehicle_id, created_at);

ALTER TABLE import_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON import_jobs TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON import_jobs TO runsten_app;

-- Claims the oldest active job of any account that may run (not_before passed) and is
-- not held by a lease: the worker then reads it within the account. Only the account,
-- the job and its claim leave the function; never the token.
CREATE FUNCTION runsten_claim_import(at timestamptz, lease interval)
  RETURNS TABLE (account_id uuid, id uuid, claimed_at timestamptz)
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    UPDATE import_jobs j SET status = 'running', claimed_at = at
    FROM (
      SELECT i.id FROM import_jobs i
      WHERE i.status IN ('pending', 'running') AND i.not_before <= at
        AND (i.claimed_at IS NULL OR i.claimed_at <= at - lease)
      ORDER BY i.not_before, i.created_at, i.id
      LIMIT 1
      FOR UPDATE SKIP LOCKED
    ) c
    WHERE j.id = c.id
    RETURNING j.account_id, j.id, j.claimed_at
  $$;

REVOKE ALL ON FUNCTION runsten_claim_import(timestamptz, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_claim_import(timestamptz, interval) TO runsten_app;
