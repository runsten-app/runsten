-- The calls are counted per provider too: an account may read one API through several
-- connections, a Pleos key per brand (pleos_hyundai, pleos_kia, pleos_genesis), each
-- with its own quota, and a restarted collector gives each its own calls back. The
-- calls saved before are left without one: the collector gives them to the API's first
-- provider the account has, as it did. The providers are those of connections, which
-- its check lists: none is repeated here.
ALTER TABLE api_calls
  ADD COLUMN provider text NOT NULL DEFAULT '',
  DROP CONSTRAINT api_calls_pkey,
  ADD PRIMARY KEY (account_id, api, provider, hour);

DROP FUNCTION runsten_api_calls(timestamptz);
CREATE FUNCTION runsten_api_calls(since timestamptz)
  RETURNS TABLE (account_id uuid, api text, provider text, hour timestamptz, calls integer)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT c.account_id, c.api, c.provider, c.hour, c.calls FROM api_calls c
    WHERE c.hour >= since
    ORDER BY c.hour, c.account_id, c.api, c.provider
  $$;

REVOKE ALL ON FUNCTION runsten_api_calls(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_api_calls(timestamptz) TO runsten_app;
