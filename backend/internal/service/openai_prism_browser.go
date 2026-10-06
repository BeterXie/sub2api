package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const prismBrowserMaxResponseBytes = 2 << 20

const prismBrowserAttemptsKey = "prism_browser_attempts"

// MarkPrismBrowserAttempt separates adapter admission/outcomes from the native
// account concurrency ramp. Keep all attempts so failover cannot lose the mark.
func MarkPrismBrowserAttempt(c *gin.Context, accountID int64) {
	value, _ := c.Get(prismBrowserAttemptsKey)
	ids, _ := value.(map[int64]bool)
	if ids == nil {
		ids = make(map[int64]bool)
	}
	ids[accountID] = true
	c.Set(prismBrowserAttemptsKey, ids)
}

func IsPrismBrowserAttempt(c *gin.Context, accountID int64) bool {
	value, _ := c.Get(prismBrowserAttemptsKey)
	ids, _ := value.(map[int64]bool)
	return ids[accountID]
}

func prismBrowserModelCatalog() []openai.Model {
	var models []openai.Model
	for _, id := range []string{"gpt-6.1-sol", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-luna", "gpt-6-astra"} {
		name := openaiCodexDisplayName(id)
		if id == "gpt-6-astra" {
			name = "GPT-6 Astra → GPT-6.1 Sol"
		}
		models = append(models, openai.Model{ID: id, Object: "model", Type: "model", DisplayName: name})
	}
	return models
}

func defaultPrismShadowModelMapping() map[string]any {
	mapping := make(map[string]any)
	for _, model := range prismBrowserModelCatalog() {
		mapping[model.ID] = model.ID
	}
	return mapping
}

// These aliases are transport rules, shared by routing and terminal validation.
// A response still reports the actual Prism model, including Astra's Sol mapping.
func prismBrowserModel(model string) (string, string) {
	model = strings.TrimSpace(model)
	model, effort, _ := strings.Cut(model, ":")
	switch model {
	case "gpt-6-astra", "prism-astra":
		model = "gpt-6.1-sol"
	case "prism-sol":
		model = "gpt-5.6-sol"
	case "prism-terra":
		model = "gpt-5.6-terra"
	}
	return model, effort
}

func prismBrowserSupportsModel(model string) bool {
	model, _ = prismBrowserModel(model)
	switch model {
	case "gpt-6.1-sol", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-luna":
		return true
	}
	return false
}

func prismBrowserMappedRequest(body []byte) ([]byte, error) {
	model := gjson.GetBytes(body, "model").String()
	mapped, suffixEffort := prismBrowserModel(model)
	var err error
	if mapped != model {
		body, err = sjson.SetBytes(body, "model", mapped)
		if err != nil {
			return nil, err
		}
	}
	effortPath := "reasoning.effort"
	effort := gjson.GetBytes(body, effortPath).String()
	if topLevel := gjson.GetBytes(body, "reasoning_effort").String(); topLevel != "" {
		effortPath, effort = "reasoning_effort", topLevel
	}
	if effort == "" {
		effort = suffixEffort
	}
	if effort == "max" || effort == "ultra" {
		effort = "xhigh"
	}
	if effort != "" && effort != gjson.GetBytes(body, effortPath).String() {
		body, err = sjson.SetBytes(body, effortPath, effort)
	}
	return body, err
}

