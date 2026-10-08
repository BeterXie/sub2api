package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) generateOAuthState(c *gin.Context) (string, error) {
	nonce, err := oauth.GenerateState()
	if err != nil {
		return "", err
	}
	scope, ok := brand.FromContext(c.Request.Context())
	if !ok {
		return nonce, nil
	}
	if h.oauthStateStore == nil {
		return "", brand.ErrScope
	}
	return h.oauthStateStore.Store(c.Request.Context(), &service.OAuthState{
		BrandID: scope.ID, Hostname: scope.Hostname, Nonce: nonce,
	}, 10*time.Minute)
}

func (h *AuthHandler) validOAuthState(c *gin.Context, value string) bool {
	scope, scoped := brand.FromContext(c.Request.Context())
	if !scoped {
		return h.cfg == nil || !h.cfg.MultiBrand.Enabled
	}
	if h.oauthStateStore == nil {
		return false
	}
	state, err := h.oauthStateStore.Consume(c.Request.Context(), value)
	if err != nil || state == nil || state.Nonce == "" || state.BrandID != scope.ID || state.Hostname != scope.Hostname {
		return false
	}
	// The signed state and the whitelisted callback must agree before any
	// identity lookup or token exchange. Never move credentials between hosts.
	c.Request = c.Request.WithContext(brand.WithScope(c.Request.Context(), scope))
	return true
}
