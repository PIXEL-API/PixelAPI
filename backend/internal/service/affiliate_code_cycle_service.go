package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const affiliateCodeCycleRefreshBatchSize = 500

type AffiliateCodeCycleService struct {
	affiliateService *AffiliateService
	stopCh           chan struct{}
	doneCh           chan struct{}
	startOnce        sync.Once
	stopOnce         sync.Once
}

func NewAffiliateCodeCycleService(affiliateService *AffiliateService) *AffiliateCodeCycleService {
	return &AffiliateCodeCycleService{
		affiliateService: affiliateService,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
	}
}

func (s *AffiliateCodeCycleService) Start() {
	if s == nil || s.affiliateService == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.run()
	})
}

func (s *AffiliateCodeCycleService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

func (s *AffiliateCodeCycleService) run() {
	defer close(s.doneCh)
	s.refresh(context.Background())
	for {
		delay := nextAffiliateCodeCycleResetDelay(time.Now())
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			s.refresh(context.Background())
		case <-s.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func (s *AffiliateCodeCycleService) refresh(ctx context.Context) {
	for {
		affected, err := s.affiliateService.RefreshExpiredAffiliateCodeCycles(ctx, affiliateCodeCycleRefreshBatchSize)
		if err != nil {
			logger.LegacyPrintf("service.affiliate", "[Affiliate] Failed to refresh affiliate invite code cycles: %v", err)
			return
		}
		if affected < affiliateCodeCycleRefreshBatchSize {
			return
		}
	}
}

func nextAffiliateCodeCycleResetDelay(now time.Time) time.Duration {
	cycle := currentAffiliateCodeCycle(now)
	delay := cycle.WindowEnd.Sub(now.UTC())
	if delay <= 0 {
		return time.Minute
	}
	return delay
}
