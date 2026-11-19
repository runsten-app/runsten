-- A connection of the demo source: one simulated vehicle that the instance generates
-- itself, with no maker's API behind it. It has no secret, yet the collector reads a
-- connection only when its credential was set (credential_set_at), and the schema ties
-- that to a credential (connections_credential_check): the credential is a marker, a
-- sealed constant like every other source's value, so that nothing in the store
-- distinguishes it (the sealed values are re-sealed together when the key rotates). Its
-- last four characters read "demo".
ALTER TABLE connections
  DROP CONSTRAINT connections_provider_check,
  ADD CONSTRAINT connections_provider_check CHECK (provider IN ('volvo', 'polestar', 'tesla', 'polestar_id',
    'tesla_fleet', 'bmw', 'pleos_hyundai', 'pleos_kia', 'pleos_genesis', 'skoda', 'renault', 'demo'));

-- The demo source makes no call: its reads are counted as one API all the same, for the
-- collector's budget.
ALTER TABLE api_calls
  DROP CONSTRAINT api_calls_api_check,
  ADD CONSTRAINT api_calls_api_check CHECK (api IN ('connected-vehicle', 'energy', 'location', 'polestar', 'tesla',
    'polestar_id', 'tesla_fleet', 'bmw', 'pleos', 'skoda', 'renault', 'demo'));
