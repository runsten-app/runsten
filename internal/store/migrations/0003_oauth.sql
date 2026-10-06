-- OAuth lifecycle of a connection: grant start, refresh token age, loss of the grant.
--
-- A connection whose grant is lost (refresh refused, refresh token unused for too
-- long) keeps its row but carries reauth_at and reauth_reason: the collector stops
-- polling its vehicles until the user authorizes again, which resets both columns.

ALTER TABLE connections
  ADD COLUMN authorized_at timestamptz, -- grant start (user consent); NULL for a token pasted by hand
  ADD COLUMN refreshed_at  timestamptz, -- issue time of the current refresh token
  ADD COLUMN reauth_at     timestamptz,
  ADD COLUMN reauth_reason text,
  ADD CONSTRAINT connections_reauth_check CHECK ((reauth_at IS NULL) = (reauth_reason IS NULL));

-- The collector no longer reads the tokens here: it asks for them per connection, and
-- skips the connections that require re-authentication.
DROP FUNCTION runsten_poll_targets();
CREATE FUNCTION runsten_poll_targets()
  RETURNS TABLE (account_id uuid, vehicle_id uuid, vin text, connection_id uuid, reauth_reason text)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT v.account_id, v.id, v.vin, c.id, c.reauth_reason
    FROM vehicles v JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
    ORDER BY v.account_id, v.id
  $$;

-- Refreshable connections, across all accounts, whose refresh token was issued before
-- a given time: the collector refreshes them so that they do not lapse (keep-alive).
-- Only identifiers leave the function; the tokens are then read within each account.
CREATE FUNCTION runsten_stale_connections(issued_before timestamptz)
  RETURNS TABLE (account_id uuid, connection_id uuid)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT c.account_id, c.id FROM connections c
    WHERE c.reauth_at IS NULL AND c.refresh_token IS NOT NULL AND c.refreshed_at < issued_before
    ORDER BY c.account_id, c.id
  $$;

REVOKE ALL ON FUNCTION runsten_poll_targets(), runsten_stale_connections(timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_poll_targets(), runsten_stale_connections(timestamptz) TO runsten_app;
