-- Cutover migration: all application instances must run brand-aware code before
-- independent brands accept registrations. No existing entity is recreated.
CREATE UNIQUE INDEX IF NOT EXISTS users_brand_email_active
    ON users(brand_id,LOWER(TRIM(email))) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS users_email_unique_active;
CREATE UNIQUE INDEX IF NOT EXISTS groups_brand_name_active
    ON groups(brand_id,name) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS groups_name_unique_active;
CREATE UNIQUE INDEX IF NOT EXISTS auth_identities_brand_subject
    ON auth_identities(brand_id,provider_type,provider_key,provider_subject);
DROP INDEX IF EXISTS auth_identities_provider_subject_key;
CREATE UNIQUE INDEX IF NOT EXISTS auth_identity_channels_brand_subject
    ON auth_identity_channels(brand_id,provider_type,provider_key,channel,channel_app_id,channel_subject);
DROP INDEX IF EXISTS auth_identity_channels_channel_key;
DROP INDEX IF EXISTS idx_idempotency_records_scope_key;
DROP INDEX IF EXISTS idempotencyrecord_scope_idempotency_key_hash;
CREATE UNIQUE INDEX IF NOT EXISTS idempotencyrecord_brand_id_scope_idempotency_key_hash
    ON idempotency_records(brand_id,scope,idempotency_key_hash);

DO $$
DECLARE r RECORD;
BEGIN
    -- Remove only confirmed single-column uniqueness for brand-owned codes.
    FOR r IN
        SELECT t.relname AS table_name,i.relname AS index_name,c.conname
        FROM pg_index x
        JOIN pg_class t ON t.oid=x.indrelid
        JOIN pg_class i ON i.oid=x.indexrelid
        JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=x.indkey[0]
        LEFT JOIN pg_constraint c ON c.conindid=x.indexrelid
        WHERE x.indisunique AND NOT x.indisprimary AND x.indnkeyatts=1
          AND ((t.relname IN ('redeem_codes','promo_codes') AND a.attname='code')
            OR (t.relname='user_attribute_definitions' AND a.attname='key'))
    LOOP
        IF r.conname IS NOT NULL THEN
            EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I',r.table_name,r.conname);
        ELSE
            EXECUTE format('DROP INDEX %I',r.index_name);
        END IF;
    END LOOP;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS redeem_codes_brand_code ON redeem_codes(brand_id,code);
CREATE UNIQUE INDEX IF NOT EXISTS promo_codes_brand_code ON promo_codes(brand_id,code);
CREATE UNIQUE INDEX IF NOT EXISTS user_attributes_brand_key_active
    ON user_attribute_definitions(brand_id,key) WHERE deleted_at IS NULL;
