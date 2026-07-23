package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	defaultAccountErrorRetention = 24 * time.Hour
	defaultAccountErrorBatchSize = 100
	accountErrorCleanupTaskName  = "account_error_cleanup"
)

type AccountErrorCleanupRepository interface {
	DeleteStaleErrorAccounts(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// AccountErrorCleanupService soft-deletes accounts that stay in error state too long.
type AccountErrorCleanupService struct {
	repo         AccountErrorCleanupRepository
	retention    time.Duration
	interval     time.Duration
	batchSize    int
	taskExecutor *ClusterTaskExecutor
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewAccountErrorCleanupService(
	repo AccountErrorCleanupRepository,
	interval time.Duration,
	taskExecutors ...*ClusterTaskExecutor,
) *AccountErrorCleanupService {
	service := &AccountErrorCleanupService{
		repo:      repo,
		retention: defaultAccountErrorRetention,
		interval:  interval,
		batchSize: defaultAccountErrorBatchSize,
		stopCh:    make(chan struct{}),
	}
	if len(taskExecutors) > 0 {
		service.taskExecutor = taskExecutors[0]
	}
	return service
}

func (s *AccountErrorCleanupService) Start() {
	if s == nil || s.repo == nil || s.interval <= 0 || s.retention <= 0 || s.batchSize <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *AccountErrorCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *AccountErrorCleanupService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		cutoff := time.Now().Add(-s.retention)
		deleted, err := s.repo.DeleteStaleErrorAccounts(taskCtx, cutoff, s.batchSize)
		if err != nil {
			return err
		}
		if deleted > 0 {
			log.Printf("[AccountErrorCleanup] Soft-deleted %d stale error accounts", deleted)
		}
		return nil
	}
	var err error
	if s.taskExecutor == nil {
		err = run(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.taskExecutor.Run(ctx, accountErrorCleanupTaskName, run)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("[AccountErrorCleanup] Delete stale error accounts failed: %v", err)
	}
}
