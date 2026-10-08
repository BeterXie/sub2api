package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type brandSettingRepository struct {
	service.SettingRepository
	store *brand.Store
}

func ProvideBrandSettingRepository(client *ent.Client, store *brand.Store) service.SettingRepository {
	return &brandSettingRepository{SettingRepository: NewSettingRepository(client), store: store}
}
func settingString(v any) string {
	if text, ok := v.(string); ok {
		return text
	}
	if flag, ok := v.(bool); ok {
		return fmt.Sprint(flag)
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
func (r *brandSettingRepository) overlay(ctx context.Context, values map[string]string) (map[string]string, error) {
	scope, ok := brand.FromContext(ctx)
	if !ok || scope.Platform {
		return values, nil
	}
	if scope.Hostname == "" {
		resolved, err := r.store.ScopeForBrand(ctx, scope.ID)
		if err != nil {
			return nil, err
		}
		scope = resolved
	}
	settings, err := r.store.Settings(ctx, scope)
	if err != nil {
		return nil, err
	}
	for k, v := range settings {
		values[k] = settingString(v)
	}
	if scope.ID != brand.LegacyID {
		values["_brand_isolated"] = "true"
		for key := range values {
			if brand.NotificationSetting(key) {
				delete(values, key)
			}
		}
		for k, v := range values {
			if strings.HasSuffix(k, "redirect_url") || k == "balance_low_notify_recharge_url" {
				if u, err := url.Parse(v); err == nil && u.IsAbs() {
					u.Scheme = "https"
					u.Host = scope.Hostname
					values[k] = u.String()
				}
			}
		}
	}
	return values, nil
}
func (r *brandSettingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	values, err := r.SettingRepository.GetMultiple(ctx, keys)
	if err != nil {
		return nil, err
	}
	all, err := r.overlay(ctx, values)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	if scope, ok := brand.FromContext(ctx); ok && !scope.Platform {
		for _, key := range keys {
			if !brand.NotificationSetting(key) {
				continue
			}
			var raw []byte
			err := r.store.DB.QueryRowContext(ctx, "SELECT value_json FROM brand_settings WHERE brand_id=$1 AND key=$2", scope.ID, key).Scan(&raw)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return nil, err
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			if value == nil {
				delete(out, key)
				continue
			}
			out[key] = settingString(value)
		}
	}
	if all["_brand_isolated"] == "true" {
		out["_brand_isolated"] = "true"
	}
	return out, nil
}
func (r *brandSettingRepository) GetValue(ctx context.Context, key string) (string, error) {
	values, err := r.GetMultiple(ctx, []string{key})
	if err != nil {
		return "", err
	}
	value, ok := values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}
func (r *brandSettingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	scope, scoped := brand.FromContext(ctx)
	if !scoped || scope.Platform {
		return r.SettingRepository.Get(ctx, key)
	}
	value, err := r.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	base, _ := r.SettingRepository.Get(ctx, key)
	result := &service.Setting{Key: key, Value: value}
	if base != nil {
		result.ID = base.ID
		result.UpdatedAt = base.UpdatedAt
	}
	var updated time.Time
	err = r.store.DB.QueryRowContext(ctx, `SELECT GREATEST(
		(SELECT MAX(updated_at) FROM brand_settings WHERE brand_id=$1),
		(SELECT updated_at FROM brands WHERE id=$1),
		(SELECT updated_at FROM domains WHERE id=$2))`, scope.ID, scope.DomainID).Scan(&updated)
	if err != nil {
		return nil, err
	}
	if updated.After(result.UpdatedAt) {
		result.UpdatedAt = updated
	}
	return result, nil
}
func (r *brandSettingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	values, err := r.SettingRepository.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return r.overlay(ctx, values)
}
func (r *brandSettingRepository) Set(ctx context.Context, key, value string) error {
	scope, ok := brand.FromContext(ctx)
	if !ok || scope.Platform {
		return r.SettingRepository.Set(ctx, key, value)
	}
	return r.SetMultiple(ctx, map[string]string{key: value})
}
func (r *brandSettingRepository) SetMultiple(ctx context.Context, values map[string]string) error {
	scope, ok := brand.FromContext(ctx)
	if !ok || scope.Platform {
		return r.SettingRepository.SetMultiple(ctx, values)
	}
	overrides := map[string]any{}
	for k, v := range values {
		if brand.NotificationSetting(k) {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if _, err := r.store.DB.ExecContext(ctx, `INSERT INTO brand_settings(brand_id,key,value_json) VALUES($1,$2,$3)
				ON CONFLICT(brand_id,key) DO UPDATE SET value_json=EXCLUDED.value_json,updated_at=NOW()`, scope.ID, k, string(raw)); err != nil {
				return err
			}
			continue
		}
		if !brand.AllowedSetting(k) {
			return fmt.Errorf("platform setting %q requires platform permission", k)
		}
		overrides[k] = v
	}
	return r.store.SaveSettings(ctx, scope.ID, overrides)
}
func (r *brandSettingRepository) Delete(ctx context.Context, key string) error {
	scope, ok := brand.FromContext(ctx)
	if !ok || scope.Platform {
		return r.SettingRepository.Delete(ctx, key)
	}
	if !brand.AllowedSetting(key) && !brand.NotificationSetting(key) {
		return fmt.Errorf("platform setting requires platform permission")
	}
	if brand.NotificationSetting(key) {
		_, err := r.store.DB.ExecContext(ctx, `INSERT INTO brand_settings(brand_id,key,value_json) VALUES($1,$2,'null'::jsonb)
			ON CONFLICT(brand_id,key) DO UPDATE SET value_json='null'::jsonb,updated_at=NOW()`, scope.ID, key)
		return err
	}
	_, err := r.store.DB.ExecContext(ctx, "DELETE FROM brand_settings WHERE brand_id=$1 AND key=$2", scope.ID, key)
	return err
}
