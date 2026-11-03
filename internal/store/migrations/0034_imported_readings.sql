-- What an imported event keeps beyond its summary, when the application read it (TeslaMate):
-- a trip's range at its start and end and its mean outside temperature; a charge's range
-- at its start and end, its usable level at the end and the energy the vehicle measured
-- into its battery, which the battery page's rated estimates rest on, as for a derived
-- charge; its highest current and its phases, its mean outside temperature, and the
-- odometer where it charged. Null for a source that tells none of them.
ALTER TABLE imported_trips
    ADD COLUMN start_range_km double precision CHECK (start_range_km >= 0),
    ADD COLUMN end_range_km double precision CHECK (end_range_km >= 0),
    ADD COLUMN outside_temp_c double precision;
ALTER TABLE imported_charges
    ADD COLUMN start_range_km double precision CHECK (start_range_km >= 0),
    ADD COLUMN end_range_km double precision CHECK (end_range_km >= 0),
    ADD COLUMN end_usable_soc double precision CHECK (end_usable_soc BETWEEN 0 AND 100),
    ADD COLUMN energy_measured_kwh double precision CHECK (energy_measured_kwh >= 0),
    ADD COLUMN max_current_a double precision CHECK (max_current_a >= 0),
    ADD COLUMN phases smallint CHECK (phases > 0),
    ADD COLUMN outside_temp_c double precision,
    ADD COLUMN odometer_km double precision CHECK (odometer_km >= 0);
