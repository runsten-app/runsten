-- The writes an access token may make besides reading: none, it only reads, as every
-- token issued before; 'trips:mark', it may mark trips with their purpose.

ALTER TABLE api_tokens
  ADD COLUMN scopes text[] NOT NULL DEFAULT '{}' CHECK (scopes <@ ARRAY['trips:mark']);

-- Its result gains the scopes: dropped and created again, as a function's result type
-- cannot change.
DROP FUNCTION runsten_api_token(bytea);
CREATE FUNCTION runsten_api_token(token bytea)
  RETURNS TABLE (id uuid, account_id uuid, user_id uuid, name text,
                 created_at timestamptz, last_used_at timestamptz, expires_at timestamptz, scopes text[])
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT t.id, t.account_id, t.user_id, t.name, t.created_at, t.last_used_at, t.expires_at, t.scopes
    FROM api_tokens t WHERE t.token_hash = token
  $$;
REVOKE ALL ON FUNCTION runsten_api_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_api_token(bytea) TO runsten_app;
