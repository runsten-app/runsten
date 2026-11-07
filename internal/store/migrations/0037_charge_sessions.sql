-- The charging sessions a vehicle's maker records, read beside the readings (a BMW's
-- chargingHistory, ADR 0045 §5): the derivation writes them from the stored responses, a
-- session by its start, a later response replacing what an earlier one said. A charge
-- takes the energy drawn from the grid of the sessions whose middle falls within its
-- window: the energy its tariff bills, losses included, which no efficiency estimates.
CREATE TABLE charge_sessions (
  account_id      uuid             NOT NULL,
  vehicle_id      uuid             NOT NULL,
  source          text             NOT NULL CHECK (source ~ '^[a-z][a-z0-9_]{0,31}$'),
  started_at      timestamptz      NOT NULL,
  ended_at        timestamptz      NOT NULL,
  energy_grid_kwh double precision CHECK (energy_grid_kwh >= 0),
  start_soc       double precision CHECK (start_soc BETWEEN 0 AND 100),
  end_soc         double precision CHECK (end_soc BETWEEN 0 AND 100),
  lat             double precision CHECK (lat BETWEEN -90 AND 90),
  lon             double precision CHECK (lon BETWEEN -180 AND 180),
  odometer_km     double precision CHECK (odometer_km >= 0),
  PRIMARY KEY (vehicle_id, source, started_at),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (started_at <= ended_at),
  CHECK ((lat IS NULL) = (lon IS NULL))
);

ALTER TABLE charge_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON charge_sessions TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());

GRANT SELECT, INSERT, UPDATE, DELETE ON charge_sessions TO runsten_app;
