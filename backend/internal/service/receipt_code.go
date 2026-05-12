package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"golang.org/x/image/webp"
)

const (
	ReceiptCodePaymentMethodAlipay = "alipay"
	ReceiptCodePaymentMethodWeChat = "wechat"

	receiptCodeStorageProviderOSS = "oss"
	defaultReceiptCodeMaxBytes    = int64(1024 * 1024)
)

var (
	ErrReceiptCodeStorageNotConfigured = infraerrors.BadRequest("RECEIPT_CODE_STORAGE_NOT_CONFIGURED", "receipt code storage is not configured")
	ErrReceiptCodeNotFound             = infraerrors.NotFound("RECEIPT_CODE_NOT_FOUND", "receipt code not found")
	ErrReceiptCodePaymentMethodInvalid = infraerrors.BadRequest("RECEIPT_CODE_PAYMENT_METHOD_INVALID", "payment method is invalid")
	ErrReceiptCodeFileRequired         = infraerrors.BadRequest("RECEIPT_CODE_FILE_REQUIRED", "receipt code image is required")
	ErrReceiptCodeFileTooLarge         = infraerrors.BadRequest("RECEIPT_CODE_FILE_TOO_LARGE", "receipt code image is too large")
	ErrReceiptCodeInvalidImage         = infraerrors.BadRequest("RECEIPT_CODE_INVALID_IMAGE", "receipt code must be a valid PNG, JPEG, GIF, or WebP image")
)

type ReceiptCode struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"user_id"`
	PaymentMethod   string    `json:"payment_method"`
	StorageProvider string    `json:"storage_provider"`
	StorageKey      string    `json:"-"`
	URL             string    `json:"url,omitempty"`
	ContentType     string    `json:"content_type"`
	ByteSize        int       `json:"byte_size"`
	SHA256          string    `json:"sha256"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ReceiptCodeUploadInput struct {
	UserID        int64
	PaymentMethod string
	FileName      string
	ContentType   string
	Body          io.Reader
	Size          int64
}

type ReceiptCodeUpsertInput struct {
	UserID          int64
	PaymentMethod   string
	StorageProvider string
	StorageKey      string
	URL             string
	ContentType     string
	ByteSize        int
	SHA256          string
}

type ReceiptCodeRepository interface {
	GetReceiptCode(ctx context.Context, userID int64, paymentMethod string) (*ReceiptCode, error)
	UpsertReceiptCode(ctx context.Context, input ReceiptCodeUpsertInput) (*ReceiptCode, error)
	DeleteReceiptCode(ctx context.Context, userID int64, paymentMethod string) (*ReceiptCode, error)
}

type ReceiptCodeObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
	Delete(ctx context.Context, key string) error
	PresignURL(ctx context.Context, key string, expiry time.Duration) (string, error)
	PublicURL(key string) string
}

type ReceiptCodeObjectStoreFactory func(ctx context.Context, cfg config.ReceiptCodeStorageConfig) (ReceiptCodeObjectStore, error)

type ReceiptCodeService struct {
	repo         ReceiptCodeRepository
	cfg          config.ReceiptCodeStorageConfig
	storeFactory ReceiptCodeObjectStoreFactory
}

func NewReceiptCodeService(repo ReceiptCodeRepository, cfg *config.Config, storeFactory ReceiptCodeObjectStoreFactory) *ReceiptCodeService {
	var storageCfg config.ReceiptCodeStorageConfig
	if cfg != nil {
		storageCfg = cfg.ReceiptCodeStorage
	}
	return &ReceiptCodeService{
		repo:         repo,
		cfg:          storageCfg,
		storeFactory: storeFactory,
	}
}

func (s *ReceiptCodeService) Get(ctx context.Context, userID int64, paymentMethod string) (*ReceiptCode, error) {
	method := normalizeReceiptCodePaymentMethod(paymentMethod)
	if method == "" {
		return nil, ErrReceiptCodePaymentMethodInvalid
	}

	code, err := s.repo.GetReceiptCode(ctx, userID, method)
	if err != nil {
		return nil, err
	}
	if code == nil {
		return nil, nil
	}
	if err := s.attachAccessURL(ctx, code); err != nil {
		return nil, err
	}
	return code, nil
}

func (s *ReceiptCodeService) Upload(ctx context.Context, input ReceiptCodeUploadInput) (*ReceiptCode, error) {
	method := normalizeReceiptCodePaymentMethod(input.PaymentMethod)
	if method == "" {
		return nil, ErrReceiptCodePaymentMethodInvalid
	}
	if input.Body == nil {
		return nil, ErrReceiptCodeFileRequired
	}

	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}

	maxSize := s.maxSizeBytes()
	if input.Size > maxSize {
		return nil, ErrReceiptCodeFileTooLarge.WithMetadata(map[string]string{
			"max_size_bytes": fmt.Sprintf("%d", maxSize),
		})
	}

	data, err := readReceiptCodeUpload(input.Body, maxSize)
	if err != nil {
		return nil, err
	}
	contentType, ext, err := detectReceiptCodeImage(data, input.ContentType, input.FileName)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	key := buildReceiptCodeObjectKey(s.cfg.Prefix, input.UserID, method, ext)

	store, err := s.store(ctx)
	if err != nil {
		return nil, err
	}
	if err := store.Upload(ctx, key, bytes.NewReader(data), contentType); err != nil {
		return nil, fmt.Errorf("upload receipt code object: %w", err)
	}

	old, err := s.repo.GetReceiptCode(ctx, input.UserID, method)
	if err != nil {
		_ = store.Delete(ctx, key)
		return nil, fmt.Errorf("get old receipt code: %w", err)
	}

	code, err := s.repo.UpsertReceiptCode(ctx, ReceiptCodeUpsertInput{
		UserID:          input.UserID,
		PaymentMethod:   method,
		StorageProvider: receiptCodeStorageProviderOSS,
		StorageKey:      key,
		URL:             store.PublicURL(key),
		ContentType:     contentType,
		ByteSize:        len(data),
		SHA256:          sha,
	})
	if err != nil {
		_ = store.Delete(ctx, key)
		return nil, fmt.Errorf("save receipt code metadata: %w", err)
	}

	if old != nil && old.StorageKey != "" && old.StorageKey != key {
		_ = store.Delete(ctx, old.StorageKey)
	}
	if err := s.attachAccessURL(ctx, code); err != nil {
		return nil, err
	}
	return code, nil
}

