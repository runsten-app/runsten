-- Initial collector schema: accounts, Volvo connections, vehicles, raw snapshots.
--
-- Isolation: the application runs as the runsten_app role, with no
-- bypass privilege. Row-Level Security restricts each transaction to the account set
-- by set_config('runsten.account_id', …, true). The table owner (the migrations user)
-- is not subject to it: this is what lets the SECURITY DEFINER functions below work
-- across all accounts.

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'runsten_app') THEN
    CREATE ROLE runsten_app NOLOGIN;
  END IF;
  -- By name, not TO CURRENT_USER: some managed PostgreSQL services refuse a role
  -- specifier in GRANT ROLE ("cannot use special role specifier").
  EXECUTE format('GRANT runsten_app TO %I', current_user);
END $$;

CREATE FUNCTION runsten_current_account() RETURNS uuid
  LANGUAGE sql STABLE
  AS $$ SELECT NULLIF(current_setting('runsten.account_id', true), '')::uuid $$;

CREATE TABLE accounts (
  id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at timestamptz NOT NULL DEFAULT now()
);

-- A connection = the OAuth authorization of one Volvo ID. Tokens are encrypted by
-- the application (AES-256-GCM, key kept outside the database).
CREATE TABLE connections (
  id            uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id    uuid        NOT NULL REFERENCES accounts ON DELETE CASCADE,
  provider      text        NOT NULL CHECK (provider IN ('volvo')),
  access_token  bytea       NOT NULL,
  refresh_token bytea,
  expires_at    timestamptz,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (account_id, provider)
);

CREATE TABLE vehicles (
  id            uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id    uuid        NOT NULL,
  connection_id uuid        NOT NULL,
  vin           text        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (account_id, vin),
  FOREIGN KEY (account_id, connection_id) REFERENCES connections (account_id, id) ON DELETE CASCADE
);

-- Raw responses, append-only: the source of truth. A response
-- identical to the previous one (same value_hash) only advances checked_at.
CREATE TABLE snapshots (
  account_id uuid        NOT NULL,
  vehicle_id uuid        NOT NULL,
  endpoint   text        NOT NULL,
  fetched_at timestamptz NOT NULL,
  checked_at timestamptz NOT NULL,
  value_hash bytea       NOT NULL,
  payload    jsonb       NOT NULL,
  PRIMARY KEY (vehicle_id, endpoint, fetched_at),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE
);

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['connections', 'vehicles', 'snapshots'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY account_isolation ON %I TO runsten_app
                    USING (account_id = runsten_current_account())
                    WITH CHECK (account_id = runsten_current_account())', t);
  END LOOP;
END $$;

ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON accounts TO runsten_app
  USING (id = runsten_current_account());

GRANT SELECT ON accounts TO runsten_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON connections, vehicles TO runsten_app;
GRANT SELECT, INSERT, UPDATE ON snapshots TO runsten_app;

-- System operations, outside any account: they go through fixed-scope
-- SECURITY DEFINER functions, never through an RLS bypass.

-- Creates an account and returns its ID.
CREATE FUNCTION runsten_create_account() RETURNS uuid
  LANGUAGE sql SECURITY DEFINER SET search_path = public
  AS $$ INSERT INTO accounts DEFAULT VALUES RETURNING id $$;

-- Self-hosting: the single account, created if it does not exist.
CREATE FUNCTION runsten_single_account() RETURNS uuid
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public
  AS $$
DECLARE n bigint; a uuid;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('runsten_single_account'));
  SELECT count(*), min(id::text)::uuid INTO n, a FROM accounts;
  IF n > 1 THEN
    RAISE EXCEPTION 'multiple accounts: multi-account instance';
  END IF;
  IF n = 0 THEN
    INSERT INTO accounts DEFAULT VALUES RETURNING id INTO a;
  END IF;
  RETURN a;
END $$;

-- Vehicles to poll, across all accounts, with the encrypted token of their
-- connection. The collector then writes within each account's scope.
CREATE FUNCTION runsten_poll_targets()
  RETURNS TABLE (account_id uuid, vehicle_id uuid, vin text, access_token bytea)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT v.account_id, v.id, v.vin, c.access_token
    FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
    ORDER BY v.account_id, v.id
  $$;

REVOKE ALL ON FUNCTION runsten_create_account(), runsten_single_account(), runsten_poll_targets() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_create_account(), runsten_single_account(), runsten_poll_targets() TO runsten_app;
