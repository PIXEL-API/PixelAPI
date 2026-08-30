package handler

import (
	"io"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type IdeasHandler struct {
	ideasService *service.IdeasService
}

func NewIdeasHandler(ideasService *service.IdeasService) *IdeasHandler {
	return &IdeasHandler{ideasService: ideasService}
}

type ideaPostRequest struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
}

func (h *IdeasHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req ideaPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.ideasService.CreateDraft(c.Request.Context(), service.IdeaPostCreateInput{
		AuthorUserID: subject.UserID,
		Title:        req.Title,
		Summary:      req.Summary,
		Body:         req.Body,
		TagSlugs:     req.Tags,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, out)
}

func (h *IdeasHandler) Update(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	var req ideaPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.ideasService.Update(c.Request.Context(), service.IdeaPostUpdateInput{
		UserID:   subject.UserID,
		PostID:   id,
		Title:    req.Title,
		Summary:  req.Summary,
		Body:     req.Body,
		TagSlugs: req.Tags,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) Get(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.Get(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.ideasService.List(c.Request.Context(), service.IdeaPostListParams{
		Page:     page,
		PageSize: pageSize,
		Sort:     c.Query("sort"),
		TagSlug:  c.Query("tag"),
		Keyword:  c.Query("keyword"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *IdeasHandler) Mine(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.ideasService.ListMine(c.Request.Context(), subject.UserID, service.IdeaPostListParams{
		Page:     page,
		PageSize: pageSize,
		Sort:     c.Query("sort"),
		Status:   c.Query("status"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *IdeasHandler) Publish(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	out, err := h.ideasService.Publish(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *IdeasHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	if err := h.ideasService.Delete(c.Request.Context(), id, subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *IdeasHandler) ListTags(c *gin.Context) {
	tags, err := h.ideasService.ListTags(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tags)
}

func parseIdeaID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid id")
		return 0, false
	}
	return id, true
}

// --- 互动 ---

func (h *IdeasHandler) Like(c *gin.Context) {
	h.interaction(c, true, true)
}

func (h *IdeasHandler) Unlike(c *gin.Context) {
	h.interaction(c, true, false)
}

func (h *IdeasHandler) Favorite(c *gin.Context) {
	h.interaction(c, false, true)
}

func (h *IdeasHandler) Unfavorite(c *gin.Context) {
	h.interaction(c, false, false)
}

func (h *IdeasHandler) interaction(c *gin.Context, like bool, add bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	var count int
	var err error
	switch {
	case like && add:
		count, err = h.ideasService.Like(c.Request.Context(), id, subject.UserID)
	case like && !add:
		count, err = h.ideasService.Unlike(c.Request.Context(), id, subject.UserID)
	case !like && add:
		count, err = h.ideasService.Favorite(c.Request.Context(), id, subject.UserID)
	default:
		count, err = h.ideasService.Unfavorite(c.Request.Context(), id, subject.UserID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": count})
}

func (h *IdeasHandler) RecordView(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	if err := h.ideasService.RecordView(c.Request.Context(), id, subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// --- 打赏 ---

type ideaRewardRequest struct {
	AssetType string  `json:"asset_type"`
	Amount    float64 `json:"amount"`
}

func (h *IdeasHandler) Reward(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	var req ideaRewardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.ideasService.Reward(c.Request.Context(), service.IdeaRewardInput{
		PayerUserID:    subject.UserID,
		PostID:         id,
		AssetType:      req.AssetType,
		Amount:         req.Amount,
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, out)
}

// --- 举报 ---

type ideaReportRequest struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

func (h *IdeasHandler) Report(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	var req ideaReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.ideasService.Report(c.Request.Context(), id, subject.UserID, req.Reason, req.Detail); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// --- 附件 ---

func (h *IdeasHandler) UploadAsset(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "file is required")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 51<<20))
	if err != nil {
		response.BadRequest(c, "read file: "+err.Error())
		return
	}
	asset, err := h.ideasService.UploadAsset(c.Request.Context(), subject.UserID, id, header.Filename, header.Header.Get("Content-Type"), data)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, asset)
}

func (h *IdeasHandler) ListAssets(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	assets, err := h.ideasService.ListAssets(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, assets)
}

func (h *IdeasHandler) GetAssetURL(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := parseIdeaID(c)
	if !ok {
		return
	}
	assetID, err := strconv.ParseInt(c.Param("asset_id"), 10, 64)
	if err != nil || assetID <= 0 {
		response.BadRequest(c, "Invalid asset id")
		return
	}
	url, err := h.ideasService.GetAssetURL(c.Request.Context(), subject.UserID, id, assetID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"url": url})
}
