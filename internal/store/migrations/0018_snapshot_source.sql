-- The source of each snapshot (ADR 0035): readings may come from elsewhere than the
-- Volvo Cars API, which wrote every row so far. No default: each writer names its source.
ALTER TABLE snapshots ADD COLUMN source text;
UPDATE snapshots SET source = 'volvo';
ALTER TABLE snapshots
  ALTER COLUMN source SET NOT NULL,
  ADD CONSTRAINT snapshots_source_check CHECK (source <> ''),
  DROP CONSTRAINT snapshots_pkey,
  ADD PRIMARY KEY (vehicle_id, source, endpoint, fetched_at);
