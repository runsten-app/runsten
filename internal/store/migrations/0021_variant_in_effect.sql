-- The variant in effect of each vehicle (ADR 0022 §3): the one its user chose while it
-- holds, else the one recognized from its details. A copy, written by the collector after
-- each poll of the vehicle, for the readers that have only SQL; the API recomputes it on
-- every read and never reads this one. NULL: none, or not written yet.
ALTER TABLE vehicles ADD COLUMN variant_in_effect text CHECK (variant_in_effect <> '');
