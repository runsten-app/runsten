-- Personal access tokens: a program reads the API with one on a user's behalf, without
-- a session. As a session, a token is known by the SHA-256 of its secret only: the
-- secret is shown once, when the token is issued. An expired token stays, for its user
-- to see why it no longer works, until they revoke it.

CREATE TABLE api_tokens (
  id           uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id   uuid        NOT NULL,
  user_id      uuid        NOT NULL,
  name         text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
  token_hash   bytea       NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  created_at   timestamptz NOT NULL,
  last_used_at timestamptz,          -- NULL: never used
  expires_at   timestamptz,          -- NULL: never expires
  PRIMARY KEY (id),
  FOREIGN KEY (account_id, user_id) REFERENCES users (account_id, id) ON DELETE CASCADE,
  CHECK (expires_at IS NULL OR created_at < expires_at)
);
CREATE INDEX api_tokens_by_user ON api_tokens (account_id, user_id, created_at DESC);

ALTER TABLE api_tokens ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON api_tokens TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON api_tokens TO runsten_app;

-- For each request with a token, the account is not known yet: as runsten_session, the
-- single row matching a hash, across accounts.
CREATE FUNCTION runsten_api_token(token bytea)
  RETURNS TABLE (id uuid, account_id uuid, user_id uuid, name text,
                 created_at timestamptz, last_used_at timestamptz, expires_at timestamptz)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT t.id, t.account_id, t.user_id, t.name, t.created_at, t.last_used_at, t.expires_at
    FROM api_tokens t WHERE t.token_hash = token
  $$;
REVOKE ALL ON FUNCTION runsten_api_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_api_token(bytea) TO runsten_app;
