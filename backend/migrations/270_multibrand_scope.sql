-- Expansion: historical records retain their primary keys and belong to LLMP.
DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['users','groups','api_keys','auth_identities','auth_identity_channels','pending_auth_sessions','identity_adoption_decisions','redeem_codes','promo_codes','promo_code_usages','announcements','announcement_reads','user_subscriptions','user_allowed_groups','user_attribute_definitions','user_attribute_values','user_platform_quotas','subscription_plans','payment_orders','payment_provider_instances','payment_audit_logs','usage_logs','batch_image_jobs','batch_image_items','batch_image_events','composite_model_routes','idempotency_records','usage_cleanup_tasks','user_affiliates','user_affiliate_ledger','user_provider_default_grants','user_avatars','user_group_rate_multipliers','passkey_user_handles','passkey_credentials','billing_usage_entries','usage_billing_dedup','usage_billing_dedup_archive','deleted_api_key_audits','sora_generations','content_moderation_logs','audit_logs','prompt_audit_jobs','prompt_audit_events','ops_error_logs','ops_ingress_reject_aggregates','usage_dashboard_hourly_users','usage_dashboard_daily_users','usage_group_daily_rollups','pelican_showcase_items','pelican_group_test_plans','pelican_group_test_results','pelican_group_test_daily_costs','channel_groups','channel_monitor_v3_categories','channel_monitor_v3_components','channel_monitor_v2_metrics_1m','channel_monitor_v2_user_metrics_1m','channel_monitor_v2_error_metrics_1m','channel_monitor_v2_latency_histograms_1m','channel_monitor_v2_metrics_rollup','channel_monitor_v2_user_metrics_rollup','channel_monitor_v2_error_metrics_rollup','channel_monitor_v2_latency_histograms_rollup','channel_monitor_v2_candy_results','request_timing_details','brand_settings','brand_pages','brand_page_revisions'] LOOP
        IF to_regclass('public.' || t) IS NOT NULL THEN
            EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS brand_id BIGINT NOT NULL DEFAULT 1 REFERENCES brands(id)', t);
            EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I(brand_id)', 'idx_' || t || '_brand', t);
        END IF;
    END LOOP;
END $$;

