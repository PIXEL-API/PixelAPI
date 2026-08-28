package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "golang.org/x/image/webp"
)

// IdeasOSSConfig 附件私有 OSS 配置。
type IdeasOSSConfig struct {
	Enabled              bool
	Endpoint             string
	Region               string
	Bucket               string
	AccessKeyID          string
	SecretAccessKey      string
	Prefix               string
	ForcePathStyle       bool
	PresignExpireSeconds int
}

// IdeaAsset 文章附件元数据（对象 key 不对外返回）。
type IdeaAsset struct {
	ID             int64     `json:"id"`
	PostID         int64     `json:"post_id"`
	RevisionID     int64     `json:"revision_id"`
	ObjectKey      string    `json:"-"`
	FileName       string    `json:"file_name"`
	MimeType       string    `json:"mime_type"`
	SizeBytes      int64     `json:"size_bytes"`
	SHA256         string    `json:"sha256,omitempty"`
	Width          int       `json:"width,omitempty"`
	Height         int       `json:"height,omitempty"`
	Status         string    `json:"status"`
	UploaderUserID int64     `json:"uploader_user_id"`
	CreatedAt      time.Time `json:"created_at"`
}

const (
	ideaAssetMaxSizeBytes         = 50 << 20 // 50 MB 单附件上限
	ideaAssetMaxPerPost           = 9
	ideaAssetPresignExpireDefault = 300
	ideaAssetMaxImageDimension    = 10000 // 图片单边最大像素，防止解压炸弹
)

var (
	ErrIdeasOSSDisabled     = infraerrors.Forbidden("IDEAS_OSS_DISABLED", "ideas attachment storage is disabled")
	ErrIdeaAssetTooLarge    = infraerrors.BadRequest("IDEA_ASSET_TOO_LARGE", "attachment is too large")
	ErrIdeaAssetTypeInvalid = infraerrors.BadRequest("IDEA_ASSET_TYPE_INVALID", "attachment type is not allowed")
	ErrIdeaAssetNotFound    = infraerrors.NotFound("IDEA_ASSET_NOT_FOUND", "attachment not found")
	ErrIdeaTooManyAssets    = infraerrors.BadRequest("IDEA_TOO_MANY_ASSETS", "too many attachments")
)

// ideaAssetImageMimeFormats 声明 MIME 到解码器格式名的映射，用于魔数一致性校验。
var ideaAssetImageMimeFormats = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
	"image/gif":  "gif",
	"image/webp": "webp",
}

func (s *IdeasService) UploadAsset(ctx context.Context, userID, postID int64, fileName, mimeType string, data []byte) (*IdeaAsset, error) {
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
	if post.Status == IdeaPostStatusDeleted || post.Status == IdeaPostStatusHidden {
		return nil, ErrIdeaPostInvalidState
	}
	if post.Revision == nil {
		return nil, ErrIdeaPostInvalidState
	}

	cfg, ready, err := s.settingService.GetIdeasOSSConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, ErrIdeasOSSDisabled
	}

	if len(data) == 0 || int64(len(data)) > ideaAssetMaxSizeBytes {
		return nil, ErrIdeaAssetTooLarge
	}
	mimeType = normalizeIdeaAssetMime(mimeType, fileName)
	if !isAllowedIdeaAssetMime(mimeType) {
		return nil, ErrIdeaAssetTypeInvalid
	}
	if detected := http.DetectContentType(data); isDangerousIdeaAssetMime(detected) {
		return nil, ErrIdeaAssetTypeInvalid
	}

	// 图片额外解码宽高并校验魔数一致性，声明类型与真实内容不符则拒绝。
	var width, height int
	if _, isImage := ideaAssetImageMimeFormats[mimeType]; isImage {
		w, h, err := decodeIdeaAssetImage(data, mimeType)
		if err != nil {
			return nil, err
		}
		width, height = w, h
	}

	existing, err := s.repo.ListAssets(ctx, postID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= ideaAssetMaxPerPost {
		return nil, ErrIdeaTooManyAssets
	}

	relKey := fmt.Sprintf("%d/%d/%d/%s%s", userID, postID, post.Revision.ID, newAssetUUID(), safeExt(fileName, mimeType))
	fullKey, err := s.repo.UploadAssetObject(ctx, cfg, relKey, data, mimeType)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	asset := &IdeaAsset{
		PostID:         postID,
		RevisionID:     post.Revision.ID,
		ObjectKey:      fullKey,
		FileName:       filepath.Base(fileName),
		MimeType:       mimeType,
		SizeBytes:      int64(len(data)),
		SHA256:         hex.EncodeToString(sum[:]),
		Width:          width,
		Height:         height,
		Status:         "active",
		UploaderUserID: userID,
	}
	if err := s.repo.CreateAsset(ctx, asset); err != nil {
		_ = s.repo.DeleteAssetObject(ctx, cfg, fullKey)
		return nil, err
	}
	return asset, nil
}

