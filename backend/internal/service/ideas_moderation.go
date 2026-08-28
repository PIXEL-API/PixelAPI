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

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ideasModerationTaskName      = "ideas-moderation"
	ideasModerationInterval      = 15 * time.Second
	ideasModerationBatchSize     = 5
	ideasModerationMaxAttempts   = 3
	ideasModerationRetryDelay    = time.Minute
	ideasModerationPromptVersion = "v1"
)

// IdeasModerationConfig 复用账号广场评论审核的 endpoint/API Key/model。
type IdeasModerationConfig struct {
	Enabled bool
	URL     string
	APIKey  string
	Model   string
}

// IdeaModerationTarget 待审核的文章版本。
type IdeaModerationTarget struct {
	PostID     int64
	RevisionID int64
	RevisionNo int
	Title      string
	Summary    string
	Body       string
	Tags       []string // 标签 slug，用于标签黑名单兜底
}

type ideasModerationDecision struct {
	Decision   string   `json:"decision"` // pass | review | reject
	RiskLevel  string   `json:"risk_level"`
	Reason     string   `json:"reason"`
	Categories []string `json:"categories"`
}

type ideasModerationRequest struct {
	Model          string                   `json:"model"`
	Messages       []ideasModerationMessage `json:"messages"`
	Temperature    float64                  `json:"temperature"`
	ResponseFormat map[string]string        `json:"response_format,omitempty"`
}

type ideasModerationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ideasModerationResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// SetModerationTaskExecutor 注入集群任务执行器（用于审核 worker 的集群租约）。
func (s *IdeasService) SetModerationTaskExecutor(executor *ClusterTaskExecutor) {
	s.moderationTaskExecutor = executor
}

// StartModerationWorker 启动 AI 审核后台 worker。
func (s *IdeasService) StartModerationWorker() {
	if s == nil || s.repo == nil || s.settingService == nil {
		return
	}
	s.moderationStartOnce.Do(func() {
		s.moderationWG.Add(1)
		go s.runModerationWorker()
	})
}

// StopModerationWorker 停止 AI 审核后台 worker（由 cleanup 调用）。
func (s *IdeasService) StopModerationWorker() {
	if s == nil {
		return
	}
	s.moderationStopOnce.Do(func() {
		close(s.moderationStopCh)
	})
	s.moderationWG.Wait()
}

func (s *IdeasService) runModerationWorker() {
	defer s.moderationWG.Done()
	ticker := time.NewTicker(ideasModerationInterval)
	defer ticker.Stop()

	s.processModerationOnce()
	for {
		select {
		case <-ticker.C:
			s.processModerationOnce()
		case <-s.moderationStopCh:
			return
		}
	}
}

func (s *IdeasService) processModerationOnce() {
	if s == nil || s.repo == nil || s.settingService == nil || s.moderationTaskExecutor == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := s.moderationTaskExecutor.Run(ctx, ideasModerationTaskName, func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		return s.processModerationLeased(taskCtx, guard)
	})
	if err != nil {
		log.Printf("[IdeasModeration] lease failed: %v", err)
	}
}

func (s *IdeasService) processModerationLeased(ctx context.Context, guard *ClusterLeaseGuard) error {
	cfg, ready, err := s.settingService.GetIdeasModerationConfig(ctx)
	if err != nil {
		return fmt.Errorf("load ideas moderation config: %w", err)
	}
	if !ready {
		return nil
	}
	for i := 0; i < ideasModerationBatchSize; i++ {
		if err := guard.Check(ctx); err != nil {
			return err
		}
		targets, err := s.repo.ClaimPendingIdeaModerations(ctx, time.Now().UTC(), 1)
		if err != nil {
			return fmt.Errorf("claim ideas moderation: %w", err)
		}
		if len(targets) == 0 {
			return nil
		}
		target := targets[0]
		if err := s.moderateTarget(ctx, guard, cfg, target); err != nil {
			if errors.Is(err, ErrClusterTaskLeaseLost) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			log.Printf("[IdeasModeration] moderate failed: post=%d revision=%d err=%v", target.PostID, target.RevisionID, err)
		}
	}
	return nil
}

func (s *IdeasService) moderateTarget(ctx context.Context, guard *ClusterLeaseGuard, cfg IdeasModerationConfig, target IdeaModerationTarget) error {
	if err := guard.Check(ctx); err != nil {
		return err
	}
	decision, err := s.decideModeration(ctx, cfg, target)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		nextRetryAt := time.Now().UTC().Add(ideasModerationRetryDelay)
		if failErr := s.repo.FailIdeaModeration(ctx, target.PostID, target.RevisionID, err.Error(), cfg.Model, cfg.URL, nextRetryAt, ideasModerationMaxAttempts); failErr != nil {
			return fmt.Errorf("mark moderation failed: %w; original: %v", failErr, err)
		}
		return err
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	_, err = s.repo.ApplyModerationDecision(ctx, target.PostID, target.RevisionID, decision.Decision, decision.RiskLevel, decision.Reason, cfg.Model, cfg.URL, ideasModerationPromptVersion)
	return err
}