-- Inserts from internal workers derive ownership from immutable parent records.
-- Request writes also validate the connection's trusted tenant scope.
CREATE OR REPLACE FUNCTION sub2api_enforce_brand() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
    scoped BIGINT := COALESCE(NULLIF(current_setting('sub2api.brand_id',true),'')::BIGINT,0);
    owner_brand BIGINT;
    parent_brand BIGINT;
    ref RECORD;
    child_value TEXT;
	child_id BIGINT;
    payload JSONB := to_jsonb(NEW);
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.brand_id <> OLD.brand_id THEN
        RAISE EXCEPTION 'brand ownership is immutable' USING ERRCODE='23514';
    END IF;
    FOR ref IN SELECT * FROM (VALUES
        ('user_id','users','id'),('used_by','users','id'),('inviter_id','users','id'),
        ('source_user_id','users','id'),('target_user_id','users','id'),
        ('group_id','groups','id'),('fallback_group_id','groups','id'),
        ('fallback_group_id_on_invalid_request','groups','id'),
        ('subscription_group_id','groups','id'),
        ('api_key_id','api_keys','id'),('promo_code_id','promo_codes','id'),
        ('announcement_id','announcements','id'),('identity_id','auth_identities','id'),
        ('pending_auth_session_id','pending_auth_sessions','id'),
        ('subscription_id','user_subscriptions','id'),('attribute_id','user_attribute_definitions','id'),
        ('order_id','payment_orders','id'),('usage_log_id','usage_logs','id'),
        ('job_id','batch_image_jobs','id'),('page_id','brand_pages','id'),
        ('plan_id','subscription_plans','id'),('provider_instance_id','payment_provider_instances','id'),
        ('invited_by','users','id')
    ) AS refs(child,parent,parent_key) LOOP
        child_value := payload->>ref.child;
        IF child_value IS NULL OR child_value = '' OR child_value = '0' THEN CONTINUE; END IF;
        IF ref.child = 'plan_id' AND TG_TABLE_NAME LIKE 'pelican_group_test_%' THEN ref.parent := 'pelican_group_test_plans'; END IF;
        IF ref.child = 'job_id' AND TG_TABLE_NAME LIKE 'prompt_audit_%' THEN ref.parent := 'prompt_audit_jobs'; END IF;
		child_id := child_value::BIGINT;
        EXECUTE format('SELECT brand_id FROM public.%I WHERE %I=$1',ref.parent,ref.parent_key)
            INTO parent_brand USING child_id;
        IF parent_brand IS NULL THEN CONTINUE; END IF;
        IF owner_brand IS NOT NULL AND owner_brand <> parent_brand THEN
            RAISE EXCEPTION 'cross-brand parent reference' USING ERRCODE='23514';
        END IF;
        owner_brand := parent_brand;
    END LOOP;
    -- Also validate declared foreign keys so newly added references cannot
    -- silently bypass ownership validation.
    FOR ref IN
		SELECT ca.attname AS child, pt.relname AS parent, pa.attname AS parent_key,
			format_type(pa.atttypid,pa.atttypmod) AS parent_type
        FROM pg_constraint fk
        JOIN pg_class pt ON pt.oid=fk.confrelid
        JOIN pg_namespace pn ON pn.oid=pt.relnamespace
        JOIN pg_attribute ca ON ca.attrelid=fk.conrelid AND ca.attnum=fk.conkey[1]
        JOIN pg_attribute pa ON pa.attrelid=fk.confrelid AND pa.attnum=fk.confkey[1]
        WHERE fk.conrelid=TG_RELID AND fk.contype='f'
          AND cardinality(fk.conkey)=1 AND pn.nspname='public'
          AND ca.attname NOT IN ('brand_id','actor_user_id','admin_user_id','created_by')
		  AND ca.attname NOT IN (
			'user_id','used_by','inviter_id','source_user_id','target_user_id',
			'group_id','fallback_group_id','fallback_group_id_on_invalid_request',
			'subscription_group_id','api_key_id','promo_code_id','announcement_id',
			'identity_id','pending_auth_session_id','subscription_id','attribute_id',
			'order_id','usage_log_id','job_id','page_id','plan_id','provider_instance_id','invited_by'
		  )
          AND EXISTS(SELECT 1 FROM pg_attribute ba WHERE ba.attrelid=pt.oid AND ba.attname='brand_id' AND NOT ba.attisdropped)
    LOOP
        child_value := payload->>ref.child;
        IF child_value IS NULL OR child_value = '' OR child_value = '0' THEN CONTINUE; END IF;
		EXECUTE format('SELECT brand_id FROM public.%I WHERE %I=$1::%s',ref.parent,ref.parent_key,ref.parent_type)
            INTO parent_brand USING child_value;
        IF parent_brand IS NOT NULL THEN
            IF owner_brand IS NOT NULL AND owner_brand <> parent_brand THEN
                RAISE EXCEPTION 'cross-brand parent reference' USING ERRCODE='23514';
            END IF;
            owner_brand := parent_brand;
        END IF;
    END LOOP;
    IF scoped > 0 AND owner_brand IS NOT NULL AND scoped <> owner_brand THEN
        RAISE EXCEPTION 'cross-brand write' USING ERRCODE='23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        NEW.brand_id := COALESCE(NULLIF(scoped,0),owner_brand,NEW.brand_id,1);
    ELSIF owner_brand IS NOT NULL AND NEW.brand_id <> owner_brand THEN
        RAISE EXCEPTION 'cross-brand parent reference' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;

DO $$
DECLARE
	t TEXT;
	part RECORD;
