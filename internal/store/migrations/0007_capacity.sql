-- The battery capacity each energy estimate rests on (ΔSoC × capacity), and where it
-- comes from: the net capacity of the vehicle's variant in the catalog (catalog_net), or
-- the one the vendor API reports (api). Both are NULL when no capacity was known, and
-- for the events derived before this migration, until a rebuild derives them again.

ALTER TABLE trips
  ADD COLUMN capacity_kwh    double precision CHECK (capacity_kwh > 0),
  ADD COLUMN capacity_source text CHECK (capacity_source IN ('catalog_net', 'api')),
  ADD CHECK ((capacity_kwh IS NULL) = (capacity_source IS NULL));

ALTER TABLE charges
  ADD COLUMN capacity_kwh    double precision CHECK (capacity_kwh > 0),
  ADD COLUMN capacity_source text CHECK (capacity_source IN ('catalog_net', 'api')),
  ADD CHECK ((capacity_kwh IS NULL) = (capacity_source IS NULL));
