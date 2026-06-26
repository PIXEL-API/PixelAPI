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

	result, err := s.activityService.RunDueDraws(ctx, time.Now(), activityAutoDrawBatchSize)
	if err != nil {
		slog.Error("[ActivityAutoDraw] failed to run due draws", "error", err)
		return
	}
	if result == nil || result.Processed == 0 {
		return
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
}

func ProvideActivityAutoDrawService(activityService *ActivityService) *ActivityAutoDrawService {
	svc := NewActivityAutoDrawService(activityService, activityAutoDrawInterval)
	svc.Start()
	return svc
}
