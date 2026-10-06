-- The last four characters of a connection's own key, in plaintext: enough to tell two
-- keys apart, for a reader without the key that seals it (RUNSTEN_TOKEN_KEY), as a
-- hosted back office. Written and cleared with the key. NULL for a key set before this
-- migration, until it is set again: SQL cannot open it.
ALTER TABLE connections
  ADD COLUMN api_key_last4 text CHECK (char_length(api_key_last4) <= 4),
  ADD CONSTRAINT connections_api_key_last4_key_check CHECK (api_key IS NOT NULL OR api_key_last4 IS NULL);
