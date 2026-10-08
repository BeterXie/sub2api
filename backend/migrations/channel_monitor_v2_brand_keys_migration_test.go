package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV2BrandKeysMigrationCoversAllFactTables(t *testing.T) {
	content, err := FS.ReadFile("274_channel_monitor_v2_brand_keys.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	keys := map[string]string{
		"channel_monitor_v2_metrics_1m":                "(brand_id, bucket_start, platform, group_id, model)",
		"channel_monitor_v2_user_metrics_1m":           "(brand_id, bucket_start, platform, group_id, model, user_id)",
		"channel_monitor_v2_error_metrics_1m":          "(brand_id, bucket_start, platform, group_id, model, error_category, taxonomy_version)",
		"channel_monitor_v2_latency_histograms_1m":     "(brand_id, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms)",
		"channel_monitor_v2_metrics_rollup":            "(brand_id, bucket_seconds, bucket_start, platform, group_id, model)",
		"channel_monitor_v2_user_metrics_rollup":       "(brand_id, bucket_seconds, bucket_start, platform, group_id, model, user_id)",
		"channel_monitor_v2_error_metrics_rollup":      "(brand_id, bucket_seconds, bucket_start, platform, group_id, model, error_category, taxonomy_version)",
		"channel_monitor_v2_latency_histograms_rollup": "(brand_id, bucket_seconds, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms)",
	}
	for table, key := range keys {
		t.Run(table, func(t *testing.T) {
			require.Contains(t, sql, "alter table "+table)
			require.Contains(t, sql, "add primary key "+key)
		})
	}
}
