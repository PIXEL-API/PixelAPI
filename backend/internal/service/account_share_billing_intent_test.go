package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingIntentStateTransitions(t *testing.T) {
	allowed := [][2]string{
		{AccountShareBillingIntentStatusCreated, AccountShareBillingIntentStatusInFlight},
		{AccountShareBillingIntentStatusCreated, AccountShareBillingIntentStatusCancelled},
		{AccountShareBillingIntentStatusInFlight, AccountShareBillingIntentStatusReady},
		{AccountShareBillingIntentStatusReady, AccountShareBillingIntentStatusProcessing},
		{AccountShareBillingIntentStatusProcessing, AccountShareBillingIntentStatusProcessing},
		{AccountShareBillingIntentStatusProcessing, AccountShareBillingIntentStatusSettled},
		{AccountShareBillingIntentStatusProcessing, AccountShareBillingIntentStatusFailed},
		{AccountShareBillingIntentStatusFailed, AccountShareBillingIntentStatusProcessing},
		{AccountShareBillingIntentStatusFailed, AccountShareBillingIntentStatusNeedsAttention},
		{AccountShareBillingIntentStatusNeedsAttention, AccountShareBillingIntentStatusReady},
		{AccountShareBillingIntentStatusNeedsAttention, AccountShareBillingIntentStatusCancelled},
	}
	for _, transition := range allowed {
		require.Truef(t, CanTransitionAccountShareBillingIntent(transition[0], transition[1]), "%s -> %s", transition[0], transition[1])
	}

	for _, transition := range [][2]string{
		{AccountShareBillingIntentStatusCreated, AccountShareBillingIntentStatusSettled},
		{AccountShareBillingIntentStatusInFlight, AccountShareBillingIntentStatusCancelled},
		{AccountShareBillingIntentStatusReady, AccountShareBillingIntentStatusSettled},
		{AccountShareBillingIntentStatusSettled, AccountShareBillingIntentStatusProcessing},
		{AccountShareBillingIntentStatusCancelled, AccountShareBillingIntentStatusCreated},
	} {
		require.Falsef(t, CanTransitionAccountShareBillingIntent(transition[0], transition[1]), "%s -> %s", transition[0], transition[1])
	}
}

func TestPrepareAccountShareBillingIntentCanonicalizesAndFingerprintsAllowlist(t *testing.T) {
	input := validAccountShareBillingIntentInput()
	input.Command.RateMultiplier = "1.0000"
	input.Command.OwnerShareRatio = "0.700000"
	input.Command.InviteShareRatio = "0.1000"
	input.Command.PlatformShareRatio = "0.200000"

	prepared, err := PrepareAccountShareBillingIntent(input)
	require.NoError(t, err)
	require.Equal(t, "1", prepared.Command.RateMultiplier)
	require.Equal(t, "0.7", prepared.Command.OwnerShareRatio)
	require.Len(t, prepared.CommandHash, 64)
	require.Len(t, prepared.RequestFingerprint, 64)

	payload := strings.ToLower(string(prepared.CommandJSON))
	for _, forbidden := range []string{
		"access_token",
		"refresh_token",
		"password",
		"credential",
		"authorization",
		"proxy_password",
	} {
		require.NotContains(t, payload, forbidden)
	}

	same := input
	same.Command.RateMultiplier = "1"
	same.Command.OwnerShareRatio = "0.7"
	same.Command.InviteShareRatio = "0.1"
	same.Command.PlatformShareRatio = "0.2"
	samePrepared, err := PrepareAccountShareBillingIntent(same)
	require.NoError(t, err)
	require.Equal(t, prepared.CommandHash, samePrepared.CommandHash)
	require.Equal(t, prepared.RequestFingerprint, samePrepared.RequestFingerprint)

	otherBinding := same
	otherBinding.BindingID++
	otherPrepared, err := PrepareAccountShareBillingIntent(otherBinding)
	require.NoError(t, err)
	require.NotEqual(t, prepared.RequestFingerprint, otherPrepared.RequestFingerprint)
}

