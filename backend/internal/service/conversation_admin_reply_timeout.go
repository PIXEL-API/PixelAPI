package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	AdminReplyTimeout             = 4 * time.Hour
	AdminReplyTimeoutNoticeSource = "admin_reply_timeout"
	AdminReplyTimeoutNoticeText   = "管理员超时未回复请加群咨询或联系群主！"

	adminReplyTimeoutCheckInterval = 5 * time.Minute
	adminReplyTimeoutBatchSize     = 100
	adminReplyTimeoutTaskName      = "conversation_admin_reply_timeout"
)

type ConversationAdminReplyTimeoutRepository interface {
	SendAdminReplyTimeoutNotices(ctx context.Context, cutoff time.Time, limit int, content string) (int, error)
}

type ConversationAdminReplyTimeoutService struct {
	repo         ConversationAdminReplyTimeoutRepository
	interval     time.Duration
	timeout      time.Duration
	batchSize    int
	taskExecutor *ClusterTaskExecutor
	now          func() time.Time
	runCtx       context.Context
	cancel       context.CancelFunc
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
}

func NewConversationAdminReplyTimeoutService(
	repo ConversationAdminReplyTimeoutRepository,
	interval time.Duration,
	taskExecutors ...*ClusterTaskExecutor,
) *ConversationAdminReplyTimeoutService {
	runCtx, cancel := context.WithCancel(context.Background())
	service := &ConversationAdminReplyTimeoutService{
		repo:      repo,
		interval:  interval,
		timeout:   AdminReplyTimeout,
		batchSize: adminReplyTimeoutBatchSize,
		now:       time.Now,
		runCtx:    runCtx,
		cancel:    cancel,
		stopCh:    make(chan struct{}),
	}
	if len(taskExecutors) > 0 {
		service.taskExecutor = taskExecutors[0]
	}
	return service
}

func (s *ConversationAdminReplyTimeoutService) Start() {
	if s == nil || s.repo == nil || s.interval <= 0 || s.timeout <= 0 || s.batchSize <= 0 || s.now == nil {
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

func (s *ConversationAdminReplyTimeoutService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *ConversationAdminReplyTimeoutService) runOnce() {
	ctx, cancel := context.WithTimeout(s.runCtx, 30*time.Second)
	defer cancel()

	run := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		cutoff := s.now().Add(-s.timeout)
		sent, err := s.repo.SendAdminReplyTimeoutNotices(
			taskCtx,
			cutoff,
			s.batchSize,
			AdminReplyTimeoutNoticeText,
		)
		if err != nil {
			return err
		}
		if sent > 0 {
			log.Printf("[ConversationAdminReplyTimeout] Sent %d timeout notices", sent)
		}
		return nil
	}

	var err error
	if s.taskExecutor == nil {
		err = run(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.taskExecutor.Run(ctx, adminReplyTimeoutTaskName, run)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[ConversationAdminReplyTimeout] Check failed: %v", err)
	}
}

func validateAdminReplyTimeoutNoticeText() bool {
	return strings.TrimSpace(AdminReplyTimeoutNoticeText) != ""
}
