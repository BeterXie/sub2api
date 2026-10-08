package routes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

const brandMediaMaxSize = 5 << 20

var brandMediaName = regexp.MustCompile(`^[a-f0-9]{32}\.(png|jpg|gif|webp)$`)

func brandMediaDirectory(dataDir, code string, create bool) (string, error) {
	if !brand.ValidSlug(code) {
		return "", brand.ErrScope
	}
	root := filepath.Join(dataDir, "brand-media")
	dir := filepath.Join(root, code)
	if create {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return "", err
		}
	}
	// Managed directories cannot point to another brand through a symlink.
	for _, path := range []string{root, dir} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", brand.ErrScope
		}
	}
	return dir, nil
}
func registerBrandMedia(v1, content *gin.RouterGroup, dataDir string) {
	serve := func(c *gin.Context) {
		name := c.Param("asset")
		if !brandMediaName.MatchString(name) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		scope, ok := brand.FromContext(c.Request.Context())
		if !ok {
			scope = brand.Scope{ID: 1, Code: "llmp"}
		}
		dir, err := brandMediaDirectory(dataDir, scope.Code, false)
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		file := filepath.Join(dir, name)
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "private, max-age=300")
		if strings.Contains(c.Request.URL.Path, "/admin/") {
			c.Header("Cache-Control", "no-store")
		}
		c.File(file)
	}
	v1.GET("/public/brand-media/:asset", serve)
	content.GET("/media/:asset", serve)
	content.POST("/media", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, brandMediaMaxSize+1<<20)
		file, err := c.FormFile("file")
		if err != nil || file.Size > brandMediaMaxSize {
			middleware.AbortWithError(c, 400, "INVALID_MEDIA", "Upload a PNG, JPEG, GIF or WebP image up to 5 MB")
			return
		}
		source, err := file.Open()
		if err != nil {
			brandResult(c, nil, err)
			return
		}
		defer source.Close()
		data, err := io.ReadAll(io.LimitReader(source, brandMediaMaxSize+1))
		if err != nil || len(data) > brandMediaMaxSize || len(data) == 0 {
			brandResult(c, nil, brand.ErrScope)
			return
		}
		ext, ok := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[http.DetectContentType(data)]
		if !ok {
			middleware.AbortWithError(c, 400, "INVALID_MEDIA", "Unsupported image type")
			return
		}
		scope, ok := brand.FromContext(c.Request.Context())
		if !ok {
			scope = brand.Scope{ID: 1, Code: "llmp"}
		}
		dir, err := brandMediaDirectory(dataDir, scope.Code, true)
		if err != nil {
			brandResult(c, nil, err)
			return
		}
		random := make([]byte, 16)
		if _, err = rand.Read(random); err != nil {
			brandResult(c, nil, err)
			return
		}
		name := hex.EncodeToString(random) + ext
		path := filepath.Join(dir, name)
		target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
		if err != nil {
			brandResult(c, nil, err)
			return
		}
		_, writeErr := target.Write(data)
		closeErr := target.Close()
		if err = errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(path)
			brandResult(c, nil, err)
			return
		}
		brandResult(c, gin.H{"asset": name, "url": "/api/v1/public/brand-media/" + name}, nil)
	})
}
