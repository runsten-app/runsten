-- A connection of the My Renault app's API (ADR 0050): the owner's sign-in to their My
-- Renault account, which reads its Renaults, Dacias and Alpines. Its credential is one
-- sealed value, as the other sources' (connections.credential): the login token Gigya
-- gave, the device it was given to, the person and the account's country. No password:
-- it crosses the request that signs in, never stored. In plaintext: the last four
-- characters of the person's id, and when the owner signed in.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet', 'bmw', 'pleos_hyundai', 'pleos_kia', 'pleos_genesis', 'skoda', 'renault'));

-- Kamereon's quota is the account's: one API.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet', 'bmw', 'pleos', 'skoda', 'renault'));
