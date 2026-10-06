-- The application key of a connection (the vcc-api-key header): Volvo counts its quota
-- on the key's application, not on the OAuth client that issued the token. NULL: the
-- instance's key (RUNSTEN_VOLVO_API_KEY), shared by the connections without their own;
-- an instance without one reads only the connections that have their own.
--
-- The key is encrypted by the application, bound to the account, as the tokens. It may
-- be set before the Volvo ID is connected: the connection then has no token yet.
ALTER TABLE connections
  ALTER COLUMN access_token DROP NOT NULL,
  ADD COLUMN api_key            bytea,
  ADD COLUMN api_key_set_at     timestamptz,
  -- Volvo refused the key (not the token): its vehicles are not read until another key.
  ADD COLUMN api_key_refused_at timestamptz,
  ADD CONSTRAINT connections_api_key_check CHECK ((api_key IS NULL) = (api_key_set_at IS NULL)),
  ADD CONSTRAINT connections_api_key_refused_check CHECK (api_key IS NOT NULL OR api_key_refused_at IS NULL),
  ADD CONSTRAINT connections_useful_check CHECK (access_token IS NOT NULL OR api_key IS NOT NULL);

ALTER TABLE collector_status
  DROP CONSTRAINT collector_status_fail_kind_check,
  ADD CONSTRAINT collector_status_fail_kind_check CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized',
    'not_found', 'unavailable', 'token', 'key_refused', 'other'));

-- The collector budgets the quota per key: the targets tell whether their connection
-- has its own (when it was set, never the key itself) and whether it was refused.
DROP FUNCTION runsten_poll_targets();
CREATE FUNCTION runsten_poll_targets()
  RETURNS TABLE (account_id uuid, vehicle_id uuid, vin text, connection_id uuid, reauth_reason text,
                 api_key_set_at timestamptz, api_key_refused boolean)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT v.account_id, v.id, v.vin, c.id, c.reauth_reason, c.api_key_set_at, c.api_key_refused_at IS NOT NULL
    FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
    ORDER BY v.account_id, v.id
  $$;

REVOKE ALL ON FUNCTION runsten_poll_targets() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_poll_targets() TO runsten_app;
