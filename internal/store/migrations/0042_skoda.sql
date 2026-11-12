-- A connection of the MyŠkoda Public API: the key the owner creates in the MyŠkoda app,
-- bound to the vehicles they ticked. The API lists no vehicle: the owner gives the VIN of
-- each car with the key, and the vehicles are the connection's. Its credential is one
-- sealed value, as the other sources' (connections.credential): the key. In plaintext:
-- its last four characters, which the owner finds again in the app, and when it was set.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet', 'bmw', 'pleos_hyundai', 'pleos_kia', 'pleos_genesis', 'skoda')),
  -- When the credential expires, as its provider tells it (Škoda's X-API-Key-Expires-At,
  -- on every response): the Connection page warns before. Null: not told. It goes with
  -- the credential, never without one.
  ADD COLUMN credential_expires_at timestamptz,
  ADD CONSTRAINT connections_credential_expires_check CHECK (credential IS NOT NULL OR credential_expires_at IS NULL);

-- Škoda counts the calls per VIN and per hour: one API, the headers its authority.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet', 'bmw', 'pleos', 'skoda'));
