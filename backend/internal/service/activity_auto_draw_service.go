package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	activityAutoDrawInterval  = time.Minute
	activityAutoDrawBatchSize = 50
	activityAutoDrawTimeout   = 60 * time.Second
)

type ActivityAutoDrawService struct {
	activityService *ActivityService
	taskExecutor    *ClusterTaskExecutor
	interval        time.Duration
	stopCh          chan struct{}
	startOnce       sync.Once
	stopOnce        sync.Once
	wg              sync.WaitGroup
}

func NewActivityAutoDrawService(activityService *ActivityService, interval time.Duration) *ActivityAutoDrawService {
	return &ActivityAutoDrawService{
		activityService: activityService,
		interval:        interval,
		stopCh:          make(chan struct{}),
	}
}

func (s *ActivityAutoDrawService) Start() {
	if s == nil || s.activityService == nil || s.interval <= 0 {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.run()
		}()
	})
}

func (s *ActivityAutoDrawService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *ActivityAutoDrawService) run() {
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
}

func (s *ActivityAutoDrawService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), activityAutoDrawTimeout)
	defer cancel()

	_, err := s.taskExecutor.Run(ctx, "activity_auto_draw", func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		return s.runOnceLeased(taskCtx)
	})
	if err != nil {
		slog.Error("[ActivityAutoDraw] failed to run due draws", "error", err)
		return
	}
}

func (s *ActivityAutoDrawService) runOnceLeased(ctx context.Context) error {
	result, err := s.activityService.RunDueDraws(ctx, time.Now(), activityAutoDrawBatchSize)
	if err != nil {
		return err
	}
	if result == nil || result.Processed == 0 {
		return nil
	}
	for _, draw := range result.Draws {
		slog.Info(
			"[ActivityAutoDraw] completed activity draw",
			"campaign_id", draw.CampaignID,
			"campaign_name", draw.CampaignName,
			"draw_id", draw.DrawID,
			"users", draw.TotalUsers,
			"tickets", draw.TotalTickets,
			"winners", draw.WinnerCount,
		)
	}
	return nil
}

func ProvideActivityAutoDrawService(
	activityService *ActivityService,
	taskExecutor *ClusterTaskExecutor,
) *ActivityAutoDrawService {
	svc := NewActivityAutoDrawService(activityService, activityAutoDrawInterval)
	svc.taskExecutor = taskExecutor
	svc.Start()
	return svc
}