func TestPrepareAccountShareBillingIntentRejectsInvalidEnvelopeAndRatios(t *testing.T) {
	input := validAccountShareBillingIntentInput()
	input.BindingID = 0
	_, err := PrepareAccountShareBillingIntent(input)
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)

	input = validAccountShareBillingIntentInput()
	input.Command.OwnerShareRatio = "0.8"
	input.Command.InviteShareRatio = "0.2"
	input.Command.PlatformShareRatio = "0.1"
	_, err = PrepareAccountShareBillingIntent(input)
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
}

func TestAccountShareBillingReadyPayloadRoundTripsAndDetectsTampering(t *testing.T) {
	input := validAccountShareBillingReadyInput()
	prepared, err := PrepareAccountShareBillingIntentReady(input)
	require.NoError(t, err)

	usage, err := DecodeAccountShareBillingUsage(prepared.UsageJSON, prepared.UsageHash)
	require.NoError(t, err)
	require.Equal(t, int64(123), usage.InputTokens)
	require.Equal(t, "0.25", usage.TotalCharge)

	var tampered map[string]any
	require.NoError(t, json.Unmarshal(prepared.UsageJSON, &tampered))
	tampered["input_tokens"] = float64(999)
	tamperedJSON, err := json.Marshal(tampered)
	require.NoError(t, err)
	_, err = DecodeAccountShareBillingUsage(tamperedJSON, prepared.UsageHash)
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)

	summary, err := DecodeAccountShareBillingResponseSummary(prepared.ResponseSummaryJSON)
	require.NoError(t, err)
	require.Equal(t, 200, summary.HTTPStatus)
}

func TestDecodeAccountShareBillingCommandValidatesCanonicalHash(t *testing.T) {
	prepared, err := PrepareAccountShareBillingIntent(validAccountShareBillingIntentInput())
	require.NoError(t, err)

	command, err := DecodeAccountShareBillingCommand(prepared.CommandJSON, prepared.CommandHash)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6", command.RoutedModel)

	_, err = DecodeAccountShareBillingCommand(prepared.CommandJSON, strings.Repeat("0", 64))
	require.True(t, errors.Is(err, ErrAccountShareBillingIntentInvalid))
}

func TestDecodeAccountShareBillingCommandPreservesHistoricalV2SettlementBehavior(t *testing.T) {
	prepared, err := PrepareAccountShareBillingIntent(validAccountShareBillingIntentInput())
	require.NoError(t, err)

	legacyCommand := prepared.Command
	legacyCommand.SchemaVersion = AccountShareBillingCommandSchemaV2
	legacyPayload, err := json.Marshal(accountShareBillingCommandV2WireFromRuntime(legacyCommand))
	require.NoError(t, err)

	decoded, err := DecodeAccountShareBillingCommand(
		legacyPayload,
		hashAccountShareBillingPayload(legacyPayload),
	)
	require.NoError(t, err)
	require.Equal(t, AccountShareBillingCommandSchemaV2, decoded.SchemaVersion)
	require.True(t, decoded.SettlementEnabled)
	require.Equal(t, prepared.Command.RoutedModel, decoded.RoutedModel)
}

func TestDecodeAccountShareBillingCommandRejectsSettlementFlagInHistoricalV2Payload(t *testing.T) {
	input := validAccountShareBillingIntentInput()
	input.Command.SchemaVersion = AccountShareBillingCommandSchemaV2
	raw, err := json.Marshal(input.Command)
	require.NoError(t, err)

	_, err = DecodeAccountShareBillingCommand(raw, hashAccountShareBillingPayload(raw))
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
	require.Contains(t, err.Error(), "unknown field")
}

