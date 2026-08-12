package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestContentModerationZhipuChunksAndAggregates(t *testing.T) {
	// 分块现在是并发发起的，计数与应答分配都必须自己加锁；
	// 每块回什么风险等级按分配序号决定即可——聚合取最坏值，与分块顺序无关。
	var mu sync.Mutex
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		seq := callCount
		mu.Unlock()
		if r.URL.Path != "/api/paas/v4/moderations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer zhipu-key" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		var payload struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != defaultZhipuContentModerationModel {
			t.Fatalf("unexpected model: %s", payload.Model)
		}
		if got := len([]rune(payload.Input)); got > maxZhipuModerationInputRunes {
			t.Fatalf("chunk too large: %d", got)
		}
		riskLevel := "PASS"
		riskType := []string{}
		if seq == 2 {
			riskLevel = "REVIEW"
			riskType = []string{"review_type"}
		}
		if seq == 3 {
			riskLevel = "REJECT"
			riskType = []string{"reject_type"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result_list": []map[string]any{{
				"content_type": "text",
				"risk_level":   riskLevel,
				"risk_type":    riskType,
			}},
		})
	}))
	defer server.Close()

	cfg := &ContentModerationConfig{
		Provider:   ContentModerationProviderZhipu,
		BaseURL:    server.URL,
		Model:      defaultZhipuContentModerationModel,
		APIKeys:    []string{"zhipu-key"},
		TimeoutMS:  defaultContentModerationTimeoutMS,
		RetryCount: 0,
	}
	cfg.normalize()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.callModeration(context.Background(), cfg, ContentModerationInput{
		Text: strings.Repeat("测", maxZhipuModerationInputRunes*2+1),
	})
	if err != nil {
		t.Fatalf("callModeration returned error: %v", err)
	}
	mu.Lock()
	gotChunks := callCount
	mu.Unlock()
	if gotChunks != 3 {
		t.Fatalf("expected 3 chunks, got %d", gotChunks)
	}
	if !result.Flagged || result.RiskLevel != "REJECT" || result.HighestCategory != "reject_type" || result.HighestScore != 1 {
		t.Fatalf("unexpected aggregate result: %#v", result)
	}
	if result.CategoryScores["review_type"] != 0.8 || result.CategoryScores["reject_type"] != 1 {
		t.Fatalf("unexpected category scores: %#v", result.CategoryScores)
	}
}

func TestContentModerationZhipuRejectsImageInputExplicitly(t *testing.T) {
	cfg := &ContentModerationConfig{
		Provider:  ContentModerationProviderZhipu,
		BaseURL:   defaultZhipuContentModerationBaseURL,
		Model:     defaultZhipuContentModerationModel,
		APIKeys:   []string{"zhipu-key"},
		TimeoutMS: defaultContentModerationTimeoutMS,
	}
	cfg.normalize()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	_, err := svc.callModeration(context.Background(), cfg, ContentModerationInput{
		Text:   "hello",
		Images: []string{"data:image/png;base64,AAAA"},
	})
	if !errors.Is(err, ErrContentModerationUnsupportedInput) {
		t.Fatalf("expected unsupported input error, got %v", err)
	}
}

func TestContentModerationOpenAIFinalFlaggedDecision(t *testing.T) {
	tests := []struct {
		name            string
		officialFlagged bool
		score           float64
		threshold       float64
		expectedFlagged bool
	}{
		{
			name:            "official and threshold both clear",
			officialFlagged: false,
			score:           0.4,
			threshold:       0.8,
			expectedFlagged: false,
		},
		{
			name:            "official flag below score gate is observation only",
			officialFlagged: true,
			score:           0.4,
			threshold:       0.8,
			expectedFlagged: false,
		},
		{
			name:            "official flag at score gate is observation only",
			officialFlagged: true,
			score:           openAIOfficialFlaggedScoreThreshold,
			threshold:       0.8,
			expectedFlagged: false,
		},
		{
			name:            "official flag above score gate can flag",
			officialFlagged: true,
			score:           math.Nextafter(openAIOfficialFlaggedScoreThreshold, 1),
			threshold:       0.8,
			expectedFlagged: true,
		},
		{
			name:            "score above official gate alone does not flag",
			officialFlagged: false,
			score:           math.Nextafter(openAIOfficialFlaggedScoreThreshold, 1),
			threshold:       0.8,
			expectedFlagged: false,
		},
		{
			name:            "local threshold remains inclusive at official score gate",
			officialFlagged: true,
			score:           openAIOfficialFlaggedScoreThreshold,
			threshold:       openAIOfficialFlaggedScoreThreshold,
			expectedFlagged: true,
		},
		{
			name:            "local threshold can flag when official result is clear",
			officialFlagged: false,
			score:           0.9,
			threshold:       0.8,
			expectedFlagged: true,
		},
		{
			name:            "official and threshold both flag",
			officialFlagged: true,
			score:           0.9,
			threshold:       0.8,
			expectedFlagged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeOpenAIModerationResult(&moderationAPIResult{
				Flagged: tt.officialFlagged,
				CategoryScores: map[string]float64{
					"violence": tt.score,
				},
			}, map[string]float64{"violence": tt.threshold})

			if result == nil {
				t.Fatal("expected normalized OpenAI moderation result")
			}
			if result.Flagged != tt.expectedFlagged {
				t.Fatalf("unexpected final flagged decision: got %t, want %t", result.Flagged, tt.expectedFlagged)
			}
			if result.HighestCategory != "violence" || result.HighestScore != tt.score {
				t.Fatalf("unexpected highest-risk details: category=%q score=%v", result.HighestCategory, result.HighestScore)
			}
		})
	}
}