func prismBrowserTerminal(body []byte, model string, stream bool) (string, error) {
	terminal := body
	var itemEvents []gjson.Result
	var detailEvents []gjson.Result
	if stream {
		terminal = nil
		for _, line := range bytes.Split(body, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if !gjson.ValidBytes(data) {
				return "", errors.New("prism adapter returned invalid SSE JSON")
			}
			if terminal != nil {
				return "", errors.New("prism adapter returned events after its terminal response")
			}
			switch gjson.GetBytes(data, "type").String() {
			case "response.created", "response.in_progress":
			case "response.content_part.added", "response.content_part.done", "response.output_text.done",
				"response.function_call_arguments.done", "response.custom_tool_call_input.done", "response.output_text.delta":
				detailEvents = append(detailEvents, gjson.ParseBytes(data))
			case "response.output_item.added", "response.output_item.done":
				itemEvents = append(itemEvents, gjson.ParseBytes(data))
			case "response.completed":
				if terminal != nil {
					return "", errors.New("prism adapter returned repeated terminal events")
				}
				terminal = []byte(gjson.GetBytes(data, "response").Raw)
			default:
				return "", errors.New("prism adapter returned unsupported SSE event")
			}
		}
	}
	if !gjson.ValidBytes(terminal) || gjson.GetBytes(terminal, "status").String() != "completed" ||
		gjson.GetBytes(terminal, "model").String() != model ||
		gjson.GetBytes(terminal, "id").String() == "" {
		return "", errors.New("prism adapter returned an invalid terminal response")
	}
	output := gjson.GetBytes(terminal, "output")
	if !output.IsArray() || len(output.Array()) == 0 {
		return "", errors.New("prism adapter returned no output items")
	}
	seen := make(map[string]bool)
	items := output.Array()
	for _, item := range output.Array() {
		switch item.Get("type").String() {
		case "", "message": // Empty type remains compatible with the P1 text adapter.
			if item.Get("content.0.text").String() == "" {
				return "", errors.New("prism adapter returned an empty message")
			}
		case "function_call", "custom_tool_call":
			id := item.Get("call_id").String()
			if id == "" || seen[id] || item.Get("id").String() == "" || item.Get("name").String() == "" || item.Get("status").String() != "completed" {
				return "", errors.New("prism adapter returned an invalid tool identity")
			}
			seen[id] = true
			if item.Get("type").String() == "function_call" {
				args := item.Get("arguments")
				if args.Type != gjson.String || !gjson.Valid(args.String()) || !gjson.Parse(args.String()).IsObject() {
					return "", errors.New("prism adapter returned invalid function arguments")
				}
			} else if item.Get("input").Type != gjson.String {
				return "", errors.New("prism adapter returned invalid custom input")
			}
		default:
			return "", errors.New("prism adapter returned an unsupported output item")
		}
	}
	// Do not release an SSE tool event that is absent from or differs from the
	// validated terminal. The gateway buffers this trial protocol's response.
	added, completed := make(map[int64]bool), make(map[int64]bool)
	for _, event := range itemEvents {
		index := event.Get("output_index")
		if index.Type != gjson.Number || index.Int() < 0 || index.Float() != float64(index.Int()) || index.Int() >= int64(len(output.Array())) {
			return "", errors.New("prism adapter returned an invalid output index")
		}
		item, final := event.Get("item"), output.Array()[index.Int()]
		if item.Get("status").String() != "completed" && item.Get("status").String() != "in_progress" {
			return "", errors.New("prism adapter returned an invalid item status")
		}
		for _, key := range []string{"id", "type", "call_id", "name", "namespace"} {
			if item.Get(key).String() != final.Get(key).String() {
				return "", errors.New("prism adapter returned mismatched item events")
			}
		}
		if event.Get("type").String() == "response.output_item.done" {
			if !added[index.Int()] || completed[index.Int()] {
				return "", errors.New("prism adapter returned unpaired item events")
			}
			completed[index.Int()] = true
			left, _ := json.Marshal(item.Value())
			right, _ := json.Marshal(final.Value())
			if !bytes.Equal(left, right) {
				return "", errors.New("prism adapter returned conflicting completed items")
			}
		} else {
			if added[index.Int()] || item.Get("status").String() != "in_progress" || item.Get("arguments").String() != "" || item.Get("input").String() != "" || len(item.Get("content").Array()) != 0 {
				return "", errors.New("prism adapter returned an invalid added item")
			}
			added[index.Int()] = true
		}
	}
	for index, item := range items {
		if added[int64(index)] != completed[int64(index)] || (stream && item.Get("call_id").String() != "" && !completed[int64(index)]) {
			return "", errors.New("prism adapter omitted a completed output item")
		}
	}
	details := make(map[string]bool)
	deltas := make(map[string]string)
	for _, event := range detailEvents {
		index, kind := event.Get("output_index"), event.Get("type").String()
		if index.Type != gjson.Number || index.Int() < 0 || index.Float() != float64(index.Int()) || index.Int() >= int64(len(items)) {
			return "", errors.New("prism adapter returned an invalid detail index")
		}
		item := items[index.Int()]
		if event.Get("item_id").String() != item.Get("id").String() || !completed[index.Int()] {
			return "", errors.New("prism adapter returned a foreign item detail")
		}
		key := fmt.Sprintf("%s:%d:%d", kind, index.Int(), event.Get("content_index").Int())
		if kind != "response.output_text.delta" && details[key] {
			return "", errors.New("prism adapter repeated an item detail")
		}
		details[key] = true
		switch kind {
		case "response.function_call_arguments.done":
			if item.Get("type").String() != "function_call" || event.Get("arguments").Type != gjson.String || event.Get("arguments").String() != item.Get("arguments").String() {
				return "", errors.New("prism adapter returned conflicting function arguments")
			}
		case "response.custom_tool_call_input.done":
			if item.Get("type").String() != "custom_tool_call" || event.Get("input").Type != gjson.String || event.Get("input").String() != item.Get("input").String() {
				return "", errors.New("prism adapter returned conflicting custom input")
			}
		default:
			contentIndex := event.Get("content_index")
			content := item.Get("content").Array()
			if item.Get("type").String() != "message" || contentIndex.Type != gjson.Number || contentIndex.Int() < 0 || contentIndex.Float() != float64(contentIndex.Int()) || contentIndex.Int() >= int64(len(content)) {
				return "", errors.New("prism adapter returned an invalid content index")
			}
			part := content[contentIndex.Int()]
			if kind == "response.output_text.delta" {
				if event.Get("delta").Type != gjson.String {
					return "", errors.New("prism adapter returned an invalid text delta")
				}
				deltas[key] += event.Get("delta").String()
				if !strings.HasPrefix(part.Get("text").String(), deltas[key]) {
					return "", errors.New("prism adapter returned conflicting text deltas")
				}
				continue
			}
			if (kind == "response.output_text.done" && event.Get("text").String() != part.Get("text").String()) ||
				(kind == "response.content_part.done" && event.Get("part.text").String() != part.Get("text").String()) ||
				(kind == "response.content_part.added" && event.Get("part.text").String() != "") {
				return "", errors.New("prism adapter returned conflicting text content")
			}
		}
	}
	for index, item := range items {
		for contentIndex, part := range item.Get("content").Array() {
			key := fmt.Sprintf("response.output_text.delta:%d:%d", index, contentIndex)
			if delta, present := deltas[key]; present && delta != part.Get("text").String() {
				return "", errors.New("prism adapter returned incomplete text deltas")
			}
		}
	}
	return gjson.GetBytes(terminal, "id").String(), nil
}

