-- A connection of the Polestar ID (ADR 0042): the Polestar app's API, read with the
-- owner's Polestar ID, for the Polestars the Data Portal does not serve. One per account,
-- beside a Data Portal's credential. Its credential is one sealed value, as the Data
-- Portal's (connections.credential): the refresh token, which the Polestar ID rotates at
-- every refresh and the collector replaces with the one it gives, and in the password mode
-- the email and password that sign in again when it is refused. In plaintext: the mode,
-- the last four characters of the refresh token the owner's connection gave, and when.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id')),
  ADD COLUMN credential_mode text CHECK (credential_mode IN ('token', 'password')),
  ADD CONSTRAINT connections_polestar_id_mode_check CHECK (
    (credential_mode IS NOT NULL) = (provider = 'polestar_id' AND credential IS NOT NULL));

-- The calls of the Polestar app's API are counted per connection: one API.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id'));
