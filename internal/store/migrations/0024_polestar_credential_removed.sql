-- The owner may remove their Polestar credential without losing their vehicles: the
-- connection stays without one, its vehicles and their history with it, and is no longer
-- read until a new credential is set. Deleting the connection would delete its vehicles.
ALTER TABLE connections
  DROP CONSTRAINT connections_credential_provider_check,
  ADD CONSTRAINT connections_credential_provider_check CHECK (provider = 'polestar' OR credential IS NULL),
  DROP CONSTRAINT connections_useful_check,
  ADD CONSTRAINT connections_useful_check CHECK (
    access_token IS NOT NULL OR api_key IS NOT NULL OR provider = 'polestar');
