-- A connection of Tesla's Fleet API: the owner's refresh token of an application the
-- instance's operator registered at Tesla, given by the owner's sign-in. The core reads
-- none of its vehicles itself: a source an extension registers does. It is kept as the
-- Owner API's refresh token is (connections.credential, sealed), one per account beside
-- it: a car moves from one to the other with its history.
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet'));

-- The calls of the Fleet API are counted per connection: one API.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet'));