// ModeratePost 管理员手动触发对某篇文章最新版本做一次 AI 审核（同步）。
func (s *IdeasService) ModeratePost(ctx context.Context, postID int64) (*IdeaPost, error) {
	cfg, ready, err := s.settingService.GetIdeasModerationConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, infraerrors.BadRequest("IDEA_MODERATION_UNAVAILABLE", "AI moderation is not configured")
	}
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post.Revision == nil {
		return nil, ErrIdeaPostNotFound
	}
	target := IdeaModerationTarget{
		PostID:     post.ID,
		RevisionID: post.Revision.ID,
		RevisionNo: post.Revision.RevisionNo,
		Title:      post.Revision.Title,
		Summary:    post.Revision.Summary,
		Body:       post.Revision.Body,
		Tags:       tagSlugsOf(post.Tags),
	}
	decision, err := s.decideModeration(ctx, cfg, target)
	if err != nil {
		nextRetryAt := time.Now().UTC().Add(ideasModerationRetryDelay)
		if failErr := s.repo.FailIdeaModeration(ctx, post.ID, post.Revision.ID, err.Error(), cfg.Model, cfg.URL, nextRetryAt, ideasModerationMaxAttempts); failErr != nil {
			return nil, failErr
		}
		return nil, err
	}
	return s.repo.ApplyModerationDecision(ctx, post.ID, post.Revision.ID, decision.Decision, decision.RiskLevel, decision.Reason, cfg.Model, cfg.URL, ideasModerationPromptVersion)
}

// decideModeration 先做标签黑名单兜底（命中即转人工复核，跳过模型调用），否则走 AI 三态审核。
func (s *IdeasService) decideModeration(ctx context.Context, cfg IdeasModerationConfig, target IdeaModerationTarget) (ideasModerationDecision, error) {
	blacklist, err := s.settingService.GetIdeasTagBlacklist(ctx)
	if err != nil {
		return ideasModerationDecision{}, err
	}
	if hitsIdeaTagBlacklist(target.Tags, blacklist) {
		return ideasModerationDecision{
			Decision:   "review",
			RiskLevel:  "high",
			Reason:     "命中标签黑名单，转人工复核",
			Categories: []string{"tag_blacklist"},
		}, nil
	}
	return s.callIdeasModerationModel(ctx, cfg, target)
}

func hitsIdeaTagBlacklist(tags, blacklist []string) bool {
	if len(blacklist) == 0 {
		return false
	}
	blocked := make(map[string]struct{}, len(blacklist))
	for _, b := range blacklist {
		if b = strings.TrimSpace(b); b != "" {
			blocked[b] = struct{}{}
		}
	}
	for _, t := range tags {
		if _, ok := blocked[strings.TrimSpace(t)]; ok {
			return true
		}
	}
	return false
}

func tagSlugsOf(tags []IdeaTag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Slug)
	}
	return out
}

func (s *IdeasService) callIdeasModerationModel(ctx context.Context, cfg IdeasModerationConfig, target IdeaModerationTarget) (ideasModerationDecision, error) {
	if s == nil || s.moderationHTTPClient == nil {
		return ideasModerationDecision{}, ErrServiceUnavailable
	}
	body := ideasModerationRequest{
		Model:       cfg.Model,
		Temperature: 0,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
		Messages: []ideasModerationMessage{
			{
				Role: "system",
				Content: strings.Join([]string{
					"你是社区文章审核器，审核用户投稿的文章、经验、建议或教程。",
					"根据内容风险给出三态结论：pass（低风险，可自动发布）、review（需人工复核）、reject（明确违规，直接驳回）。",
					"明确违规包括：违法违规、色情低俗、诈骗赌博、暴力恐怖、人身攻击、泄露他人隐私、广告引流、交易诱导、联系方式交换、恶意灌水刷屏。",
					"只返回严格 JSON：{\"decision\":\"pass|review|reject\",\"risk_level\":\"low|medium|high\",\"reason\":\"简短中文原因\",\"categories\":[]}。",
					"decision 只能是 pass、review、reject 之一；risk_level 只能是 low、medium、high 之一；reason 必须是简短中文；categories 是字符串数组，可为空数组。",
				}, "\n"),
			},
			{
				Role: "user",
				Content: fmt.Sprintf("标题：%s\n摘要：%s\n正文：\n%s",
					strings.TrimSpace(target.Title),
					strings.TrimSpace(target.Summary),
					truncateRunes(target.Body, 6000),
				),
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ideasModerationDecision{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return ideasModerationDecision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpClient := *s.moderationHTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return ideasModerationDecision{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return ideasModerationDecision{}, fmt.Errorf("moderation api returned non-success status %d", resp.StatusCode)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ideasModerationDecision{}, err
	}
	var apiResp ideasModerationResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return ideasModerationDecision{}, fmt.Errorf("parse moderation api response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return ideasModerationDecision{}, fmt.Errorf("moderation api returned no choices")
	}
	content := strings.TrimSpace(apiResp.Choices[0].Message.Content)
	if content == "" {
		return ideasModerationDecision{}, fmt.Errorf("moderation api returned empty content")
	}
	var decision ideasModerationDecision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return ideasModerationDecision{}, fmt.Errorf("parse moderation decision: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(decision.Decision)) {
	case "pass", "review", "reject":
		if strings.TrimSpace(decision.Reason) == "" {
			return ideasModerationDecision{}, fmt.Errorf("moderation decision reason is required")
		}
		return decision, nil
	default:
		return ideasModerationDecision{}, fmt.Errorf("invalid moderation decision %q", decision.Decision)
	}
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
