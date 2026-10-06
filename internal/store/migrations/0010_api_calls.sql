-- The calls the collector made to the Volvo API, per account, API and hour. The
-- accounts share the application's quota: after a restart, the collector's budget
-- starts from the calls of the last 24 hours, and the accounts that called the least
-- lately still go first. Kept 30 days (store.callsKept).
CREATE TABLE api_calls (
  account_id uuid        NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
  api        text        NOT NULL CHECK (api IN ('connected-vehicle', 'energy', 'location')),
  hour       timestamptz NOT NULL CHECK (extract(epoch FROM hour)::numeric % 3600 = 0), -- start of the hour
  calls      integer     NOT NULL CHECK (calls > 0),
  PRIMARY KEY (account_id, api, hour)
);

ALTER TABLE api_calls ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON api_calls TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON api_calls TO runsten_app;

-- The calls of every account since a time, for the budget of the shared quota: only
-- identifiers and counts leave the function.
CREATE FUNCTION runsten_api_calls(since timestamptz)
  RETURNS TABLE (account_id uuid, api text, hour timestamptz, calls integer)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT c.account_id, c.api, c.hour, c.calls FROM api_calls c
    WHERE c.hour >= since
    ORDER BY c.hour, c.account_id, c.api
  $$;

REVOKE ALL ON FUNCTION runsten_api_calls(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_api_calls(timestamptz) TO runsten_app;
