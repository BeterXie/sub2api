package repository

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const oauthStatePrefix = "oauth:state:"

type oauthStateStore struct {
	redis *redis.Client
}

func NewOAuthStateStore(redisClient *redis.Client) service.OAuthStateStore {
	return &oauthStateStore{redis: redisClient}
}

func (s *oauthStateStore) Store(ctx context.Context, state *service.OAuthState, ttl time.Duration) (string, error) {
	if s == nil || s.redis == nil || state == nil || state.BrandID <= 0 || strings.TrimSpace(state.Hostname) == "" || strings.TrimSpace(state.Nonce) == "" || ttl <= 0 {
		return "", fmt.Errorf("invalid oauth state")
	}
	if state.BrandID != brand.ID(ctx) {
		return "", brand.ErrScope
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate oauth state token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode oauth state: %w", err)
	}
	if err := s.redis.Set(ctx, brand.CacheKey(ctx, oauthStatePrefix+token), payload, ttl).Err(); err != nil {
		return "", fmt.Errorf("store oauth state: %w", err)
	}
	return token, nil
}

func (s *oauthStateStore) Consume(ctx context.Context, token string) (*service.OAuthState, error) {
	token = strings.TrimSpace(token)
	if s == nil || s.redis == nil || token == "" || len(token) > 128 {
		return nil, service.ErrOAuthStateInvalid
	}
	payload, err := s.redis.GetDel(ctx, brand.CacheKey(ctx, oauthStatePrefix+token)).Bytes()
	if err == redis.Nil {
		return nil, service.ErrOAuthStateInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("consume oauth state: %w", err)
	}
	var state service.OAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, service.ErrOAuthStateInvalid
	}
	return &state, nil
}
