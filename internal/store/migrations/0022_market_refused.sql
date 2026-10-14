-- A vehicle whose market the Volvo Cars API does not serve: every endpoint refuses it,
-- the Energy API naming the market. The collector marks it and no longer polls it; the
-- vehicles listed again (a new authorization of the Volvo ID, a new key) clear the mark.
ALTER TABLE vehicles ADD COLUMN market_refused_at timestamptz;

ALTER TABLE collector_status
  DROP CONSTRAINT collector_status_fail_kind_check,
  ADD CONSTRAINT collector_status_fail_kind_check CHECK (fail_kind IN ('quota', 'rate_limited', 'unauthorized',
    'not_found', 'unavailable', 'token', 'key_refused', 'market', 'other'));

DROP FUNCTION runsten_poll_targets();
CREATE FUNCTION runsten_poll_targets()
  RETURNS TABLE (account_id uuid, vehicle_id uuid, vin text, connection_id uuid, reauth_reason text,
                 api_key_set_at timestamptz, api_key_refused boolean, market_refused boolean)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT v.account_id, v.id, v.vin, c.id, c.reauth_reason, c.api_key_set_at, c.api_key_refused_at IS NOT NULL,
      v.market_refused_at IS NOT NULL
    FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
    ORDER BY v.account_id, v.id
  $$;

REVOKE ALL ON FUNCTION runsten_poll_targets() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_poll_targets() TO runsten_app;
