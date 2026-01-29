package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/uuidv7"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// SoraCacheService 提供 Sora 视频缓存能力。
type SoraCacheService struct {
	cfg            *config.Config
	cacheRepo      SoraCacheFileRepository
	settingService *SettingService
	accountRepo    AccountRepository
	httpUpstream   HTTPUpstream
}

// NewSoraCacheService 创建 SoraCacheService。
func NewSoraCacheService(cfg *config.Config, cacheRepo SoraCacheFileRepository, settingService *SettingService, accountRepo AccountRepository, httpUpstream HTTPUpstream) *SoraCacheService {
	return &SoraCacheService{
		cfg:            cfg,
		cacheRepo:      cacheRepo,
		settingService: settingService,
		accountRepo:    accountRepo,
		httpUpstream:   httpUpstream,
	}
}

func (s *SoraCacheService) CacheVideo(ctx context.Context, accountID, userID int64, taskID, mediaURL string) (*SoraCacheFile, error) {
	cfg := s.getSoraConfig(ctx)
	if !cfg.Cache.Enabled {
		return nil, nil
	}
	trimmed := strings.TrimSpace(mediaURL)
	if trimmed == "" {
		return nil, nil
	}

	allowedHosts := cfg.Cache.AllowedHosts
	useAllowlist := true
	if len(allowedHosts) == 0 {
		if s.cfg != nil {
			allowedHosts = s.cfg.Security.URLAllowlist.UpstreamHosts
			useAllowlist = s.cfg.Security.URLAllowlist.Enabled
		} else {
			useAllowlist = false
		}
	}

	if useAllowlist {
		if _, err := urlvalidator.ValidateHTTPSURL(trimmed, urlvalidator.ValidationOptions{
			AllowedHosts:     allowedHosts,
			RequireAllowlist: true,
			AllowPrivate:     s.cfg != nil && s.cfg.Security.URLAllowlist.AllowPrivateHosts,
		}); err != nil {
			return nil, fmt.Errorf("缓存下载地址不合法: %w", err)
		}
	} else {
		allowInsecure := false
		if s.cfg != nil {
			allowInsecure = s.cfg.Security.URLAllowlist.AllowInsecureHTTP
		}
		if _, err := urlvalidator.ValidateURLFormat(trimmed, allowInsecure); err != nil {
			return nil, fmt.Errorf("缓存下载地址不合法: %w", err)
		}
	}

	videoDir := strings.TrimSpace(cfg.Cache.VideoDir)
	if videoDir == "" {
		return nil, nil
	}

	if cfg.Cache.MaxBytes > 0 {
		size, err := dirSize(videoDir)
		if err != nil {
			return nil, err
		}
		if size >= cfg.Cache.MaxBytes {
			return nil, nil
		}
	}

	relativeDir := ""
	if cfg.Cache.UserDirEnabled && userID > 0 {
		relativeDir = fmt.Sprintf("u_%d", userID)
	}

	targetDir := filepath.Join(videoDir, relativeDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}

	uuid, err := uuidv7.New()
	if err != nil {
		return nil, err
	}

	name := deriveFileName(trimmed)
	if name == "" {
		name = "video.mp4"
	}
	name = sanitizeFileName(name)
	filename := uuid + "_" + name
	cachePath := filepath.Join(targetDir, filename)

	resp, err := s.downloadMedia(ctx, accountID, trimmed, time.Duration(cfg.Timeout)*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("缓存下载失败: %d", resp.StatusCode)
	}

	out, err := os.Create(cachePath)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return nil, err
	}

	cacheURL := buildCacheURL(relativeDir, filename)

	record := &SoraCacheFile{
		TaskID:      taskID,
		AccountID:   accountID,
		UserID:      userID,
		MediaType:   "video",
		OriginalURL: trimmed,
		CachePath:   cachePath,
		CacheURL:    cacheURL,
		SizeBytes:   written,
		CreatedAt:   time.Now(),
	}
	if s.cacheRepo != nil {
		if err := s.cacheRepo.Create(ctx, record); err != nil {
			return nil, err
		}
	}
	return record, nil
}

func buildCacheURL(relativeDir, filename string) string {
	base := "/data/video"
	if relativeDir != "" {
		return path.Join(base, relativeDir, filename)
	}
	return path.Join(base, filename)
}

func (s *SoraCacheService) getSoraConfig(ctx context.Context) config.SoraConfig {
	if s.settingService != nil {
		return s.settingService.GetSoraConfig(ctx)
	}
	if s.cfg != nil {
		return s.cfg.Sora
	}
	return config.SoraConfig{}
}

func (s *SoraCacheService) downloadMedia(ctx context.Context, accountID int64, mediaURL string, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", mediaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	if s.httpUpstream == nil {
		client := &http.Client{Timeout: timeout}
		return client.Do(req)
	}

	var accountConcurrency int
	proxyURL := ""
	if s.accountRepo != nil && accountID > 0 {
		account, err := s.accountRepo.GetByID(ctx, accountID)
		if err == nil && account != nil {
			accountConcurrency = account.Concurrency
			if account.Proxy != nil {
				proxyURL = account.Proxy.URL()
			}
		}
	}
	enableTLS := false
	if s.cfg != nil {
		enableTLS = s.cfg.Gateway.TLSFingerprint.Enabled
	}
	return s.httpUpstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, enableTLS)
}

func deriveFileName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "/" || name == "." {
		return ""
	}
	return name
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_' || r == '.':
			return r
		case r == ' ': // 空格替换为下划线
			return '_'
		default:
			return -1
		}
	}, name)
	return strings.TrimLeft(sanitized, ".")
}
