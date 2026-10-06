-- The collector's latest pass over any vehicle, across the accounts, for runsten-api's
-- health report: whether the collector runs. Counts and a time only, nothing of an
-- account.
CREATE FUNCTION runsten_last_pass()
  RETURNS TABLE (vehicles bigint, passed_at timestamptz)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT (SELECT count(*) FROM vehicles), (SELECT max(s.passed_at) FROM collector_status s)
  $$;

REVOKE ALL ON FUNCTION runsten_last_pass() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_last_pass() TO runsten_app;
