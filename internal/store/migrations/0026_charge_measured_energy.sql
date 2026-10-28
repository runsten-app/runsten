-- The energy a vehicle measured into its battery over a charge, from its own counter
-- (Tesla's charge_energy_added), beside the two estimates: null for a vehicle without
-- one, and for the charges derived before it was kept, until the derived data is rebuilt.
ALTER TABLE charges ADD COLUMN energy_measured_kwh double precision;
