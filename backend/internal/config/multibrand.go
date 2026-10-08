package config

import (
	"fmt"
	"time"
)

// MultiBrandConfig contains the rollout switch and a finite JWT transition window.
// Domain and brand policy live in PostgreSQL.
type MultiBrandConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	LegacyJWTUntil string `mapstructure:"legacy_jwt_until"`
}

func (c MultiBrandConfig) LegacyDeadline() time.Time {
	t, _ := time.Parse(time.RFC3339, c.LegacyJWTUntil)
	return t
}

func (c MultiBrandConfig) Validate() error {
	if c.LegacyJWTUntil != "" {
		if _, err := time.Parse(time.RFC3339, c.LegacyJWTUntil); err != nil {
			return fmt.Errorf("multibrand.legacy_jwt_until must be an RFC3339 timestamp")
		}
	}
	return nil
}