BEGIN
    FOREACH t IN ARRAY ARRAY['users','groups','api_keys','auth_identities','auth_identity_channels','pending_auth_sessions','identity_adoption_decisions','redeem_codes','promo_codes','promo_code_usages','announcements','announcement_reads','user_subscriptions','user_allowed_groups','user_attribute_definitions','user_attribute_values','user_platform_quotas','subscription_plans','payment_orders','payment_provider_instances','payment_audit_logs','usage_logs','batch_image_jobs','batch_image_items','batch_image_events','composite_model_routes','idempotency_records','usage_cleanup_tasks','user_affiliates','user_affiliate_ledger','user_provider_default_grants','user_avatars','user_group_rate_multipliers','passkey_user_handles','passkey_credentials','billing_usage_entries','usage_billing_dedup','usage_billing_dedup_archive','deleted_api_key_audits','sora_generations','content_moderation_logs','audit_logs','prompt_audit_jobs','prompt_audit_events','ops_error_logs','ops_ingress_reject_aggregates','usage_dashboard_hourly_users','usage_dashboard_daily_users','usage_group_daily_rollups','pelican_showcase_items','pelican_group_test_plans','pelican_group_test_results','pelican_group_test_daily_costs','channel_groups','channel_monitor_v3_categories','channel_monitor_v3_components','channel_monitor_v2_metrics_1m','channel_monitor_v2_user_metrics_1m','channel_monitor_v2_error_metrics_1m','channel_monitor_v2_latency_histograms_1m','channel_monitor_v2_metrics_rollup','channel_monitor_v2_user_metrics_rollup','channel_monitor_v2_error_metrics_rollup','channel_monitor_v2_latency_histograms_rollup','channel_monitor_v2_candy_results','request_timing_details','brand_settings','brand_pages','brand_page_revisions'] LOOP
        IF to_regclass('public.' || t) IS NOT NULL THEN
            EXECUTE format('DROP TRIGGER IF EXISTS enforce_brand ON %I',t);
            EXECUTE format('CREATE TRIGGER enforce_brand BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION sub2api_enforce_brand()',t);
            EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
            EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
            EXECUTE format('DROP POLICY IF EXISTS tenant_scope ON %I',t);
            EXECUTE format('CREATE POLICY tenant_scope ON %I USING (current_user <> ''sub2api_brand_runtime'' OR brand_id = COALESCE(NULLIF(current_setting(''sub2api.brand_id'',true),'''')::BIGINT,0)) WITH CHECK (current_user <> ''sub2api_brand_runtime'' OR brand_id = COALESCE(NULLIF(current_setting(''sub2api.brand_id'',true),'''')::BIGINT,0))',t);
        END IF;
    END LOOP;

	-- Partition policies are independent from the parent policy. Existing
	-- usage_logs partitions must be secured explicitly; row triggers are
	-- normally cloned from the parent, but are added when an older PostgreSQL
	-- layout does not have the clone.
	FOR part IN
		SELECT n.nspname AS schema_name,c.relname AS table_name,c.oid AS table_oid
		FROM pg_inherits i
		JOIN pg_class c ON c.oid=i.inhrelid
		JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE i.inhparent=to_regclass('public.usage_logs')
	LOOP
		EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY',part.schema_name,part.table_name);
		EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY',part.schema_name,part.table_name);
		EXECUTE format('DROP POLICY IF EXISTS tenant_scope ON %I.%I',part.schema_name,part.table_name);
		EXECUTE format('CREATE POLICY tenant_scope ON %I.%I USING (current_user <> ''sub2api_brand_runtime'' OR brand_id = COALESCE(NULLIF(current_setting(''sub2api.brand_id'',true),'''')::BIGINT,0)) WITH CHECK (current_user <> ''sub2api_brand_runtime'' OR brand_id = COALESCE(NULLIF(current_setting(''sub2api.brand_id'',true),'''')::BIGINT,0))',part.schema_name,part.table_name);
		IF NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid=part.table_oid AND tgname='enforce_brand' AND tgenabled='O') THEN
			EXECUTE format('CREATE TRIGGER enforce_brand BEFORE INSERT OR UPDATE ON %I.%I FOR EACH ROW EXECUTE FUNCTION sub2api_enforce_brand()',part.schema_name,part.table_name);
		END IF;
	END LOOP;
END $$;

-- A non-login role enforces policies even when the migration login is a
-- superuser. Restricted managed databases can provision this role separately.
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolcreaterole)) THEN
        IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='sub2api_brand_runtime') THEN
            CREATE ROLE sub2api_brand_runtime NOLOGIN NOSUPERUSER NOBYPASSRLS;
        END IF;
        EXECUTE format('GRANT sub2api_brand_runtime TO %I',current_user);
        GRANT USAGE ON SCHEMA public TO sub2api_brand_runtime;
        GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO sub2api_brand_runtime;
        GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO sub2api_brand_runtime;
        REVOKE INSERT,UPDATE,DELETE ON brands,domains,brand_admins,security_secrets,settings FROM sub2api_brand_runtime;
    END IF;
END $$;
