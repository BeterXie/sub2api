//go:build unit

package routes

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBrandMediaRejectsSymlinkOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{"root", "brand", "asset"} {
		t.Run(target, func(t *testing.T) {
			dataDir := t.TempDir()
			foreign := t.TempDir()
			name := "0123456789abcdef0123456789abcdef.png"
			require.NoError(t, os.WriteFile(filepath.Join(foreign, name), []byte("foreign content"), 0600))
			root := filepath.Join(dataDir, "brand-media")
			dir := filepath.Join(root, "mues")
			switch target {
			case "root":
				require.NoError(t, os.MkdirAll(filepath.Join(foreign, "mues"), 0700))
				require.NoError(t, os.Symlink(foreign, root))
			case "brand":
				require.NoError(t, os.MkdirAll(root, 0700))
				require.NoError(t, os.Symlink(foreign, dir))
			case "asset":
				require.NoError(t, os.MkdirAll(dir, 0700))
				require.NoError(t, os.Symlink(filepath.Join(foreign, name), filepath.Join(dir, name)))
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(brand.WithScope(c.Request.Context(), brand.Scope{ID: 2, Code: "mues"}))
				c.Next()
			})
			v1 := r.Group("/api/v1")
			registerBrandMedia(v1, v1.Group("/admin/brand-content"), dataDir)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/brand-media/"+name, nil))
			require.Equal(t, 404, w.Code)
			require.NotContains(t, w.Body.String(), "foreign content")
		})
	}
}
