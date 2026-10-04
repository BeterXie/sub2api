ALTER TABLE accounts DROP CONSTRAINT IF EXISTS chk_accounts_quota_dimension;
ALTER TABLE accounts ADD CONSTRAINT chk_accounts_quota_dimension
    CHECK (quota_dimension IN ('global', 'spark', 'prism'));

CREATE UNIQUE INDEX IF NOT EXISTS uq_accounts_prism_shadow_per_parent
    ON accounts (parent_account_id)
    WHERE parent_account_id IS NOT NULL AND quota_dimension = 'prism' AND deleted_at IS NULL;
