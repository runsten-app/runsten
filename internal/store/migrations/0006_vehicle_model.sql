-- What the user says of a vehicle's model, where the recognition from its details falls
-- short. The recognized variant is not stored: it is recomputed on each read, so that a
-- correction of the catalog shows without a migration.
--
-- variant_id names a variant of the catalog embedded in the binary: no foreign key. A
-- variant the catalog no longer has, or one of another family than the vehicle reports,
-- is ignored on reading, and the recognition applies again.
--
-- ac_max_kw is the onboard charger the user states, when the variant has an optional
-- one; without it, the costs take the more powerful (an upper bound). One that is not a
-- charger of the variant in effect is ignored on reading.
ALTER TABLE vehicles
  ADD COLUMN variant_id text             CHECK (variant_id <> ''),
  ADD COLUMN ac_max_kw  double precision CHECK (ac_max_kw > 0);
