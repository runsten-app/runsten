-- The ZIP archive of an account's whole history, asked for in the interface and built
-- by runsten-api's export worker, out of the request: GET /account/export streams the
-- same document, but of an account that outgrows the request a month of readings alone
-- can do (1 GB), the worker builds it in the background and holds it until it is
-- downloaded. At most one export per account: the one asked for, ready or being built.
--
-- The archive is the account's export (GET /account/export's document) zipped, kept as
-- a large object: backup and restore carry it with the database, the account's
-- rows included, and nothing is added of the deployment (a volume, a path). The large
-- object of a build in progress is held by the row for as long as the build runs: a
-- worker that stopped leaves it, and the next claim throws it away and builds another.
CREATE TABLE account_exports (
  id         uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id uuid        NOT NULL UNIQUE REFERENCES accounts ON DELETE CASCADE,
  status     text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed')),
  -- Why the export failed: a kind, never the database's message.
  error      text        CHECK (error = 'internal'),
  -- The archive: a large object once the build has opened it (running) and once it is
  -- held (done); the row keeps it for its account's next export to throw away. A
  -- claim comes before its build, a failed one has none anymore, an interrupted one
  -- may still have its half: only a done export must have one.
  oid       oid          CHECK (oid <> 0) CHECK (status <> 'done' OR oid IS NOT NULL),
  -- The archive's size in bytes, written with its status done.
  bytes     bigint       NOT NULL DEFAULT 0 CHECK (bytes >= 0),
  created_at timestamptz NOT NULL,
  -- Not claimed before: a failure is retried later.
  not_before timestamptz NOT NULL,
  -- The latest claim of the worker: another may claim the export once its lease is
  -- over, which builds the archive again from its start.
  claimed_at timestamptz,
  -- Failures in a row: the export fails at the worker's maximum.
  failures   integer     NOT NULL DEFAULT 0 CHECK (failures >= 0),
  ended_at    timestamptz,
  PRIMARY KEY (id),
  CHECK ((status IN ('done', 'failed')) = (ended_at IS NOT NULL)),
  CHECK ((status = 'failed') = (error IS NOT NULL)),
  CHECK ((status = 'pending') = (claimed_at IS NULL)),
  CHECK ((status = 'done') = (bytes > 0))
);

ALTER TABLE account_exports ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON account_exports TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON account_exports TO runsten_app;

-- Claims the account's export to build once it may run (not_before passed) and is not
-- held by a lease: the worker then builds it within the account. Only the account, the
-- export and its claim leave the function.
CREATE FUNCTION runsten_claim_account_export(at timestamptz, lease interval)
  RETURNS TABLE (account_id uuid, id uuid, claimed_at timestamptz)
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    UPDATE account_exports e SET status = 'running', claimed_at = at
    FROM (
      SELECT x.id FROM account_exports x
      WHERE x.status IN ('pending', 'running') AND x.not_before <= at
        AND (x.claimed_at IS NULL OR x.claimed_at <= at - lease)
      ORDER BY x.not_before, x.created_at, x.id
      LIMIT 1
      FOR UPDATE SKIP LOCKED
    ) c
    WHERE e.id = c.id
    RETURNING e.account_id, e.id, e.claimed_at
  $$;

REVOKE ALL ON FUNCTION runsten_claim_account_export(timestamptz, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_claim_account_export(timestamptz, interval) TO runsten_app;
