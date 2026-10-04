package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// The two account modes are exclusive: direct Prism or OpenAI with a quota fallback.
func accountHasPrismFallback(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() || accountHasPrismBrowser(account) {
		return false
	}
	enabled, _ := account.Extra["openai_prism_fallback"].(bool)
	return enabled
}

func prismFallbackSupportsModel(account *Account, model string) bool {
	return accountHasPrismFallback(account) && prismBrowserSupportsModel(model) &&
		!account.IsExcelBPSEnabledForModel(model) && !account.IsCopilotSDKEnabled()
}

func prismFallbackQuotaBlocked(ctx context.Context, account *Account, model string) bool {
	if !prismFallbackSupportsModel(account, model) {
		return false
	}
	if account.IsRateLimited() {
		return true
	}
	for _, window := range []string{"5h", "7d"} {
		if used, ok := resolveOpenAIQuotaUtilization(account.Extra, window, time.Now()); ok && used >= 1 {
			return true
		}
	}
	paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, account)
	return paused
}

func (s *OpenAIGatewayService) prismFallbackEnabled(account *Account, model string) bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.PrismBrowser.Enabled && prismFallbackSupportsModel(account, model)
}

// Only a completed HTTP rejection proves that the OpenAI turn was not accepted.
// Never infer this from transport errors, 5xx responses, or an interrupted stream.
func prismFallbackQuotaRejection(status int, headers http.Header, body []byte) bool {
	if status != http.StatusTooManyRequests || !gjson.ValidBytes(body) {
		return false
	}
	if hit, _, _ := detectOpenAICyberPolicy(body); hit {
		return false
	}
	for _, path := range []string{"error.code", "error.type"} {
		switch strings.ToLower(gjson.GetBytes(body, path).String()) {
		case "usage_limit_reached", "rate_limit_exceeded", "insufficient_quota", "gousagelimiterror":
			return true
		}
	}
	disposition, _ := classifyOpenAIOAuth429(headers, body)
	return disposition != openAIOAuth429Transient
}

func (s *OpenAIGatewayService) tryPrismFallbackAfterRejection(
	ctx context.Context, c *gin.Context, account *Account, canonicalBody []byte,
	resp *http.Response, rejectedBody []byte, started time.Time,
) (*OpenAIForwardResult, error, bool) {
	model := gjson.GetBytes(canonicalBody, "model").String()
	if !s.prismFallbackEnabled(account, model) || c == nil || c.Writer.Written() ||
		ctx.Err() != nil || isOpenAIResponsesCompactPath(c) || resp == nil ||
		!prismFallbackQuotaRejection(resp.StatusCode, resp.Header, rejectedBody) {
		return nil, nil, false
	}
	schedulingModel := canonicalOpenAIAccountSchedulingModel(account, model)
	if disabled := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, rejectedBody, schedulingModel); disabled {
		return nil, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, rejectedBody,
			sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(rejectedBody)), true, false), true
	}
	// Recheck administrator policy and credentials after the rejection. A quota
	// cooldown allows Prism; a disabled account or broken shared OAuth does not.
	latest, err := s.admitOpenAITurn(ctx, c, account, schedulingModel)
	if err != nil {
		return nil, err, true
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
		Kind: "prism_fallback", Message: "OpenAI rejected the turn due to quota or rate limits; using Prism",
	})
	clearOpenAIResponsesClientToolMapping(c)
	clearOpenAIResponsesNamespaceNames(c)
	setCodexToolNameReverse(c, nil)
	c.Header("X-Sub2API-Prism-Fallback", "openai_rate_limited")
	result, err := s.forwardPrismBrowser(ctx, c, latest, canonicalBody, started)
	return result, err, true
}
