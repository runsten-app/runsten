-- A connection of the Pleos Vehicle Data API (ADR 0049): the key the owner creates in
-- the Pleos Playground for their own cars, one per brand (Hyundai, Kia, Genesis), read
-- with the Brand header of the API's calls. Three providers, each with its own line,
-- its own refusal and its own last four characters, beside the other sources'. Its
-- credential is one sealed value, as the Data Portal's (connections.credential): the
-- client ID and the secret. In plaintext: the last four characters of the client ID,
-- which the owner finds again in the Playground, and when it was set.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet', 'bmw', 'pleos_hyundai', 'pleos_kia', 'pleos_genesis'));

-- The calls of each brand's key are counted apart: one API per key.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet', 'bmw', 'pleos'));

-- Another client reads with the same key, which the provider allows one of at a time
-- (Pleos keeps one token per key, which Home Assistant or evcc may take in turn).
ALTER TABLE collector_status
  DROP CONSTRAINT collector_status_fail_kind_check,
  ADD CONSTRAINT collector_status_fail_kind_check CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized',
    'not_found', 'unavailable', 'token', 'key_refused', 'market', 'credential_refused', 'unshared', 'not_granted',
    'api_closed', 'stream_held', 'key_shared', 'other'));
