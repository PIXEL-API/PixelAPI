package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type accountShareCommentReviewConfig struct {
	Enabled bool
	URL     string
	APIKey  string
	Model   string
}

type accountShareModerationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type accountShareModerationRequest struct {
	Model          string                          `json:"model"`
	Messages       []accountShareModerationMessage `json:"messages"`
	Temperature    float64                         `json:"temperature"`
	ResponseFormat map[string]string               `json:"response_format,omitempty"`
}

type accountShareModerationResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type accountShareModerationDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (s *AccountShareModeService) SetReviewModerationSettingRepository(settingRepo SettingRepository) {
	if s == nil {
		return
	}
	s.reviewSettingRepo = settingRepo
	if s.reviewHTTPClient == nil {
		s.reviewHTTPClient = &http.Client{Timeout: 45 * time.Second}
	}
}

func (s *AccountShareModeService) StartReviewModerationWorker() {
	if s == nil || s.repo == nil || s.reviewSettingRepo == nil {
		return
	}
	s.reviewStartOnce.Do(func() {
		s.reviewWG.Add(1)
		go s.runReviewModerationWorker()
	})
}

func (s *AccountShareModeService) StopReviewModerationWorker() {
	if s == nil {
		return
	}
	s.reviewStopOnce.Do(func() {
		close(s.reviewStopCh)
	})
	s.reviewWG.Wait()
}

func (s *AccountShareModeService) runReviewModerationWorker() {
	defer s.reviewWG.Done()
	ticker := time.NewTicker(AccountShareReviewModerationInterval)
	defer ticker.Stop()

	s.processReviewModerationOnce()
	for {
		select {
		case <-ticker.C:
			s.processReviewModerationOnce()
		case <-s.reviewStopCh:
			return
		}
	}
}

func (s *AccountShareModeService) processReviewModerationOnce() {
	if s == nil || s.repo == nil || s.reviewSettingRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, accountShareReviewModerationTaskName, func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		return s.processReviewModerationOnceLeased(taskCtx, guard)
	})
	if err != nil {
		log.Printf("[AccountShareReview] moderation lease failed: %v", err)
	}
}

