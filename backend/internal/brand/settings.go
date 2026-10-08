package brand

import (
	"fmt"
	"strings"
)

// Brand settings never contain upstream credentials, network or scheduler
// configuration. LLMP inherits its historical presentation; other brands use
// independent defaults until an operator provides content.
var presentation = map[string]bool{
	"site_name": true, "site_logo": true, "site_subtitle": true, "contact_info": true,
	"doc_url": true, "home_content": true, "compact_home_enabled": true, "custom_menu_items": true,
	"purchase_subscription_url": true, "login_agreement_documents": true,
	"login_agreement_enabled": true, "login_agreement_mode": true,
	"seo_title": true, "seo_description": true, "theme_color": true, "home_headline": true,
	"home_description": true, "footer_text": true, "smtp_from_name": true, "smtp_from": true,
	"home_sections": true, "brand_nav": true, "brand_footer": true, "home_template": true,
}

func init() {
	for _, key := range []string{
		"default_concurrency", "default_balance", "default_subscriptions", "default_user_rpm_limit",
		"force_email_on_third_party_signup", "affiliate_rebate_rate", "affiliate_rebate_freeze_hours",
		"affiliate_rebate_duration_days", "affiliate_rebate_per_invitee_cap", "affiliate_admin_recharge_enabled",
		"login_agreement_updated_at", "custom_endpoints", "hide_ccs_import_button",
		"smtp_host", "smtp_port", "smtp_username", "smtp_password", "smtp_use_tls",
		"MIN_RECHARGE_AMOUNT", "MAX_RECHARGE_AMOUNT", "DAILY_RECHARGE_LIMIT", "ORDER_TIMEOUT_MINUTES",
		"MAX_PENDING_ORDERS", "ENABLED_PAYMENT_TYPES", "LOAD_BALANCE_STRATEGY", "BALANCE_PAYMENT_DISABLED",
		"BALANCE_RECHARGE_MULTIPLIER", "SUBSCRIPTION_USD_TO_CNY_RATE", "RECHARGE_FEE_RATE",
		"PRODUCT_NAME_PREFIX", "PRODUCT_NAME_SUFFIX", "PAYMENT_HELP_IMAGE_URL", "PAYMENT_HELP_TEXT",
		"CANCEL_RATE_LIMIT_ENABLED", "CANCEL_RATE_LIMIT_MAX", "CANCEL_RATE_LIMIT_WINDOW",
		"CANCEL_RATE_LIMIT_UNIT", "CANCEL_RATE_LIMIT_WINDOW_MODE", "ALIPAY_FORCE_QRCODE",
		"ALIPAY_MOBILE_PRECREATE_DEEP_LINK", "RECHARGE_BONUS_TIERS", "RECHARGE_BONUS_MODE", "RECHARGE_BONUS_NOTICE",
		"payment_visible_method_alipay_source", "payment_visible_method_wxpay_source",
		"payment_visible_method_alipay_enabled", "payment_visible_method_wxpay_enabled",
		"totp_enabled", "passkey_enabled", "session_binding_enabled",
		"balance_low_notify_enabled", "balance_low_notify_threshold", "balance_low_notify_recharge_url", "subscription_expiry_notify_enabled",
		"turnstile_enabled", "turnstile_site_key", "turnstile_secret_key",
		"tencent_captcha_enabled", "tencent_captcha_app_id", "tencent_captcha_app_secret_key",
		"tencent_captcha_cloud_secret_id", "tencent_captcha_cloud_secret_key", "tencent_captcha_region",
		"aliyun_captcha_enabled", "aliyun_captcha_access_key_id", "aliyun_captcha_access_key_secret",
		"aliyun_captcha_scene_id", "aliyun_captcha_prefix", "aliyun_captcha_region",
	} {
		operational[key] = true
	}
	for _, source := range []string{"email", "linuxdo", "oidc", "wechat", "github", "google", "dingtalk"} {
		for _, field := range []string{"balance", "concurrency", "subscriptions", "grant_on_signup", "grant_on_first_bind"} {
			operational["auth_source_default_"+source+"_"+field] = true
		}
	}
	// Authentication belongs to each brand; upstream model credentials do not.
	for _, provider := range []string{"linuxdo_connect", "dingtalk_connect", "wechat_connect", "oidc_connect", "github_oauth", "google_oauth"} {
		for _, field := range []string{"enabled", "client_id", "client_secret", "redirect_url", "frontend_redirect_url"} {
			operational[provider+"_"+field] = true
		}
	}
	for _, key := range []string{
		"oidc_connect_provider_name", "oidc_connect_issuer_url", "oidc_connect_discovery_url",
		"oidc_connect_authorize_url", "oidc_connect_token_url", "oidc_connect_userinfo_url", "oidc_connect_jwks_url",
		"oidc_connect_scopes", "oidc_connect_token_auth_method", "oidc_connect_use_pkce", "oidc_connect_validate_id_token",
		"oidc_connect_allowed_signing_algs", "oidc_connect_clock_skew_seconds", "oidc_connect_require_email_verified",
		"oidc_connect_userinfo_email_path", "oidc_connect_userinfo_id_path", "oidc_connect_userinfo_username_path",
		"wechat_connect_app_id", "wechat_connect_app_secret", "wechat_connect_open_app_id", "wechat_connect_open_app_secret",
		"wechat_connect_mp_app_id", "wechat_connect_mp_app_secret", "wechat_connect_mobile_app_id", "wechat_connect_mobile_app_secret",
		"wechat_connect_open_enabled", "wechat_connect_mp_enabled", "wechat_connect_mobile_enabled", "wechat_connect_mode", "wechat_connect_scopes",
		"dingtalk_connect_corp_restriction_policy", "dingtalk_connect_internal_corp_id", "dingtalk_connect_bypass_registration",
		"dingtalk_connect_sync_corp_email", "dingtalk_connect_sync_display_name", "dingtalk_connect_sync_dept",
		"dingtalk_connect_sync_corp_email_attr_key", "dingtalk_connect_sync_display_name_attr_key", "dingtalk_connect_sync_dept_attr_key",
	} {
		operational[key] = true
	}
}
func SecretSetting(key string) bool {
	return strings.Contains(key, "secret") || key == "smtp_password"
}
func PublicPresentation(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		if presentation[key] && key != "smtp_from" && key != "smtp_from_name" {
			out[key] = value
		}
	}
	return out
}
func RedactSettings(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		if SecretSetting(key) {
			out[key] = ""
			out[key+"_configured"] = value != ""
		} else {
			out[key] = value
		}
	}
	return out
}

