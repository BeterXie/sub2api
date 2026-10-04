package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// This opt-in check receives secrets on stdin, never in argv or test fixtures.
// Run its compiled test binary in the production application's network namespace.
func TestPrismBrowserNativeLive(t *testing.T) {
	if os.Getenv("PRISM_NATIVE_LIVE") != "1" {
		t.Skip("requires an explicitly authorized live account and local bridge")
	}
	var input struct {
		AccountID   int64
		AccessToken string
		BridgeKey   string
		Proxy       *Proxy
	}
	require.NoError(t, json.NewDecoder(os.Stdin).Decode(&input))
	s, account := prismTestService("http://127.0.0.1:8090")
	s.cfg.Gateway.PrismBrowser.APIKey = input.BridgeKey
	account.ID = input.AccountID
	account.Credentials["access_token"] = input.AccessToken
	account.Proxy = input.Proxy
	for _, stream := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		body, err := json.Marshal(map[string]any{
			"model": "gpt-6-astra", "input": "Reply with OK only.", "stream": stream,
			"reasoning": map[string]any{"effort": "ultra"},
		})
		require.NoError(t, err)
		result, err := s.forwardPrismBrowser(context.Background(), c, account, body, time.Now())
		if err != nil {
			t.Fatalf("native Prism check failed (stream=%t, HTTP=%d): %v", stream, rec.Code, err)
		}
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "unavailable", rec.Header().Get("X-Prism-Usage"))
		require.False(t, result.UsageUnavailable)
		require.Equal(t, UsageSourceEstimatedVisibleText, result.UsageSource)
		require.Equal(t, "gpt-6-astra", result.Model)
		require.Equal(t, "gpt-6.1-sol", result.UpstreamModel)
		require.Contains(t, rec.Header().Get("Server-Timing"), "prism_prepare;dur=")
		require.Contains(t, rec.Header().Get("Server-Timing"), "prism_verify;dur=")
		if stream {
			require.NotNil(t, result.FirstTokenMs)
			t.Logf("native Prism first output: account=%d first_token_ms=%d", input.AccountID, *result.FirstTokenMs)
		} else {
			require.Nil(t, result.FirstTokenMs)
		}
		t.Logf("native Prism completed: account=%d stream=%t model=%s duration=%s timing=%s", input.AccountID, stream, result.UpstreamModel, result.Duration, rec.Header().Get("Server-Timing"))
	}
}

// A synthetic, definitive OpenAI rejection exercises the compiled gateway's
// real Prism fallback without intentionally exhausting production quota.
func TestPrismFallbackNativeLive(t *testing.T) {
	if os.Getenv("PRISM_FALLBACK_LIVE") != "1" {
		t.Skip("requires an authorized live account and local bridge")
	}
	var input struct {
		AccountID   int64
		AccessToken string
		BridgeKey   string
		Proxy       *Proxy
	}
	require.NoError(t, json.NewDecoder(os.Stdin).Decode(&input))
	for _, stream := range []bool{false, true} {
		s, account := prismTestService("http://127.0.0.1:8090")
		s.cfg.Gateway.PrismBrowser.APIKey = input.BridgeKey
		account.ID, account.Status, account.Schedulable = input.AccountID, StatusActive, true
		account.Credentials["access_token"] = input.AccessToken
		account.Proxy = input.Proxy
		account.Extra = map[string]any{"openai_prism_fallback": true}
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests,
			Header: http.Header{"Content-Type": {"application/json"}},
			Body:   io.NopCloser(strings.NewReader(`{"error":{"type":"usage_limit_reached"}}`))}}
		s.httpUpstream = upstream
		if stream {
			until := time.Now().Add(time.Minute)
			account.RateLimitResetAt = &until
		}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		body, err := json.Marshal(map[string]any{
			"model": "gpt-6-astra", "input": "Reply with OK only.", "stream": stream,
			"reasoning": map[string]any{"effort": "ultra"},
		})
		require.NoError(t, err)
		result, err := s.Forward(context.Background(), c, account, body)
		if err != nil {
			t.Fatalf("fallback live failed stream=%t HTTP=%d: %v", stream, rec.Code, err)
		}
		require.Equal(t, http.StatusOK, rec.Code)
		require.False(t, result.UsageUnavailable)
		require.Equal(t, UsageSourceEstimatedVisibleText, result.UsageSource)
		require.Equal(t, "gpt-6.1-sol", result.UpstreamModel)
		if stream {
			require.Empty(t, upstream.requests)
		} else {
			require.Len(t, upstream.requests, 1)
		}
		t.Logf("fallback completed: account=%d stream=%t trigger=%s duration=%s", account.ID, stream,
			rec.Header().Get("X-Sub2API-Prism-Fallback"), result.Duration)
	}
}