func prismBrowserAdapterURL(baseURL string) (string, error) {
	parsed, err := url.Parse(prismBrowserResponsesURL(baseURL))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("prism adapter must use a local HTTP endpoint")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip == nil || (!ip.Equal(net.ParseIP("127.0.0.1")) && !ip.Equal(net.IPv6loopback)) {
		return "", errors.New("prism adapter must bind to a numeric loopback address")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 || parsed.Path != "/v1/responses" {
		return "", errors.New("invalid Prism adapter endpoint")
	}
	return parsed.String(), nil
}

// prismBrowserAdapterMisconfigured reports the adapter's own authentication and
// routing failures: the gateway and adapter disagree on the bridge key or path.
// Passing those statuses through would tell the client its API key was rejected.
func prismBrowserAdapterMisconfigured(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	}
	return false
}

// prismBrowserAdapterErrorMessage tells an admin why the adapter refused a test
// turn (unsupported model, busy browser, retained pending turn). The adapter only
// returns fixed error codes and messages, never credentials or prompt text.
func prismBrowserAdapterErrorMessage(status int, body []byte) string {
	code := strings.TrimSpace(gjson.GetBytes(body, "error.type").String())
	message := strings.TrimSpace(gjson.GetBytes(body, "error.message").String())
	switch {
	case code != "" && message != "":
		return fmt.Sprintf("Prism adapter returned HTTP %d (%s): %s", status, code, truncateString(message, 300))
	case code != "":
		return fmt.Sprintf("Prism adapter returned HTTP %d (%s)", status, code)
	default:
		return fmt.Sprintf("Prism adapter returned HTTP %d", status)
	}
}

