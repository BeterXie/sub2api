package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuthBrandState(t *testing.T) {
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer redisClient.Close()
	h := &AuthHandler{
		cfg:             &config.Config{MultiBrand: config.MultiBrandConfig{Enabled: true}},
		oauthStateStore: repository.NewOAuthStateStore(redisClient),
	}
	contextFor := func(id int64, host string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "https://"+host+"/callback", nil)
		c.Request = c.Request.WithContext(brand.WithScope(context.Background(), brand.Scope{ID: id, Hostname: host}))
		return c
	}
	c := contextFor(1, "llmp.org")
	state, err := h.generateOAuthState(c)
	require.NoError(t, err)
	require.LessOrEqual(t, len(state), 128)
	require.True(t, h.validOAuthState(c, state))
	require.False(t, h.validOAuthState(c, state), "state must be consumed exactly once")

	state, err = h.generateOAuthState(c)
	require.NoError(t, err)
	require.False(t, h.validOAuthState(contextFor(2, "mues.cc"), state))
	require.True(t, h.validOAuthState(c, state), "a foreign brand namespace must not consume the state")

	state, err = h.generateOAuthState(c)
	require.NoError(t, err)
	require.False(t, h.validOAuthState(contextFor(1, "llmp.cc"), state))
	require.False(t, h.validOAuthState(c, state), "a same-brand hostname mismatch consumes the attempted state")

	state, err = h.generateOAuthState(c)
	require.NoError(t, err)
	require.False(t, h.validOAuthState(c, state+"tampered"))

	state, err = h.generateOAuthState(c)
	require.NoError(t, err)
	redisServer.FastForward(11 * time.Minute)
	require.False(t, h.validOAuthState(c, state))
}
