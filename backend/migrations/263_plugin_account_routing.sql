ALTER TABLE sub2api_plugin_installations
    ADD COLUMN IF NOT EXISTS account_routing JSONB;

COMMENT ON COLUMN sub2api_plugin_installations.account_routing IS
    'Plugin-validated account scope stored atomically with config; NULL uses the capability binding, an empty account_ids list routes nobody.';
