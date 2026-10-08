//go:build embed

package web

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// HTMLCache manages the cached index.html with injected settings
type HTMLCache struct {
	mu              sync.RWMutex
	entries         map[string]CachedHTML
	baseHTMLHash    string // Hash of the original index.html (immutable after build)
	settingsVersion uint64 // Incremented when settings change
	accessSequence  uint64
}

// CachedHTML represents the cache state
type CachedHTML struct {
	Content []byte
	ETag    string
	Expires time.Time
	lastUse uint64
}

// NewHTMLCache creates a new HTML cache instance
func NewHTMLCache() *HTMLCache {
	return &HTMLCache{entries: make(map[string]CachedHTML)}
}

// SetBaseHTML initializes the cache with the base HTML template
func (c *HTMLCache) SetBaseHTML(baseHTML []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash := sha256.Sum256(baseHTML)
	c.baseHTMLHash = hex.EncodeToString(hash[:8]) // First 8 bytes for brevity
}

// Invalidate marks the cache as stale
func (c *HTMLCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.settingsVersion++
	c.entries = make(map[string]CachedHTML)
}
func (c *HTMLCache) Version() uint64 { c.mu.RLock(); defer c.mu.RUnlock(); return c.settingsVersion }

// Get returns the cached HTML or nil if cache is stale
func (c *HTMLCache) Get(keys ...string) *CachedHTML {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := "legacy"
	if len(keys) > 0 {
		key = keys[0]
	}
	cached, ok := c.entries[key]
	if !ok || time.Now().After(cached.Expires) {
		if ok {
			delete(c.entries, key)
		}
		return nil
	}
	c.accessSequence++
	cached.lastUse = c.accessSequence
	c.entries[key] = cached
	return &cached
}

// Set updates the cache with new rendered HTML
func (c *HTMLCache) Set(html []byte, settingsJSON []byte) {
	c.SetFor("legacy", html, settingsJSON, c.Version())
}
func (c *HTMLCache) SetFor(key string, html []byte, settingsJSON []byte, version uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if version != c.settingsVersion {
		return
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= 128 {
		for oldKey, cached := range c.entries {
			if time.Now().After(cached.Expires) {
				delete(c.entries, oldKey)
			}
		}
		if len(c.entries) >= 128 {
			var oldestKey string
			var oldestUse uint64
			for entryKey, cached := range c.entries {
				if oldestKey == "" || cached.lastUse < oldestUse {
					oldestKey = entryKey
					oldestUse = cached.lastUse
				}
			}
			delete(c.entries, oldestKey)
		}
	}
	c.accessSequence++
	c.entries[key] = CachedHTML{Content: html, ETag: c.generateETag(append([]byte(key), settingsJSON...)), Expires: time.Now().Add(30 * time.Second), lastUse: c.accessSequence}
}

// generateETag creates an ETag from base HTML hash + settings hash
func (c *HTMLCache) generateETag(settingsJSON []byte) string {
	settingsHash := sha256.Sum256(settingsJSON)
	return `"` + c.baseHTMLHash + "-" + hex.EncodeToString(settingsHash[:8]) + `"`
}
