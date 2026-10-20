-- A connection of the Polestar Data Portal: the owner's own credential (an OAuth client,
-- its secret, and the account it reads), pasted by the user, one per account as a Volvo
-- ID. Its vehicles are read with an access token the collector asks for with it and keeps
-- in memory: no token is stored.
--
-- The credential is one value, sealed by the application, bound to the account and to
-- its column, as the MQTT broker's password. In plaintext: the last four characters of
-- its client ID and when it was set, to tell it apart and to warn before it expires (90
-- days).
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar')),
  ADD COLUMN credential            bytea,
  ADD COLUMN credential_set_at     timestamptz,
  ADD COLUMN credential_last4      text CHECK (char_length(credential_last4) <= 4),
  -- The Data Portal refused the credential (refused, expired, or an account ID not its
  -- own): its vehicles are not read until the user sets another.
  ADD COLUMN credential_refused_at timestamptz,
  ADD CONSTRAINT connections_credential_check CHECK (
    (credential IS NULL) = (credential_set_at IS NULL) AND (credential IS NULL) = (credential_last4 IS NULL)),
  ADD CONSTRAINT connections_credential_refused_check CHECK (credential IS NOT NULL OR credential_refused_at IS NULL),
  -- A Polestar connection is its credential alone; a Volvo connection never has one.
  ADD CONSTRAINT connections_credential_provider_check CHECK ((provider = 'polestar') = (credential IS NOT NULL)),
  ADD CONSTRAINT connections_polestar_check CHECK (provider = 'volvo' OR (access_token IS NULL AND
    refresh_token IS NULL AND api_key IS NULL AND reauth_at IS NULL)),
  DROP CONSTRAINT connections_useful_check,
  ADD CONSTRAINT connections_useful_check CHECK (access_token IS NOT NULL OR api_key IS NOT NULL OR credential IS NOT NULL);

-- A vehicle its owner no longer shares with the credential (AUTHZ_VIN_UNAUTHORIZED): the
-- collector marks it and no longer polls it; the vehicles listed again clear the mark.
ALTER TABLE vehicles ADD COLUMN unshared_at timestamptz;

ALTER TABLE collector_status
  DROP CONSTRAINT collector_status_fail_kind_check,
  ADD CONSTRAINT collector_status_fail_kind_check CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized',
    'not_found', 'unavailable', 'token', 'key_refused', 'market', 'credential_refused', 'unshared', 'not_granted',
    'other'));

-- The Data Portal's quota is counted per credential, over every domain: one API.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar'));

-- The collector reads each vehicle through its connection's provider: the targets tell
-- which, and, for a credential, when it was set (never the credential itself) and
-- whether it was refused.
DROP FUNCTION runsten_poll_targets();
CREATE FUNCTION runsten_poll_targets()
  RETURNS TABLE (account_id uuid, vehicle_id uuid, vin text, connection_id uuid, provider text, reauth_reason text,
                 api_key_set_at timestamptz, api_key_refused boolean, market_refused boolean,
                 credential_set_at timestamptz, credential_refused boolean, unshared boolean)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT v.account_id, v.id, v.vin, c.id, c.provider, c.reauth_reason, c.api_key_set_at,
      c.api_key_refused_at IS NOT NULL, v.market_refused_at IS NOT NULL, c.credential_set_at,
      c.credential_refused_at IS NOT NULL, v.unshared_at IS NOT NULL
    FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
    ORDER BY v.account_id, v.id
  $$;

REVOKE ALL ON FUNCTION runsten_poll_targets() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_poll_targets() TO runsten_app;
