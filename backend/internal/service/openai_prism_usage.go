package service

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const UsageSourceEstimatedVisibleText = "estimated_visible_text"

// Prism does not report provider usage. Count visible request/output content
// locally; cache hits and hidden reasoning are outside this estimate.
func estimatePrismBrowserUsage(request, response []byte) (OpenAIUsage, error) {
	var req openAIInputTokensCountRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return OpenAIUsage{}, err
	}
	// Count raw tool declarations below, including custom formats/grammars that
	// the typed Responses tool representation does not retain.
	req.Tools = nil
	req.ToolChoice = nil
	inputTokens, err := estimateOpenAIInputTokens(req)
	if err != nil {
		return OpenAIUsage{}, err
	}
	codec, err := openAIInputTokensCodecForModel(req.Model)
	if err != nil {
		return OpenAIUsage{}, err
	}
	add := func(total *int, text string) error {
		n, err := codec.Count(text)
		*total += n
		return err
	}
	for _, item := range gjson.GetBytes(request, "input").Array() {
		if item.Get("type").String() == "custom_tool_call" {
			if err := add(&inputTokens, item.Get("input").String()); err != nil {
				return OpenAIUsage{}, err
			}
		}
	}
	for _, tool := range gjson.GetBytes(request, "tools").Array() {
		raw, err := compactOpenAIInputTokensJSON([]byte(tool.Raw))
		if err != nil {
			return OpenAIUsage{}, err
		}
		if err := add(&inputTokens, raw); err != nil {
			return OpenAIUsage{}, err
		}
	}
	if choice := gjson.GetBytes(request, "tool_choice"); choice.Exists() {
		raw, err := compactOpenAIInputTokensJSON([]byte(choice.Raw))
		if err != nil {
			return OpenAIUsage{}, err
		}
		if err := add(&inputTokens, raw); err != nil {
			return OpenAIUsage{}, err
		}
	}
	outputTokens := 0
	for _, item := range gjson.GetBytes(response, "output").Array() {
		for _, field := range []string{"name", "namespace", "arguments", "input"} {
			if err := add(&outputTokens, item.Get(field).String()); err != nil {
				return OpenAIUsage{}, err
			}
		}
		for _, part := range item.Get("content").Array() {
			if err := add(&outputTokens, part.Get("text").String()); err != nil {
				return OpenAIUsage{}, err
			}
		}
	}
	return OpenAIUsage{InputTokens: inputTokens, OutputTokens: outputTokens}, nil
}

// Called only after terminal and tool validation. Use the completed output once
// for both JSON and SSE, never summing deltas and repeated output-item events.
func prismBrowserResponseWithUsage(request, body []byte, stream bool) ([]byte, OpenAIUsage, error) {
	terminal := body
	var lines [][]byte
	terminalIndex := -1
	if stream {
		lines = bytes.Split(body, []byte("\n"))
		for index, line := range lines {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if gjson.GetBytes(data, "type").String() == "response.completed" {
				terminal = []byte(gjson.GetBytes(data, "response").Raw)
				terminalIndex = index
			}
		}
		if terminalIndex < 0 {
			return nil, OpenAIUsage{}, fmt.Errorf("Prism response has no completed event")
		}
	}
	usage, err := estimatePrismBrowserUsage(request, terminal)
	if err != nil {
		return nil, OpenAIUsage{}, err
	}
	terminal, err = sjson.SetBytes(terminal, "usage", map[string]any{
		"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"total_tokens":          usage.InputTokens + usage.OutputTokens,
		"input_tokens_details":  map[string]int{"cached_tokens": 0},
		"output_tokens_details": map[string]int{"reasoning_tokens": 0},
	})
	if err != nil {
		return nil, OpenAIUsage{}, err
	}
	terminal, err = sjson.SetBytes(terminal, "metadata.prism_usage_source", UsageSourceEstimatedVisibleText)
	if err != nil {
		return nil, OpenAIUsage{}, err
	}
	if !stream {
		return terminal, usage, nil
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(lines[terminalIndex], []byte("data:")))
	data, err = sjson.SetRawBytes(data, "response", terminal)
	if err != nil {
		return nil, OpenAIUsage{}, err
	}
	lines[terminalIndex] = append([]byte("data: "), data...)
	return bytes.Join(lines, []byte("\n")), usage, nil
}
