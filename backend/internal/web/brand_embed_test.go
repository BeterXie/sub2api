//go:build embed

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type brandPublicProvider struct{ fail bool }

func (p *brandPublicProvider) GetPublicSettingsForInjection(ctx context.Context) (any, error) {
	if p.fail {
		return nil, context.DeadlineExceeded
	}
	scope, _ := brand.FromContext(ctx)
	return map[string]any{"site_name": scope.Name, "site_logo": "/brands/" + scope.Code + "/logo.svg"}, nil
}
func TestBrandEmbeddedHTML(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	store := brand.NewStore(db)
	provider := &brandPublicProvider{}
	server, err := NewFrontendServer(provider)
	require.NoError(t, err)
	a := brand.Scope{ID: 1, DomainID: 1, Code: "llmp", Hostname: "llmp.org", Name: "LLMP", CanonicalAPIOrigin: "https://llmp.org"}
	b := brand.Scope{ID: 2, DomainID: 5, Code: "mues", Hostname: "mues.cc", Name: "MUES", CanonicalAPIOrigin: "https://mues.cc"}
	expect := func(scope brand.Scope, title string) {
		if scope.ID == 1 {
			mock.ExpectQuery("SELECT key,value FROM settings").WillReturnRows(sqlmock.NewRows([]string{"key", "value"}))
		}
		mock.ExpectQuery("SELECT key,value_json FROM brand_settings").WithArgs(scope.ID).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value_json"}).AddRow("seo_title", `"`+title+`"`).AddRow("smtp_password", `"never-in-html"`))
		mock.ExpectQuery("SELECT overrides FROM domains").WithArgs(scope.DomainID, scope.ID).
			WillReturnRows(sqlmock.NewRows([]string{"overrides"}).AddRow(`{}`))
	}
	request := func(scope brand.Scope, path, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "https://"+scope.Hostname+path, nil)
		req = req.WithContext(brand.WithStore(brand.WithScope(req.Context(), scope), store))
		req.Header.Set("If-None-Match", etag)
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.CSPNonceKey, "brand-test-nonce"); c.Next() }, server.Middleware())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	expect(a, "LLMP")
	first := request(a, "/docs/sdk", "")
	require.Equal(t, 200, first.Code)
	require.Contains(t, first.Body.String(), `href="https://llmp.org/docs/sdk"`)
	require.NotContains(t, first.Body.String(), "never-in-html")
	otherPath := request(a, "/admin/users", "")
	require.Equal(t, 200, otherPath.Code)
	require.Contains(t, otherPath.Body.String(), `href="https://llmp.org/admin/users"`)
	require.NotEqual(t, first.Header().Get("ETag"), otherPath.Header().Get("ETag"))
	require.Len(t, server.cache.entries, 1)
	expect(b, "MUES")
	second := request(b, "/docs/sdk", first.Header().Get("ETag"))
	require.Equal(t, 200, second.Code)
	require.NotEqual(t, first.Header().Get("ETag"), second.Header().Get("ETag"))
	require.Contains(t, second.Body.String(), "/brands/mues/logo.svg")
	require.Equal(t, 304, request(a, "/docs/sdk", first.Header().Get("ETag")).Code)
	alias := a
	alias.Hostname = "llmp.cc"
	alias.DomainID = 2
	expect(alias, "LLMP alias")
	third := request(alias, "/", "")
	require.Contains(t, third.Body.String(), "<title>LLMP alias</title>")
	require.NotEqual(t, first.Header().Get("ETag"), third.Header().Get("ETag"))
	server.InvalidateCache()
	expect(b, "MUES updated")
	updated := request(b, "/docs/sdk", "")
	require.Contains(t, updated.Body.String(), "<title>MUES updated</title>")
	require.NotEqual(t, second.Header().Get("ETag"), updated.Header().Get("ETag"))
	server.InvalidateCache()
	provider.fail = true
	require.Equal(t, 503, request(b, "/", "").Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
