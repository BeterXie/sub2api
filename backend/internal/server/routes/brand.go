package routes

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func brandResult(c *gin.Context, value any, err error) {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			middleware.AbortWithError(c, 404, "NOT_FOUND", "Record not found or changed")
			return
		}
		middleware.AbortWithError(c, 400, "INVALID_BRAND_OPERATION", "Invalid brand operation")
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": value})
}
func brandParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		middleware.AbortWithError(c, 400, "INVALID_ID", "Invalid ID")
		return 0, false
	}
	return id, true
}
func RegisterBrandRoutes(v1 *gin.RouterGroup, store *brand.Store, cfg *config.Config, jwtAuth middleware.JWTAuthMiddleware, adminAuth middleware.AdminAuthMiddleware, auditLog middleware.AuditLogMiddleware, settings *service.SettingService, panelRateLimiter *middleware.PanelRateLimiter) {
	public := v1.Group("/public", panelRateLimiter.PublicIP())
	public.GET("/brand-config", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		scope, ok := brand.FromContext(c.Request.Context())
		if !ok || !cfg.MultiBrand.Enabled {
			brandResult(c, gin.H{"enabled": false}, nil)
			return
		}
		values, err := store.Settings(c.Request.Context(), scope)
		if err != nil {
			brandResult(c, nil, err)
			return
		}
		public, err := settings.GetPublicSettingsForInjection(c.Request.Context())
		if err != nil {
			brandResult(c, nil, err)
			return
		}
		brandResult(c, gin.H{"enabled": true, "brand": scope, "settings": brand.PublicPresentation(values), "public_settings": public}, nil)
	})
	public.GET("/docs", func(c *gin.Context) {
		pages, err := store.Pages(c.Request.Context(), brand.ID(c.Request.Context()), true)
		// Index returns metadata only; full documents are fetched by slug.
		for i := range pages {
			pages[i].ContentMD = ""
		}
		brandResult(c, pages, err)
	})
	public.GET("/docs/:slug", func(c *gin.Context) {
		locale := c.DefaultQuery("locale", "zh")
		p, err := store.Page(c.Request.Context(), brand.ID(c.Request.Context()), c.Param("slug"), locale, true)
		brandResult(c, p, err)
	})
	v1.GET("/user/brand-access", gin.HandlerFunc(jwtAuth), panelRateLimiter.Global(), func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok {
			middleware.AbortWithError(c, 401, "UNAUTHORIZED", "Authorization required")
			return
		}
		role, err := store.AdminRole(c.Request.Context(), subject.UserID, brand.ID(c.Request.Context()))
		brandResult(c, gin.H{"role": role, "brand_id": brand.ID(c.Request.Context())}, err)
	})
	admin := v1.Group("/admin/brands", gin.HandlerFunc(adminAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
	invalidate := func(c *gin.Context) {
		c.Next()
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Writer.Status() < 300 {
			settings.InvalidatePublicSettings()
		}
	}
	admin.Use(invalidate)
	admin.GET("", func(c *gin.Context) { values, err := store.List(c.Request.Context()); brandResult(c, values, err) })
	admin.POST("", func(c *gin.Context) {
		var b brand.Brand
		if c.ShouldBindJSON(&b) != nil {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		b.ID = 0
		err := store.SaveBrand(c.Request.Context(), &b)
		brandResult(c, b, err)
	})
	admin.PUT("/:id", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		var b brand.Brand
		if c.ShouldBindJSON(&b) != nil {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		b.ID = id
		err := store.SaveBrand(c.Request.Context(), &b)
		brandResult(c, b, err)
	})
	admin.DELETE("/:id", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		brandResult(c, true, store.DeleteBrand(c.Request.Context(), id))
	})
	admin.GET("/:id/domains", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		values, err := store.Domains(c.Request.Context(), id)
		brandResult(c, values, err)
	})
	saveDomain := func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		var d brand.Domain
		if c.ShouldBindJSON(&d) != nil {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		d.BrandID = id
		d.ID = 0
		if c.Param("domainId") != "" {
			var valid bool
			d.ID, valid = brandParam(c, "domainId")
			if !valid {
				return
			}
		}
		err := store.SaveDomain(c.Request.Context(), &d)
		brandResult(c, d, err)
	}
	admin.POST("/:id/domains", saveDomain)
	admin.PUT("/:id/domains/:domainId", saveDomain)
	admin.DELETE("/:id/domains/:domainId", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		domainID, ok := brandParam(c, "domainId")
		if !ok {
			return
		}
		brandResult(c, true, store.DeleteDomain(c.Request.Context(), id, domainID))
	})
	admin.GET("/:id/admins", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		values, err := store.Grants(c.Request.Context(), id)
		brandResult(c, values, err)
	})
	admin.POST("/:id/admins", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		var g brand.Grant
		if c.ShouldBindJSON(&g) != nil {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		g.BrandID = &id
		brandResult(c, true, store.SaveGrant(c.Request.Context(), g))
	})
	admin.DELETE("/:id/admins/:userId", func(c *gin.Context) {
		id, ok := brandParam(c, "id")
		if !ok {
			return
		}
		userID, ok := brandParam(c, "userId")
		if !ok {
			return
		}
		brandResult(c, true, store.DeleteGrant(c.Request.Context(), id, userID))
	})

	// Content routes take only the scope authorized by admin middleware.
	content := v1.Group("/admin/brand-content", gin.HandlerFunc(adminAuth), panelRateLimiter.Global(), gin.HandlerFunc(auditLog))
	content.Use(invalidate)
	content.GET("/settings", func(c *gin.Context) {
		scope, _ := brand.FromContext(c.Request.Context())
		if scope.ID == 0 {
			scope.ID = brand.LegacyID
		}
		values, err := store.Settings(c.Request.Context(), scope)
		brandResult(c, brand.RedactSettings(values), err)
	})
	content.PUT("/settings", func(c *gin.Context) {
		var values map[string]any
		if c.ShouldBindJSON(&values) != nil {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		brandResult(c, true, store.SaveSettings(c.Request.Context(), brand.ID(c.Request.Context()), values))
	})
	content.GET("/pages", func(c *gin.Context) {
		values, err := store.Pages(c.Request.Context(), brand.ID(c.Request.Context()), false)
		brandResult(c, values, err)
	})
	savePage := func(c *gin.Context) {
		var p brand.Page
		if c.ShouldBindJSON(&p) != nil {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		p.BrandID = brand.ID(c.Request.Context())
		p.ID = 0
		if c.Param("pageId") != "" {
			var ok bool
			p.ID, ok = brandParam(c, "pageId")
			if !ok {
				return
			}
		}
		err := store.SavePage(c.Request.Context(), &p)
		brandResult(c, p, err)
	}
	content.POST("/pages", savePage)
	content.PUT("/pages/:pageId", savePage)
	content.GET("/pages/:pageId/revisions", func(c *gin.Context) {
		id, ok := brandParam(c, "pageId")
		if !ok {
			return
		}
		values, err := store.Revisions(c.Request.Context(), brand.ID(c.Request.Context()), id)
		brandResult(c, values, err)
	})
	content.GET("/summary", panelRateLimiter.Heavy(), func(c *gin.Context) {
		value, err := store.Summary(c.Request.Context())
		brandResult(c, value, err)
	})
	registerBrandMedia(v1, content, cfg.Pricing.DataDir)
}
