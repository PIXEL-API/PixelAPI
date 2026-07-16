package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSanitizeUserContentModerationTestResultHidesScoresAndThresholds(t *testing.T) {
	raw, err := json.Marshal(sanitizeUserContentModerationTestResult(&service.ContentModerationTestAuditResult{
		Flagged:         true,
		RiskLevel:       "REJECT",
		HighestCategory: "violence",
		HighestScore:    0.99,
		CompositeScore:  0.99,
		CategoryScores:  map[string]float64{"violence": 0.99},
		Thresholds:      map[string]float64{"violence": 0.5},
	}))

	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, `"flagged":true`)
	require.Contains(t, body, `"highest_category":"violence"`)
	require.NotContains(t, body, "risk_level")
	require.NotContains(t, body, "highest_score")
	require.NotContains(t, body, "composite_score")
	require.NotContains(t, body, "category_scores")
	require.NotContains(t, body, "thresholds")
}

func TestSanitizeUserContentModerationLogsHidesPromptAndScoreDetails(t *testing.T) {
	consumerUserID := int64(8)
	apiKeyID := int64(9)
	groupID := int64(10)
	raw, err := json.Marshal(sanitizeUserContentModerationLogs([]service.UserContentModerationLog{{
		ID:              1,
		RequestID:       "req-1",
		OwnerUserID:     7,
		AccountID:       42,
		ConsumerUserID:  &consumerUserID,
		APIKeyID:        &apiKeyID,
		APIKeyName:      "consumer-key",
		GroupID:         &groupID,
		Endpoint:        "/v1/responses",
		Provider:        service.ContentModerationProviderOpenAI,
		Model:           "omni-moderation-latest",
		Mode:            service.ContentModerationModeObserve,
		Action:          service.ContentModerationActionAllow,
		Flagged:         true,
		HighestCategory: "violence",
		HighestScore:    0.99,
		CategoryScores:  map[string]float64{"violence": 0.99},
		Sampled:         true,
		Error:           "moderation_api_error",
		CreatedAt:       time.Unix(1, 0).UTC(),
	}}))

	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, `"highest_category":"violence"`)
	require.NotContains(t, body, "owner_user_id")
	require.NotContains(t, body, "consumer_user_id")
	require.NotContains(t, body, "api_key_id")
	require.NotContains(t, body, "api_key_name")
	require.NotContains(t, body, "group_id")
	require.NotContains(t, body, "highest_score")
	require.NotContains(t, body, "category_scores")
	require.NotContains(t, body, "prompt")
	require.NotContains(t, body, "input_excerpt")
}
