-- Live Activities: a charge in progress shown on a phone's lock screen, which the
-- instance starts, updates and ends through the PushSender (notify.Notifier.Live).

-- The token the push service starts a charge's activity on the device with (an iOS
-- push-to-start token); NULL: none is started.
ALTER TABLE devices
  ADD COLUMN live_activity_token text CHECK (char_length(live_activity_token) BETWEEN 1 AND 4096);

-- An activity a device shows of a vehicle's charge, or was asked to: token NULL until the
-- device registers the activity's own, and once it dismissed it. One per device and
-- vehicle; forgotten once the charge is over, and with its device or its vehicle.
-- charge_id is the charge's ID as the API gives it, which sorts as its detection time.
CREATE TABLE live_activities (
  account_id uuid        NOT NULL,
  device_id  uuid        NOT NULL,
  vehicle_id uuid        NOT NULL,
  charge_id  text        NOT NULL CHECK (char_length(charge_id) BETWEEN 1 AND 64),
  token      text        CHECK (char_length(token) BETWEEN 1 AND 4096),
  state      jsonb,
  pushed_at  timestamptz,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, device_id, vehicle_id),
  FOREIGN KEY (account_id, device_id) REFERENCES devices (account_id, id) ON DELETE CASCADE,
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE
);
CREATE INDEX live_activities_by_vehicle ON live_activities (account_id, vehicle_id);

ALTER TABLE live_activities ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON live_activities TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON live_activities TO runsten_app;
