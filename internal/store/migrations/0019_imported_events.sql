-- Trips and charges imported from another application (ADR 0030): summaries it already
-- cut, never readings. Not derived: a rebuild never touches these tables. Each row names
-- its source, in the vocabulary of snapshots.source (ADR 0035), and its ID there: a new
-- import of the same event replaces the row, keeping its id. Times are the source's own,
-- exact; values in SI units, NULL when the source gives none. Only the events that start
-- before the vehicle's first snapshot are written (store.SaveImport): they never overlap
-- a derived one.
--
-- The sources of the derived events too: those of the readings they were derived from.
-- Every snapshot so far is the Volvo Cars API's.

ALTER TABLE trips ADD COLUMN sources text[];
UPDATE trips SET sources = '{volvo}';
ALTER TABLE trips ALTER COLUMN sources SET NOT NULL, ADD CHECK (cardinality(sources) > 0);

ALTER TABLE charges ADD COLUMN sources text[];
UPDATE charges SET sources = '{volvo}';
ALTER TABLE charges ALTER COLUMN sources SET NOT NULL, ADD CHECK (cardinality(sources) > 0);

CREATE TABLE imported_trips (
  id                uuid             NOT NULL DEFAULT gen_random_uuid(),
  account_id        uuid             NOT NULL,
  vehicle_id        uuid             NOT NULL,
  source            text             NOT NULL CHECK (source ~ '^[a-z][a-z0-9_]{0,31}$'),
  -- URL-unreserved characters: the API's ID of the event is source:source_id.
  source_id         text             NOT NULL CHECK (source_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
  imported_at       timestamptz      NOT NULL DEFAULT now(),
  started_at        timestamptz      NOT NULL,
  ended_at          timestamptz      NOT NULL,
  distance_km       double precision CHECK (distance_km >= 0),
  start_odometer_km double precision CHECK (start_odometer_km >= 0),
  end_odometer_km   double precision CHECK (end_odometer_km >= 0),
  start_soc         double precision CHECK (start_soc BETWEEN 0 AND 100),
  end_soc           double precision CHECK (end_soc BETWEEN 0 AND 100),
  -- The source's figure, or else ΔSoC × capacity_kwh, estimated by the import.
  energy_kwh        double precision,
  capacity_kwh      double precision CHECK (capacity_kwh > 0),
  capacity_source   text             CHECK (capacity_source IN ('catalog_net', 'api')),
  start_lat         double precision CHECK (start_lat BETWEEN -90 AND 90),
  start_lon         double precision CHECK (start_lon BETWEEN -180 AND 180),
  end_lat           double precision CHECK (end_lat BETWEEN -90 AND 90),
  end_lon           double precision CHECK (end_lon BETWEEN -180 AND 180),
  -- The source's names of the places, shown where neither a place nor the instance's
  -- geocoder names the position.
  start_address     text             CHECK (char_length(start_address) BETWEEN 1 AND 500),
  end_address       text             CHECK (char_length(end_address) BETWEEN 1 AND 500),
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (vehicle_id, source, source_id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (started_at <= ended_at),
  CHECK ((capacity_kwh IS NULL) = (capacity_source IS NULL)),
  CHECK ((start_lat IS NULL) = (start_lon IS NULL) AND (end_lat IS NULL) = (end_lon IS NULL))
);
CREATE INDEX imported_trips_started ON imported_trips (vehicle_id, started_at);

CREATE TABLE imported_charges (
  id               uuid             NOT NULL DEFAULT gen_random_uuid(),
  account_id       uuid             NOT NULL,
  vehicle_id       uuid             NOT NULL,
  source           text             NOT NULL CHECK (source ~ '^[a-z][a-z0-9_]{0,31}$'),
  source_id        text             NOT NULL CHECK (source_id ~ '^[A-Za-z0-9._~-]{1,128}$'),
  imported_at      timestamptz      NOT NULL DEFAULT now(),
  started_at       timestamptz      NOT NULL,
  ended_at         timestamptz      NOT NULL,
  charge_type      text             CHECK (charge_type IN ('AC', 'DC')),
  start_soc        double precision CHECK (start_soc BETWEEN 0 AND 100),
  end_soc          double precision CHECK (end_soc BETWEEN 0 AND 100),
  -- The energy the source gives, a measured flow rather than a change of SoC; the
  -- API's energy_power_kwh.
  energy_kwh       double precision CHECK (energy_kwh >= 0),
  -- ΔSoC × capacity_kwh, estimated by the import as the derivation does.
  energy_soc_kwh   double precision,
  capacity_kwh     double precision CHECK (capacity_kwh > 0),
  capacity_source  text             CHECK (capacity_source IN ('catalog_net', 'api')),
  average_power_kw double precision CHECK (average_power_kw >= 0),
  -- What the source says the charge cost, in minor units of its currency (ISO 4217).
  cost_minor       bigint           CHECK (cost_minor >= 0),
  cost_currency    text             CHECK (cost_currency ~ '^[A-Z]{3}$'),
  lat              double precision CHECK (lat BETWEEN -90 AND 90),
  lon              double precision CHECK (lon BETWEEN -180 AND 180),
  address          text             CHECK (char_length(address) BETWEEN 1 AND 500),
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (vehicle_id, source, source_id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (started_at <= ended_at),
  CHECK ((capacity_kwh IS NULL) = (capacity_source IS NULL)),
  CHECK ((cost_minor IS NULL) = (cost_currency IS NULL)),
  CHECK ((lat IS NULL) = (lon IS NULL))
);
CREATE INDEX imported_charges_started ON imported_charges (vehicle_id, started_at);

-- The track of an imported trip, when the source gives one: a GeoJSON LineString. Apart
-- from the trip, read on demand only; deleted with it.
CREATE TABLE imported_tracks (
  account_id uuid  NOT NULL,
  trip_id    uuid  NOT NULL,
  track      jsonb NOT NULL,
  PRIMARY KEY (trip_id),
  FOREIGN KEY (account_id, trip_id) REFERENCES imported_trips (account_id, id) ON DELETE CASCADE,
  CHECK (track ->> 'type' = 'LineString' AND jsonb_typeof(track -> 'coordinates') = 'array')
);

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['imported_trips', 'imported_charges', 'imported_tracks'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY account_isolation ON %I TO runsten_app
                    USING (account_id = runsten_current_account())
                    WITH CHECK (account_id = runsten_current_account())', t);
  END LOOP;
END $$;

GRANT SELECT, INSERT, UPDATE, DELETE ON imported_trips, imported_charges, imported_tracks TO runsten_app;
