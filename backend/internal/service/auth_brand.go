package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/brand"
)

func (s *AuthService) RequestBrandMatches(ctx context.Context, claims *JWTClaims, user *User) bool {
	if claims == nil || user == nil {
		return false
	}
	if _, ok := brand.FromContext(ctx); !ok {
		return true
	}
	if s.cfg == nil {
		return false
	}
	return brand.TokenMatches(ctx, claims.BrandID, user.BrandID, s.cfg.MultiBrand.LegacyDeadline())
}
func (s *AuthService) refreshBrandMatches(ctx context.Context, data *RefreshTokenData) bool {
	if data == nil {
		return false
	}
	if _, ok := brand.FromContext(ctx); !ok {
		return true
	}
	if data.BrandID == 0 {
		return s.cfg != nil && brand.TokenMatches(ctx, 0, brand.LegacyID, s.cfg.MultiBrand.LegacyDeadline())
	}
	return data.BrandID == brand.ID(ctx)
}

// Loading a presented secret is platform-wide; admission always compares the
// resulting user, key and group to the original trusted Host scope.
func (s *APIKeyService) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	k, err := s.getByKey(brand.CredentialContext(ctx), key)
	if err != nil {
		return nil, err
	}
	if k == nil || k.User == nil {
		return nil, ErrAPIKeyNotFound
	}
	if !brand.Matches(ctx, k.BrandID) || !brand.Matches(ctx, k.User.BrandID) ||
		(k.Group != nil && (!brand.Matches(ctx, k.Group.BrandID) || k.Group.BrandID != k.User.BrandID)) {
		return nil, ErrAPIKeyNotFound
	}
	return k, nil
}
