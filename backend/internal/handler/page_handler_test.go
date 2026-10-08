package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCleanPageImageRelativePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "single filename", in: "logo.png", want: "logo.png", ok: true},
		{name: "nested path", in: "images/logo.png", want: filepath.Join("images", "logo.png"), ok: true},
		{name: "dot prefix", in: "./logo.png", want: "logo.png", ok: true},
		{name: "url escaped slash", in: "images%2Flogo.png", want: filepath.Join("images", "logo.png"), ok: true},
		{name: "parent traversal", in: "../secret.png", ok: false},
		{name: "encoded parent traversal", in: "%2e%2e/secret.png", ok: false},
		{name: "backslash traversal", in: `images\secret.png`, ok: false},
		{name: "absolute path", in: "/etc/passwd", ok: false},
		{name: "encoded absolute path", in: "%2fetc/passwd", ok: false},
		{name: "encoded nul byte", in: "logo.png%00", ok: false},
		{name: "invalid escape", in: "logo.png%zz", ok: false},
		{name: "empty path", in: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cleanPageImageRelativePath(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePageImagePath(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")
	base := filepath.Join(pagesDir, "guide")
	if err := os.MkdirAll(filepath.Join(base, "images"), 0755); err != nil {
		t.Fatalf("create images dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "logo.png"), []byte("fake"), 0644); err != nil {
		t.Fatalf("create direct image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "images", "logo.png"), []byte("fake"), 0644); err != nil {
		t.Fatalf("create image: %v", err)
	}

	got, ok := resolvePageImagePath(pagesDir, base, "logo.png")
	if !ok {
		t.Fatal("expected direct image path to be accepted")
	}
	want := mustEvalSymlinks(t, filepath.Join(base, "logo.png"))
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	got, ok = resolvePageImagePath(pagesDir, base, "images/logo.png")
	if !ok {
		t.Fatal("expected nested image path to be accepted")
	}
	want = mustEvalSymlinks(t, filepath.Join(base, "images", "logo.png"))
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	if got, ok := resolvePageImagePath(pagesDir, base, "../guide.md"); ok {
		t.Fatalf("expected traversal to be rejected, got %q", got)
	}
}

func TestResolvePageImagePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")
	base := filepath.Join(pagesDir, "guide")
	outside := filepath.Join(root, "outside")

	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatalf("create page dir: %v", err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("secret"), 0644); err != nil {
		t.Fatalf("create outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "images")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	if got, ok := resolvePageImagePath(pagesDir, base, "images/secret.png"); ok {
		t.Fatalf("expected symlink escape to be rejected, got %q", got)
	}
}

func TestLegacyBrandPageSourcesMergeAndImagesFallbackByFile(t *testing.T) {
	pagesDir := filepath.Join(t.TempDir(), "pages")
	brandDir := filepath.Join(pagesDir, "llmp")
	require.NoError(t, os.MkdirAll(filepath.Join(brandDir, "guide"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(pagesDir, "guide"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(brandDir, "brand-only.md"), []byte("brand"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pagesDir, "legacy-only.md"), []byte("legacy"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(brandDir, "shared.md"), []byte("brand shared"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(pagesDir, "shared.md"), []byte("legacy shared"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(brandDir, "guide.md"), []byte("guide"), 0644))
	legacyImage := filepath.Join(pagesDir, "guide", "logo.png")
	require.NoError(t, os.WriteFile(legacyImage, []byte("image"), 0644))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages", nil)
	c.Request = req.WithContext(brand.WithScope(req.Context(), brand.Scope{ID: brand.LegacyID, Code: "llmp"}))
	handler := &PageHandler{pagesDir: pagesDir}

	require.Equal(t, brandDir, handler.pageDirectory(c, "shared"))
	image, ok := handler.pageImagePath(c, "guide", "logo.png")
	require.True(t, ok)
	require.Equal(t, mustEvalSymlinks(t, legacyImage), image)

	handler.ListPages(c)
	require.JSONEq(t, `{"code":0,"message":"success","data":["brand-only","guide","legacy-only","shared"]}`, w.Body.String())
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()

	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("eval symlinks for %q: %v", path, err)
	}
	return realPath
}