func (s *ReceiptCodeService) Delete(ctx context.Context, userID int64, paymentMethod string) error {
	method := normalizeReceiptCodePaymentMethod(paymentMethod)
	if method == "" {
		return ErrReceiptCodePaymentMethodInvalid
	}

	deleted, err := s.repo.DeleteReceiptCode(ctx, userID, method)
	if err != nil {
		return err
	}
	if deleted == nil || deleted.StorageKey == "" {
		return nil
	}
	if err := s.ensureConfigured(); err != nil {
		return err
	}
	store, err := s.store(ctx)
	if err != nil {
		return err
	}
	return store.Delete(ctx, deleted.StorageKey)
}

func (s *ReceiptCodeService) ensureConfigured() error {
	if s == nil || s.storeFactory == nil || !s.cfg.Enabled ||
		strings.TrimSpace(s.cfg.Endpoint) == "" ||
		strings.TrimSpace(s.cfg.Bucket) == "" ||
		strings.TrimSpace(s.cfg.AccessKeyID) == "" ||
		strings.TrimSpace(s.cfg.SecretAccessKey) == "" {
		return ErrReceiptCodeStorageNotConfigured
	}
	return nil
}

func (s *ReceiptCodeService) store(ctx context.Context) (ReceiptCodeObjectStore, error) {
	if err := s.ensureConfigured(); err != nil {
		return nil, err
	}
	store, err := s.storeFactory(ctx, s.cfg)
	if err != nil {
		return nil, fmt.Errorf("create receipt code object store: %w", err)
	}
	return store, nil
}

func (s *ReceiptCodeService) attachAccessURL(ctx context.Context, code *ReceiptCode) error {
	if code == nil || strings.TrimSpace(code.StorageKey) == "" {
		return nil
	}
	store, err := s.store(ctx)
	if err != nil {
		if errors.Is(err, ErrReceiptCodeStorageNotConfigured) {
			code.URL = ""
			return nil
		}
		return err
	}
	if url := store.PublicURL(code.StorageKey); url != "" {
		code.URL = url
		return nil
	}
	expiry := time.Duration(s.cfg.PresignExpireSeconds) * time.Second
	if expiry <= 0 {
		expiry = 5 * time.Minute
	}
	url, err := store.PresignURL(ctx, code.StorageKey, expiry)
	if err != nil {
		return fmt.Errorf("presign receipt code object: %w", err)
	}
	code.URL = url
	return nil
}

func (s *ReceiptCodeService) maxSizeBytes() int64 {
	if s == nil || s.cfg.MaxSizeBytes <= 0 {
		return defaultReceiptCodeMaxBytes
	}
	return s.cfg.MaxSizeBytes
}

func normalizeReceiptCodePaymentMethod(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ReceiptCodePaymentMethodAlipay:
		return ReceiptCodePaymentMethodAlipay
	case ReceiptCodePaymentMethodWeChat, "weixin", "wxpay":
		return ReceiptCodePaymentMethodWeChat
	default:
		return ""
	}
}

func readReceiptCodeUpload(r io.Reader, maxSize int64) ([]byte, error) {
	if maxSize <= 0 {
		maxSize = defaultReceiptCodeMaxBytes
	}
	limited := io.LimitReader(r, maxSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read receipt code image: %w", err)
	}
	if len(data) == 0 {
		return nil, ErrReceiptCodeFileRequired
	}
	if int64(len(data)) > maxSize {
		return nil, ErrReceiptCodeFileTooLarge.WithMetadata(map[string]string{
			"max_size_bytes": fmt.Sprintf("%d", maxSize),
		})
	}
	return data, nil
}

func detectReceiptCodeImage(data []byte, declaredContentType, fileName string) (string, string, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png":
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			return "", "", ErrReceiptCodeInvalidImage
		}
		return contentType, ".png", nil
	case "image/jpeg":
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			return "", "", ErrReceiptCodeInvalidImage
		}
		return contentType, ".jpg", nil
	case "image/gif":
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			return "", "", ErrReceiptCodeInvalidImage
		}
		return contentType, ".gif", nil
	case "application/octet-stream":
		if strings.EqualFold(normalizeContentType(declaredContentType), "image/webp") ||
			strings.EqualFold(filepath.Ext(fileName), ".webp") {
			if _, err := webp.DecodeConfig(bytes.NewReader(data)); err != nil {
				return "", "", ErrReceiptCodeInvalidImage
			}
			return "image/webp", ".webp", nil
		}
	}
	return "", "", ErrReceiptCodeInvalidImage
}

func normalizeContentType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func buildReceiptCodeObjectKey(prefix string, userID int64, paymentMethod, ext string) string {
	normalizedPrefix := strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/")
	if normalizedPrefix == "" {
		normalizedPrefix = "receipt-codes"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("%s/%d/%s-%s%s", normalizedPrefix, userID, paymentMethod, uuid.NewString(), ext)
}

