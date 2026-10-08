package service

import (
	"context"
	"errors"
	"time"
)

var ErrOAuthStateInvalid = errors.New("oauth state is invalid or expired")

type OAuthState struct {
	BrandID  int64  `json:"brand_id"`
	Hostname string `json:"hostname"`
	Nonce    string `json:"nonce"`
}

type OAuthStateStore interface {
	Store(ctx context.Context, state *OAuthState, ttl time.Duration) (string, error)
	Consume(ctx context.Context, token string) (*OAuthState, error)
}
