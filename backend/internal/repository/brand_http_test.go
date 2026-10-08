//go:build multibrand

package repository_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	. "github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func brandHTTP(r http.Handler, method, host, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func brandAuthResponse(t *testing.T, w *httptest.ResponseRecorder) handler.AuthResponse {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	var result struct {
		Data handler.AuthResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.NotEmpty(t, result.Data.AccessToken)
	return result.Data
}

func TestMultiBrandHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := os.Getenv("MULTIBRAND_TEST_DSN")
	require.NotEmpty(t, dsn)
	control, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer control.Close()
	name := fmt.Sprintf("multibrand_http_%d", time.Now().UnixNano())
	_, err = control.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	defer func() {
		_, _ = control.ExecContext(context.Background(), "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
	}()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		require.NoError(t, err)
		u.Path = "/" + name
		dsn = u.String()
	} else {
		dsn += " dbname=" + name
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	require.NoError(t, ApplyMigrations(ctx, admin))
	db, err := NewBrandTestDatabase(dsn)
	require.NoError(t, err)
	defer db.Close()
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	address := os.Getenv("MULTIBRAND_TEST_REDIS")
	require.NotEmpty(t, address, "set MULTIBRAND_TEST_REDIS to a dedicated local Redis")
	cache := redis.NewClient(&redis.Options{Addr: address, DB: 15})
	defer cache.Close()
	require.NoError(t, cache.Ping(ctx).Err())
	store := brand.NewStore(db)
	cfg := &config.Config{MultiBrand: config.MultiBrandConfig{Enabled: true, LegacyJWTUntil: time.Now().Add(time.Hour).Format(time.RFC3339)},
		JWT:     config.JWTConfig{Secret: strings.Repeat("brand-test-secret", 3), ExpireHour: 1, RefreshTokenExpireDays: 1},
		Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024, TextMaxBodySize: 1024 * 1024}}
	cfg.APIKeyAuth.L1Size = 64
	cfg.APIKeyAuth.L1TTLSeconds = 60
	cfg.APIKeyAuth.L2TTLSeconds = 60
	cfg.APIKeyAuth.NegativeTTLSeconds = 60
	cfg.APIKeyAuth.Singleflight = true
	cfg.LinuxDo.ClientSecret = "legacy-linuxdo-secret"
	cfg.DingTalk.ClientSecret = "legacy-dingtalk-secret"
	cfg.OIDC.ClientSecret = "legacy-oidc-secret"
	userRepo := NewUserRepository(client, db)
	groupRepo := NewGroupRepository(client, db)
	redeemRepo := NewRedeemCodeRepository(client)
	settingsRepo := ProvideBrandSettingRepository(client, store)
	settings := service.NewSettingService(settingsRepo, cfg)
	emailCache := NewEmailCache(cache)
	email := service.NewEmailService(settingsRepo, emailCache)
	auth := service.NewAuthService(client, userRepo, redeemRepo, NewRefreshTokenCache(cache), cfg, settings, email, nil, nil, nil, nil, nil, nil)
	users := service.NewUserService(userRepo, settingsRepo, nil, nil)
	keys := service.NewAPIKeyService(NewAPIKeyRepository(client, db), userRepo, groupRepo, nil, nil, NewAPIKeyCache(cache), cfg)
	jwtAuth := middleware.NewJWTAuthMiddleware(auth, users, settings, nil)
	adminAuth := middleware.NewAdminAuthMiddleware(auth, users, settings, nil)
	keyAuth := middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg)
	authHandler := handler.NewAuthHandler(cfg, auth, users, settings, nil, nil, nil, nil, NewOAuthStateStore(cache))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BrandResolver(cfg, store))
	v1 := r.Group("/api/v1")
	v1.POST("/auth/register", authHandler.Register)
	v1.POST("/auth/login", authHandler.Login)
	v1.POST("/auth/refresh", authHandler.RefreshToken)
	v1.POST("/auth/reset-password", authHandler.ResetPassword)
	v1.GET("/user/me", gin.HandlerFunc(jwtAuth), func(c *gin.Context) {
		subject, _ := middleware.GetAuthSubjectFromContext(c)
		c.JSON(200, gin.H{"id": subject.UserID})
	})
	v1.GET("/key", gin.HandlerFunc(keyAuth), func(c *gin.Context) { c.Status(200) })
	noAudit := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	cfg.Pricing.DataDir = t.TempDir()
	routes.RegisterBrandRoutes(v1, store, cfg, jwtAuth, adminAuth, noAudit, settings, middleware.NewPanelRateLimiter(cache, settings))
	v1.GET("/admin/users", gin.HandlerFunc(adminAuth), func(c *gin.Context) {
		count, err := client.User.Query().Count(c.Request.Context())
		if err != nil {
			c.AbortWithStatus(500)
			return
		}
		c.JSON(200, gin.H{"count": count})
	})
	v1.GET("/admin/accounts", gin.HandlerFunc(adminAuth), func(c *gin.Context) { c.Status(200) })
	_, err = admin.ExecContext(ctx, "UPDATE brands SET status='active',registration_enabled=TRUE; UPDATE domains SET enabled=TRUE")
	require.NoError(t, err)
	hosts := []string{"llmp.org", "mues.cc", "aisi.plus", "opensi.codes", "opensi.in"}
	logins := make([]handler.AuthResponse, 5)
	scopes := make([]context.Context, 5)
	for i, host := range hosts {
		scope, err := store.Resolve(ctx, host)
		require.NoError(t, err)
		scopes[i] = brand.WithStore(brand.WithScope(ctx, scope), store)
		body := fmt.Sprintf(`{"email":"http-shared@multibrand.test","password":"Password%d!"}`, i)
		logins[i] = brandAuthResponse(t, brandHTTP(r, "POST", host, "/api/v1/auth/register", "", body))
		require.Equal(t, logins[i].User.ID, brandAuthResponse(t, brandHTTP(r, "POST", host, "/api/v1/auth/login", "", body)).User.ID)
		claims, err := auth.ValidateToken(logins[i].AccessToken)
		require.NoError(t, err)
		require.Equal(t, int64(i+1), claims.BrandID)
		if i > 0 {
			require.NotEqual(t, logins[0].User.ID, logins[i].User.ID)
		}
		require.Equal(t, 401, brandHTTP(r, "POST", host, "/api/v1/auth/login", "", `{"email":"http-shared@multibrand.test","password":"foreign-password"}`).Code)
	}
	t.Run("JWT and refresh tokens cannot move between brands", func(t *testing.T) {
		for i, host := range hosts {
			require.NoError(t, store.SaveSettings(scopes[i], int64(i+1), map[string]any{"email_verify_enabled": true, "password_reset_enabled": true}))
			require.Equal(t, 200, brandHTTP(r, "GET", host, "/api/v1/user/me", logins[i].AccessToken, "").Code)
			other := (i + 1) % 5
			require.Equal(t, 401, brandHTTP(r, "GET", hosts[other], "/api/v1/user/me", logins[i].AccessToken, "").Code)
			wrong := brandHTTP(r, "POST", hosts[other], "/api/v1/auth/refresh", "", fmt.Sprintf(`{"refresh_token":%q}`, logins[i].RefreshToken))
			require.Equal(t, 401, wrong.Code, wrong.Body.String())
			right := brandHTTP(r, "POST", host, "/api/v1/auth/refresh", "", fmt.Sprintf(`{"refresh_token":%q}`, logins[i].RefreshToken))
			require.Equal(t, 200, right.Code, right.Body.String())
		}
		for _, host := range []string{"llmp.cc", "llmp.xyz", "llmp.site"} {
			require.Equal(t, 200, brandHTTP(r, "GET", host, "/api/v1/user/me", logins[0].AccessToken, "").Code)
			res := brandAuthResponse(t, brandHTTP(r, "POST", host, "/api/v1/auth/login", "", `{"email":"http-shared@multibrand.test","password":"Password0!"}`))
			require.Equal(t, logins[0].User.ID, res.User.ID)
		}
		claims, err := auth.ValidateToken(logins[0].AccessToken)
		require.NoError(t, err)
		claims.BrandID = 0
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWT.Secret))
		require.NoError(t, err)
		require.Equal(t, 200, brandHTTP(r, "GET", "llmp.org", "/api/v1/user/me", token, "").Code)
		require.Equal(t, 401, brandHTTP(r, "GET", "mues.cc", "/api/v1/user/me", token, "").Code)
		cfg.MultiBrand.LegacyJWTUntil = time.Now().Add(-time.Hour).Format(time.RFC3339)
		require.Equal(t, 401, brandHTTP(r, "GET", "llmp.org", "/api/v1/user/me", token, "").Code)
	})
	t.Run("reset tokens and verification codes belong to one brand", func(t *testing.T) {
		for i, host := range hosts {
			token := fmt.Sprintf("reset-brand-%d", i)
			hash := sha256.Sum256([]byte(token))
			require.NoError(t, emailCache.SetPasswordResetToken(scopes[i], "http-shared@multibrand.test", &service.PasswordResetTokenData{Token: hex.EncodeToString(hash[:]), CreatedAt: time.Now()}, time.Minute))
			body := fmt.Sprintf(`{"email":"http-shared@multibrand.test","token":%q,"new_password":"NewPassword!"}`, token)
			wrong := brandHTTP(r, "POST", hosts[(i+1)%5], "/api/v1/auth/reset-password", "", body)
			require.NotEqual(t, 200, wrong.Code, wrong.Body.String())
			right := brandHTTP(r, "POST", host, "/api/v1/auth/reset-password", "", body)
			require.Equal(t, 200, right.Code, right.Body.String())
			require.NoError(t, emailCache.SetVerificationCode(scopes[i], "http-shared@multibrand.test", &service.VerificationCodeData{Code: fmt.Sprint(100000 + i), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}, time.Minute))
		}
		for i := range scopes {
			require.NoError(t, email.VerifyCode(scopes[i], "http-shared@multibrand.test", fmt.Sprint(100000+i)))
		}
		// Password reset invalidates prior JWTs only for the corresponding users.
		for i, host := range hosts {
			logins[i] = brandAuthResponse(t, brandHTTP(r, "POST", host, "/api/v1/auth/login", "", `{"email":"http-shared@multibrand.test","password":"NewPassword!"}`))
		}
	})
	t.Run("API key cache rejects foreign hosts without poisoning the owner", func(t *testing.T) {
		group, err := client.Group.Create().SetName("http-key-group").Save(scopes[0])
		require.NoError(t, err)
		_, err = client.User.UpdateOneID(logins[0].User.ID).SetBalance(100).Save(scopes[0])
		require.NoError(t, err)
		key := fmt.Sprintf("sk_brand_http_%d", time.Now().UnixNano())
		_, err = client.APIKey.Create().SetUserID(logins[0].User.ID).SetGroupID(group.ID).SetName("HTTP key").SetKey(key).Save(scopes[0])
		require.NoError(t, err)
		for n := 0; n < 3; n++ {
			require.Equal(t, 401, brandHTTP(r, "GET", "mues.cc", "/api/v1/key", key, "").Code)
			w := brandHTTP(r, "GET", "llmp.org", "/api/v1/key", key, "")
			require.Equal(t, 200, w.Code, w.Body.String())
		}
		for _, role := range []string{config.RuntimeRoleFull, config.RuntimeRoleGateway} {
			cfg.Runtime.Role = role
			gateway := gin.New()
			gateway.Use(middleware.BrandResolver(cfg, store))
			routes.RegisterGatewayRoutes(gateway, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, keyAuth, keys, nil, nil, settings, nil, cfg, cache)
			for _, route := range gateway.Routes() {
				if strings.HasPrefix(route.Path, "/api/bps-images/") {
					continue
				} // Existing capability URL has separate ownership.
				path := route.Path
				for _, part := range strings.Split(path, "/") {
					if strings.HasPrefix(part, ":") || strings.HasPrefix(part, "*") {
						path = strings.ReplaceAll(path, part, "test")
					}
				}
				w := brandHTTP(gateway, route.Method, "mues.cc", path, key, `{"model":"test"}`)
				require.Equal(t, 401, w.Code, role+" "+route.Method+" "+path+" "+w.Body.String())
			}
		}
	})
	t.Run("explicit grants, CMS drafts, platform selection and settings", func(t *testing.T) {
		for i := range scopes {
			id := int64(i + 1)
			require.NoError(t, store.SaveGrant(ctx, brand.Grant{UserID: logins[i].User.ID, BrandID: &id, Role: "owner"}))
		}
		_, err := admin.ExecContext(ctx, "INSERT INTO brand_admins(user_id,role) VALUES($1,'super_admin')", logins[0].User.ID)
		require.NoError(t, err)
		body := `{"slug":"guide","locale":"en","status":"draft","title":"LLMP draft","category":"SDK","content_md":"private draft"}`
		require.NoError(t, store.SavePage(scopes[0], &brand.Page{BrandID: 1, Slug: "probe", Locale: "en", Status: "draft", Title: "probe"}))
		draft := brandHTTP(r, "POST", "llmp.org", "/api/v1/admin/brand-content/pages", logins[0].AccessToken, body)
		require.Equal(t, 200, draft.Code, draft.Body.String())
		var created struct {
			Data brand.Page `json:"data"`
		}
		require.NoError(t, json.Unmarshal(draft.Body.Bytes(), &created))
		require.Equal(t, 404, brandHTTP(r, "GET", "llmp.org", "/api/v1/public/docs/guide?locale=en", "", "").Code)
		published := created.Data
		published.Status = "published"
		published.ContentMD = "Published SDK {{API_BASE_URL}}"
		encoded, err := json.Marshal(published)
		require.NoError(t, err)
		pagePath := fmt.Sprintf("/api/v1/admin/brand-content/pages/%d", published.ID)
		w := brandHTTP(r, "PUT", "llmp.org", pagePath, logins[0].AccessToken, string(encoded))
		require.Equal(t, 200, w.Code, w.Body.String())
		var saved struct {
			Data brand.Page `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
		published = saved.Data
		require.Equal(t, 200, brandHTTP(r, "GET", "llmp.cc", "/api/v1/public/docs/guide?locale=en", "", "").Code)
		require.Equal(t, 404, brandHTTP(r, "GET", "mues.cc", "/api/v1/public/docs/guide?locale=en", "", "").Code)
		published.Status = "draft"
		published.Title = "Withdrawn SDK"
		encoded, err = json.Marshal(published)
		require.NoError(t, err)
		w = brandHTTP(r, "PUT", "llmp.org", pagePath, logins[0].AccessToken, string(encoded))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, 404, brandHTTP(r, "GET", "llmp.org", "/api/v1/public/docs/guide?locale=en", "", "").Code)
		revisions := brandHTTP(r, "GET", "llmp.org", pagePath+"/revisions", logins[0].AccessToken, "")
		require.Equal(t, 200, revisions.Code)
		var versions struct {
			Data []brand.Page `json:"data"`
		}
		require.NoError(t, json.Unmarshal(revisions.Body.Bytes(), &versions))
		require.Len(t, versions.Data, 3)
		require.Contains(t, revisions.Body.String(), "Published SDK")
		require.NotContains(t, brandHTTP(r, "GET", "mues.cc", pagePath+"/revisions", logins[1].AccessToken, "").Body.String(), "Published SDK")
		var pngData bytes.Buffer
		require.NoError(t, png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))))
		upload := func(data []byte) *httptest.ResponseRecorder {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "upload.png")
			require.NoError(t, err)
			_, err = part.Write(data)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			req := httptest.NewRequest("POST", "https://llmp.org/api/v1/admin/brand-content/media", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+logins[0].AccessToken)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			return response
		}
		media := upload(pngData.Bytes())
		require.Equal(t, 200, media.Code, media.Body.String())
		var asset struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(media.Body.Bytes(), &asset))
		require.Equal(t, 200, brandHTTP(r, "GET", "llmp.xyz", asset.Data.URL, "", "").Code)
		require.Equal(t, 404, brandHTTP(r, "GET", "mues.cc", asset.Data.URL, "", "").Code)
		require.Equal(t, 400, upload([]byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`)).Code)
		mues := brandHTTP(r, "GET", "mues.cc", "/api/v1/admin/brand-content/pages", logins[1].AccessToken, "")
		require.Equal(t, 200, mues.Code)
		require.NotContains(t, mues.Body.String(), "private draft")
		require.Equal(t, 404, brandHTTP(r, "PUT", "mues.cc", fmt.Sprintf("/api/v1/admin/brand-content/pages/%d", created.Data.ID), logins[1].AccessToken, body).Code)
		require.Equal(t, 403, brandHTTP(r, "GET", "mues.cc", "/api/v1/admin/accounts", logins[1].AccessToken, "").Code)
		require.Equal(t, 403, brandHTTP(r, "GET", "mues.cc", "/api/v1/admin/users?brand_id=1", logins[1].AccessToken, "").Code)
		require.Equal(t, 403, brandHTTP(r, "GET", "llmp.cc", "/api/v1/admin/users", logins[0].AccessToken, "").Code)
		selection := brandHTTP(r, "GET", "llmp.org", "/api/v1/admin/users?brand_id=2", logins[0].AccessToken, "")
		require.Equal(t, 200, selection.Code, selection.Body.String())
		require.NoError(t, store.SaveSettings(scopes[1], 2, map[string]any{"site_name": "Independent MUES", "smtp_host": "smtp.mues.test", "smtp_port": "587", "smtp_from": "mail@mues.test", "smtp_from_name": "MUES"}))
		smtp, err := email.GetSMTPConfig(scopes[1])
		require.NoError(t, err)
		require.Equal(t, "smtp.mues.test", smtp.Host)
		require.Equal(t, "mail@mues.test", smtp.From)
		backgroundScope := brand.WithScope(ctx, brand.Scope{ID: 2})
		origin, err := settingsRepo.GetValue(backgroundScope, service.SettingKeyAPIBaseURL)
		require.NoError(t, err)
		require.Equal(t, "https://mues.cc", origin)
		all, err := settings.GetAllSettings(scopes[1])
		require.NoError(t, err)
		require.Empty(t, all.LinuxDoConnectClientSecret)
		require.Empty(t, all.DingTalkConnectClientSecret)
		require.Empty(t, all.OIDCConnectClientSecret)
		stateKey := "notification_email_preference:v2:shared-http-test"
		require.NoError(t, settingsRepo.Set(scopes[0], stateKey, "unsubscribed"))
		_, err = settingsRepo.GetValue(scopes[1], stateKey)
		require.ErrorIs(t, err, service.ErrSettingNotFound)
		require.NoError(t, settingsRepo.Set(scopes[1], stateKey, "subscribed"))
		value, err := settingsRepo.GetValue(scopes[0], stateKey)
		require.NoError(t, err)
		require.Equal(t, "unsubscribed", value)
		id := int64(2)
		require.NoError(t, store.SaveGrant(ctx, brand.Grant{UserID: logins[1].User.ID, BrandID: &id, Role: "support"}))
		require.Equal(t, 403, brandHTTP(r, "POST", "mues.cc", "/api/v1/admin/brand-content/pages", logins[1].AccessToken, body).Code)
		// Headers cannot redirect the resolver to a different brand.
		req := httptest.NewRequest("GET", "https://unknown.test/api/v1/public/docs", nil)
		req.Header.Set("X-Forwarded-Host", "llmp.org")
		req.Header.Set("X-Brand-ID", "1")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 421, w.Code)
	})
	t.Run("signed merchant callbacks are isolated and idempotent", func(t *testing.T) {
		testMultiBrandPayments(t, ctx, scopes, client, db, store, settingsRepo, cfg, cache, logins)
	})
	t.Run("async audit batches retain the selected brand", func(t *testing.T) {
		repo := NewAuditLogRepository(db)
		entries := []*service.AuditLog{{BrandID: 1, Action: "llmp.audit.test", Method: "PUT", Path: "/test", StatusCode: 200}, {BrandID: 2, Action: "mues.audit.test", Method: "PUT", Path: "/test", StatusCode: 200}}
		count, err := repo.BatchInsert(ctx, entries)
		require.NoError(t, err)
		require.EqualValues(t, 2, count)
		for i := 0; i < 2; i++ {
			list, err := repo.List(scopes[i], &service.AuditLogFilter{Page: 1, PageSize: 10})
			require.NoError(t, err)
			require.Len(t, list.Logs, 1)
			require.Equal(t, int64(i+1), list.Logs[0].BrandID)
		}
	})
	if address := os.Getenv("MULTIBRAND_PREVIEW_LISTEN"); address != "" {
		require.True(t, web.HasEmbeddedFrontend(), "preview requires -tags embed")
		frontend, err := web.NewFrontendServer(settings)
		require.NoError(t, err)
		v1.GET("/auth/me", gin.HandlerFunc(jwtAuth), authHandler.GetCurrentUser)
		v1.GET("/settings/public", handler.NewSettingHandler(settings, "local-preview").GetPublicSettings)
		v1.POST("/auth/logout", func(c *gin.Context) { c.JSON(200, gin.H{"code": 0, "data": true}) })
		for i := range scopes {
			require.NoError(t, store.SaveSettings(scopes[i], int64(i+1), map[string]any{"email_verify_enabled": false, "password_reset_enabled": true}))
			for _, locale := range []string{"en", "zh"} {
				require.NoError(t, store.SavePage(scopes[i], &brand.Page{BrandID: int64(i + 1), Slug: "local-sdk", Locale: locale, Status: "published", Title: "SDK Quickstart", Category: "SDK", ContentMD: "# SDK Quickstart\n\nUse this brand endpoint: `{{canonical_api_origin}}`\n\n```js\nconst baseURL = '{{api_base_url}}'\n```"}))
			}
		}
		r.Use(frontend.Middleware())
		stopped := make(chan struct{})
		var stopOnce sync.Once
		localHosts := map[string]string{"llmp.localhost": "llmp.org", "mues.localhost": "mues.cc", "aisi.localhost": "aisi.plus", "codes.localhost": "opensi.codes", "in.localhost": "opensi.in"}
		server := &http.Server{Addr: address, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/__preview/stop" && req.Method == http.MethodPost {
				stopOnce.Do(func() { close(stopped) })
				w.WriteHeader(204)
				return
			}
			host := req.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			canonical, ok := localHosts[host]
			if !ok {
				http.Error(w, "Local preview host required", 421)
				return
			}
			req.Host = canonical
			r.ServeHTTP(w, req)
		})}
		listener, err := net.Listen("tcp", address)
		require.NoError(t, err)
		go func() {
			if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
				t.Log(err)
			}
		}()
		t.Logf("Local preview ready at http://llmp.localhost:28080; fixture login: http-shared@multibrand.test / NewPassword!")
		select {
		case <-stopped:
		case <-time.After(25 * time.Minute):
		}
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		require.NoError(t, server.Shutdown(shutdownCtx))
	}
}

