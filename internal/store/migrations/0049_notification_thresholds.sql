-- The setting of a notification kind that has one: the low battery's threshold, in % of
-- the state of charge. vehicle_id NULL: the value for every vehicle; set: that vehicle's
-- own. A row may now hold a threshold alone, with no choice of on or off (enabled NULL):
-- the kind then follows its default, or the vehicles' default.
ALTER TABLE notification_preferences ALTER COLUMN enabled DROP NOT NULL;
ALTER TABLE notification_preferences
  ADD COLUMN threshold smallint CHECK (threshold BETWEEN 1 AND 100);
ALTER TABLE notification_preferences
  ADD CONSTRAINT notification_preferences_something CHECK (enabled IS NOT NULL OR threshold IS NOT NULL);
