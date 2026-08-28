package service

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 文章状态机（idea_posts.status）
const (
	IdeaPostStatusDraft            = "draft"
	IdeaPostStatusPendingReview    = "pending_review"
	IdeaPostStatusManualReview     = "manual_review"
	IdeaPostStatusPublished        = "published"
	IdeaPostStatusPendingRevision  = "pending_revision"
	IdeaPostStatusRejected         = "rejected"
	IdeaPostStatusHidden           = "hidden"
	IdeaPostStatusDeleted          = "deleted"
	IdeaPostStatusModerationFailed = "moderation_failed"
)

// 版本审核状态（idea_post_revisions.moderation_status）
const (
	IdeaRevisionStatusDraft            = "draft"
	IdeaRevisionStatusPendingReview    = "pending_review"
	IdeaRevisionStatusManualReview     = "manual_review"
	IdeaRevisionStatusApproved         = "approved"
	IdeaRevisionStatusPendingRevision  = "pending_revision"
	IdeaRevisionStatusRejected         = "rejected"
	IdeaRevisionStatusModerationFailed = "moderation_failed"
)

var (
	ErrIdeasDisabled           = infraerrors.Forbidden("IDEAS_DISABLED", "ideas feature is disabled")
	ErrIdeaPostNotFound        = infraerrors.NotFound("IDEA_POST_NOT_FOUND", "idea post not found")
	ErrIdeaPostNotAuthor       = infraerrors.Forbidden("IDEA_POST_NOT_AUTHOR", "only the author can perform this action")
	ErrIdeaPostInvalidState    = infraerrors.Conflict("IDEA_POST_INVALID_STATE", "idea post is not in a state that allows this action")
	ErrIdeaTitleRequired       = infraerrors.BadRequest("IDEA_TITLE_REQUIRED", "title is required")
	ErrIdeaBodyRequired        = infraerrors.BadRequest("IDEA_BODY_REQUIRED", "body is required")
	ErrIdeaTooManyTags         = infraerrors.BadRequest("IDEA_TOO_MANY_TAGS", "too many tags")
	ErrIdeaTagNameInvalid      = infraerrors.BadRequest("IDEA_TAG_NAME_INVALID", "tag name is invalid")
	ErrIdeaAlreadyRewarded     = infraerrors.Conflict("IDEA_REWARD_ALREADY", "you have already rewarded this post")
	ErrIdeaInsufficientBalance = infraerrors.BadRequest("IDEA_REWARD_INSUFFICIENT_BALANCE", "insufficient balance")
	ErrIdeaInsufficientPoints  = infraerrors.BadRequest("IDEA_REWARD_INSUFFICIENT_POINTS", "insufficient points")
	ErrIdeaTagNotFound         = infraerrors.NotFound("IDEA_TAG_NOT_FOUND", "tag not found")
)

const (
	IdeaMaxTagsPerPost  = 5
	IdeaMaxTitleRunes   = 120
	IdeaMaxSummaryRunes = 500
	IdeaMaxBodyRunes    = 20000
)

type IdeaTag struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Status     string `json:"status"`
	SortOrder  int    `json:"sort_order"`
	UsageCount int    `json:"usage_count"`
}

