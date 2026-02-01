package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

const (
	soraStorageDefaultRoot = "/app/data/sora"
)

// SoraMediaStorage 负责下载并落地 Sora 媒体
type SoraMediaStorage struct {
	cfg                *config.Config
	root               string
	imageRoot          string
	videoRoot          string
	maxConcurrent      int
	fallbackToUpstream bool
	debug              bool
	sem                chan struct{}
	ready              bool
}

func NewSoraMediaStorage(cfg *config.Config) *SoraMediaStorage {
	storage := &SoraMediaStorage{cfg: cfg}
	storage.refreshConfig()
	if storage.Enabled() {
		if err := storage.EnsureLocalDirs(); err != nil {
			log.Printf("[SoraStorage] 初始化失败: %v", err)
		}
	}
	return storage
}

func (s *SoraMediaStorage) Enabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(s.cfg.Sora.Storage.Type)) == "local"
}

func (s *SoraMediaStorage) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *SoraMediaStorage) ImageRoot() string {
	if s == nil {
		return ""
	}
	return s.imageRoot
}

func (s *SoraMediaStorage) VideoRoot() string {
	if s == nil {
		return ""
	}
	return s.videoRoot
}

func (s *SoraMediaStorage) refreshConfig() {
	if s == nil || s.cfg == nil {
		return
	}
	root := strings.TrimSpace(s.cfg.Sora.Storage.LocalPath)
	if root == "" {
		root = soraStorageDefaultRoot
	}
	s.root = root
	s.imageRoot = filepath.Join(root, "image")
	s.videoRoot = filepath.Join(root, "video")

	maxConcurrent := s.cfg.Sora.Storage.MaxConcurrentDownloads
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	s.maxConcurrent = maxConcurrent
	s.fallbackToUpstream = s.cfg.Sora.Storage.FallbackToUpstream
	s.debug = s.cfg.Sora.Storage.Debug
	s.sem = make(chan struct{}, maxConcurrent)
}

// EnsureLocalDirs 创建并校验本地目录
func (s *SoraMediaStorage) EnsureLocalDirs() error {
	if s == nil || !s.Enabled() {
		return nil
	}
	if err := os.MkdirAll(s.imageRoot, 0o755); err != nil {
		return fmt.Errorf("create image dir: %w", err)
	}
	if err := os.MkdirAll(s.videoRoot, 0o755); err != nil {
		return fmt.Errorf("create video dir: %w", err)
	}
	s.ready = true
	return nil
}

// StoreFromURLs 下载并存储媒体，返回相对路径或回退 URL
func (s *SoraMediaStorage) StoreFromURLs(ctx context.Context, mediaType string, urls []string) ([]string, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	if s == nil || !s.Enabled() {
		return urls, nil
	}
	if !s.ready {
		if err := s.EnsureLocalDirs(); err != nil {
			return nil, err
		}
	}
	results := make([]string, 0, len(urls))
	for _, raw := range urls {
		relative, err := s.downloadAndStore(ctx, mediaType, raw)
		if err != nil {
			if s.fallbackToUpstream {
				results = append(results, raw)
				continue
			}
			return nil, err
		}
		results = append(results, relative)
	}
	return results, nil
}

func (s *SoraMediaStorage) downloadAndStore(ctx context.Context, mediaType, rawURL string) (string, error) {
	if strings.TrimSpace(rawURL) == "" {
		return "", errors.New("empty url")
	}
	root := s.imageRoot
	if mediaType == "video" {
		root = s.videoRoot
	}
	if root == "" {
		return "", errors.New("storage root not configured")
	}

	retries := 3
	for attempt := 1; attempt <= retries; attempt++ {
		release, err := s.acquire(ctx)
		if err != nil {
			return "", err
		}
		relative, err := s.downloadOnce(ctx, root, mediaType, rawURL)
		release()
		if err == nil {
			return relative, nil
		}
		if s.debug {
			log.Printf("[SoraStorage] 下载失败(%d/%d): %s err=%v", attempt, retries, sanitizeSoraLogURL(rawURL), err)
		}
		if attempt < retries {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
			continue
		}
		return "", err
	}
	return "", errors.New("download retries exhausted")
}

func (s *SoraMediaStorage) downloadOnce(ctx context.Context, root, mediaType, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return "", fmt.Errorf("download failed: %d %s", resp.StatusCode, string(body))
	}

	ext := fileExtFromURL(rawURL)
	if ext == "" {
		ext = fileExtFromContentType(resp.Header.Get("Content-Type"))
	}
	if ext == "" {
		ext = ".bin"
	}

	datePath := time.Now().Format("2006/01/02")
	destDir := filepath.Join(root, filepath.FromSlash(datePath))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	filename := uuid.NewString() + ext
	destPath := filepath.Join(destDir, filename)
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = os.Remove(destPath)
		return "", err
	}

	relative := path.Join("/", mediaType, datePath, filename)
	if s.debug {
		log.Printf("[SoraStorage] 已落地 %s -> %s", sanitizeSoraLogURL(rawURL), relative)
	}
	return relative, nil
}

func (s *SoraMediaStorage) acquire(ctx context.Context) (func(), error) {
	if s.sem == nil {
		return func() {}, nil
	}
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func fileExtFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	ext := path.Ext(parsed.Path)
	return strings.ToLower(ext)
}

func fileExtFromContentType(ct string) string {
	if ct == "" {
		return ""
	}
	if exts, err := mime.ExtensionsByType(ct); err == nil && len(exts) > 0 {
		return strings.ToLower(exts[0])
	}
	return ""
}
