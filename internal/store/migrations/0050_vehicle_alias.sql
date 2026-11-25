-- A vehicle's alias: the name its user gave it in Runsten, shown and published in place of
-- the label derived from its model. NULL: none. Trimmed and bounded by the API, checked
-- here too.
ALTER TABLE vehicles
  ADD COLUMN alias text CHECK (alias = btrim(alias) AND char_length(alias) BETWEEN 1 AND 40);
