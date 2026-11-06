-- A connection of BMW CarData (ADR 0045): the owner's client ID, created in the MyBMW or
-- MyMINI portal, and what its device flow gave. One per account: BMW allows one connection
-- to the stream per BMW account anyway. Its credential is one sealed value, as the Polestar
-- ID's (connections.credential): the client ID, the GCID (the stream's username), the
-- refresh token, which BMW rotates at every refresh and the collector replaces with the one
-- it gives, and the ID of Runsten's container. In plaintext: the last four characters of
-- the client ID, which the owner finds again in the portal, and when it was set.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet', 'bmw'));

-- The calls of CarData's API are counted per connection: one API.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet', 'bmw'));

-- Another client holds the account's only connection to the stream (Home Assistant, evcc).
ALTER TABLE collector_status
  DROP CONSTRAINT collector_status_fail_kind_check,
  ADD CONSTRAINT collector_status_fail_kind_check CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized',
    'not_found', 'unavailable', 'token', 'key_refused', 'market', 'credential_refused', 'unshared', 'not_granted',
    'api_closed', 'stream_held', 'other'));
