-- The Pleos source now reads reservedCharging with charge.charging true as an active
-- charge, where it read it as scheduled: a charge a schedule started was derived as
-- reconstructed pieces. Without their cursor, the next pass of the collector derives
-- the history of the vehicles it reads again, as one charge each.
--
-- Two cases keep the pieces: a vehicle the collector does not read (its key refused:
-- no reading, no derivation), until it reads it again; and a pass of a collector of
-- the previous version that read a cursor before this migration and saved it after.
-- `runsten-collector rebuild`, once every collector runs this version, derives them
-- again whatever their cursor.

DELETE FROM derivation_cursors
WHERE vehicle_id IN (
  SELECT v.id FROM vehicles v
  JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
  WHERE c.provider LIKE 'pleos\_%'
);
