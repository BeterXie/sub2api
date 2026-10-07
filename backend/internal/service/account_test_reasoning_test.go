package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrismAccountTestReasoningUsesAdapterContractAndScope(t *testing.T) {
	s := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{}}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{}, Extra: map[string]any{"openai_prism_browser": true}}
	account.Extra[PrismBrowserModelsKey] = []string{"gpt-5.6-sol"}
	account.Credentials["model_mapping"] = map[string]any{"my-sol": "gpt-5.6-sol"}
	for _, model := range []string{"gpt-5.6-sol", "my-sol"} {
		result := s.GetAccountTestReasoning(context.Background(), account, model)
		require.Equal(t, []string{"low", "medium", "high", "xhigh"}, result.SupportedReasoningLevels)
		require.Equal(t, "medium", result.DefaultReasoningLevel)
	}
	for _, model := range []string{"gpt-6-luna", "unknown-model"} {
		result := s.GetAccountTestReasoning(context.Background(), account, model)
		require.Empty(t, result.SupportedReasoningLevels)
		require.Empty(t, result.DefaultReasoningLevel)
	}
}

func TestAccountTestReasoningFromMetadataUsesSupportedLevelsAndDefault(t *testing.T) {
	enabled := true
	result, ok := accountTestReasoningFromMetadata(UpstreamModelMetadata{
		Reasoning:                &enabled,
		DefaultReasoningLevel:    "ultra",
		SupportedReasoningLevels: []string{"low", "high", "ultra"},
	})
	require.True(t, ok)
	require.Equal(t, []string{"low", "high", "ultra"}, result.SupportedReasoningLevels)
	require.Equal(t, "ultra", result.DefaultReasoningLevel)
}

func TestAccountTestReasoningFromMetadataKeepsDisabledModelsEmpty(t *testing.T) {
	disabled := false
	result, ok := accountTestReasoningFromMetadata(UpstreamModelMetadata{Reasoning: &disabled})
	require.True(t, ok)
	require.Empty(t, result.SupportedReasoningLevels)
	require.Empty(t, result.DefaultReasoningLevel)
}
