-- The highest current a charge's readings told, in A, and the AC phases of the latest one
-- with a current: a Tesla's and a Polestar's. Null for a vehicle that does not tell them,
-- a reconstructed or imported charge, and the charges derived before they were kept,
-- until the derived data is rebuilt.
ALTER TABLE charges ADD COLUMN max_current_a double precision, ADD COLUMN phases smallint;
