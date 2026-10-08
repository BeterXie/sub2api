//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type brandSettingBaseRepoStub struct {
	values           map[string]string
	setKey           string
	setValue         string
	setMultipleCalls int
}

func (r *brandSettingBaseRepoStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (r *brandSettingBaseRepoStub) GetValue(ctx context.Context, key string) (string, error) {
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

func (r *brandSettingBaseRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey = key
	r.setValue = value
	return nil
}

func (r *brandSettingBaseRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *brandSettingBaseRepoStub) SetMultiple(context.Context, map[string]string) error {
	r.setMultipleCalls++
	return nil
}

func (r *brandSettingBaseRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *brandSettingBaseRepoStub) Delete(context.Context, string) error { return nil }

func TestBrandSettingSetDelegatesPlatformSingleWrite(t *testing.T) {
	base := &brandSettingBaseRepoStub{}
	repo := &brandSettingRepository{SettingRepository: base}

	err := repo.Set(context.Background(), service.SettingKeyOAuthAutoConfig, `{"enabled":true}`)

	require.NoError(t, err)
	require.Equal(t, service.SettingKeyOAuthAutoConfig, base.setKey)
	require.Equal(t, `{"enabled":true}`, base.setValue)
	require.Zero(t, base.setMultipleCalls)
}

func TestBrandSettingDeleteMasksLegacyNotificationValue(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	key := "notification_email_template:balance_recharge_success:zh"
	base := &brandSettingBaseRepoStub{values: map[string]string{key: `{"subject":"legacy custom"}`}}
	repo := &brandSettingRepository{
		SettingRepository: base,
		store:             &brand.Store{DB: db},
	}
	ctx := brand.WithScope(context.Background(), brand.Scope{
		ID:       brand.LegacyID,
		Code:     "llmp",
		Hostname: "llmp.org",
	})

	mock.ExpectExec("INSERT INTO brand_settings").
		WithArgs(brand.LegacyID, key).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.Delete(ctx, key))

	mock.ExpectQuery("SELECT key,value FROM settings").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}))
	mock.ExpectQuery("SELECT key,value_json FROM brand_settings").
		WithArgs(brand.LegacyID).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value_json"}).AddRow(key, "null"))
	mock.ExpectQuery("SELECT value_json FROM brand_settings").
		WithArgs(brand.LegacyID, key).
		WillReturnRows(sqlmock.NewRows([]string{"value_json"}).AddRow("null"))

	_, err = repo.GetValue(ctx, key)
	require.ErrorIs(t, err, service.ErrSettingNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
