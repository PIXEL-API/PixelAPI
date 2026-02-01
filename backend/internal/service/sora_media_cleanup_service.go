package service

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/robfig/cron/v3"
)

var soraCleanupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// SoraMediaCleanupService 定期清理本地媒体文件
type SoraMediaCleanupService struct {
	storage *SoraMediaStorage
	cfg     *config.Config

	cron *cron.Cron

	startOnce sync.Once
	stopOnce  sync.Once
}

func NewSoraMediaCleanupService(storage *SoraMediaStorage, cfg *config.Config) *SoraMediaCleanupService {
	return &SoraMediaCleanupService{
		storage: storage,
		cfg:     cfg,
	}
}

func (s *SoraMediaCleanupService) Start() {
	if s == nil || s.cfg == nil {
		return
	}
	if !s.cfg.Sora.Storage.Cleanup.Enabled {
		log.Printf("[SoraCleanup] not started (disabled)")
		return
	}
	if s.storage == nil || !s.storage.Enabled() {
		log.Printf("[SoraCleanup] not started (storage disabled)")
		return
	}

	s.startOnce.Do(func() {
		schedule := strings.TrimSpace(s.cfg.Sora.Storage.Cleanup.Schedule)
		if schedule == "" {
			log.Printf("[SoraCleanup] not started (empty schedule)")
			return
		}
		loc := time.Local
		if strings.TrimSpace(s.cfg.Timezone) != "" {
			if parsed, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err == nil && parsed != nil {
				loc = parsed
			}
		}
		c := cron.New(cron.WithParser(soraCleanupCronParser), cron.WithLocation(loc))
		if _, err := c.AddFunc(schedule, func() { s.runCleanup() }); err != nil {
			log.Printf("[SoraCleanup] not started (invalid schedule=%q): %v", schedule, err)
			return
		}
		s.cron = c
		s.cron.Start()
		log.Printf("[SoraCleanup] started (schedule=%q tz=%s)", schedule, loc.String())
	})
}

func (s *SoraMediaCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				log.Printf("[SoraCleanup] cron stop timed out")
			}
		}
	})
}

func (s *SoraMediaCleanupService) runCleanup() {
	retention := s.cfg.Sora.Storage.Cleanup.RetentionDays
	if retention <= 0 {
		log.Printf("[SoraCleanup] skipped (retention_days=%d)", retention)
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retention)
	deleted := 0

	roots := []string{s.storage.ImageRoot(), s.storage.VideoRoot()}
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if info.ModTime().Before(cutoff) {
				if rmErr := os.Remove(p); rmErr == nil {
					deleted++
				}
			}
			return nil
		})
	}
	log.Printf("[SoraCleanup] cleanup finished, deleted=%d", deleted)
}