func TestContentModerationBuildLogPreservesFullRedactedInput(t *testing.T) {
	longText := strings.Repeat("完整送审内容", 80) + " password=super-secret-value"
	expected := redactContentModerationSecrets(longText)
	if len([]rune(expected)) <= 240 {
		t.Fatalf("test input must exceed the removed excerpt limit, got %d runes", len([]rune(expected)))
	}

	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	cfg := defaultContentModerationConfig()
	log := svc.buildLog(
		ContentModerationCheckInput{},
		cfg,
		ContentModerationScopeContext{},
		ContentModerationActionBlock,
		true,
		"violence",
		0.9,
		map[string]float64{"violence": 0.9},
		longText,
		nil,
		nil,
		"",
	)

	if log.InputExcerpt != expected {
		t.Fatalf("stored moderation input was truncated or changed: got %d runes, want %d", len([]rune(log.InputExcerpt)), len([]rune(expected)))
	}
	if strings.Contains(log.InputExcerpt, "super-secret-value") {
		t.Fatal("stored moderation input must retain secret redaction")
	}
}

func TestContentModerationAccountShareScope(t *testing.T) {
	groupID := int64(20)
	resolver := &contentModerationScopeResolverStub{
		modeGroupID: groupID,
		membership: &AccountShareMembership{
			ID:             301,
			ListingID:      101,
			AccountID:      201,
			OwnerUserID:    401,
			ConsumerUserID: 501,
			APIKeyID:       601,
		},
		listing: &AccountShareListing{
			ID:          101,
			AccountID:   201,
			OwnerUserID: 401,
		},
	}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetAccountShareModeResolver(resolver)
	input := ContentModerationCheckInput{
		UserID:   501,
		APIKeyID: 601,
		GroupID:  &groupID,
	}

	cfg := defaultContentModerationConfig()
	cfg.AllGroups = false
	cfg.GroupIDs = []int64{groupID}
	cfg.AccountShareModeScope = ContentModerationAccountShareModeScopeConfig{Enabled: false}
	cfg.normalize()
	inScope, _ := svc.resolveScope(context.Background(), cfg, input)
	if inScope {
		t.Fatalf("account mode group_ids must not enable moderation when account_share_mode_scope is disabled")
	}

	cfg.AccountShareModeScope = ContentModerationAccountShareModeScopeConfig{
		Enabled:    true,
		All:        false,
		Platforms:  []string{AccountShareModeGroupPlatformOpenAI},
		ListingIDs: []int64{101},
	}
	cfg.normalize()
	inScope, scope := svc.resolveScope(context.Background(), cfg, input)
	if !inScope || scope.ScopeType != contentModerationScopeTypeAccountShareMode {
		t.Fatalf("expected account share listing scope hit, inScope=%v scope=%#v", inScope, scope)
	}
	if scope.AccountShareListingID == nil || *scope.AccountShareListingID != 101 || scope.ConsumerUserID == nil || *scope.ConsumerUserID != 501 {
		t.Fatalf("unexpected scope context: %#v", scope)
	}

	resolver.err = ErrAccountShareModeGroupUnbound
	inScope, _ = svc.resolveScope(context.Background(), cfg, input)
	if inScope {
		t.Fatalf("unbound account mode group should be skipped by moderation")
	}
}

type contentModerationScopeResolverStub struct {
	modeGroupID int64
	membership  *AccountShareMembership
	listing     *AccountShareListing
	err         error
}

func (s *contentModerationScopeResolverStub) IsModeGroup(_ context.Context, groupID int64) bool {
	return s != nil && s.modeGroupID == groupID
}

func (s *contentModerationScopeResolverStub) IsModeGroupChecked(_ context.Context, groupID int64) (bool, error) {
	return s != nil && s.modeGroupID == groupID, nil
}

func (s *contentModerationScopeResolverStub) ResolveActiveBindingForRequest(context.Context, int64, int64, int64) (*AccountShareMembership, *AccountShareListing, error) {
	if s == nil {
		return nil, nil, nil
	}
	return s.membership, s.listing, s.err
}
