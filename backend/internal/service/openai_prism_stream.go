package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const prismBrowserStreamKey = "prism_browser_stream"

// Empty reasoning deltas keep clients' model-event timers alive. They are
// transport events, never provider reasoning, usage, or a first model token.
// The event heartbeat idea follows yyyllllming/prism-bridge (MIT).
// One state survives safe admission failover. The keepalive lock serializes
// heartbeat generation; terminal encoding happens only after Stop returns.
type prismBrowserStream struct {
	id, reasoningID, model string
	createdAt              int64
	sequence               int
	started                bool
}

func startPrismSSEKeepalive(c *gin.Context, model string, interval time.Duration) func() {
	value, _ := c.Get(prismBrowserStreamKey)
	state, _ := value.(*prismBrowserStream)
	if state == nil {
		state = &prismBrowserStream{id: "resp_" + uuid.NewString(), reasoningID: "rs_" + uuid.NewString(),
			model: model, createdAt: time.Now().Unix()}
		c.Set(prismBrowserStreamKey, state)
	}
	return startOpenAISSEKeepalivePayload(c, interval, state.heartbeat)
}

func (s *prismBrowserStream) event(kind string, payload map[string]any) []byte {
	payload["type"], payload["sequence_number"] = kind, s.sequence
	s.sequence++
	data, _ := json.Marshal(payload) // Values are trusted scalars or decoded JSON.
	return append(append([]byte("event: "+kind+"\ndata: "), data...), '\n', '\n')
}

func (s *prismBrowserStream) reasoningItem() map[string]any {
	return map[string]any{"id": s.reasoningID, "type": "reasoning", "status": "completed",
		"summary": []any{map[string]any{"type": "summary_text", "text": ""}}}
}

func (s *prismBrowserStream) heartbeat() []byte {
	var data []byte
	if !s.started {
		s.started = true
		data = append(data, s.event("response.created", map[string]any{"response": map[string]any{
			"id": s.id, "object": "response", "created_at": s.createdAt, "model": s.model,
			"status": "in_progress", "output": []any{}, "usage": nil}})...)
		data = append(data, s.event("response.output_item.added", map[string]any{"output_index": 0,
			"item": map[string]any{"id": s.reasoningID, "type": "reasoning", "status": "in_progress", "summary": []any{}}})...)
		data = append(data, s.event("response.reasoning_summary_part.added", map[string]any{
			"item_id": s.reasoningID, "output_index": 0, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": ""}})...)
	}
	return append(data, s.event("response.reasoning_summary_text.delta", map[string]any{
		"item_id": s.reasoningID, "output_index": 0, "summary_index": 0, "delta": ""})...)
}

func (s *prismBrowserStream) closeReasoning() []byte {
	data := s.event("response.reasoning_summary_text.done", map[string]any{
		"item_id": s.reasoningID, "output_index": 0, "summary_index": 0, "text": ""})
	data = append(data, s.event("response.reasoning_summary_part.done", map[string]any{
		"item_id": s.reasoningID, "output_index": 0, "summary_index": 0,
		"part": map[string]any{"type": "summary_text", "text": ""}})...)
	return append(data, s.event("response.output_item.done", map[string]any{
		"output_index": 0, "item": s.reasoningItem()})...)
}

// Called after the original adapter terminal, tools and usage are validated.
func prismBrowserStreamResponse(c *gin.Context, body []byte) ([]byte, string, error) {
	value, _ := c.Get(prismBrowserStreamKey)
	s, _ := value.(*prismBrowserStream)
	if s == nil || !s.started {
		return body, "", nil
	}
	data := s.closeReasoning()
	for _, line := range bytes.Split(body, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, "", err
		}
		kind, _ := event["type"].(string)
		if kind == "response.created" || kind == "response.in_progress" {
			continue
		}
		if index, ok := event["output_index"].(float64); ok {
			event["output_index"] = index + 1
		}
		if _, present := event["response_id"]; present {
			event["response_id"] = s.id
		}
		if kind == "response.completed" {
			response, ok := event["response"].(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("Prism terminal is missing its response")
			}
			output, _ := response["output"].([]any)
			response["id"], response["created_at"] = s.id, s.createdAt
			response["output"] = append([]any{s.reasoningItem()}, output...)
		}
		data = append(data, s.event(kind, event)...)
	}
	return data, s.id, nil
}

func writePrismSSEFailure(c *gin.Context, status int, code, message string) {
	value, _ := c.Get(prismBrowserStreamKey)
	s, _ := value.(*prismBrowserStream)
	if s == nil || !s.started {
		writeOpenAICompactSSEFailureMessage(c, status, code, message)
		return
	}
	MarkOpsStreamError(c, code, message, status)
	data := s.closeReasoning()
	data = append(data, s.event("response.failed", map[string]any{"response": map[string]any{
		"id": s.id, "object": "response", "created_at": s.createdAt, "model": s.model,
		"status": "failed", "output": []any{s.reasoningItem()}, "error": map[string]any{"code": code, "message": message}}})...)
	c.Data(http.StatusOK, "text/event-stream", data)
	c.Writer.Flush()
}
