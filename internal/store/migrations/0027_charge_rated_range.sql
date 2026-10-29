-- The range a vehicle displayed as a charge started and ended, and its usable level at
-- the end, read with the start and end SoC: a Tesla's rated range, a fixed conversion
-- of its usable level, which the battery page's rated estimate rests on. Null for a
-- vehicle without them, and for the charges derived before they were kept, until the
-- derived data is rebuilt.
ALTER TABLE charges
    ADD COLUMN start_range_km double precision,
    ADD COLUMN end_range_km double precision,
    ADD COLUMN end_usable_soc double precision;
