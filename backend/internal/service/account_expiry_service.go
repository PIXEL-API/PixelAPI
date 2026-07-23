package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const accountExpiryTaskName = "account_expiry"

// AccountExpiryService periodically pauses expired accounts when auto-pause is enabled.
type AccountExpiryService struct {
	accountRepo  AccountRepository
	taskExecutor *ClusterTaskExecutor
	interval     time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewAccountExpiryService(
	accountRepo AccountRepository,
	interval time.Duration,
	taskExecutors ...*ClusterTaskExecutor,
) *AccountExpiryService {
	service := &AccountExpiryService{
		accountRepo: accountRepo,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}
	if len(taskExecutors) > 0 {
		service.taskExecutor = taskExecutors[0]
	}
	return service
}

func (s *AccountExpiryService) Start() {
	if s == nil || s.accountRepo == nil || s.interval <= 0 {
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

func (s *AccountExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *AccountExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	run := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		updated, err := s.accountRepo.AutoPauseExpiredAccounts(taskCtx, time.Now())
		if err != nil {
			return err
		}
		if updated > 0 {
			log.Printf("[AccountExpiry] Auto paused %d expired accounts", updated)
		}
		return nil
	}
	var err error
	if s.taskExecutor == nil {
		err = run(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.taskExecutor.Run(ctx, accountExpiryTaskName, run)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("[AccountExpiry] Auto pause expired accounts failed: %v", err)
	}
}