// SlugifyIdeaTag 将标签名规范化为 URL 安全的 slug（小写，仅保留字母/数字/汉字，其余折叠为连字符）。
func SlugifyIdeaTag(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

type IdeaRevision struct {
	ID               int64      `json:"id"`
	PostID           int64      `json:"post_id"`
	RevisionNo       int        `json:"revision_no"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	Body             string     `json:"body"`
	BodyHash         string     `json:"body_hash"`
	ModerationStatus string     `json:"moderation_status"`
	ModerationReason string     `json:"moderation_reason,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	CreatedBy        int64      `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
}

type IdeaPost struct {
	ID                int64         `json:"id"`
	AuthorUserID      int64         `json:"author_user_id"`
	AuthorName        string        `json:"author_name"`
	CurrentRevisionID int64         `json:"current_revision_id"`
	Status            string        `json:"status"`
	PublishedAt       *time.Time    `json:"published_at,omitempty"`
	LikeCount         int           `json:"like_count"`
	FavoriteCount     int           `json:"favorite_count"`
	ViewCount         int           `json:"view_count"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	Revision          *IdeaRevision `json:"revision,omitempty"`
	Tags              []IdeaTag     `json:"tags,omitempty"`
	CanEdit           bool          `json:"can_edit"`
	CanReward         bool          `json:"can_reward"`
	Liked             bool          `json:"liked"`
	Favorited         bool          `json:"favorited"`
}

type IdeaPostCreateInput struct {
	AuthorUserID int64
	Title        string
	Summary      string
	Body         string
	TagSlugs     []string
}

type IdeaPostUpdateInput struct {
	UserID   int64
	PostID   int64
	Title    string
	Summary  string
	Body     string
	TagSlugs []string
}

type IdeaPostListParams struct {
	Page     int
	PageSize int
	Sort     string // latest | hot | featured
	TagSlug  string
	Keyword  string
}

// 打赏资产类型
const (
	IdeaRewardAssetBalance = "balance"
	IdeaRewardAssetPoints  = "points"
)

// 打赏业务记录
type IdeaReward struct {
	ID              int64     `json:"id"`
	PayerUserID     int64     `json:"payer_user_id"`
	RecipientUserID int64     `json:"recipient_user_id"`
	PostID          int64     `json:"post_id"`
	RevisionID      int64     `json:"revision_id"`
	AssetType       string    `json:"asset_type"`
	Amount          float64   `json:"amount"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

type IdeaRewardInput struct {
	PayerUserID    int64
	PostID         int64
	AssetType      string
	Amount         float64
	IdempotencyKey string
}

// IdeaReport 文章举报记录。
type IdeaReport struct {
	ID              int64     `json:"id"`
	PostID          int64     `json:"post_id"`
	PostTitle       string    `json:"post_title,omitempty"`
	ReporterUserID  int64     `json:"reporter_user_id"`
	Reason          string    `json:"reason"`
	Detail          string    `json:"detail"`
	Status          string    `json:"status"`
	HandledByUserID *int64    `json:"handled_by_user_id,omitempty"`
	Resolution      string    `json:"resolution,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type IdeasRepository interface {
	CreatePostWithRevision(ctx context.Context, input IdeaPostCreateInput, tags []IdeaTag) (*IdeaPost, error)
	UpdateDraftRevision(ctx context.Context, input IdeaPostUpdateInput, tags []IdeaTag) (*IdeaPost, error)
	PublishRevision(ctx context.Context, postID, userID int64) (*IdeaPost, error)
	EditPublishedRevision(ctx context.Context, input IdeaPostUpdateInput, tags []IdeaTag) (*IdeaPost, error)
	SoftDeletePost(ctx context.Context, postID, userID int64) error
	GetPost(ctx context.Context, postID int64) (*IdeaPost, error)
	ListPublished(ctx context.Context, params IdeaPostListParams) ([]IdeaPost, int64, error)
	ListMine(ctx context.Context, userID int64, params IdeaPostListParams) ([]IdeaPost, int64, error)
	ListAdmin(ctx context.Context, params IdeaPostListParams) ([]IdeaPost, int64, error)
	SetPostStatus(ctx context.Context, postID int64, status, revisionStatus, reason string, operatorUserID int64, isApprove bool) (*IdeaPost, error)
	ListTags(ctx context.Context) ([]IdeaTag, error)
	ResolveTags(ctx context.Context, slugs []string) ([]IdeaTag, error)

	// 互动
	Like(ctx context.Context, postID, userID int64) (int, error)
	Unlike(ctx context.Context, postID, userID int64) (int, error)
	Favorite(ctx context.Context, postID, userID int64) (int, error)
	Unfavorite(ctx context.Context, postID, userID int64) (int, error)
	RecordView(ctx context.Context, postID, userID, revisionID int64) error
	GetInteractionState(ctx context.Context, postID, userID int64) (liked, favorited bool, err error)

	// 打赏
	CreateReward(ctx context.Context, input IdeaRewardInput, post *IdeaPost) (*IdeaReward, error)
	GetRewardByIdempotencyKey(ctx context.Context, key string) (*IdeaReward, error)

	// 举报
	CreateReport(ctx context.Context, postID, reporterUserID int64, reason, detail string) error
	ListReports(ctx context.Context, status string, page, pageSize int) ([]IdeaReport, int64, error)
	ResolveReport(ctx context.Context, reportID, adminUserID int64, resolution string) error

	// 标签治理
	ListAllTags(ctx context.Context) ([]IdeaTag, error)
	CreateTag(ctx context.Context, name string) (*IdeaTag, error)
	UpdateTag(ctx context.Context, tagID int64, name, status string) (*IdeaTag, error)
	MergeTags(ctx context.Context, sourceTagID, targetTagID int64) error

	// 审核
	ClaimPendingIdeaModerations(ctx context.Context, now time.Time, limit int) ([]IdeaModerationTarget, error)
	ApplyModerationDecision(ctx context.Context, postID, revisionID int64, decision, riskLevel, reason, model, url, promptVersion string) (*IdeaPost, error)
	FailIdeaModeration(ctx context.Context, postID, revisionID int64, errMsg, model, url string, nextRetryAt time.Time, maxAttempts int) error

	// 附件
	UploadAssetObject(ctx context.Context, cfg IdeasOSSConfig, key string, data []byte, contentType string) (string, error)
	PresignAssetURL(ctx context.Context, cfg IdeasOSSConfig, key string, expiry time.Duration) (string, error)
	DeleteAssetObject(ctx context.Context, cfg IdeasOSSConfig, key string) error
	CreateAsset(ctx context.Context, asset *IdeaAsset) error
	GetAsset(ctx context.Context, assetID int64) (*IdeaAsset, error)
	ListAssets(ctx context.Context, postID int64) ([]IdeaAsset, error)
}

type IdeasService struct {
	repo           IdeasRepository
	settingService *SettingService

	// 审核 worker 状态（见 ideas_moderation.go）
	moderationTaskExecutor *ClusterTaskExecutor
	moderationHTTPClient   *http.Client
	moderationWG           sync.WaitGroup
	moderationStopCh       chan struct{}
	moderationStopOnce     sync.Once
	moderationStartOnce    sync.Once
}

func NewIdeasService(repo IdeasRepository, settingService *SettingService) *IdeasService {
	return &IdeasService{
		repo:                 repo,
		settingService:       settingService,
		moderationHTTPClient: &http.Client{Timeout: 45 * time.Second},
		moderationStopCh:     make(chan struct{}),
	}
}

func (s *IdeasService) ensureEnabled(ctx context.Context) error {
	if s == nil || s.settingService == nil {
		return ErrIdeasDisabled
	}
	if !s.settingService.IsIdeasEnabled(ctx) {
		return ErrIdeasDisabled
	}
	return nil
}

func (s *IdeasService) CreateDraft(ctx context.Context, input IdeaPostCreateInput) (*IdeaPost, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	if err := validateIdeaContent(input.Title, input.Summary, input.Body, input.TagSlugs); err != nil {
		return nil, err
	}
	tags, err := s.repo.ResolveTags(ctx, input.TagSlugs)
	if err != nil {
		return nil, err
	}
	return s.repo.CreatePostWithRevision(ctx, input, tags)
}

func (s *IdeasService) Update(ctx context.Context, input IdeaPostUpdateInput) (*IdeaPost, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	if err := validateIdeaContent(input.Title, input.Summary, input.Body, input.TagSlugs); err != nil {
		return nil, err
	}
	post, err := s.repo.GetPost(ctx, input.PostID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID != input.UserID {
		return nil, ErrIdeaPostNotAuthor
	}
	tags, err := s.repo.ResolveTags(ctx, input.TagSlugs)
	if err != nil {
		return nil, err
	}
	switch post.Status {
	case IdeaPostStatusDraft:
		return s.repo.UpdateDraftRevision(ctx, input, tags)
	case IdeaPostStatusPublished:
		return s.repo.EditPublishedRevision(ctx, input, tags)
	default:
		return nil, ErrIdeaPostInvalidState
	}
}

func (s *IdeasService) Publish(ctx context.Context, postID, userID int64) (*IdeaPost, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID != userID {
		return nil, ErrIdeaPostNotAuthor
	}
	switch post.Status {
	case IdeaPostStatusDraft, IdeaPostStatusRejected:
		// 草稿或已拒绝文章提交审核
		return s.repo.PublishRevision(ctx, postID, userID)
	default:
		return nil, ErrIdeaPostInvalidState
	}
}

func (s *IdeasService) Get(ctx context.Context, postID, userID int64) (*IdeaPost, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post.Status == IdeaPostStatusDeleted {
		return nil, ErrIdeaPostNotFound
	}
	// published 对所有人可见；其余状态仅作者可见（hidden 作者也不可见）
	if post.Status != IdeaPostStatusPublished && post.AuthorUserID != userID {
		return nil, ErrIdeaPostNotFound
	}
	if post.Status == IdeaPostStatusHidden && post.AuthorUserID != userID {
		return nil, ErrIdeaPostNotFound
	}
	post.CanEdit = post.AuthorUserID == userID && post.Status != IdeaPostStatusHidden
	post.CanReward = post.Status == IdeaPostStatusPublished && post.AuthorUserID != userID
	if post.Status == IdeaPostStatusPublished {
		liked, favorited, err := s.repo.GetInteractionState(ctx, postID, userID)
		if err != nil {
			return nil, err
		}
		post.Liked = liked
		post.Favorited = favorited
	}
	return post, nil
}

func (s *IdeasService) List(ctx context.Context, params IdeaPostListParams) ([]IdeaPost, int64, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, 0, err
	}
	return s.repo.ListPublished(ctx, params)
}

func (s *IdeasService) ListMine(ctx context.Context, userID int64, params IdeaPostListParams) ([]IdeaPost, int64, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, 0, err
	}
	return s.repo.ListMine(ctx, userID, params)
}

func (s *IdeasService) Delete(ctx context.Context, postID, userID int64) error {
	if err := s.ensureEnabled(ctx); err != nil {
		return err
	}
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return err
	}
	if post.AuthorUserID != userID {
		return ErrIdeaPostNotAuthor
	}
	if post.Status == IdeaPostStatusDeleted {
		return ErrIdeaPostNotFound
	}
	return s.repo.SoftDeletePost(ctx, postID, userID)
}

func (s *IdeasService) ListTags(ctx context.Context) ([]IdeaTag, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	return s.repo.ListTags(ctx)
}

// --- 互动 ---

func (s *IdeasService) ensurePublishedPost(ctx context.Context, postID int64) (*IdeaPost, error) {
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post.Status != IdeaPostStatusPublished {
		return nil, ErrIdeaPostInvalidState
	}
	return post, nil
}

func (s *IdeasService) Like(ctx context.Context, postID, userID int64) (int, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return 0, err
	}
	if _, err := s.ensurePublishedPost(ctx, postID); err != nil {
		return 0, err
	}
	return s.repo.Like(ctx, postID, userID)
}

func (s *IdeasService) Unlike(ctx context.Context, postID, userID int64) (int, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return 0, err
	}
	if _, err := s.ensurePublishedPost(ctx, postID); err != nil {
		return 0, err
	}
	return s.repo.Unlike(ctx, postID, userID)
}

func (s *IdeasService) Favorite(ctx context.Context, postID, userID int64) (int, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return 0, err
	}
	if _, err := s.ensurePublishedPost(ctx, postID); err != nil {
		return 0, err
	}
	return s.repo.Favorite(ctx, postID, userID)
}

func (s *IdeasService) Unfavorite(ctx context.Context, postID, userID int64) (int, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return 0, err
	}
	if _, err := s.ensurePublishedPost(ctx, postID); err != nil {
		return 0, err
	}
	return s.repo.Unfavorite(ctx, postID, userID)
}

func (s *IdeasService) RecordView(ctx context.Context, postID, userID int64) error {
	if err := s.ensureEnabled(ctx); err != nil {
		return err
	}
	post, err := s.ensurePublishedPost(ctx, postID)
	if err != nil {
		return err
	}
	return s.repo.RecordView(ctx, postID, userID, post.CurrentRevisionID)
}

// --- 打赏 ---

func (s *IdeasService) Reward(ctx context.Context, input IdeaRewardInput) (*IdeaReward, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, infraerrors.BadRequest("IDEA_REWARD_IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	}
	// 幂等：同一 key 返回原结果
	if existing, err := s.repo.GetRewardByIdempotencyKey(ctx, input.IdempotencyKey); err == nil && existing != nil {
		return existing, nil
	}

	var enabled bool
	var maxAmount float64
	switch input.AssetType {
	case IdeaRewardAssetBalance:
		enabled = s.settingService.IsIdeasRewardsBalanceEnabled(ctx)
		maxAmount = s.settingService.GetIdeasRewardBalanceMaxAmount(ctx)
	case IdeaRewardAssetPoints:
		enabled = s.settingService.IsIdeasRewardsPointsEnabled(ctx)
		maxAmount = s.settingService.GetIdeasRewardPointsMaxAmount(ctx)
	default:
		return nil, infraerrors.BadRequest("IDEA_REWARD_ASSET_INVALID", "invalid reward asset type")
	}
	if !enabled {
		return nil, infraerrors.Forbidden("IDEA_REWARD_DISABLED", "this reward type is disabled")
	}
	if input.Amount <= 0 || input.Amount > maxAmount {
		return nil, infraerrors.BadRequest("IDEA_REWARD_AMOUNT_INVALID", "reward amount is out of allowed range")
	}

	post, err := s.ensurePublishedPost(ctx, input.PostID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID == input.PayerUserID {
		return nil, infraerrors.BadRequest("IDEA_REWARD_SELF", "cannot reward your own post")
	}
	return s.repo.CreateReward(ctx, input, post)
}

// --- 举报 ---

func (s *IdeasService) Report(ctx context.Context, postID, reporterUserID int64, reason, detail string) error {
	if err := s.ensureEnabled(ctx); err != nil {
		return err
	}
	if _, err := s.ensurePublishedPost(ctx, postID); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return infraerrors.BadRequest("IDEA_REPORT_REASON_REQUIRED", "report reason is required")
	}
	return s.repo.CreateReport(ctx, postID, reporterUserID, strings.TrimSpace(reason), strings.TrimSpace(detail))
}

// --- 管理员 ---

func (s *IdeasService) AdminList(ctx context.Context, params IdeaPostListParams) ([]IdeaPost, int64, error) {
	return s.repo.ListAdmin(ctx, params)
}

func (s *IdeasService) AdminGet(ctx context.Context, postID int64) (*IdeaPost, error) {
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post.Status == IdeaPostStatusDeleted {
		return nil, ErrIdeaPostNotFound
	}
	return post, nil
}

func (s *IdeasService) AdminApprove(ctx context.Context, postID, adminUserID int64) (*IdeaPost, error) {
	return s.repo.SetPostStatus(ctx, postID, IdeaPostStatusPublished, IdeaRevisionStatusApproved, "", adminUserID, true)
}

func (s *IdeasService) AdminReject(ctx context.Context, postID, adminUserID int64, reason string) (*IdeaPost, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, infraerrors.BadRequest("IDEA_REJECTION_REASON_REQUIRED", "rejection reason is required")
	}
	return s.repo.SetPostStatus(ctx, postID, IdeaPostStatusRejected, IdeaRevisionStatusRejected, reason, adminUserID, false)
}

func (s *IdeasService) AdminHide(ctx context.Context, postID, adminUserID int64) (*IdeaPost, error) {
	return s.repo.SetPostStatus(ctx, postID, IdeaPostStatusHidden, "", "", adminUserID, false)
}

func (s *IdeasService) AdminRestore(ctx context.Context, postID, adminUserID int64) (*IdeaPost, error) {
	return s.repo.SetPostStatus(ctx, postID, IdeaPostStatusPublished, "", "", adminUserID, false)
}

func (s *IdeasService) AdminListReports(ctx context.Context, status string, page, pageSize int) ([]IdeaReport, int64, error) {
	return s.repo.ListReports(ctx, status, page, pageSize)
}

func (s *IdeasService) AdminResolveReport(ctx context.Context, reportID, adminUserID int64, resolution string) error {
	return s.repo.ResolveReport(ctx, reportID, adminUserID, strings.TrimSpace(resolution))
}

func (s *IdeasService) AdminListTags(ctx context.Context) ([]IdeaTag, error) {
	return s.repo.ListAllTags(ctx)
}

func (s *IdeasService) AdminCreateTag(ctx context.Context, name string) (*IdeaTag, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrIdeaTagNameInvalid
	}
	return s.repo.CreateTag(ctx, name)
}

func (s *IdeasService) AdminUpdateTag(ctx context.Context, tagID int64, name, status string) (*IdeaTag, error) {
	return s.repo.UpdateTag(ctx, tagID, name, status)
}

func (s *IdeasService) AdminMergeTags(ctx context.Context, sourceTagID, targetTagID int64) error {
	if sourceTagID == targetTagID {
		return infraerrors.BadRequest("IDEA_TAG_MERGE_SAME", "cannot merge a tag into itself")
	}
	return s.repo.MergeTags(ctx, sourceTagID, targetTagID)
}

func validateIdeaContent(title, summary, body string, tagSlugs []string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return ErrIdeaTitleRequired
	}
	if len([]rune(title)) > IdeaMaxTitleRunes {
		return infraerrors.BadRequest("IDEA_TITLE_TOO_LONG", "title is too long")
	}
	if len([]rune(summary)) > IdeaMaxSummaryRunes {
		return infraerrors.BadRequest("IDEA_SUMMARY_TOO_LONG", "summary is too long")
	}
	if strings.TrimSpace(body) == "" {
		return ErrIdeaBodyRequired
	}
	if len([]rune(body)) > IdeaMaxBodyRunes {
		return infraerrors.BadRequest("IDEA_BODY_TOO_LONG", "body is too long")
	}
	if len(tagSlugs) > IdeaMaxTagsPerPost {
		return ErrIdeaTooManyTags
	}
	for _, slug := range tagSlugs {
		if strings.TrimSpace(slug) == "" {
			return ErrIdeaTagNameInvalid
		}
	}
	return nil
}
