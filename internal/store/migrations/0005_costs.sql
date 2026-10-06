-- What the cost of a charge is computed from, on each read: the account's currency, its
-- places with their dated tariffs, and the costs the user entered. Nothing here is
-- derived: a rebuild never touches these tables.
--
-- A place's position is its owner's home, most often: it never goes to the logs or the
-- errors, and it goes with the place.

CREATE TABLE account_settings (
  account_id uuid NOT NULL REFERENCES accounts ON DELETE CASCADE,
  -- ISO 4217. The accepted ones, with their minor digits, are the application's list.
  currency   text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  PRIMARY KEY (account_id)
);

CREATE TABLE places (
  id               uuid             NOT NULL DEFAULT gen_random_uuid(),
  account_id       uuid             NOT NULL REFERENCES accounts ON DELETE CASCADE,
  name             text             NOT NULL CHECK (name <> ''),
  lat              double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
  lon              double precision NOT NULL CHECK (lon BETWEEN -180 AND 180),
  radius_m         double precision NOT NULL CHECK (radius_m > 0),
  time_zone        text             NOT NULL CHECK (time_zone <> ''), -- IANA name
  -- Also takes the AC charges, and those of unknown type, that have no position.
  without_position boolean          NOT NULL,
  max_power_kw     double precision CHECK (max_power_kw > 0),
  efficiency       double precision CHECK (efficiency > 0 AND efficiency <= 1),
  -- The order of the places, which decides between two at the same distance.
  created_at       timestamptz      NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (account_id, id)
);
CREATE UNIQUE INDEX places_one_without_position ON places (account_id) WHERE without_position;

-- One row per version of a place's tariff, valid from the start of valid_from (a day in
-- the place's zone, not an instant) until the next version.
--
-- windows is a JSON array, read and written with its version, tried in order (the last
-- that holds an instant gives its price):
--   {"days": [0-6], "from": 0-1439, "to": 0-1439, "months": [1-12], "price_per_kwh": n}
-- days are those the window opens on, 0 being Sunday (Go's time.Weekday and PostgreSQL's
-- dow); from and to are minutes past local midnight, to before from crossing midnight
-- and to equal to from lasting a full day; months are those of the instant. An empty
-- days or months means every one.
CREATE TABLE place_tariffs (
  account_id    uuid          NOT NULL,
  place_id      uuid          NOT NULL,
  valid_from    date          NOT NULL,
  price_per_kwh numeric(10,5) NOT NULL CHECK (price_per_kwh >= 0), -- outside any window
  windows       jsonb         NOT NULL DEFAULT '[]',
  PRIMARY KEY (place_id, valid_from),
  FOREIGN KEY (account_id, place_id) REFERENCES places (account_id, id) ON DELETE CASCADE,
  CHECK (jsonb_typeof(windows) = 'array'),
  CHECK (NOT jsonb_path_exists(windows,
    '$[*] ? (@.from < 0 || @.from > 1439 || @.to < 0 || @.to > 1439 || @.price_per_kwh < 0
             || exists(@.days[*] ? (@ < 0 || @ > 6)) || exists(@.months[*] ? (@ < 1 || @ > 12)))'))
);

-- The costs entered by the user. A charge is named by its detection time, the identity
-- of a charge across rebuilds, without a foreign key: a derivation deletes and inserts
-- the charges again, and would take the entered cost with it. The charge's window when
-- the cost was entered finds the charge again should a change of the detection move its
-- detected_at.
CREATE TABLE charge_costs (
  id                 uuid             NOT NULL DEFAULT gen_random_uuid(),
  account_id         uuid             NOT NULL,
  vehicle_id         uuid             NOT NULL,
  charge_detected_at timestamptz      NOT NULL,
  window_after       timestamptz      NOT NULL,
  window_before      timestamptz      NOT NULL,
  amount_minor       bigint           NOT NULL CHECK (amount_minor >= 0), -- 0 is free
  energy_kwh         double precision CHECK (energy_kwh >= 0), -- billed, from the receipt
  note               text             NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
  entered_at         timestamptz      NOT NULL DEFAULT now(),
  PRIMARY KEY (id),
  UNIQUE (account_id, id),
  UNIQUE (vehicle_id, charge_detected_at),
  FOREIGN KEY (account_id, vehicle_id) REFERENCES vehicles (account_id, id) ON DELETE CASCADE,
  CHECK (window_after <= window_before)
);

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['account_settings', 'places', 'place_tariffs', 'charge_costs'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY account_isolation ON %I TO runsten_app
                    USING (account_id = runsten_current_account())
                    WITH CHECK (account_id = runsten_current_account())', t);
  END LOOP;
END $$;

GRANT SELECT, INSERT, UPDATE, DELETE ON account_settings, places, place_tariffs, charge_costs TO runsten_app;
