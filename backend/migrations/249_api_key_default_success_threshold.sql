-- Change only the default for future API keys. Existing key settings, runtime
-- state versions, and historical routing facts must remain unchanged.
ALTER TABLE api_keys ALTER COLUMN routing_min_success_rate SET DEFAULT 80;