var operational = map[string]bool{
	"email_verify_enabled": true, "promo_code_enabled": true, "password_reset_enabled": true,
	"invitation_code_enabled": true, "purchase_subscription_enabled": true,
	"registration_email_suffix_whitelist": true, "registration_email_domain_quota_enabled": true,
	"affiliate_enabled": true, "subscription_enabled": true, "model_plaza_enabled": true,
	"model_plaza_require_auth": true, "channel_monitor_enabled": true, "available_channels_enabled": true,
	"pelican_showcase_enabled": true, "payment_enabled": true, "balance_pay_disabled": true,
}

func AllowedSetting(key string) bool { return presentation[key] || operational[key] }

// Internal notification state is tenant data, even though the legacy service
// stores it in the settings repository. Public presentation never exposes it.
func NotificationSetting(key string) bool {
	if key == "notification_email_unsubscribe_secret" {
		return true
	}
	for _, prefix := range []string{"notification_email_template:", "notification_email_preference:", "notification_email_delivery:", "notification_email_locale:"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
func DomainSetting(key string) bool {
	return key == "seo_title" || key == "seo_description" || key == "site_logo" || key == "home_headline" || key == "home_description"
}
func ValidateSettings(values map[string]any, domain bool) error {
	for k, v := range values {
		if !AllowedSetting(k) || (domain && !DomainSetting(k)) {
			return fmt.Errorf("unsupported brand setting %q", k)
		}
		if k == "home_template" {
			name, ok := v.(string)
			if !ok || (name != "llmp" && name != "mues" && name != "aisi" && name != "opensi_codes" && name != "opensi_in") {
				return fmt.Errorf("invalid home template")
			}
		}
		switch v.(type) {
		case string, bool, float64, int, []any, map[string]any:
		default:
			return fmt.Errorf("invalid setting %q", k)
		}
	}
	return nil
}
func Defaults(scope Scope) map[string]any {
	values := map[string]any{
		"site_name": scope.Name, "site_logo": "/brands/" + scope.Code + "/logo.svg", "site_subtitle": "",
		"contact_info": "", "doc_url": "/docs", "home_content": "", "compact_home_enabled": false,
		"custom_menu_items": "[]", "purchase_subscription_url": "/subscriptions",
		"login_agreement_documents": "[]", "login_agreement_enabled": false, "login_agreement_mode": "checkbox",
		"seo_title": scope.Name, "seo_description": "", "theme_color": "", "home_headline": "",
		"home_description": "", "footer_text": "", "smtp_from_name": scope.Name, "smtp_from": "",
		"home_sections": "[]", "brand_nav": "[]", "brand_footer": "[]", "home_template": scope.Code,
		"email_verify_enabled": false, "promo_code_enabled": false, "password_reset_enabled": true,
		"invitation_code_enabled": false, "purchase_subscription_enabled": false,
		"registration_email_suffix_whitelist": "[]", "registration_email_domain_quota_enabled": false,
		"affiliate_enabled": false, "subscription_enabled": false, "model_plaza_enabled": false,
		"model_plaza_require_auth": true, "channel_monitor_enabled": false, "available_channels_enabled": false,
		"pelican_showcase_enabled": false, "payment_enabled": false, "balance_pay_disabled": false,
		"default_balance": "0", "default_concurrency": "1", "default_subscriptions": "[]", "default_user_rpm_limit": "0",
		"MIN_RECHARGE_AMOUNT": "1", "MAX_RECHARGE_AMOUNT": "1000", "DAILY_RECHARGE_LIMIT": "0",
		"ORDER_TIMEOUT_MINUTES": "30", "MAX_PENDING_ORDERS": "3", "ENABLED_PAYMENT_TYPES": "[]",
		"BALANCE_PAYMENT_DISABLED": "false", "BALANCE_RECHARGE_MULTIPLIER": "1", "SUBSCRIPTION_USD_TO_CNY_RATE": "0",
		"RECHARGE_FEE_RATE": "0", "PRODUCT_NAME_PREFIX": scope.Name + " ", "PRODUCT_NAME_SUFFIX": "",
		"PAYMENT_HELP_IMAGE_URL": "", "PAYMENT_HELP_TEXT": "", "RECHARGE_BONUS_TIERS": "[]", "RECHARGE_BONUS_MODE": "bonus", "RECHARGE_BONUS_NOTICE": "",
	}
	// An allowlisted operational field never inherits another brand's credentials
	// or campaign. Each brand supplies its own value or uses the feature default.
	for key := range operational {
		if _, exists := values[key]; !exists {
			values[key] = ""
		}
	}
	for _, source := range []string{"email", "linuxdo", "oidc", "wechat", "github", "google", "dingtalk"} {
		prefix := "auth_source_default_" + source + "_"
		values[prefix+"balance"] = "0"
		values[prefix+"concurrency"] = "1"
		values[prefix+"subscriptions"] = "[]"
		values[prefix+"grant_on_signup"] = false
		values[prefix+"grant_on_first_bind"] = false
	}
	for _, provider := range []string{"linuxdo_connect", "dingtalk_connect", "wechat_connect", "oidc_connect", "github_oauth", "google_oauth"} {
		values[provider+"_enabled"] = false
		values[provider+"_client_id"] = ""
		values[provider+"_client_secret"] = ""
		values[provider+"_redirect_url"] = "https://" + scope.Hostname + "/api/v1/auth/oauth/" + strings.Split(provider, "_")[0] + "/callback"
		name := strings.Split(provider, "_")[0]
		path := "/auth/" + name + "/callback"
		if name == "github" || name == "google" {
			path = "/auth/callback"
		}
		values[provider+"_frontend_redirect_url"] = "https://" + scope.Hostname + path
	}
	values["oidc_connect_provider_name"] = "OIDC"
	values["oidc_connect_use_pkce"] = true
	values["oidc_connect_validate_id_token"] = true
	values["oidc_connect_scopes"] = "openid email profile"
	values["oidc_connect_allowed_signing_algs"] = "RS256,ES256,PS256"
	values["oidc_connect_clock_skew_seconds"] = "120"
	return values
}
