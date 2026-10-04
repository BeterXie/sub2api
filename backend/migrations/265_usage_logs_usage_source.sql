-- Empty preserves provider usage for existing rows. Prism estimates must remain
-- identifiable after account settings change or the account is deleted.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS usage_source VARCHAR(32) NOT NULL DEFAULT '';
