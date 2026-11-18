-- The Pleos source now reads reservedCharging with charge.charging true as an active
-- charge, where it read it as scheduled: a charge a schedule started was derived as
-- reconstructed pieces. Without their cursor, the next pass of the collector derives
-- the history of the vehicles it reads again, as one charge each.

DELETE FROM derivation_cursors
WHERE vehicle_id IN (
  SELECT v.id FROM vehicles v
  JOIN connections c ON c.account_id = v.account_id AND c.id = v.connection_id
  WHERE c.provider LIKE 'pleos\_%'
);
