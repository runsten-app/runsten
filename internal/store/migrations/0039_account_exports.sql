-- The ZIP archive of an account's whole history, asked for in the interface and built
-- by runsten-api's export worker, out of the request: GET /account/export streams the
-- same document, but of an account that outgrows the request a month of readings alone
-- can do (1 GB), the worker builds it in the background and keeps it a few days, to be
-- downloaded, a download cut short resumed where it stopped. At most one export per
-- account: the one asked for, being built or held.
--
-- The archive itself is no row: runsten-api keeps it out of the database (a directory,
-- or an S3 bucket), named by its account and its export. Its worker throws away the
-- archives no export of this table holds anymore: replaced, expired, failed, or of an
-- account deleted.
CREATE TABLE account_exports (
  id         uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id uuid        NOT NULL UNIQUE REFERENCES accounts ON DELETE CASCADE,
  status     text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed')),
  -- Why the export failed: a kind, never the database's message.
  error      text        CHECK (error = 'internal'),
  -- The archive's bytes written so far, its size once done: how far the build is.
  bytes      bigint      NOT NULL DEFAULT 0 CHECK (bytes >= 0),
  created_at timestamptz NOT NULL,
  -- Not claimed before: a failure is retried later.
  not_before timestamptz NOT NULL,
  -- The worker's claim, and its latest sign of life: every part written renews it.
  -- Another worker may claim the export once it has gone a lease without one, which
  -- builds the archive again from its start.
  claimed_at timestamptz,
  renewed_at timestamptz,
  -- Failures in a row: the export fails at the worker's maximum.
  failures   integer     NOT NULL DEFAULT 0 CHECK (failures >= 0),
  ended_at   timestamptz,
  -- When the archive held goes: the export's row is deleted, its archive then.
  expires_at timestamptz,
  PRIMARY KEY (id),
  CHECK ((status IN ('done', 'failed')) = (ended_at IS NOT NULL)),
  CHECK ((status = 'failed') = (error IS NOT NULL)),
  CHECK ((status = 'pending') = (claimed_at IS NULL)),
  CHECK ((claimed_at IS NULL) = (renewed_at IS NULL)),
  CHECK (status <> 'done' OR bytes > 0),
  CHECK ((status = 'done') = (expires_at IS NOT NULL))
);

ALTER TABLE account_exports ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON account_exports TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON account_exports TO runsten_app;

-- Claims the account's export to build once it may run (not_before passed) and its
-- previous claim, if any, has gone a lease without a sign of life: the worker then
-- builds it within the account. Only the account, the export and its claim leave the
-- function.
CREATE FUNCTION runsten_claim_account_export(at timestamptz, lease interval)
  RETURNS TABLE (account_id uuid, id uuid, claimed_at timestamptz)
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    UPDATE account_exports e SET status = 'running', claimed_at = at, renewed_at = at
    FROM (
      SELECT x.id FROM account_exports x
      WHERE x.status IN ('pending', 'running') AND x.not_before <= at
        AND (x.renewed_at IS NULL OR x.renewed_at <= at - lease)
      ORDER BY x.not_before, x.created_at, x.id
      LIMIT 1
      FOR UPDATE SKIP LOCKED
    ) c
    WHERE e.id = c.id
    RETURNING e.account_id, e.id, e.claimed_at
  $$;

-- Ends the exports held past their time, across the accounts: their archives are then
-- no export's. Only their number leaves the function.
CREATE FUNCTION runsten_expire_account_exports(at timestamptz)
  RETURNS integer
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    WITH gone AS (DELETE FROM account_exports WHERE expires_at <= at RETURNING 1)
    SELECT count(*)::integer FROM gone
  $$;

-- The exports that may hold an archive, across the accounts: an archive of none is
-- thrown away. Only their ids leave the function.
CREATE FUNCTION runsten_account_exports_held()
  RETURNS SETOF uuid
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$ SELECT id FROM account_exports WHERE status <> 'failed' $$;

REVOKE ALL ON FUNCTION runsten_claim_account_export(timestamptz, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_claim_account_export(timestamptz, interval) TO runsten_app;
REVOKE ALL ON FUNCTION runsten_expire_account_exports(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_expire_account_exports(timestamptz) TO runsten_app;
REVOKE ALL ON FUNCTION runsten_account_exports_held() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_account_exports_held() TO runsten_app;
