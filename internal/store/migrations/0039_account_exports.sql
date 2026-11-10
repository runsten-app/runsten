-- The ZIP archive of an account's whole history, asked for in the interface and built
-- by runsten-api's export worker, out of the request: GET /account/export streams the
-- same document, but of an account that outgrows the request a month of readings alone
-- can do (1 GB), the worker builds it in the background and keeps it a few days, to be
-- downloaded, a download cut short resumed where it stopped. At most one export per
-- account: the one asked for, being built or held.
--
-- The archive is kept in parts (account_export_parts), rows of the account like any
-- other: row-level security isolates them, the account's deletion takes them, backup
-- and restore carry them, and each is written or read in a transaction of its own, so
-- that neither a long build nor a slow download holds a connection or a snapshot.
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
  -- When the archive held goes, its parts with it.
  expires_at timestamptz,
  PRIMARY KEY (id),
  UNIQUE (id, account_id),
  CHECK ((status IN ('done', 'failed')) = (ended_at IS NOT NULL)),
  CHECK ((status = 'failed') = (error IS NOT NULL)),
  CHECK ((status = 'pending') = (claimed_at IS NULL)),
  CHECK ((claimed_at IS NULL) = (renewed_at IS NULL)),
  CHECK (status <> 'done' OR bytes > 0),
  CHECK ((status = 'done') = (expires_at IS NOT NULL))
);

-- The archive's bytes, in parts written in order: a part starts where the previous one
-- ends. A ZIP archive does not compress again: its parts are stored as they are.
CREATE TABLE account_export_parts (
  account_id uuid   NOT NULL,
  export_id  uuid   NOT NULL,
  -- The part's first byte in the archive.
  start      bigint NOT NULL CHECK (start >= 0),
  data       bytea  NOT NULL CHECK (octet_length(data) > 0),
  PRIMARY KEY (export_id, start),
  FOREIGN KEY (export_id, account_id) REFERENCES account_exports (id, account_id) ON DELETE CASCADE
);
ALTER TABLE account_export_parts ALTER COLUMN data SET STORAGE EXTERNAL;

ALTER TABLE account_exports ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON account_exports TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
ALTER TABLE account_export_parts ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON account_export_parts TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON account_exports, account_export_parts TO runsten_app;

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

-- Throws away the archives held past their time, across the accounts: the export goes,
-- its parts with it. Only their number leaves the function.
CREATE FUNCTION runsten_expire_account_exports(at timestamptz)
  RETURNS integer
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    WITH gone AS (DELETE FROM account_exports WHERE expires_at <= at RETURNING 1)
    SELECT count(*)::integer FROM gone
  $$;

REVOKE ALL ON FUNCTION runsten_claim_account_export(timestamptz, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_claim_account_export(timestamptz, interval) TO runsten_app;
REVOKE ALL ON FUNCTION runsten_expire_account_exports(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_expire_account_exports(timestamptz) TO runsten_app;
