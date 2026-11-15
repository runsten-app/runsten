-- What a trip was made for: business, commute or personal. The user's writing, as an
-- entered cost is: a rebuild never touches it.

-- A place's rule: the trips of its scope (those ending there, or those starting or ending
-- there) have its purpose, unless marked by hand. Both or neither.
ALTER TABLE places
  ADD COLUMN trip_purpose text CHECK (trip_purpose IN ('business', 'commute', 'personal')),
  ADD COLUMN trip_purpose_scope text CHECK (trip_purpose_scope IN ('end', 'either')),
  ADD CONSTRAINT places_trip_purpose_rule_check CHECK ((trip_purpose IS NULL) = (trip_purpose_scope IS NULL));

-- The marks set by hand. purpose NULL: the user said the trip has none, which no rule
-- overrides. A derived trip is named by its detection time, as an entered cost names its
-- charge, without a foreign key: a derivation deletes and inserts the trips again. Its
-- window when it was marked finds it again should a change of the detection move its
-- detected_at. An imported trip is named by its source and its ID there.
CREATE TABLE trip_marks (
  id               uuid        NOT NULL DEFAULT gen_random_uuid(),
  account_id       uuid        NOT NULL,
  vehicle_id       uuid        NOT NULL,
  trip_detected_at timestamptz,
  window_after     timestamptz,
  window_before    timestamptz,
  source           text,
  source_id        text,
  purpose          text        CHECK (purpose IN ('business', 'commute', 'personal')),
  marked_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  -- A derived trip's mark, or an imported one's.
  CHECK ((trip_detected_at IS NOT NULL AND window_after IS NOT NULL AND window_before IS NOT NULL
          AND window_after <= window_before AND source IS NULL AND source_id IS NULL)
      OR (trip_detected_at IS NULL AND window_after IS NULL AND window_before IS NULL
          AND source IS NOT NULL AND source_id IS NOT NULL))
);
CREATE UNIQUE INDEX trip_marks_derived ON trip_marks (vehicle_id, trip_detected_at) WHERE trip_detected_at IS NOT NULL;
CREATE UNIQUE INDEX trip_marks_imported ON trip_marks (vehicle_id, source, source_id) WHERE source IS NOT NULL;

ALTER TABLE trip_marks ENABLE ROW LEVEL SECURITY;
CREATE POLICY account_isolation ON trip_marks TO runsten_app
  USING (account_id = runsten_current_account())
  WITH CHECK (account_id = runsten_current_account());
GRANT SELECT, INSERT, UPDATE, DELETE ON trip_marks TO runsten_app;
