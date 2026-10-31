-- When the vehicle started installing a version of its software: after the last reading
-- without an installation in progress, before the first reading of one. Null when no
-- reading saw it installing, and for the versions derived before it was kept, until the
-- derived data is rebuilt. The installation ended within the version's own bounds.
ALTER TABLE software_versions
    ADD COLUMN install_started_after timestamptz,
    ADD COLUMN install_started_before timestamptz,
    ADD CHECK ((install_started_after IS NULL) = (install_started_before IS NULL)),
    ADD CHECK (install_started_after <= install_started_before AND install_started_before <= installed_after);
