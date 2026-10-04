package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const prismFallbackFixtureBody = `{"model":"gpt-6-astra","stream":false,"instructions":"Answer briefly.","input":"Reply OK","reasoning":{"effort":"ultra"}}`
const prismFallbackFixtureResponse = `{"id":"resp_fixture","status":"completed","model":"gpt-6.1-sol","usage":null,"output":[{"type":"message","id":"msg_fixture","status":"completed","content":[{"type":"output_text","text":"OK"}]}]}`

func TestPrismFallbackForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, scenario := range []string{"openai_success", "quota_rejected", "cooldown", "quota_snapshot", "quota_recovered", "disabled", "bridge_disabled", "auth_error", "server_error", "transport_unknown", "stream_unknown", "rejection_read_failure", "unsupported_model"} {
			t.Run(scenario+map[bool]string{false: "/transform", true: "/passthrough"}[passthrough], func(t *testing.T) {
				bridgeCalls := 0
				bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					bridgeCalls++
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(body, "model").String())
					require.Equal(t, "xhigh", gjson.GetBytes(body, "reasoning.effort").String())
					require.Equal(t, "Reply OK", gjson.GetBytes(body, "input").String())
					require.Equal(t, "Answer briefly.", gjson.GetBytes(body, "instructions").String())
					_, _ = io.WriteString(w, prismFallbackFixtureResponse)
				}))
				defer bridge.Close()
				s, account := prismTestService(bridge.URL)
				account.Status, account.Schedulable = StatusActive, true
				account.Extra = map[string]any{"openai_prism_fallback": true, "openai_oauth_passthrough": passthrough}
				openAIResponse := strings.ReplaceAll(prismFallbackFixtureResponse, "gpt-6.1-sol", "gpt-6-astra")
				openAIResponse = strings.ReplaceAll(openAIResponse, `"usage":null`, `"usage":{"input_tokens":2,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}`)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body:   io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":" + openAIResponse + "}\n\n"))}}
				s.httpUpstream = upstream
				body := prismFallbackFixtureBody
				expectBridge, expectOpenAI, expectSuccess := 0, 1, false
				switch scenario {
				case "openai_success", "quota_recovered":
					expectSuccess = true
					past := time.Now().Add(-time.Minute)
					account.RateLimitResetAt = &past
					account.Extra["codex_5h_used_percent"] = 100.0
					account.Extra["codex_5h_reset_at"] = past.Format(time.RFC3339)
				case "cooldown", "quota_snapshot":
					until := time.Now().Add(time.Hour)
					if scenario == "cooldown" {
						account.RateLimitResetAt = &until
					} else {
						account.Extra["codex_5h_used_percent"] = 100.0
						account.Extra["codex_5h_reset_at"] = until.Format(time.RFC3339)
					}
					expectBridge, expectOpenAI, expectSuccess = 1, 0, true
				case "quota_rejected", "disabled", "bridge_disabled", "unsupported_model":
					upstream.resp.StatusCode = http.StatusTooManyRequests
					upstream.resp.Body = io.NopCloser(strings.NewReader(`{"error":{"type":"usage_limit_reached","message":"Quota exhausted"}}`))
					if scenario == "quota_rejected" {
						expectBridge, expectSuccess = 1, true
					} else if scenario == "disabled" {
						delete(account.Extra, "openai_prism_fallback")
					} else if scenario == "bridge_disabled" {
						s.cfg.Gateway.PrismBrowser.Enabled = false
					} else {
						body = strings.ReplaceAll(body, "gpt-6-astra", "gpt-5.4")
					}
				case "auth_error", "server_error":
					upstream.resp.StatusCode = map[string]int{"auth_error": 401, "server_error": 503}[scenario]
					upstream.resp.Body = io.NopCloser(strings.NewReader(`{"error":{"type":"upstream_error"}}`))
				case "transport_unknown":
					upstream.err = errors.New("connection lost; acceptance unknown")
				case "stream_unknown":
					upstream.resp.Header.Set("Content-Type", "text/event-stream")
					upstream.resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_pending\"}}\n\n"))
				case "rejection_read_failure":
					upstream.resp.StatusCode = http.StatusTooManyRequests
					upstream.resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(`{"error":{"type":"usage_limit_reached"}}`), passthroughErrReadCloser{err: io.ErrUnexpectedEOF}))
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				result, err := s.Forward(context.Background(), c, account, []byte(body))
				if expectSuccess {
					if err != nil {
						t.Fatalf("forward failed HTTP=%d: %v", rec.Code, err)
					}
					require.NotNil(t, result)
				}
				require.Equal(t, expectBridge, bridgeCalls, "Prism must receive at most one turn")
				require.Len(t, upstream.requests, expectOpenAI)
				if expectBridge > 0 {
					require.False(t, result.UsageUnavailable)
					require.Equal(t, UsageSourceEstimatedVisibleText, result.UsageSource)
					require.Equal(t, "gpt-6-astra", result.Model)
					require.Equal(t, "gpt-6.1-sol", result.UpstreamModel)
				}
			})
		}
	}
}

func TestPrismFallbackAdmissionAndScheduling(t *testing.T) {
	s, account := prismTestService("http://127.0.0.1:1")
	account.Extra = map[string]any{"openai_prism_fallback": true}
	account.Status, account.Schedulable = StatusActive, true
	until := time.Now().Add(time.Hour)
	account.RateLimitResetAt = &until
	s.accountRepo = &turnAdmissionRepo{account: account}
	require.True(t, account.IsSchedulableForModel("gpt-6-astra"))
	require.False(t, account.IsSchedulableForModel("gpt-5.4"))
	_, err := s.admitOpenAITurn(context.Background(), nil, account, "gpt-6-astra")
	require.NoError(t, err)
	_, err = s.admitOpenAITurn(context.Background(), nil, account, "gpt-5.4")
	require.True(t, IsOpenAITurnAdmissionError(err))
	require.True(t, s.isOpenAIAccountTransportCompatible(account, OpenAIUpstreamTransportHTTPSSE))
	require.False(t, s.isOpenAIAccountTransportCompatible(account, OpenAIUpstreamTransportResponsesWebsocketV2Ingress))
	account.TempUnschedulableUntil = &until
	_, err = s.admitOpenAITurn(context.Background(), nil, account, "gpt-6-astra")
	require.True(t, IsOpenAITurnAdmissionError(err), "shared credential/proxy failures must block both channels")
	account.TempUnschedulableUntil = nil
	account.Schedulable = false
	require.False(t, account.IsSchedulable())
	account.Schedulable = true
	account.Type = AccountTypeAPIKey
	require.False(t, account.IsSchedulable())
}

func TestPrismFallbackRejectionRequiresExplicitRefusal(t *testing.T) {
	for _, tc := range []struct {
		status   int
		body     string
		accepted bool
	}{
		{429, `{"error":{"type":"usage_limit_reached"}}`, true},
		{429, `{"error":{"code":"rate_limit_exceeded"}}`, true},
		{429, `{"error":{"code":"insufficient_quota"}}`, true},
		{429, `{"error":{"message":"arbitrary error"}}`, false},
		{429, `upstream unavailable`, false},
		{503, `{"error":{"type":"usage_limit_reached"}}`, false},
		{401, `{"error":{"type":"usage_limit_reached"}}`, false},
	} {
		require.Equal(t, tc.accepted, prismFallbackQuotaRejection(tc.status, nil, []byte(tc.body)))
	}
}
