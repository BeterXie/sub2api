-- Keep passive channel facts independent when two brands produce the same
-- minute/platform/group/model dimensions. Migration 270 added brand_id, but
-- the original primary keys still treated those dimensions as global.

ALTER TABLE channel_monitor_v2_metrics_1m
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_metrics_1m_pkey;
ALTER TABLE channel_monitor_v2_metrics_1m
    ADD PRIMARY KEY (brand_id, bucket_start, platform, group_id, model);

ALTER TABLE channel_monitor_v2_user_metrics_1m
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_user_metrics_1m_pkey;
ALTER TABLE channel_monitor_v2_user_metrics_1m
    ADD PRIMARY KEY (brand_id, bucket_start, platform, group_id, model, user_id);

ALTER TABLE channel_monitor_v2_error_metrics_1m
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_error_metrics_1m_pkey;
ALTER TABLE channel_monitor_v2_error_metrics_1m
    ADD PRIMARY KEY (brand_id, bucket_start, platform, group_id, model, error_category, taxonomy_version);

ALTER TABLE channel_monitor_v2_latency_histograms_1m
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_latency_histograms_1m_pkey;
ALTER TABLE channel_monitor_v2_latency_histograms_1m
    ADD PRIMARY KEY (brand_id, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms);

ALTER TABLE channel_monitor_v2_metrics_rollup
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_metrics_rollup_pkey;
ALTER TABLE channel_monitor_v2_metrics_rollup
    ADD PRIMARY KEY (brand_id, bucket_seconds, bucket_start, platform, group_id, model);

ALTER TABLE channel_monitor_v2_user_metrics_rollup
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_user_metrics_rollup_pkey;
ALTER TABLE channel_monitor_v2_user_metrics_rollup
    ADD PRIMARY KEY (brand_id, bucket_seconds, bucket_start, platform, group_id, model, user_id);

ALTER TABLE channel_monitor_v2_error_metrics_rollup
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_error_metrics_rollup_pkey;
ALTER TABLE channel_monitor_v2_error_metrics_rollup
    ADD PRIMARY KEY (brand_id, bucket_seconds, bucket_start, platform, group_id, model, error_category, taxonomy_version);

ALTER TABLE channel_monitor_v2_latency_histograms_rollup
    DROP CONSTRAINT IF EXISTS channel_monitor_v2_latency_histograms_rollup_pkey;
ALTER TABLE channel_monitor_v2_latency_histograms_rollup
    ADD PRIMARY KEY (brand_id, bucket_seconds, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms);