func testMultiBrandPayments(t *testing.T, ctx context.Context, scopes []context.Context, client *ent.Client, db *sql.DB, store *brand.Store, settings service.SettingRepository, cfg *config.Config, cache *redis.Client, users []handler.AuthResponse) {
	refundGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"refund success"}`))
	}))
	defer refundGateway.Close()
	registry := payment.NewRegistry()
	userRepo := NewUserRepository(client, db)
	groupRepo := NewGroupRepository(client, db)
	redeem := service.NewRedeemService(NewRedeemCodeRepository(client), userRepo, nil, NewRedeemCache(cache), nil, client, nil, nil)
	paymentConfig := service.NewPaymentConfigService(client, settings, []byte(strings.Repeat("e", 32)))
	svc := service.ProvidePaymentService(client, registry, payment.NewDefaultLoadBalancer(client, []byte(strings.Repeat("e", 32))), redeem, nil, paymentConfig, userRepo, groupRepo, nil, nil, store, cfg)
	webhook := handler.NewPaymentWebhookHandler(svc, registry)
	r := gin.New()
	r.Use(middleware.BrandResolver(cfg, store))
	r.POST("/api/v1/payment/webhook/easypay", webhook.EasyPayNotify)
	for i := 0; i < 2; i++ {
		pid := fmt.Sprint(200 + i)
		pkey := fmt.Sprintf("merchant-brand-%d-secret", i)
		raw, _ := json.Marshal(map[string]string{"pid": pid, "pkey": pkey, "apiBase": refundGateway.URL, "notifyUrl": "https://" + []string{"llmp.org", "mues.cc"}[i] + "/api/v1/payment/webhook/easypay", "returnUrl": "https://unused.test"})
		instance, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).SetName("local merchant").SetConfig(string(raw)).SetRefundEnabled(true).Save(scopes[i])
		require.NoError(t, err)
		order, err := client.PaymentOrder.Create().SetUserID(users[i].User.ID).SetUserEmail(users[i].User.Email).SetUserName("test").
			SetAmount(10).SetPayAmount(70).SetRechargeCode(fmt.Sprintf("brand-credit-%d", i)).
			SetOutTradeNo(fmt.Sprintf("brand-payment-%d", i)).SetPaymentType("alipay").SetPaymentTradeNo("").
			SetProviderInstanceID(strconv.FormatInt(instance.ID, 10)).SetStatus(service.OrderStatusPending).SetClientIP("127.0.0.1").SetSrcHost("local-test").SetExpiresAt(time.Now().Add(time.Hour)).Save(scopes[i])
		require.NoError(t, err)
		before, err := userRepo.GetByID(scopes[i], users[i].User.ID)
		require.NoError(t, err)
		values := url.Values{"pid": {pid}, "out_trade_no": {order.OutTradeNo}, "trade_no": {fmt.Sprintf("gateway-trade-%d", i)}, "money": {"70.00"}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}, "sign_type": {"MD5"}}
		sign := func(secret string) string {
			keys := []string{}
			for key := range values {
				if key != "sign" && key != "sign_type" {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			parts := []string{}
			for _, key := range keys {
				parts = append(parts, key+"="+values.Get(key))
			}
			sum := md5.Sum([]byte(strings.Join(parts, "&") + secret))
			return hex.EncodeToString(sum[:])
		}
		values.Set("sign", sign("wrong-merchant"))
		call := func() *httptest.ResponseRecorder {
			req := httptest.NewRequest("POST", "https://unlisted-payment.test/api/v1/payment/webhook/easypay", bytes.NewBufferString(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w
		}
		w := call()
		require.NotEqual(t, "success", strings.TrimSpace(w.Body.String()))
		after, err := userRepo.GetByID(scopes[i], users[i].User.ID)
		require.NoError(t, err)
		require.Equal(t, before.Balance, after.Balance)
		values.Set("sign", sign(pkey))
		for n := 0; n < 2; n++ {
			w = call()
			require.Equal(t, "success", strings.TrimSpace(w.Body.String()), w.Body.String())
		}
		after, err = userRepo.GetByID(scopes[i], users[i].User.ID)
		require.NoError(t, err)
		require.InDelta(t, before.Balance+10, after.Balance, 1e-8)
		require.Error(t, svc.HandlePaymentNotification(scopes[(i+1)%2], &payment.PaymentNotification{OrderID: order.OutTradeNo, Status: payment.NotificationStatusSuccess, Amount: 70, TradeNo: "wrong-brand"}, payment.TypeEasyPay))
		require.Error(t, svc.ExecuteBalanceFulfillment(scopes[(i+1)%2], order.ID))
		other, err := userRepo.GetByID(scopes[(i+1)%2], users[(i+1)%2].User.ID)
		require.NoError(t, err)
		_, _, err = svc.PrepareRefund(scopes[(i+1)%2], order.ID, 5, "cross-brand", false, true)
		require.Error(t, err)
		plan, warning, err := svc.PrepareRefund(scopes[i], order.ID, 5, "local refund", false, true)
		require.NoError(t, err)
		require.Nil(t, warning)
		_, err = svc.ExecuteRefund(scopes[(i+1)%2], plan)
		require.Error(t, err)
		result, err := svc.ExecuteRefund(scopes[i], plan)
		require.NoError(t, err)
		require.True(t, result.Success)
		after, err = userRepo.GetByID(scopes[i], users[i].User.ID)
		require.NoError(t, err)
		require.InDelta(t, before.Balance+5, after.Balance, 1e-8)
		stats, err := store.Summary(scopes[i])
		require.NoError(t, err)
		require.Len(t, stats.Payments, 1)
		require.Equal(t, "CNY", stats.Payments[0].Currency)
		require.InDelta(t, 70, stats.Payments[0].Collected, 1e-8)
		require.InDelta(t, 35, stats.Payments[0].Refunded, 1e-8)
		unchanged, err := userRepo.GetByID(scopes[(i+1)%2], other.ID)
		require.NoError(t, err)
		require.Equal(t, other.Balance, unchanged.Balance)
		w = call()
		require.Equal(t, "success", strings.TrimSpace(w.Body.String()), "late callback must not credit a refunded order")
		after, err = userRepo.GetByID(scopes[i], users[i].User.ID)
		require.NoError(t, err)
		require.InDelta(t, before.Balance+5, after.Balance, 1e-8)
	}
}
