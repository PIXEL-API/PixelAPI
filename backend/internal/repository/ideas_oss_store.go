package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ideasOSSStore struct {
	client *s3.Client
	bucket string
	prefix string
}

func newIdeasOSSStore(ctx context.Context, cfg service.IdeasOSSConfig) (*ideasOSSStore, error) {
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "oss-cn-hangzhou"
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
			o.BaseEndpoint = &endpoint
		}
		o.UsePathStyle = cfg.ForcePathStyle
		o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return &ideasOSSStore{
		client: client,
		bucket: strings.TrimSpace(cfg.Bucket),
		prefix: strings.Trim(strings.TrimSpace(cfg.Prefix), "/"),
	}, nil
}

func (s *ideasOSSStore) fullKey(key string) string {
	if s.prefix == "" {
		return strings.TrimLeft(key, "/")
	}
	return s.prefix + "/" + strings.TrimLeft(key, "/")
}

func (r *ideasRepository) UploadAssetObject(ctx context.Context, cfg service.IdeasOSSConfig, key string, data []byte, contentType string) (string, error) {
	store, err := newIdeasOSSStore(ctx, cfg)
	if err != nil {
		return "", err
	}
	full := store.fullKey(key)
	if _, err := store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &store.bucket,
		Key:         &full,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	}); err != nil {
		return "", fmt.Errorf("OSS PutObject: %w", err)
	}
	return full, nil
}

func (r *ideasRepository) PresignAssetURL(ctx context.Context, cfg service.IdeasOSSConfig, key string, expiry time.Duration) (string, error) {
	store, err := newIdeasOSSStore(ctx, cfg)
	if err != nil {
		return "", err
	}
	pc := s3.NewPresignClient(store.client)
	res, err := pc.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &store.bucket, Key: &key}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("presign asset url: %w", err)
	}
	return res.URL, nil
}

func (r *ideasRepository) DeleteAssetObject(ctx context.Context, cfg service.IdeasOSSConfig, key string) error {
	store, err := newIdeasOSSStore(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = store.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &store.bucket, Key: &key})
	return err
}

const ideaAssetColumns = `id, post_id, revision_id, object_key, file_name, mime_type, size_bytes, COALESCE(sha256, ''), COALESCE(width, 0), COALESCE(height, 0), status, uploader_user_id, created_at`

func (r *ideasRepository) CreateAsset(ctx context.Context, asset *service.IdeaAsset) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO idea_post_assets (post_id, revision_id, object_key, file_name, mime_type, size_bytes, sha256, width, height, status, uploader_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, 0), NULLIF($9, 0), $10, $11)
		RETURNING `+ideaAssetColumns,
		asset.PostID, asset.RevisionID, asset.ObjectKey, asset.FileName, asset.MimeType, asset.SizeBytes, asset.SHA256, asset.Width, asset.Height, asset.Status, asset.UploaderUserID,
	).Scan(
		&asset.ID, &asset.PostID, &asset.RevisionID, &asset.ObjectKey, &asset.FileName, &asset.MimeType, &asset.SizeBytes, &asset.SHA256, &asset.Width, &asset.Height, &asset.Status, &asset.UploaderUserID, &asset.CreatedAt,
	)
	return err
}

func (r *ideasRepository) GetAsset(ctx context.Context, assetID int64) (*service.IdeaAsset, error) {
	asset, err := scanIdeaAsset(r.db.QueryRowContext(ctx, `
		SELECT `+ideaAssetColumns+` FROM idea_post_assets WHERE id = $1`, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrIdeaAssetNotFound
	}
	return asset, err
}

func (r *ideasRepository) ListAssets(ctx context.Context, postID int64) ([]service.IdeaAsset, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ideaAssetColumns+` FROM idea_post_assets
		WHERE post_id = $1 AND deleted_at IS NULL
		ORDER BY id ASC`, postID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	assets := make([]service.IdeaAsset, 0)
	for rows.Next() {
		a, err := scanIdeaAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, *a)
	}
	return assets, rows.Err()
}

func scanIdeaAsset(row ideaRowScanner) (*service.IdeaAsset, error) {
	var a service.IdeaAsset
	if err := row.Scan(
		&a.ID, &a.PostID, &a.RevisionID, &a.ObjectKey, &a.FileName, &a.MimeType, &a.SizeBytes, &a.SHA256, &a.Width, &a.Height, &a.Status, &a.UploaderUserID, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}
