//go:build multibrand

package middleware

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestMultiBrandCapacity(t *testing.T) {
	addr := os.Getenv("MULTIBRAND_TEST_REDIS")
	require.NotEmpty(t, addr)
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 14})
	defer client.Close()
	ctx := context.Background()
	require.NoError(t, client.Ping(ctx).Err())
	id := time.Now().UnixNano()
	serve := func(scope brand.Scope, handler gin.HandlerFunc) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(brand.WithScope(c.Request.Context(), scope))
			c.Set(string(ContextKeyAPIKey), &service.APIKey{BrandID: scope.ID})
			c.Next()
		}, BrandGatewayCapacity(client))
		r.GET("/stream", handler)
		return r
	}
	request := func(r *gin.Engine) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/stream", nil))
		return w
	}
	t.Run("RPM is independent per brand", func(t *testing.T) {
		a := serve(brand.Scope{ID: id, RPMLimit: 1}, func(c *gin.Context) { c.Status(200) })
		b := serve(brand.Scope{ID: id + 1, RPMLimit: 1}, func(c *gin.Context) { c.Status(200) })
		require.Equal(t, 200, request(a).Code)
		require.Equal(t, 429, request(a).Code)
		require.Equal(t, 200, request(b).Code)
	})
	t.Run("streams hold and renew slots; cancellation releases them", func(t *testing.T) {
		scope := brand.Scope{ID: id + 2, MaxConcurrent: 1}
		started := make(chan struct{})
		release := make(chan struct{})
		done := make(chan struct{})
		r := serve(scope, func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.Writer.WriteString("data: started\n\n")
			c.Writer.Flush()
			close(started)
			select {
			case <-release:
			case <-c.Request.Context().Done():
			}
		})
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		req := httptest.NewRequest("GET", "/stream", nil).WithContext(cancelCtx)
		w := httptest.NewRecorder()
		go func() { defer close(done); r.ServeHTTP(w, req) }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("stream did not start")
		}
		prefix := "brand:{" + strconv.FormatInt(scope.ID, 10) + "}:gateway:slots"
		slots, err := client.ZRangeWithScores(ctx, prefix, 0, -1).Result()
		require.NoError(t, err)
		require.Len(t, slots, 1)
		before := slots[0].Score
		rejected := request(r)
		require.Equal(t, 429, rejected.Code)
		other := serve(brand.Scope{ID: id + 3, MaxConcurrent: 1}, func(c *gin.Context) { c.Status(200) })
		require.Equal(t, 200, request(other).Code)
		require.Eventually(t, func() bool {
			values, err := client.ZRangeWithScores(ctx, prefix, 0, -1).Result()
			return err == nil && len(values) == 1 && values[0].Score > before
		}, 24*time.Second, 250*time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("cancelled stream did not exit")
		}
		require.EqualValues(t, 0, client.ZCard(ctx, prefix).Val())
		require.Contains(t, w.Body.String(), "data: started")
	})
	t.Run("lost lease cancels its stream", func(t *testing.T) {
		scope := brand.Scope{ID: id + 4, MaxConcurrent: 1}
		started := make(chan struct{})
		done := make(chan struct{})
		r := serve(scope, func(c *gin.Context) { close(started); <-c.Request.Context().Done() })
		req := httptest.NewRequest("GET", "/stream", nil)
		w := httptest.NewRecorder()
		go func() { defer close(done); r.ServeHTTP(w, req) }()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("stream did not start")
		}
		require.NoError(t, client.Del(ctx, fmt.Sprintf("brand:{%d}:gateway:slots", scope.ID)).Err())
		select {
		case <-done:
		case <-time.After(24 * time.Second):
			t.Fatal("lost lease did not cancel stream")
		}
	})
}
