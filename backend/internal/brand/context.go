// Package brand defines the trusted scope carried from ingress through persistence.
package brand

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const LegacyID int64 = 1
const RuntimeRole = "sub2api_brand_runtime"

var (
	ErrUnknownDomain = errors.New("unknown domain")
	ErrUnavailable   = errors.New("brand or domain is unavailable")
	ErrScope         = errors.New("brand scope mismatch")
)

type Scope struct {
	ID                  int64  `json:"brand_id"`
	Code                string `json:"brand_code"`
	Name                string `json:"name"`
	DomainID            int64  `json:"domain_id"`
	Hostname            string `json:"hostname"`
	CanonicalAPIOrigin  string `json:"canonical_api_origin"`
	PublicEnabled       bool   `json:"public_enabled"`
	GatewayEnabled      bool   `json:"gateway_enabled"`
	RegistrationEnabled bool   `json:"registration_enabled"`
	MaxConcurrent       int    `json:"max_concurrent"`
	RPMLimit            int    `json:"rpm_limit"`
	// Platform is set only after checking an explicit platform administrator grant.
	Platform bool `json:"-"`
}

type scopeKey struct{}
type storeKey struct{}

func WithStore(ctx context.Context, store *Store) context.Context {
	return context.WithValue(ctx, storeKey{}, store)
}
func StoreFromContext(ctx context.Context) *Store {
	s, _ := ctx.Value(storeKey{}).(*Store)
	return s
}

// CredentialContext is used only to load the identity represented by a secret.
// Callers must compare that identity's brand to the original request afterwards.
func CredentialContext(ctx context.Context) context.Context {
	if s, ok := FromContext(ctx); ok {
		s.Platform = true
		return WithScope(ctx, s)
	}
	return ctx
}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func FromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	return s, ok && s.ID > 0
}

func ID(ctx context.Context) int64 {
	if s, ok := FromContext(ctx); ok {
		return s.ID
	}
	return LegacyID
}

func IsPlatform(ctx context.Context) bool {
	s, ok := FromContext(ctx)
	return ok && s.Platform
}

// Detached preserves tenant authority when work outlives the HTTP connection.
func Detached(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

func CacheKey(ctx context.Context, key string) string {
	if s, ok := FromContext(ctx); ok {
		if s.Platform {
			return "brand:platform:" + key
		}
		return "brand:" + strconv.FormatInt(s.ID, 10) + ":" + key
	}
	return key
}

func Matches(ctx context.Context, id int64) bool {
	s, ok := FromContext(ctx)
	return !ok || s.Platform || s.ID == id
}

func TokenMatches(ctx context.Context, tokenBrand, userBrand int64, legacyUntil time.Time) bool {
	if _, ok := FromContext(ctx); !ok {
		return true
	}
	if tokenBrand == 0 {
		return userBrand == LegacyID && ID(ctx) == LegacyID && time.Now().Before(legacyUntil)
	}
	return tokenBrand == userBrand && ID(ctx) == userBrand
}

// NormalizeHost reads only Request.Host. Forwarded host and client brand headers
// never participate in domain selection.
func NormalizeHost(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "/\\@?#, \t\r\n") {
		return "", ErrUnknownDomain
	}
	host := raw
	if strings.Contains(raw, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(raw)
		if err != nil {
			return "", ErrUnknownDomain
		}
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return "", ErrUnknownDomain
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	host, err := idna.Lookup.ToASCII(host)
	if err != nil || host == "" || len(host) > 253 || net.ParseIP(host) != nil {
		return "", ErrUnknownDomain
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrUnknownDomain
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return "", ErrUnknownDomain
			}
		}
	}
	return host, nil
}
