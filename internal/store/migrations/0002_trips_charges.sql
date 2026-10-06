-- Trips and charges derived from the snapshots. They can be deleted and recomputed at
-- any time: the snapshots remain the only source of truth.
--
-- Times are bounds, not instants: polling only brackets a transition. A trip started
-- in [started_after, started_before] and ended in [ended_after, ended_before]. A
-- reconstructed event was not seen at all: it lies in [started_after, ended_before],
-- and both pairs are that same interval. detected_at, the first reading that revealed
-- the event, identifies it for its vehicle. A NULL value is unknown, never zero.

CREATE TABLE trips (
  id              uuid             NOT NULL DEFAULT gen_random_uuid(),
  account_id      uuid             NOT NULL,
  vehicle_id      uuid             NOT NULL,
  detected_at     timestamptz      NOT NULL,
  reconstructed   boolean          NOT NULL,
  started_after   timestamptz      NOT NULL,
  started_before  timestamptz      NOT NULL,
  ended_after     timestamptz      NOT NULL,
  ended_before    timestamptz      NOT NULL,
  start_odometer_km double precision,
  end_odometer_km   double precision,
  distance_km       double precision,
  start_soc         double precision,
  end_soc           double precision,
  start_range_km    double precision,
  end_range_km      double precision,
  energy_kwh        double precision, -- ΔSoC × battery capacity
  start_lat         double precision,
  start_lon         double precision,
  end_lat           double precision,
  end_lon           double precision,
  -- The vehicle's own figures at the end of the trip, for comparison only.
  trip_meter_km                double precision,
  consumption_kwh_per_100km    double precision,
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (vehicle_id, detected_at),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (started_after <= started_before AND started_before <= ended_before),
  CHECK (started_after <= ended_after AND ended_after <= ended_before),
  CHECK ((start_lat IS NULL) = (start_lon IS NULL) AND (end_lat IS NULL) = (end_lon IS NULL))
);

CREATE TABLE charges (
  id              uuid             NOT NULL DEFAULT gen_random_uuid(),
  account_id      uuid             NOT NULL,
  vehicle_id      uuid             NOT NULL,
  detected_at     timestamptz      NOT NULL,
  reconstructed   boolean          NOT NULL,
  started_after   timestamptz      NOT NULL,
  started_before  timestamptz      NOT NULL,
  ended_after     timestamptz      NOT NULL,
  ended_before    timestamptz      NOT NULL,
  charge_type     text             CHECK (charge_type IN ('AC', 'DC')),
  start_soc       double precision,
  end_soc         double precision,
  target_soc      double precision,
  -- Two estimates, kept apart: the API exposes no energy meter.
  energy_soc_kwh   double precision, -- ΔSoC × battery capacity
  energy_power_kwh double precision, -- integral of chargingPower over the observed readings
  lat             double precision,
  lon             double precision,
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (vehicle_id, detected_at),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (started_after <= started_before AND started_before <= ended_before),
  CHECK (started_after <= ended_after AND ended_after <= ended_before),
  CHECK ((lat IS NULL) = (lon IS NULL))
);

-- Where each vehicle's incremental derivation resumes: no event was in progress at
-- settled_at, and replaying the snapshots from replay_from rebuilds the state held then.
CREATE TABLE derivation_cursors (
  account_id  uuid        NOT NULL,
  vehicle_id  uuid        NOT NULL,
  settled_at  timestamptz NOT NULL,
  replay_from timestamptz NOT NULL,
  PRIMARY KEY (vehicle_id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (replay_from <= settled_at)
);

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['trips', 'charges', 'derivation_cursors'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY account_isolation ON %I TO runsten_app
                    USING (account_id = runsten_current_account())
                    WITH CHECK (account_id = runsten_current_account())', t);
  END LOOP;
END $$;

GRANT SELECT, INSERT, UPDATE, DELETE ON trips, charges, derivation_cursors TO runsten_app;