func (s *AccountShareModeService) processReviewModerationOnceLeased(
	ctx context.Context,
	guard *ClusterLeaseGuard,
) error {
	cfg, ready, err := s.loadAccountShareCommentReviewConfig(ctx)
	if err != nil {
		return fmt.Errorf("load moderation config: %w", err)
	}
	if !ready {
		return nil
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	reviews, err := s.repo.ClaimPendingReviewModerations(ctx, time.Now().UTC(), AccountShareReviewModerationBatchSize)
	if err != nil {
		return fmt.Errorf("claim moderation jobs: %w", err)
	}
	for i := range reviews {
		review := reviews[i]
		if err := s.processSingleReviewModeration(ctx, guard, cfg, &review); err != nil {
			if errors.Is(err, ErrClusterTaskLeaseLost) ||
				errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			log.Printf("[AccountShareReview] moderate review failed: review_id=%d err=%v", review.ID, err)
		}
	}
	return nil
}

func (s *AccountShareModeService) processSingleReviewModeration(
	ctx context.Context,
	guard *ClusterLeaseGuard,
	cfg accountShareCommentReviewConfig,
	review *AccountShareReview,
) error {
	if review == nil || review.ID <= 0 {
		return nil
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	result, err := s.callAccountShareCommentReviewModel(ctx, cfg, review)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if guardErr := guard.Check(ctx); guardErr != nil {
			return guardErr
		}
		nextRetryAt := time.Now().UTC().Add(time.Minute)
		if failErr := s.repo.FailReviewModeration(ctx, review.ID, err.Error(), nextRetryAt, AccountShareReviewModerationMaxAttempts); failErr != nil {
			return fmt.Errorf("mark moderation failed: %w; original: %v", failErr, err)
		}
		return err
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	if err := s.repo.CompleteReviewModeration(ctx, review.ID, result); err != nil {
		return fmt.Errorf("complete moderation: %w", err)
	}
	return nil
}

func (s *AccountShareModeService) SubmitReview(ctx context.Context, consumerUserID, membershipID int64, input SubmitAccountShareReviewInput) (*AccountShareReview, error) {
	if consumerUserID <= 0 {
		return nil, ErrUserNotFound
	}
	if membershipID <= 0 {
		return nil, ErrAccountShareListingNotFound
	}
	if input.Score < 0 || input.Score > 10 {
		return nil, ErrAccountShareReviewInvalidScore
	}
	input.Comment = strings.TrimSpace(input.Comment)
	if utf8.RuneCountInString(input.Comment) > AccountShareReviewMaxCommentRunes {
		return nil, ErrAccountShareReviewCommentTooLong
	}
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	if input.Comment != "" {
		_, ready, err := s.loadAccountShareCommentReviewConfig(ctx)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, ErrAccountShareCommentReviewUnavailable
		}
	}
	return s.repo.SubmitReview(ctx, consumerUserID, membershipID, input)
}

func (s *AccountShareModeService) ListListingReviews(ctx context.Context, viewerUserID, listingID int64, params pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error) {
	if viewerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	if listingID <= 0 {
		return nil, nil, ErrAccountShareListingNotFound
	}
	if s == nil || s.repo == nil {
		return nil, nil, ErrServiceUnavailable
	}
	return s.repo.ListListingReviews(ctx, viewerUserID, listingID, params)
}

func (s *AccountShareModeService) ListOwnerReviews(ctx context.Context, viewerUserID, ownerUserID int64, params pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error) {
	if viewerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	if ownerUserID <= 0 {
		return nil, nil, ErrUserNotFound
	}
	if s == nil || s.repo == nil {
		return nil, nil, ErrServiceUnavailable
	}
	return s.repo.ListOwnerReviews(ctx, viewerUserID, ownerUserID, params)
}

func (s *AccountShareModeService) loadAccountShareCommentReviewConfig(ctx context.Context) (accountShareCommentReviewConfig, bool, error) {
	if s == nil || s.reviewSettingRepo == nil {
		return accountShareCommentReviewConfig{}, false, nil
	}
	values, err := s.reviewSettingRepo.GetMultiple(ctx, []string{
		SettingKeyAccountShareCommentReviewEnabled,
		SettingKeyAccountShareCommentReviewURL,
		SettingKeyAccountShareCommentReviewAPIKey,
		SettingKeyAccountShareCommentReviewModel,
	})
	if err != nil {
		return accountShareCommentReviewConfig{}, false, err
	}
	cfg := accountShareCommentReviewConfig{
		Enabled: values[SettingKeyAccountShareCommentReviewEnabled] == "true",
		URL:     strings.TrimSpace(values[SettingKeyAccountShareCommentReviewURL]),
		APIKey:  strings.TrimSpace(values[SettingKeyAccountShareCommentReviewAPIKey]),
		Model:   strings.TrimSpace(values[SettingKeyAccountShareCommentReviewModel]),
	}
	ready := cfg.Enabled && cfg.URL != "" && cfg.APIKey != "" && cfg.Model != ""
	return cfg, ready, nil
}

func (s *AccountShareModeService) callAccountShareCommentReviewModel(ctx context.Context, cfg accountShareCommentReviewConfig, review *AccountShareReview) (AccountShareReviewModerationResult, error) {
	if s == nil || s.reviewHTTPClient == nil {
		return AccountShareReviewModerationResult{}, ErrServiceUnavailable
	}
	body := accountShareModerationRequest{
		Model:       cfg.Model,
		Temperature: 0,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
		Messages: []accountShareModerationMessage{
			{
				Role: "system",
				Content: strings.Join([]string{
					"你是账号广场评论审核器，只审核用户对共享账号或号主的评论。",
					"评论必须与本账号使用体验、账号稳定性、速度、可用性、费用体验或号主服务相关。",
					"广告、引流、无关内容、辱骂、人身攻击、违法违规、泄露隐私、联系方式交换、交易诱导、恶意刷屏都必须驳回。",
					"只返回严格 JSON：{\"decision\":\"pass\",\"reason\":\"\"} 或 {\"decision\":\"reject\",\"reason\":\"驳回原因\"}。",
					"decision 只能是 pass 或 reject。reject 时 reason 必须是简短中文原因；pass 时 reason 必须为空字符串。",
				}, "\n"),
			},
			{
				Role: "user",
				Content: fmt.Sprintf("账号平台：%s\n账号名称：%s\n评分：%d/10\n评论：%s",
					strings.TrimSpace(review.Platform),
					strings.TrimSpace(review.AccountName),
					review.Score,
					strings.TrimSpace(review.Comment),
				),
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return AccountShareReviewModerationResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return AccountShareReviewModerationResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := s.reviewHTTPClient.Do(req)
	if err != nil {
		return AccountShareReviewModerationResult{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AccountShareReviewModerationResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AccountShareReviewModerationResult{}, fmt.Errorf("moderation api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var apiResp accountShareModerationResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return AccountShareReviewModerationResult{}, fmt.Errorf("parse moderation api response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return AccountShareReviewModerationResult{}, fmt.Errorf("moderation api returned no choices")
	}
	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	if content == "" {
		return AccountShareReviewModerationResult{}, fmt.Errorf("moderation api returned empty content")
	}
	var decision accountShareModerationDecision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return AccountShareReviewModerationResult{}, fmt.Errorf("parse moderation decision: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(decision.Decision)) {
	case "pass":
		if strings.TrimSpace(decision.Reason) != "" {
			return AccountShareReviewModerationResult{}, fmt.Errorf("pass decision reason must be empty")
		}
		return AccountShareReviewModerationResult{
			Passed:        true,
			ModelSnapshot: cfg.Model,
			URLSnapshot:   cfg.URL,
		}, nil
	case "reject":
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			return AccountShareReviewModerationResult{}, fmt.Errorf("reject decision reason is required")
		}
		return AccountShareReviewModerationResult{
			Passed:        false,
			RejectReason:  reason,
			ModelSnapshot: cfg.Model,
			URLSnapshot:   cfg.URL,
		}, nil
	default:
		return AccountShareReviewModerationResult{}, fmt.Errorf("invalid moderation decision %q", decision.Decision)
	}
}