func TestDecodeAccountShareBillingCommandRejectsFieldsOutsideAllowlist(t *testing.T) {
	prepared, err := PrepareAccountShareBillingIntent(validAccountShareBillingIntentInput())
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(prepared.CommandJSON, &payload))
	payload["access_token"] = "must-not-be-persisted"
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = DecodeAccountShareBillingCommand(raw, hashAccountShareBillingPayload(raw))
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)
	require.Contains(t, err.Error(), "unknown field")
}

func TestAccountShareBillingV3JSONAllowlistKeys(t *testing.T) {
	prepared, err := PrepareAccountShareBillingIntent(validAccountShareBillingIntentInput())
	require.NoError(t, err)
	var commandPayload map[string]any
	require.NoError(t, json.Unmarshal(prepared.CommandJSON, &commandPayload))
	require.ElementsMatch(t, []string{
		"schema_version",
		"request_payload_hash",
		"group_id",
		"subscription_id",
		"account_type",
		"requested_model",
		"routed_model",
		"inbound_endpoint",
		"upstream_endpoint",
		"request_type",
		"service_tier",
		"reasoning_effort",
		"billing_type",
		"prefer_points_billing",
		"rate_multiplier",
		"rate_multiplier_source",
		"account_rate_multiplier",
		"hourly_rate",
		"owner_share_ratio",
		"invite_share_ratio",
		"platform_share_ratio",
		"policy_id",
		"policy_version",
		"channel_id",
		"model_mapping_chain",
		"settlement_enabled",
		"share_mode_snapshot",
		"share_status_snapshot",
		"share_platform_snapshot",
	}, accountShareBillingMapKeys(commandPayload))

	preparedReady, err := PrepareAccountShareBillingIntentReady(validAccountShareBillingReadyInput())
	require.NoError(t, err)
	var usagePayload map[string]any
	require.NoError(t, json.Unmarshal(preparedReady.UsageJSON, &usagePayload))
	require.ElementsMatch(t, []string{
		"schema_version",
		"usage_occurred_at",
		"model",
		"upstream_model",
		"service_tier",
		"reasoning_effort",
		"input_tokens",
		"output_tokens",
		"cache_creation_tokens",
		"cache_creation_5m_tokens",
		"cache_creation_1h_tokens",
		"cache_read_tokens",
		"image_input_tokens",
		"image_output_tokens",
		"image_count",
		"image_size",
		"media_type",
		"video_count",
		"video_resolution",
		"video_duration_seconds",
		"duration_ms",
		"first_token_ms",
		"billing_tier",
		"billing_mode",
		"cache_ttl_overridden",
		"applied_rate_multiplier",
		"input_cost",
		"output_cost",
		"cache_creation_cost",
		"cache_read_cost",
		"image_input_cost",
		"image_output_cost",
		"total_cost",
		"actual_cost",
		"account_stats_cost",
		"balance_cost",
		"subscription_cost",
		"private_group_commission_cost",
		"api_key_quota_cost",
		"api_key_rate_limit_cost",
		"account_quota_cost",
		"base_charge",
		"hourly_charge",
		"total_charge",
	}, accountShareBillingMapKeys(usagePayload))
}

func TestAccountShareBillingClaimAndLeaseValidation(t *testing.T) {
	claim, err := NormalizeAccountShareBillingClaim(ClaimAccountShareBillingIntentsInput{
		WorkerID:      " worker-a ",
		Limit:         1000,
		LeaseDuration: 30 * time.Second,
	})
	require.NoError(t, err)
	require.Equal(t, "worker-a", claim.WorkerID)
	require.Equal(t, AccountShareBillingIntentMaxClaimLimit, claim.Limit)

	_, err = NormalizeAccountShareBillingClaim(ClaimAccountShareBillingIntentsInput{
		WorkerID:      "worker-a",
		LeaseDuration: time.Second,
	})
	require.ErrorIs(t, err, ErrAccountShareBillingIntentInvalid)

	require.NoError(t, ValidateAccountShareBillingIntentLeaseTransition(AccountShareBillingIntentLeaseTransition{
		ID:                 1,
		ExpectedStateToken: 4,
		LeaseToken:         2,
		WorkerID:           "worker-a",
	}))
	require.Error(t, ValidateAccountShareBillingIntentLeaseTransition(AccountShareBillingIntentLeaseTransition{
		ID:                 1,
		ExpectedStateToken: 4,
		LeaseToken:         0,
		WorkerID:           "worker-a",
	}))
}

