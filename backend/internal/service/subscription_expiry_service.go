package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const subscriptionExpiryTaskName = "subscription_expiry"

// SubscriptionExpiryService periodically updates expired subscription status.
type SubscriptionExpiryService struct {
	userSubRepo  UserSubscriptionRepository
	taskExecutor *ClusterTaskExecutor
	interval     time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewSubscriptionExpiryService(
	userSubRepo UserSubscriptionRepository,
	interval time.Duration,
	taskExecutors ...*ClusterTaskExecutor,
) *SubscriptionExpiryService {
	service := &SubscriptionExpiryService{
		userSubRepo: userSubRepo,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}
	if len(taskExecutors) > 0 {
		service.taskExecutor = taskExecutors[0]
	}
	return service
}

func (s *SubscriptionExpiryService) Start() {
	if s == nil || s.userSubRepo == nil || s.interval <= 0 {
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

func (s *SubscriptionExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SubscriptionExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		updated, err := s.userSubRepo.BatchUpdateExpiredStatus(taskCtx)
		if err != nil {
			return err
		}
		if updated > 0 {
			log.Printf("[SubscriptionExpiry] Updated %d expired subscriptions", updated)
		}
		return nil
	}
	var err error
	if s.taskExecutor == nil {
		err = run(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.taskExecutor.Run(ctx, subscriptionExpiryTaskName, run)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("[SubscriptionExpiry] Update expired subscriptions failed: %v", err)
	}
}
