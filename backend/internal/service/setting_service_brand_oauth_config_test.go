//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBrandLinuxDoOAuthUsesProtocolDefaultsWithoutLegacyCredentials(t *testing.T) {
	cfg := &config.Config{LinuxDo: config.LinuxDoConnectConfig{
		Enabled:      true,
		ClientID:     "legacy-client",
		ClientSecret: "legacy-secret",
		RedirectURL:  "https://llmp.org/api/v1/auth/oauth/linuxdo/callback",
	}}
	repo := &settingOIDCRepoStub{values: map[string]string{
		SettingKeyLinuxDoConnectEnabled:      "true",
		SettingKeyLinuxDoConnectClientID:     "mues-client",
		SettingKeyLinuxDoConnectClientSecret: "mues-secret",
		SettingKeyLinuxDoConnectRedirectURL:  "https://mues.cc/api/v1/auth/oauth/linuxdo/callback",
	}}
	svc := NewSettingService(repo, cfg)
	ctx := brand.WithScope(context.Background(), brand.Scope{ID: 2, Code: "mues"})

	got, err := svc.GetLinuxDoConnectOAuthConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "mues-client", got.ClientID)
	require.Equal(t, "mues-secret", got.ClientSecret)
	require.Equal(t, "https://mues.cc/api/v1/auth/oauth/linuxdo/callback", got.RedirectURL)
	require.Equal(t, "https://connect.linux.do/oauth2/authorize", got.AuthorizeURL)
	require.Equal(t, "https://connect.linux.do/oauth2/token", got.TokenURL)
	require.Equal(t, "https://connect.linux.do/api/user", got.UserInfoURL)
	require.Equal(t, "/auth/linuxdo/callback", got.FrontendRedirectURL)
}

func TestBrandDingTalkOAuthUsesProtocolDefaultsWithoutLegacyCredentials(t *testing.T) {
	cfg := &config.Config{DingTalk: config.DingTalkConnectConfig{
		Enabled:        true,
		ClientID:       "legacy-client",
		ClientSecret:   "legacy-secret",
		RedirectURL:    "https://llmp.org/api/v1/auth/oauth/dingtalk/callback",
		InternalCorpID: "legacy-corp",
	}}
	repo := &settingOIDCRepoStub{values: map[string]string{
		SettingKeyDingTalkConnectEnabled:      "true",
		SettingKeyDingTalkConnectClientID:     "mues-client",
		SettingKeyDingTalkConnectClientSecret: "mues-secret",
		SettingKeyDingTalkConnectRedirectURL:  "https://mues.cc/api/v1/auth/oauth/dingtalk/callback",
	}}
	svc := NewSettingService(repo, cfg)
	ctx := brand.WithScope(context.Background(), brand.Scope{ID: 2, Code: "mues"})

	got, err := svc.GetDingTalkConnectOAuthConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, "mues-client", got.ClientID)
	require.Equal(t, "mues-secret", got.ClientSecret)
	require.Equal(t, "https://mues.cc/api/v1/auth/oauth/dingtalk/callback", got.RedirectURL)
	require.Equal(t, "https://login.dingtalk.com/oauth2/auth", got.AuthorizeURL)
	require.Equal(t, "https://api.dingtalk.com/v1.0/oauth2/userAccessToken", got.TokenURL)
	require.Equal(t, "https://api.dingtalk.com/v1.0/contact/users/me", got.UserInfoURL)
	require.Equal(t, "/auth/dingtalk/callback", got.FrontendRedirectURL)
	require.Empty(t, got.InternalCorpID)
}