func validAccountShareBillingIntentInput() CreateAccountShareBillingIntentInput {
	groupID := int64(91)
	policyID := int64(92)
	return CreateAccountShareBillingIntentInput{
		RequestID:           "req-account-share-1",
		ClientRequestID:     "client-account-share-1",
		DispatchID:          "87d4be89-f16d-4544-8b2b-bb7d2acb25aa",
		AttemptNo:           1,
		APIKeyID:            10,
		MembershipID:        11,
		ListingID:           12,
		AccountID:           13,
		BindingID:           14,
		ListingRevisionID:   15,
		TermsRevisionNumber: 3,
		ActorUserID:         16,
		ActorRole:           "consumer",
		ConsumerUserID:      16,
		OwnerUserID:         17,
		Command: AccountShareBillingCommand{
			SchemaVersion:         AccountShareBillingCommandSchemaV3,
			RequestPayloadHash:    strings.Repeat("a", 64),
			GroupID:               &groupID,
			AccountType:           "openai",
			RequestedModel:        "gpt-5.6",
			RoutedModel:           "gpt-5.6",
			InboundEndpoint:       "/v1/responses",
			UpstreamEndpoint:      "/v1/responses",
			RequestType:           "stream",
			ServiceTier:           "priority",
			ReasoningEffort:       "high",
			BillingType:           0,
			PreferPointsBilling:   true,
			RateMultiplier:        "1",
			RateMultiplierSource:  RateMultiplierSourceAccountShare,
			AccountRateMultiplier: "1",
			HourlyRate:            "0.5",
			OwnerShareRatio:       "0.7",
			InviteShareRatio:      "0.1",
			PlatformShareRatio:    "0.2",
			PolicyID:              &policyID,
			PolicyVersion:         2,
			ModelMappingChain:     "gpt-5.6",
			SettlementEnabled:     true,
			ShareModeSnapshot:     AccountShareModePrivate,
			ShareStatusSnapshot:   AccountShareStatusApproved,
			SharePlatformSnapshot: PlatformOpenAI,
		},
	}
}

func validAccountShareBillingReadyInput() MarkAccountShareBillingIntentReadyInput {
	return MarkAccountShareBillingIntentReadyInput{
		AccountShareBillingIntentTransition: AccountShareBillingIntentTransition{
			ID:                 100,
			ExpectedStateToken: 2,
		},
		Usage: AccountShareBillingUsagePayloadV2{
			SchemaVersion:          AccountShareBillingUsageSchemaV2,
			UsageOccurredAt:        time.Date(2026, 7, 27, 1, 2, 3, 0, time.UTC),
			Model:                  "gpt-5.6",
			InputTokens:            123,
			OutputTokens:           45,
			DurationMilliseconds:   1500,
			FirstTokenMilliseconds: accountShareBillingTestInt64Pointer(120),
			AppliedRateMultiplier:  "1",
			InputCost:              "0.2",
			OutputCost:             "0.05",
			TotalCost:              "0.25",
			ActualCost:             "0.25",
			BalanceCost:            "0.2500",
			BaseCharge:             "0.25",
			TotalCharge:            "0.250000",
		},
		ResponseSummary: AccountShareBillingResponseSummaryV1{
			SchemaVersion:     AccountShareBillingResponseSchemaV1,
			HTTPStatus:        200,
			ProviderRequestID: "upstream-1",
			FinishReason:      "stop",
			Streamed:          true,
		},
	}
}

func accountShareBillingTestInt64Pointer(value int64) *int64 {
	return &value
}

func accountShareBillingMapKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	return keys
}
