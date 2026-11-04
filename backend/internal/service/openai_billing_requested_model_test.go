package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openAIMappedBillingAccount(mapping map[string]any) *Account {
	return &Account{
		ID:       30,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": mapping,
		},
	}
}

func TestOpenAIClientBillingModel(t *testing.T) {
	account := openAIMappedBillingAccount(map[string]any{
		"gpt-5.4":  "gpt-5.5",
		"gpt-5.2*": "gpt-5.1",
	})
	tests := []struct {
		name     string
		account  *Account
		result   *OpenAIForwardResult
		want     string
		wantNorm bool
	}{
		{name: "account mapping is undone", account: account, result: &OpenAIForwardResult{Model: "gpt-5.4", BillingModel: "gpt-5.5", UpstreamModel: "gpt-5.5"}, want: "gpt-5.4"},
		{name: "wildcard mapping is undone", account: account, result: &OpenAIForwardResult{Model: "gpt-5.2-foo", BillingModel: "gpt-5.1"}, want: "gpt-5.2-foo"},
		{name: "unmapped keeps explicit billing model", account: account, result: &OpenAIForwardResult{Model: "gpt-5.4", BillingModel: "grok-imagine-image-quality"}, want: "grok-imagine-image-quality"},
		{name: "empty billing model uses requested", account: account, result: &OpenAIForwardResult{Model: "gpt-5.4", UpstreamModel: "gpt-5.5"}, want: "gpt-5.4"},
		{name: "no mapping keeps explicit billing model", account: &Account{Platform: PlatformOpenAI}, result: &OpenAIForwardResult{Model: "a", BillingModel: "b"}, want: "b"},
		{name: "nil account keeps explicit billing model", account: nil, result: &OpenAIForwardResult{Model: "a", BillingModel: "b"}, want: "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := openAIClientBillingModel(tt.account, tt.result)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOpenAIClientBillingModel_MessagesNormalizedModelIsRestored(t *testing.T) {
	// /v1/messages 链路在账号映射前会把 gpt-5.4-high 归一化为 gpt-5.4，
	// 账号映射按归一化名匹配；计费应还原为归一化名（与未映射时一致）。
	account := openAIMappedBillingAccount(map[string]any{"gpt-5.4": "gpt-5.5"})
	got, fallbacks := openAIClientBillingModel(account, &OpenAIForwardResult{Model: "gpt-5.4-high", BillingModel: "gpt-5.5"})
	require.Equal(t, "gpt-5.4", got)
	require.Contains(t, fallbacks, "gpt-5.4-high")
}

func recordOpenAIMappedUsage(t *testing.T, source string, requested, mapped string, account *Account) (*openAIRecordUsageLogRepoStub, *openAIRecordUsageUserRepoStub, *OpenAIGatewayService) {
	t.Helper()
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	subRepo := &openAIRecordUsageSubRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, subRepo, nil)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:     "resp_account_mapping_billing_" + source,
			Model:         requested,
			BillingModel:  mapped,
			UpstreamModel: mapped,
			Usage:         OpenAIUsage{InputTokens: 20, OutputTokens: 10},
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 10},
		User:    &User{ID: 20},
		Account: account,
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      requested,
			ChannelMappedModel: requested,
			BillingModelSource: source,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	return usageRepo, userRepo, svc
}

func TestOpenAIGatewayServiceRecordUsage_AccountMappingBillsRequestedModelByDefault(t *testing.T) {
	account := openAIMappedBillingAccount(map[string]any{"gpt-5.4": "gpt-5.5"})
	tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}

	for _, source := range []string{"", BillingModelSourceChannelMapped, BillingModelSourceRequested} {
		t.Run("source="+source, func(t *testing.T) {
			usageRepo, userRepo, svc := recordOpenAIMappedUsage(t, source, "gpt-5.4", "gpt-5.5", account)
			want, err := svc.billingService.CalculateCost("gpt-5.4", tokens, 1.1)
			require.NoError(t, err)
			notWant, err := svc.billingService.CalculateCost("gpt-5.5", tokens, 1.1)
			require.NoError(t, err)
			require.NotEqual(t, want.ActualCost, notWant.ActualCost, "test needs two different prices")
			require.InDelta(t, want.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
			require.InDelta(t, want.ActualCost, userRepo.lastAmount, 1e-12)
			require.Equal(t, "gpt-5.4", usageRepo.lastLog.Model)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_ExplicitUpstreamSourceStillBillsMappedModel(t *testing.T) {
	account := openAIMappedBillingAccount(map[string]any{"gpt-5.4": "gpt-5.5"})
	usageRepo, userRepo, svc := recordOpenAIMappedUsage(t, BillingModelSourceUpstream, "gpt-5.4", "gpt-5.5", account)
	want, err := svc.billingService.CalculateCost("gpt-5.5", UsageTokens{InputTokens: 20, OutputTokens: 10}, 1.1)
	require.NoError(t, err)
	require.InDelta(t, want.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, want.ActualCost, userRepo.lastAmount, 1e-12)
}

func TestOpenAIGatewayServiceRecordUsage_AccountMappingFallsBackToMappedModelWhenRequestedUnpriced(t *testing.T) {
	account := openAIMappedBillingAccount(map[string]any{"my-private-alias": "gpt-5.4"})
	usageRepo, userRepo, svc := recordOpenAIMappedUsage(t, "", "my-private-alias", "gpt-5.4", account)
	want, err := svc.billingService.CalculateCost("gpt-5.4", UsageTokens{InputTokens: 20, OutputTokens: 10}, 1.1)
	require.NoError(t, err)
	require.True(t, usageRepo.lastLog.ActualCost > 0)
	require.InDelta(t, want.ActualCost, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, want.ActualCost, userRepo.lastAmount, 1e-12)
}
