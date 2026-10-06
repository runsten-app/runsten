-- What a later estimate of the battery capacity rests on: for each charge, the stretch
-- of its power integral where the energy and the SoC change were read at the same
-- readings (the SoC at both ends, the integrated energy, the widest gap between two
-- of its readings, and the time between its first and last reading), and the odometer
-- at the reading that ended the charge. The span stops at the SoC above which the
-- power goes into balancing the cells without raising it, so the energy and the SoC
-- change still match. A charge the readings report no power for (a reconstructed one,
-- or a vehicle that reports none) has no span: the five columns are NULL together. The
-- events derived before this migration have neither span nor odometer, until a rebuild
-- derives them again.

ALTER TABLE charges
  ADD COLUMN span_start_soc  double precision CHECK (span_start_soc BETWEEN 0 AND 100),
  ADD COLUMN span_end_soc    double precision CHECK (span_end_soc BETWEEN 0 AND 100),
  ADD COLUMN span_energy_kwh double precision CHECK (span_energy_kwh >= 0),
  ADD COLUMN span_max_gap_s  double precision CHECK (span_max_gap_s >= 0),
  ADD COLUMN span_duration_s double precision CHECK (span_duration_s >= 0),
  ADD COLUMN odometer_km     double precision CHECK (odometer_km >= 0),
  ADD CHECK ((span_start_soc IS NULL) = (span_end_soc IS NULL)
    AND (span_start_soc IS NULL) = (span_energy_kwh IS NULL)
    AND (span_start_soc IS NULL) = (span_max_gap_s IS NULL)
    AND (span_start_soc IS NULL) = (span_duration_s IS NULL));
