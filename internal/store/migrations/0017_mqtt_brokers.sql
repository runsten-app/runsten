-- The accounts that have an MQTT broker, and when its configuration changed, for the
-- collector: identifiers only. It reads the configuration and the password within each
-- account.
CREATE FUNCTION runsten_mqtt_brokers()
  RETURNS TABLE (account_id uuid, updated_at timestamptz)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT b.account_id, b.updated_at FROM mqtt_brokers b ORDER BY b.account_id
  $$;

REVOKE ALL ON FUNCTION runsten_mqtt_brokers() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runsten_mqtt_brokers() TO runsten_app;
