package service

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	soraCacheCleanupInterval = time.Hour
	soraCacheCleanupBatch    = 200
)

// SoraCacheCleanupService 负责清理 Sora 视频缓存文件。
type SoraCacheCleanupService struct {
	cacheRepo      SoraCacheFileRepository
	settingService *SettingService
	cfg            *config.Config
	stopCh         chan struct{}
	stopOnce       sync.Once
}

func NewSoraCacheCleanupService(cacheRepo SoraCacheFileRepository, settingService *SettingService, cfg *config.Config) *SoraCacheCleanupService {
	return &SoraCacheCleanupService{
		cacheRepo:      cacheRepo,
		settingService: settingService,
		cfg:            cfg,
		stopCh:         make(chan struct{}),
	}
}

func (s *SoraCacheCleanupService) Start() {
	if s == nil || s.cacheRepo == nil {
		return
	}
	go s.cleanupLoop()
}

func (s *SoraCacheCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *SoraCacheCleanupService) cleanupLoop() {
	ticker := time.NewTicker(soraCacheCleanupInterval)
	defer ticker.Stop()

	s.cleanupOnce()
	for {
		select {
		case <-ticker.C:
			s.cleanupOnce()
		case <-s.stopCh:
			return
		}
	}
}

func (s *SoraCacheCleanupService) cleanupOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if s.cacheRepo == nil {
		return
	}

	cfg := s.getSoraConfig(ctx)
	videoDir := strings.TrimSpace(cfg.Cache.VideoDir)
	if videoDir == "" {
		return
	}
	maxBytes := cfg.Cache.MaxBytes
	if maxBytes <= 0 {
		return
	}

	size, err := dirSize(videoDir)
	if err != nil {
		log.Printf("[SoraCacheCleanup] 计算目录大小失败: %v", err)
		return
	}
	if size <= maxBytes {
		return
	}

	for size > maxBytes {
		entries, err := s.cacheRepo.ListOldest(ctx, soraCacheCleanupBatch)
		if err != nil {
			log.Printf("[SoraCacheCleanup] 读取缓存记录失败: %v", err)
			return
		}
		if len(entries) == 0 {
			log.Printf("[SoraCacheCleanup] 无缓存记录但目录仍超限: size=%d max=%d", size, maxBytes)
			return
		}

		ids := make([]int64, 0, len(entries))
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			removedSize := entry.SizeBytes
			if entry.CachePath != "" {
				if info, err := os.Stat(entry.CachePath); err == nil {
					if removedSize <= 0 {
						removedSize = info.Size()
					}
				}
				if err := os.Remove(entry.CachePath); err != nil && !os.IsNotExist(err) {
					log.Printf("[SoraCacheCleanup] 删除缓存文件失败: path=%s err=%v", entry.CachePath, err)
				}
			}

			if entry.ID > 0 {
				ids = append(ids, entry.ID)
			}
			if removedSize > 0 {
				size -= removedSize
				if size < 0 {
					size = 0
				}
			}
		}

		if len(ids) > 0 {
			if err := s.cacheRepo.DeleteByIDs(ctx, ids); err != nil {
				log.Printf("[SoraCacheCleanup] 删除缓存记录失败: %v", err)
			}
		}

		if size > maxBytes {
			if refreshed, err := dirSize(videoDir); err == nil {
				size = refreshed
			}
		}
	}
}

func (s *SoraCacheCleanupService) getSoraConfig(ctx context.Context) config.SoraConfig {
	if s.settingService != nil {
		return s.settingService.GetSoraConfig(ctx)
	}
	if s.cfg != nil {
		return s.cfg.Sora
	}
	return config.SoraConfig{}
}
