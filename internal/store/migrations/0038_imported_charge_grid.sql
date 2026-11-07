-- The energy an imported charge drew from the grid, losses included, when its source
-- recorded it (a BMW's charging history, imported at the connection: ADR 0045 §5): read
-- as a derived charge's, the energy its place's tariff bills.
ALTER TABLE imported_charges
    ADD COLUMN energy_grid_kwh double precision CHECK (energy_grid_kwh >= 0);
