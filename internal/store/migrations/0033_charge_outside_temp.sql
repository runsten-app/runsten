-- The mean outside temperature a charge's readings told, in °C: a Tesla's, from its
-- climate. Null for a vehicle that does not tell it, a reconstructed or imported charge,
-- and the charges derived before it was kept, until the derived data is rebuilt.
ALTER TABLE charges ADD COLUMN outside_temp_c double precision;
