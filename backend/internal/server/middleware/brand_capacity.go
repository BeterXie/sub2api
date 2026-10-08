package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/brand"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Like existing account/user concurrency leases, brand slots use Redis server
// time and expire after process loss. Live streams renew until their handler exits.
var brandAcquire = redis.NewScript(`
local t=redis.call('TIME')
local now=tonumber(t[1])
if tonumber(ARGV[2])>0 then
  local rpmKey=KEYS[2]..':'..math.floor(now/60)
  local used=redis.call('INCR',rpmKey)
  redis.call('EXPIRE',rpmKey,120)
  if used>tonumber(ARGV[2]) then return 2 end
end
if tonumber(ARGV[1])>0 then
  redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
  if redis.call('ZCARD',KEYS[1])>=tonumber(ARGV[1]) then return 3 end
  redis.call('ZADD',KEYS[1],now+60,ARGV[3])
  redis.call('EXPIRE',KEYS[1],120)
end
return 1
`)
var brandRenew = redis.NewScript(`
if not redis.call('ZSCORE',KEYS[1],ARGV[1]) then return 0 end
local t=redis.call('TIME')
redis.call('ZADD',KEYS[1],'XX',tonumber(t[1])+60,ARGV[1])
redis.call('EXPIRE',KEYS[1],120)
return 1
`)

func BrandGatewayCapacity(client *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := brand.FromContext(c.Request.Context())
		if !ok || (scope.MaxConcurrent == 0 && scope.RPMLimit == 0) {
			c.Next()
			return
		}
		key, authenticated := GetAPIKeyFromContext(c)
		if !authenticated || key == nil || !brand.Matches(c.Request.Context(), key.BrandID) {
			AbortWithError(c, 401, "UNAUTHORIZED", "Invalid API key")
			return
		}
		if client == nil {
			AbortWithError(c, 503, "CAPACITY_UNAVAILABLE", "Capacity service unavailable")
			return
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			AbortWithError(c, 503, "CAPACITY_UNAVAILABLE", "Capacity service unavailable")
			return
		}
		member := hex.EncodeToString(random[:])
		prefix := "brand:{" + strconv.FormatInt(scope.ID, 10) + "}:gateway"
		opCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		result, err := brandAcquire.Run(opCtx, client, []string{prefix + ":slots", prefix + ":rpm"}, scope.MaxConcurrent, scope.RPMLimit, member).Int()
		cancel()
		if err != nil {
			AbortWithError(c, 503, "CAPACITY_UNAVAILABLE", "Capacity service unavailable")
			return
		}
		if result != 1 {
			c.Header("Retry-After", "1")
			AbortWithError(c, 429, "BRAND_CAPACITY_LIMIT", "Brand capacity limit reached")
			return
		}
		if scope.MaxConcurrent == 0 {
			c.Next()
			return
		}
		base := brand.Detached(c.Request.Context())
		requestCtx, requestCancel := context.WithCancel(c.Request.Context())
		c.Request = c.Request.WithContext(requestCtx)
		stop := make(chan struct{})
		go func() {
			ticker := time.NewTicker(20 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-requestCtx.Done():
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(base, 2*time.Second)
					renewed, err := brandRenew.Run(ctx, client, []string{prefix + ":slots"}, member).Int()
					cancel()
					if err != nil || renewed != 1 {
						requestCancel()
						return
					}
				}
			}
		}()
		defer func() {
			close(stop)
			requestCancel()
			ctx, cancel := context.WithTimeout(base, 2*time.Second)
			defer cancel()
			_ = client.ZRem(ctx, prefix+":slots", member).Err()
		}()
		c.Next()
	}
}
