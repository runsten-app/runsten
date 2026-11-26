-- A connection of FordConnect Query: the owner's application on Ford's developer portal
-- and the sign-in they made with it, which reads one car. Its credential is one sealed
-- value, as the other sources' (connections.credential): the application's client ID and
-- secret, the redirect address the sign-in was asked with, and the refresh token, which
-- the collector replaces with the one a refresh gives. In plaintext: the last four
-- characters of the client ID, which the owner finds again in the portal, when they
-- connected, and when the grant ends (credential_expires_at: a year after the sign-in).
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet', 'bmw', 'pleos_hyundai', 'pleos_kia', 'pleos_genesis', 'skoda', 'renault', 'demo', 'ford'));

-- Ford publishes no quota: the calls are counted per connection, one API.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet', 'bmw', 'pleos', 'skoda', 'renault', 'demo', 'ford'));