// Only log protocol codes we own. Never copy arbitrary adapter messages or
// reflected input into gateway logs when diagnosing fast 422 refusals.
func prismBrowserForwardError(status int, body []byte) error {
	code := gjson.GetBytes(body, "error.type").String()
	switch code {
	case "tools_disabled", "unsupported_model", "unsupported_request", "unsupported_reasoning",
		"unsupported_input", "unsupported_tool_model", "unsupported_tool", "invalid_tools",
		"invalid_tool_choice", "invalid_tool_payload", "unsupported_reasoning_history",
		"model_unavailable", "reasoning_unavailable", "pending_turn", "prism_busy",
		"prism_pending_turn", "prism_pending_request", "prism_worker_transport",
		"prism_upstream", "prism_upstream_auth", "prism_upstream_rate_limit",
		"prism_context_too_large", "prism_model_unavailable", "prism_sandbox_unavailable", "invalid_tool_call":
		return fmt.Errorf("prism adapter returned HTTP %d (%s)", status, code)
	default:
		return fmt.Errorf("prism adapter returned HTTP %d", status)
	}
}

func prismBrowserCapacityRejected(status int, body []byte) bool {
	if status != http.StatusConflict || !gjson.ValidBytes(body) {
		return false
	}
	// Only the manager can confirm that this request was rejected before a
	// worker received it. An unknown submitted turn must never be replayed.
	submitted := gjson.GetBytes(body, "error.request_submitted")
	if submitted.Type != gjson.False {
		return false
	}
	switch gjson.GetBytes(body, "error.type").String() {
	case "prism_busy", "prism_pending_turn":
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) forwardPrismBrowser(ctx context.Context, c *gin.Context, account *Account, body []byte, started time.Time) (*OpenAIForwardResult, error) {
	MarkPrismBrowserAttempt(c, account.ID)
	// This path buffers and writes a complete JSON or SSE response, including
	// errors. The handler must never append another response.failed envelope.
	defer func() {
		if OpenAICompactKeepaliveAdjustedWrittenSize(c) > 0 {
			MarkResponseCommitted(c)
		}
	}()
	writeError := func(status int, raw []byte) {
		code := gjson.GetBytes(raw, "error.type").String()
		message := gjson.GetBytes(raw, "error.message").String()
		if code == "" || message == "" {
			if code == "" {
				code = "prism_adapter_error"
			}
			if message == "" {
				message = fmt.Sprintf("Prism adapter returned HTTP %d", status)
			}
			if gjson.ValidBytes(raw) && gjson.GetBytes(raw, "error").IsObject() {
				raw, _ = sjson.SetBytes(raw, "error.type", code)
				raw, _ = sjson.SetBytes(raw, "error.message", message)
			} else {
				raw, _ = json.Marshal(gin.H{"error": gin.H{"type": code, "message": message}})
			}
		}
		committed := StopOpenAICompactSSEKeepaliveCommitted(c)
		if committed || c.Writer.Written() {
			writePrismSSEFailure(c, status, code, message)
			return
		}
		c.Data(status, "application/json", raw)
	}
	fail := func(status int, code, message string) {
		raw, _ := json.Marshal(gin.H{"error": gin.H{"type": code, "message": message}})
		writeError(status, raw)
	}
	if isOpenAIResponsesCompactPath(c) {
		fail(http.StatusBadRequest, "invalid_request_error", "Prism adapter does not support responses/compact")
		return nil, errors.New("prism adapter does not support responses/compact")
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	stream := gjson.GetBytes(body, "stream").Bool()
	requestedEffort := extractOpenAIReasoningEffortFromBody(body, model)
	if model == "" {
		fail(http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, errors.New("prism adapter model is required")
	}
	turnID, err := prismBrowserTurnID(c, body)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", "invalid Prism request identity")
		return nil, err
	}
	upstreamModel := account.GetMappedModel(model)
	if upstreamModel != model {
		mapped, mapErr := sjson.SetBytes(body, "model", upstreamModel)
		if mapErr != nil {
			fail(http.StatusBadRequest, "invalid_request_error", "invalid Prism request")
			return nil, mapErr
		}
		body = mapped
	}
	sessionID, err := prismBrowserSessionID(c, account.ID, body)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	body, err = prismBrowserMappedRequest(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Invalid Prism request"}})
		return nil, err
	}
	upstreamModel = gjson.GetBytes(body, "model").String()
	// Prism supplies its complete result at the end of the turn. Keep the
	// downstream alive while the adapter prepares, polls and returns it;
	// adapter-side heartbeats would still be held by our response buffer.
	stopKeepalive := func() {}
	if stream {
		interval := 8 * time.Second
		if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
			interval = min(interval, time.Duration(s.cfg.Gateway.StreamKeepaliveInterval)*time.Second)
		}
		stopKeepalive = startPrismSSEKeepalive(c, upstreamModel, interval)
	}
	defer stopKeepalive()
	responseBody, upstreamHeaders, status, err := s.callPrismBrowserForTurn(ctx, account, body, sessionID, prismBrowserCallerID(c, account.ID), turnID)
	stopKeepalive()
	if err != nil {
		fail(http.StatusBadGateway, "prism_unavailable", "Prism adapter unavailable; request was not replayed")
		return nil, err
	}
	if status != http.StatusOK {
		if prismBrowserCapacityRejected(status, responseBody) {
			return nil, &UpstreamFailoverError{
				StatusCode:        status,
				ResponseBody:      responseBody,
				ResponseHeaders:   upstreamHeaders,
				NextAccountAction: NextAccountRetry,
			}
		}
		if prismBrowserAdapterMisconfigured(status) {
			fail(http.StatusBadGateway, "prism_unavailable", "Prism adapter rejected the gateway; check the adapter key and endpoint")
			return nil, fmt.Errorf("prism adapter returned HTTP %d", status)
		}
		writeError(status, responseBody)
		return nil, prismBrowserForwardError(status, responseBody)
	}
	responseID, err := prismBrowserTerminal(responseBody, upstreamModel, stream)
	if err != nil {
		fail(http.StatusBadGateway, "invalid_prism_response", "Prism adapter returned no valid terminal response")
		return nil, err
	}
	if err := prismBrowserValidateToolCatalog(body, responseBody, stream); err != nil {
		fail(http.StatusBadGateway, "invalid_prism_response", "Prism adapter returned an undeclared client tool")
		return nil, err
	}
	responseBody, usage, err := prismBrowserResponseWithUsage(body, responseBody, stream)
	if err != nil {
		fail(http.StatusBadGateway, "prism_usage_unavailable", "Failed to estimate Prism usage")
		return nil, err
	}
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
		var streamResponseID string
		responseBody, streamResponseID, err = prismBrowserStreamResponse(c, responseBody)
		if err != nil {
			fail(http.StatusBadGateway, "invalid_prism_response", "Failed to encode validated Prism response")
			return nil, err
		}
		if streamResponseID != "" {
			responseID = streamResponseID
		}
	}
	SetActualOpenAIUpstreamEndpoint(c, "/v1/responses")
	c.Header("X-Prism-Usage", UsageSourceEstimatedVisibleText)
	if timing := upstreamHeaders.Get("Server-Timing"); timing != "" {
		c.Header("Server-Timing", timing)
	}
	var firstTokenMs *int
	if stream {
		// Prism returns text at completion. Record when the validated output is
		// released to the client, including preparation and terminal validation.
		ms := int(time.Since(started).Milliseconds())
		firstTokenMs = &ms
	}
	c.Data(http.StatusOK, contentType, responseBody)
	return &OpenAIForwardResult{
		RequestID:                responseID,
		ResponseID:               responseID,
		UpstreamHeaders:          upstreamHeaders,
		Model:                    model,
		BillingModel:             upstreamModel,
		UpstreamModel:            upstreamModel,
		UpstreamEndpoint:         "/v1/responses",
		UpstreamResponseModel:    upstreamModel,
		ReasoningEffort:          extractOpenAIReasoningEffortFromBody(body, upstreamModel),
		RequestedReasoningEffort: requestedEffort,
		Usage:                    usage,
		UsageSource:              UsageSourceEstimatedVisibleText,
		Stream:                   stream,
		Duration:                 time.Since(started),
		FirstTokenMs:             firstTokenMs,
	}, nil
}

