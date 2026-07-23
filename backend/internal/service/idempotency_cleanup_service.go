package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// IdempotencyCleanupService 定期清理已过期的幂等记录，避免表无限增长。
type IdempotencyCleanupService struct {
	repo         IdempotencyRepository
	taskExecutor *ClusterTaskExecutor
	interval     time.Duration
	batch        int

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
}

func NewIdempotencyCleanupService(
	repo IdempotencyRepository,
	cfg *config.Config,
	taskExecutors ...*ClusterTaskExecutor,
) *IdempotencyCleanupService {
	interval := 60 * time.Second
	batch := 500
	if cfg != nil {
		if cfg.Idempotency.CleanupIntervalSeconds > 0 {
			interval = time.Duration(cfg.Idempotency.CleanupIntervalSeconds) * time.Second
		}
		if cfg.Idempotency.CleanupBatchSize > 0 {
			batch = cfg.Idempotency.CleanupBatchSize
		}
	}
	service := &IdempotencyCleanupService{
		repo:     repo,
		interval: interval,
		batch:    batch,
		stopCh:   make(chan struct{}),
	}
	if len(taskExecutors) > 0 {
		service.taskExecutor = taskExecutors[0]
	}
	return service
}

func (s *IdempotencyCleanupService) Start() {
	if s == nil || s.repo == nil {
		return
	}
	s.startOnce.Do(func() {
		logger.LegacyPrintf("service.idempotency_cleanup", "[IdempotencyCleanup] started interval=%s batch=%d", s.interval, s.batch)
		go s.runLoop()
	})
}

func (s *IdempotencyCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		logger.LegacyPrintf("service.idempotency_cleanup", "[IdempotencyCleanup] stopped")
	})
}

func (s *IdempotencyCleanupService) runLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 启动后先清理一轮，防止重启后积压。
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

func (s *IdempotencyCleanupService) cleanupOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		deleted, err := s.repo.DeleteExpired(taskCtx, time.Now(), s.batch)
		if err != nil {
			return err
		}
		if deleted > 0 {
			logger.LegacyPrintf("service.idempotency_cleanup", "[IdempotencyCleanup] cleaned expired records count=%d", deleted)
		}
		return nil
	}
	var err error
	if s.taskExecutor == nil {
		err = run(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.taskExecutor.Run(ctx, "idempotency_cleanup", run)
	}
	if err != nil {
		logger.LegacyPrintf("service.idempotency_cleanup", "[IdempotencyCleanup] cleanup failed err=%v", err)
	}
}
