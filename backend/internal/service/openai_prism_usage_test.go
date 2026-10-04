package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrismBrowserUsageJSONAndStreamCountCompletedOutputOnce(t *testing.T) {
	request := []byte(`{"model":"gpt-6.1-sol","instructions":"Answer briefly.","input":"你好，say hello"}`)
	response := []byte(`{"id":"resp_usage","status":"completed","model":"gpt-6.1-sol","output":[{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"Hello 你好"}]}],"usage":null,"metadata":{"keep":"yes","prism_usage_source":"unavailable"}}`)
	jsonBody, jsonUsage, err := prismBrowserResponseWithUsage(request, response, false)
	require.NoError(t, err)
	codec, err := openAIInputTokensCodecForModel("gpt-6.1-sol")
	require.NoError(t, err)
	wantOutput, err := codec.Count("Hello 你好")
	require.NoError(t, err)
	require.Equal(t, wantOutput, jsonUsage.OutputTokens)
	require.Greater(t, jsonUsage.InputTokens, 0)
	require.Zero(t, jsonUsage.CacheReadInputTokens)
	require.Equal(t, UsageSourceEstimatedVisibleText, gjson.GetBytes(jsonBody, "metadata.prism_usage_source").String())
	require.Equal(t, "yes", gjson.GetBytes(jsonBody, "metadata.keep").String())
	prefix := "data: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello 你好\"}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"content\":[{\"text\":\"Hello 你好\"}]}}\n\n"
	stream := []byte(prefix + "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":4,\"response\":" + string(response) + "}\n\n")
	streamBody, streamUsage, err := prismBrowserResponseWithUsage(request, stream, true)
	require.NoError(t, err)
	require.Equal(t, jsonUsage, streamUsage)
	require.True(t, strings.HasPrefix(string(streamBody), prefix))
	for _, line := range strings.Split(string(streamBody), "\n") {
		if strings.Contains(line, `"type":"response.completed"`) {
			data := []byte(strings.TrimPrefix(line, "data: "))
			require.Equal(t, int64(wantOutput), gjson.GetBytes(data, "response.usage.output_tokens").Int())
			require.Equal(t, int64(jsonUsage.InputTokens+wantOutput), gjson.GetBytes(data, "response.usage.total_tokens").Int())
			require.Equal(t, UsageSourceEstimatedVisibleText, gjson.GetBytes(data, "response.metadata.prism_usage_source").String())
			require.Equal(t, int64(4), gjson.GetBytes(data, "sequence_number").Int())
		}
	}
}

func TestPrismBrowserUsageCountsToolContentAndCustomGrammar(t *testing.T) {
	plain := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"custom_tool_call","name":"exec","input":"echo hello"},{"type":"custom_tool_call_output","output":"hello"}]}`)
	withGrammar := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"custom_tool_call","name":"exec","input":"echo hello longer content"},{"type":"custom_tool_call_output","output":"hello"}],"tools":[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"lark","definition":"start: WORD+\nWORD: /[a-z]+/"}}]}`)
	response := []byte(`{"output":[{"type":"function_call","name":"lookup","arguments":"{\"city\":\"Beijing\"}"},{"type":"custom_tool_call","name":"exec","input":"echo hello"}]}`)
	baseUsage, err := estimatePrismBrowserUsage(plain, response)
	require.NoError(t, err)
	toolUsage, err := estimatePrismBrowserUsage(withGrammar, response)
	require.NoError(t, err)
	require.Greater(t, toolUsage.InputTokens, baseUsage.InputTokens)
	require.Equal(t, toolUsage.OutputTokens, baseUsage.OutputTokens)
	require.Greater(t, toolUsage.OutputTokens, 5)
	_, _, err = prismBrowserResponseWithUsage(plain, []byte("data: {\"type\":\"response.created\"}\n\n"), true)
	require.Error(t, err)
}

func TestPrismShadowRecordUsageStandardActualModelPriceAndGroupMultiplier(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-luna"} {
		t.Run(model, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			bills := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, bills, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.accountRepo = &openAIRecordUsageAccountRepoStub{account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}
			shadow := &Account{ID: 23, ParentAccountID: i64p(4), QuotaDimension: QuotaDimensionPrism, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			groupID := int64(18)
			key := &APIKey{ID: 2, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 0.08}}
			usage := OpenAIUsage{InputTokens: 1000, OutputTokens: 200}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "resp_prism_bill", Model: "gpt-6-astra", BillingModel: model, UpstreamModel: model,
					UpstreamResponseModel: model, Usage: usage, UsageSource: UsageSourceEstimatedVisibleText, Duration: time.Second},
				APIKey: key, User: &User{ID: 1}, Account: shadow, OriginalModel: "gpt-6-astra", BillingModelSource: BillingModelSourceRequested,
			})
			require.NoError(t, err)
			expected := expectedOpenAICost(t, svc, model, usage, 0.08)
			require.Equal(t, 1, logs.calls)
			require.Equal(t, int64(23), logs.lastLog.AccountID)
			require.Equal(t, int64(18), *logs.lastLog.GroupID)
			require.Equal(t, UsageSourceEstimatedVisibleText, logs.lastLog.UsageSource)
			require.Equal(t, model, *logs.lastLog.UpstreamModel)
			require.Equal(t, 0.08, logs.lastLog.RateMultiplier)
			require.InDelta(t, expected.TotalCost, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, expected.ActualCost, logs.lastLog.ActualCost, 1e-12)
			require.Equal(t, 1, bills.calls)
			require.InDelta(t, expected.ActualCost, bills.lastCmd.BalanceCost, 1e-12)
		})
	}
}

func TestPrismUsageUnavailableStillCannotBeBilled(t *testing.T) {
	err := (&OpenAIGatewayService{}).RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{UsageUnavailable: true}})
	require.Error(t, err)
}