func (s *OpenAIGatewayService) callPrismBrowser(ctx context.Context, account *Account, body []byte) ([]byte, http.Header, int, error) {
	// Admin account tests always use a fresh project, even when a client sends
	// a session header. A previous answer must not contaminate a capability test.
	return s.callPrismBrowserWithSession(ctx, account, body, "")
}

func (s *OpenAIGatewayService) callPrismBrowserWithSession(ctx context.Context, account *Account, body []byte, sessionID string) ([]byte, http.Header, int, error) {
	return s.callPrismBrowserForCaller(ctx, account, body, sessionID, "")
}

func (s *OpenAIGatewayService) callPrismBrowserForCaller(ctx context.Context, account *Account, body []byte, sessionID, callerID string) ([]byte, http.Header, int, error) {
	return s.callPrismBrowserForTurn(ctx, account, body, sessionID, callerID, "")
}

func (s *OpenAIGatewayService) callPrismBrowserForTurn(ctx context.Context, account *Account, body []byte, sessionID, callerID, turnID string) ([]byte, http.Header, int, error) {
	runtime := s.prismBrowserRuntime(ctx)
	if !runtime.Enabled || (!accountHasPrismBrowser(account) && !prismFallbackSupportsModel(account, gjson.GetBytes(body, "model").String())) {
		return nil, nil, 0, errors.New("prism adapter is disabled; native fallback is prohibited")
	}
	endpoint, err := prismBrowserAdapterURL(runtime.BaseURL)
	if err != nil {
		return nil, nil, 0, err
	}
	key := strings.TrimSpace(runtime.APIKey)
	if key == "" {
		return nil, nil, 0, errors.New("prism adapter key is not configured")
	}
	credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return nil, nil, 0, err
	}
	token, _, err := s.GetAccessToken(ctx, credentialAccount)
	if err != nil {
		return nil, nil, 0, err
	}
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, nil, 0, errors.New("invalid Prism OAuth token")
	}
	proxy := credentialAccount.Proxy
	if proxy == nil && credentialAccount.ProxyID != nil {
		if s.proxyRepo == nil {
			return nil, nil, 0, errors.New("Prism account proxy is unavailable")
		}
		proxy, err = s.proxyRepo.GetByID(ctx, *credentialAccount.ProxyID)
		if err != nil || proxy == nil {
			return nil, nil, 0, errors.New("Prism account proxy is unavailable")
		}
	}
	proxyURL := ""
	if proxy != nil {
		proxyURL = proxy.URL()
	}
	body, err = prismBrowserMappedRequest(body)
	if err != nil {
		return nil, nil, 0, errors.New("invalid Prism request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Prism-Account-ID", strconv.FormatInt(credentialAccount.ID, 10))
	req.Header.Set("X-Prism-OAuth-Token", token)
	// Only the local adapter receives the proxy configuration. This HTTP hop
	// itself stays on loopback and never uses the account's outbound proxy.
	req.Header.Set("X-Prism-Proxy", proxyURL)
	if sessionID != "" {
		req.Header.Set("X-Prism-Session-ID", sessionID)
	}
	if callerID != "" {
		req.Header.Set("X-Prism-Caller-ID", callerID)
	}
	if turnID != "" {
		req.Header.Set("X-Prism-Turn-ID", turnID)
	}
	// The token must never pass through an account proxy, environment proxy,
	// plugin transport, or an HTTP redirect.
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		// The worker has a ten-minute total turn budget. Leave time for cold
		// browser preparation and reading the validated terminal response.
		Timeout:       12 * time.Minute,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("prism adapter request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, prismBrowserMaxResponseBytes+1))
	if err != nil || len(responseBody) > prismBrowserMaxResponseBytes {
		return nil, nil, 0, errors.New("prism adapter response exceeded limit")
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, nil, 0, errors.New("prism adapter redirected unexpectedly")
	}
	return responseBody, resp.Header, resp.StatusCode, nil
}

func (s *OpenAIGatewayService) prismBrowserRuntime(ctx context.Context) PrismBrowserRuntime {
	if s == nil {
		return PrismBrowserRuntime{}
	}
	if s.settingService != nil {
		return s.settingService.GetPrismBrowserRuntime(ctx)
	}
	if s.cfg == nil {
		return PrismBrowserRuntime{}
	}
	return PrismBrowserRuntime{Enabled: s.cfg.Gateway.PrismBrowser.Enabled, BaseURL: s.cfg.Gateway.PrismBrowser.BaseURL, APIKey: s.cfg.Gateway.PrismBrowser.APIKey}
}

// Account-independent identity prevents a client retry from submitting the same
// uncertain turn through another shadow account. Never trust private headers.
func prismBrowserTurnID(c *gin.Context, body []byte) (string, error) {
	if c == nil || getAPIKeyIDFromContext(c) <= 0 {
		return "", nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var request map[string]any
	if err := decoder.Decode(&request); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	identity := resolveOpenAIClientSessionIdentity(c, body)
	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "prism-turn-v1:%d:%s:%s:", getAPIKeyIDFromContext(c), identity.identity.kind, identity.identity.value)
	_, _ = digest.Write(canonical)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// Always derive the private tool identity from authenticated server context.
// External callers cannot select another tenant's tool history by a header.
func prismBrowserCallerID(c *gin.Context, accountID int64) string {
	if c == nil || accountID <= 0 {
		return ""
	}
	keyID := getAPIKeyIDFromContext(c)
	if keyID <= 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("prism-caller-v1:%d:%d", keyID, accountID)))
	return hex.EncodeToString(sum[:])
}

