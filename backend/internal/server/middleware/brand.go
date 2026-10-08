package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

func BrandResolver(cfg *config.Config, store *brand.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg == nil || !cfg.MultiBrand.Enabled {
			c.Next()
			return
		}
		c.Request = c.Request.WithContext(brand.WithStore(c.Request.Context(), store))
		// Infrastructure checks and signed callbacks have explicit ownership rules.
		if brandInfrastructurePath(c.Request.Method, c.Request.URL.Path) || brandWebhookPath(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		if _, ok := brand.FromContext(c.Request.Context()); ok {
			c.Next()
			return
		}
		if store == nil {
			AbortWithError(c, 503, "SITE_UNAVAILABLE", "Site is unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		scope, err := store.Resolve(ctx, c.Request.Host)
		cancel()
		if err != nil {
			if errors.Is(err, brand.ErrUnknownDomain) {
				AbortWithError(c, http.StatusMisdirectedRequest, "UNKNOWN_HOST", "Unknown site")
				return
			}
			AbortWithError(c, 503, "SITE_UNAVAILABLE", "Site is unavailable")
			return
		}
		gateway := brandGatewayPath(c.Request.URL.Path)
		if (gateway && !scope.GatewayEnabled) || (!gateway && !scope.PublicEnabled) {
			AbortWithError(c, 503, "SITE_UNAVAILABLE", "Site is unavailable")
			return
		}
		c.Request = c.Request.WithContext(brand.WithStore(brand.WithScope(c.Request.Context(), scope), store))
		c.Next()
	}
}
func brandInfrastructurePath(method, path string) bool {
	return (method == http.MethodGet || method == http.MethodHead) && (path == "/health" || path == "/internal/serverless/probe")
}
func brandWebhookPath(method, path string) bool {
	if method != http.MethodPost && method != http.MethodGet {
		return false
	}
	switch path {
	case "/api/v1/payment/webhook/easypay":
		return true
	case "/api/v1/payment/webhook/alipay", "/api/v1/payment/webhook/wxpay", "/api/v1/payment/webhook/stripe", "/api/v1/payment/webhook/airwallex":
		return method == http.MethodPost
	}
	return false
}
func brandGatewayPath(path string) bool {
	if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/backend-api/") ||
		strings.HasPrefix(path, "/responses") || strings.HasPrefix(path, "/chat/") || strings.HasPrefix(path, "/messages") ||
		strings.HasPrefix(path, "/models") || strings.HasPrefix(path, "/images/") || strings.HasPrefix(path, "/videos") ||
		strings.HasPrefix(path, "/audio/") || strings.HasPrefix(path, "/api/bps-images/") ||
		strings.HasPrefix(path, "/antigravity/") || strings.HasPrefix(path, "/embeddings") ||
		strings.HasPrefix(path, "/tts") || strings.HasPrefix(path, "/stt") || strings.HasPrefix(path, "/custom-voices") ||
		strings.HasPrefix(path, "/realtime") || strings.HasPrefix(path, "/web_search") || strings.HasPrefix(path, "/x_search") || strings.HasPrefix(path, "/systemone") {
		return true
	}
	if strings.HasPrefix(path, "/contents/") || strings.HasPrefix(path, "/v3/") || strings.HasPrefix(path, "/api/v3/") || strings.HasPrefix(path, "/alpha/") {
		return true
	}
	return false
}

// authorizeBrandAdmin executes after authentication, before any admin handler.
func authorizeBrandAdmin(c *gin.Context) bool {
	scope, scoped := brand.FromContext(c.Request.Context())
	if !scoped {
		return true
	}
	role, _ := c.Get("brand_admin_role")
	roleName, _ := role.(string)
	if roleName == "" {
		AbortWithError(c, 403, "FORBIDDEN", "Admin access required")
		return false
	}
	path := c.Request.URL.Path
	if roleName == "super_admin" {
		if scope.ID != brand.LegacyID || scope.Hostname != "llmp.org" {
			AbortWithError(c, 403, "FORBIDDEN", "Use the platform administration site")
			return false
		}
		if raw := c.Query("brand_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				AbortWithError(c, 400, "INVALID_BRAND", "Invalid brand")
				return false
			}
			store := brand.StoreFromContext(c.Request.Context())
			b, err := store.GetBrand(c.Request.Context(), id)
			if err != nil {
				AbortWithError(c, 404, "BRAND_NOT_FOUND", "Brand not found")
				return false
			}
			scope.ID, scope.Code, scope.Name = b.ID, b.Code, b.Name
			scope.DomainID = 0
			scope.RegistrationEnabled, scope.MaxConcurrent, scope.RPMLimit = b.RegistrationEnabled, b.MaxConcurrent, b.RPMLimit
			domains, err := store.Domains(c.Request.Context(), b.ID)
			if err != nil {
				AbortWithError(c, 503, "SITE_UNAVAILABLE", "Site is unavailable")
				return false
			}
			for _, domain := range domains {
				if domain.Primary {
					scope.Hostname = domain.Hostname
					scope.CanonicalAPIOrigin = "https://" + domain.Hostname
					break
				}
			}
		}
		// Global read access is explicit. Mutations retain a selected brand.
		scope.Platform = strings.HasPrefix(path, "/api/v1/admin/brands") ||
			(c.Request.Method == http.MethodGet && c.Query("all_brands") == "true" &&
				(strings.HasPrefix(path, "/api/v1/admin/dashboard/") || path == "/api/v1/admin/brand-content/summary")) ||
			!brandAdminTenantPath(path)
		c.Request = c.Request.WithContext(brand.WithScope(c.Request.Context(), scope))
		return true
	}
	if c.Query("all_brands") != "" || (c.Query("brand_id") != "" && c.Query("brand_id") != strconv.FormatInt(scope.ID, 10)) {
		AbortWithError(c, 403, "FORBIDDEN", "Brand access denied")
		return false
	}
	if !brandAdminTenantPath(path) || strings.HasPrefix(path, "/api/v1/admin/brands") {
		AbortWithError(c, 403, "FORBIDDEN", "Platform permission required")
		return false
	}
	if roleName == "support" && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		AbortWithError(c, 403, "FORBIDDEN", "Read access only")
		return false
	}
	if roleName == "operator" && c.Request.Method != http.MethodGet &&
		(strings.Contains(path, "/payment/config") || strings.Contains(path, "/payment/providers") || strings.Contains(path, "/refund") || strings.HasSuffix(path, "/brand-content/settings") || path == "/api/v1/admin/settings/test-smtp" || path == "/api/v1/admin/settings/send-test-email") {
		AbortWithError(c, 403, "FORBIDDEN", "Brand owner permission required")
		return false
	}
	return true
}
func brandAdminTenantPath(path string) bool {
	if path == "/api/v1/admin/groups/live-capability" {
		return false
	}
	for _, prefix := range []string{"/api/v1/admin/settings/email-templates", "/api/v1/admin/settings/email-template-preview", "/api/v1/admin/settings/test-smtp", "/api/v1/admin/settings/send-test-email"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	if strings.HasPrefix(path, "/api/v1/admin/dashboard/aggregation/") {
		return false
	}
	for _, prefix := range []string{"/api/v1/admin/users", "/api/v1/admin/groups", "/api/v1/admin/api-keys",
		"/api/v1/admin/redeem-codes", "/api/v1/admin/promo-codes", "/api/v1/admin/announcements", "/api/v1/admin/subscriptions",
		"/api/v1/admin/payment", "/api/v1/admin/usage", "/api/v1/admin/dashboard", "/api/v1/admin/brand-content",
		"/api/v1/admin/user-attributes", "/api/v1/admin/affiliates", "/api/v1/admin/audit-logs"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
