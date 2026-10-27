-- A connection of Tesla's Owner API: the owner's refresh token, pasted by the user, one
-- per account as a Volvo ID or a Polestar credential. It is kept as the Polestar
-- credential is (connections.credential, sealed, its last four characters and when it
-- was set in plaintext). A refresh may give a new refresh token in exchange: the
-- collector replaces the one kept with it, the time it was set and its last four left as
-- the owner set them. The access token is never stored.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla')),
  DROP CONSTRAINT connections_credential_provider_check,
  ADD CONSTRAINT connections_credential_provider_check CHECK (provider <> 'volvo' OR credential IS NULL),
  DROP CONSTRAINT connections_useful_check,
  ADD CONSTRAINT connections_useful_check CHECK (
    access_token IS NOT NULL OR api_key IS NOT NULL OR provider <> 'volvo');

-- The Owner API's calls are counted per refresh token: one API. The Owner API closed to
-- the account (a 403 sending to the Fleet API) is a failure of its own.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla'));

ALTER TABLE collector_status
  DROP CONSTRAINT collector_status_fail_kind_check,
  ADD CONSTRAINT collector_status_fail_kind_check CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized',
    'not_found', 'unavailable', 'token', 'key_refused', 'market', 'credential_refused', 'unshared', 'not_granted',
    'api_closed', 'other'));