// prismBrowserSessionID uses identities already sent by unmodified Codex clients.
// A bare API key identifies a tenant, not a conversation: missing identity must
// create a fresh project so unrelated requests cannot inherit project files.
func prismBrowserSessionID(c *gin.Context, accountID int64, body []byte) (string, error) {
	if c == nil || c.Request == nil {
		return "", nil
	}
	for _, names := range [][]string{openAIThreadIdentityHeaders, openAISessionIdentityHeaders} {
		for _, name := range names {
			if len(c.Request.Header.Values(name)) > 1 {
				return "", errors.New("prism conversation identity headers must not be repeated")
			}
		}
	}
	resolution := resolveOpenAIClientSessionIdentity(c, body)
	switch resolution.metadata.Status {
	case OpenAIClientSessionIdentityMissing:
		return "", nil
	case OpenAIClientSessionIdentityResolved:
	default:
		return "", errors.New("prism conversation identity is invalid or conflicting")
	}
	keyID := getAPIKeyIDFromContext(c)
	if keyID <= 0 || accountID <= 0 {
		return "", errors.New("prism session reuse requires an authenticated API key")
	}
	// The private adapter header is always derived here; client-supplied
	// X-Prism-Session-ID values cannot select an existing cached context.
	digest := sha256.Sum256([]byte(fmt.Sprintf("prism-session-v1:%d:%d:%s:%s",
		keyID, accountID, resolution.identity.kind, resolution.identity.value)))
	return hex.EncodeToString(digest[:]), nil
}