func (s *IdeasService) ListAssets(ctx context.Context, userID, postID int64) ([]IdeaAsset, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return nil, err
	}
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return nil, err
	}
	if post.AuthorUserID != userID && post.Status != IdeaPostStatusPublished {
		return nil, ErrIdeaPostNotFound
	}
	return s.repo.ListAssets(ctx, postID)
}

func (s *IdeasService) GetAssetURL(ctx context.Context, userID, postID, assetID int64) (string, error) {
	if err := s.ensureEnabled(ctx); err != nil {
		return "", err
	}
	post, err := s.repo.GetPost(ctx, postID)
	if err != nil {
		return "", err
	}
	asset, err := s.repo.GetAsset(ctx, assetID)
	if err != nil {
		return "", err
	}
	if asset.PostID != postID {
		return "", ErrIdeaAssetNotFound
	}
	if post.AuthorUserID != userID && post.Status != IdeaPostStatusPublished {
		return "", ErrIdeaPostNotFound
	}
	cfg, ready, err := s.settingService.GetIdeasOSSConfig(ctx)
	if err != nil {
		return "", err
	}
	if !ready {
		return "", ErrIdeasOSSDisabled
	}
	expiry := time.Duration(cfg.PresignExpireSeconds) * time.Second
	if expiry <= 0 {
		expiry = ideaAssetPresignExpireDefault * time.Second
	}
	return s.repo.PresignAssetURL(ctx, cfg, asset.ObjectKey, expiry)
}

func normalizeIdeaAssetMime(mimeType, fileName string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if mimeType != "" && mimeType != "application/octet-stream" {
		return mimeType
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".md", ".markdown", ".txt":
		return "text/plain"
	case ".zip":
		return "application/zip"
	case ".json":
		return "application/json"
	default:
		return mimeType
	}
}

func isAllowedIdeaAssetMime(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif",
		"application/pdf", "text/plain", "text/markdown", "application/zip", "application/json":
		return true
	default:
		return false
	}
}

func isDangerousIdeaAssetMime(mime string) bool {
	m := strings.ToLower(mime)
	return strings.Contains(m, "html") || strings.Contains(m, "svg") ||
		strings.Contains(m, "xml") || strings.Contains(m, "javascript") ||
		strings.Contains(m, "x-msdownload")
}

func safeExt(fileName, mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "application/json":
		return ".json"
	default:
		ext := strings.ToLower(filepath.Ext(fileName))
		if len(ext) > 1 && len(ext) <= 6 {
			return ext
		}
		return ".bin"
	}
}

func newAssetUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// decodeIdeaAssetImage 解码图片头部获取宽高，并校验声明类型与真实魔数一致。
func decodeIdeaAssetImage(data []byte, mimeType string) (int, int, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, ErrIdeaAssetTypeInvalid
	}
	if want, ok := ideaAssetImageMimeFormats[mimeType]; ok && format != want {
		return 0, 0, ErrIdeaAssetTypeInvalid
	}
	if cfg.Width > ideaAssetMaxImageDimension || cfg.Height > ideaAssetMaxImageDimension {
		return 0, 0, ErrIdeaAssetTooLarge
	}
	return cfg.Width, cfg.Height, nil
}
