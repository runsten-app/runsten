-- Users of runsten-api and their sessions, and the indexes of the event lists.
--
-- A user belongs to an account; for now, an account has a single user (the constraint
-- users_one_per_account goes when accounts get several users). A session is known by
-- the SHA-256 of its token only: the token itself stays in the browser's cookie.

CREATE TABLE users (
  id                  uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id          uuid        NOT NULL REFERENCES accounts ON DELETE CASCADE,
  username            text        NOT NULL, -- lowercase, normalized by the application
  password_hash       text        NOT NULL, -- argon2id, PHC string
  created_at          timestamptz NOT NULL DEFAULT now(),
  password_changed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  CONSTRAINT users_username_unique UNIQUE (username),
  CONSTRAINT users_one_per_account UNIQUE (account_id),
  CHECK (username = lower(username) AND username <> '')
);

CREATE TABLE sessions (
  id           uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id   uuid        NOT NULL,
  user_id      uuid        NOT NULL,
  token_hash   bytea       NOT NULL UNIQUE,
  created_at   timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  expires_at   timestamptz NOT NULL, -- absolute; idleness is checked by the application
  PRIMARY KEY (id),
  FOREIGN KEY (account_id, user_id) REFERENCES users (account_id, id) ON DELETE CASCADE,
  CHECK (created_at <= last_seen_at AND created_at < expires_at),
  CHECK (length(token_hash) = 32)
);
CREATE INDEX sessions_by_user ON sessions (account_id, user_id);

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['users', 'sessions'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY account_isolation ON %I TO runsten_app
                    USING (account_id = runsten_current_account())
                    WITH CHECK (account_id = runsten_current_account())', t);
  END LOOP;
END $$;

GRANT SELECT, INSERT, UPDATE, DELETE ON users, sessions TO runsten_app;

-- Before a login, and for each request's session, the account is not known yet: these
-- functions find the single row matching a username or a token hash, across accounts.
-- The application then works within that account.

CREATE FUNCTION runsten_user_by_name(name text)
  RETURNS TABLE (id uuid, account_id uuid, username text, password_hash text)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$ SELECT u.id, u.account_id, u.username, u.password_hash FROM users u WHERE u.username = name $$;

CREATE FUNCTION runsten_session(token bytea)
  RETURNS TABLE (id uuid, account_id uuid, user_id uuid, username text,
                 created_at timestamptz, last_seen_at timestamptz, expires_at timestamptz)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT s.id, s.account_id, s.user_id, u.username, s.created_at, s.last_seen_at, s.expires_at
    FROM sessions s JOIN users u ON u.account_id = s.account_id AND u.id = s.user_id
    WHERE s.token_hash = token
  $$;

-- Whether any user exists: runsten-api tells at startup how to create the first one.
CREATE FUNCTION runsten_has_users() RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$ SELECT EXISTS (SELECT FROM users) $$;

REVOKE ALL ON FUNCTION runsten_user_by_name(text), runsten_session(bytea), runsten_has_users() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_user_by_name(text), runsten_session(bytea), runsten_has_users() TO runsten_app;

-- The API lists events newest first, a page at a time (keyset pagination).
CREATE INDEX trips_newest_first ON trips (vehicle_id, started_after DESC, detected_at DESC);
CREATE INDEX charges_newest_first ON charges (vehicle_id, started_after DESC, detected_at DESC);
