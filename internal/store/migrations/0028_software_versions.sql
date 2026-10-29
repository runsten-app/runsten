-- The versions of a vehicle's software (a Tesla's), each from when it was first read:
-- derived from the snapshots as the trips and charges, deleted and recomputed with them.
-- detected_at, the first reading of the version, identifies it for its vehicle; the
-- vehicle took it after installed_after, the last reading of the previous version, NULL
-- for the first version Runsten read.

CREATE TABLE software_versions (
  account_id      uuid        NOT NULL,
  vehicle_id      uuid        NOT NULL,
  detected_at     timestamptz NOT NULL,
  version         text        NOT NULL CHECK (char_length(version) BETWEEN 1 AND 100),
  installed_after timestamptz CHECK (installed_after <= detected_at),
  PRIMARY KEY (vehicle_id, detected_at),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE
);

ALTER TABLE software_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON software_versions TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON software_versions TO runsten_app;
