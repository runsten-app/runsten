-- The mean outside temperature a trip's readings told, in °C: a Tesla's, from its
-- climate. Null for a vehicle that does not tell it, a reconstructed or imported trip,
-- and the trips derived before it was kept, until the derived data is rebuilt.
ALTER TABLE trips ADD COLUMN outside_temp_c double precision;
