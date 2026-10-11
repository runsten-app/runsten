-- The imports the browser tests read (scripts/compose-test.sh, task e2e): a trip and a
-- charge imported from Aximote, and one trip for each Playwright project to delete, as
-- an importer would write them: a month and three days before the vehicle's first
-- snapshot, always in an earlier month, so that neither a list "since yesterday" nor the
-- statistics of this month hold them. Neither cost nor position for the charge: the
-- costs' tests price the DC charge alone, this one's cost stays unknown. Run as the
-- database's superuser, again on each run: what a project deleted comes back, the rest
-- stays as it is.
INSERT INTO imported_trips (account_id, vehicle_id, source, source_id, started_at, ended_at,
  distance_km, start_soc, end_soc, energy_kwh, start_address, end_address)
SELECT v.account_id, v.id, s.source, 'e2e-trip', f.at - interval '1 month 3 days', f.at - interval '1 month 3 days' + interval '35 minutes',
  24.5, 80, 72, 5.1, 'Place Bellecour, Lyon', 'Part-Dieu, Lyon'
FROM vehicles v
CROSS JOIN LATERAL (SELECT min(fetched_at) AS at FROM snapshots WHERE vehicle_id = v.id) f
CROSS JOIN (VALUES ('aximote'), ('e2e_desktop_light'), ('e2e_desktop_dark'), ('e2e_mobile_light'), ('e2e_mobile_dark')) s (source)
WHERE f.at IS NOT NULL
ON CONFLICT (vehicle_id, source, source_id) DO NOTHING;

INSERT INTO imported_charges (account_id, vehicle_id, source, source_id, started_at, ended_at,
  charge_type, start_soc, end_soc, energy_kwh, energy_soc_kwh, capacity_kwh, capacity_source, average_power_kw)
SELECT v.account_id, v.id, 'aximote', 'e2e-charge', f.at - interval '1 month 3 days' + interval '1 hour',
  f.at - interval '1 month 3 days' + interval '3 hours', 'AC', 72, 90, 12.1, 11.5, 64, 'api', 6.5
FROM vehicles v
CROSS JOIN LATERAL (SELECT min(fetched_at) AS at FROM snapshots WHERE vehicle_id = v.id) f
WHERE f.at IS NOT NULL
ON CONFLICT (vehicle_id, source, source_id) DO NOTHING;
