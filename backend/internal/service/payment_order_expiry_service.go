package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const expiryCheckTimeout = 30 * time.Second

// PaymentOrderExpiryService periodically expires timed-out payment orders.
type PaymentOrderExpiryService struct {
	paymentSvc   *PaymentService
	taskExecutor *ClusterTaskExecutor
	interval     time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewPaymentOrderExpiryService(paymentSvc *PaymentService, interval time.Duration) *PaymentOrderExpiryService {
	return &PaymentOrderExpiryService{
		paymentSvc: paymentSvc,
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

func (s *PaymentOrderExpiryService) Start() {
	if s == nil || s.paymentSvc == nil || s.interval <= 0 {
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

func (s *PaymentOrderExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *PaymentOrderExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	defer cancel()

	_, err := s.taskExecutor.Run(ctx, "payment_order_expiry", func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		return s.runOnceLeased(taskCtx)
	})
	if err != nil {
		slog.Error("[PaymentOrderExpiry] failed to expire orders", "error", err)
	}
}

func (s *PaymentOrderExpiryService) runOnceLeased(ctx context.Context) error {
	processed, err := s.paymentSvc.ExpireTimedOutOrders(ctx)
	if err != nil {
		return err
	}
	if processed > 0 {
		slog.Info("[PaymentOrderExpiry] processed timed-out orders", "count", processed)
	}
	return nil
}
