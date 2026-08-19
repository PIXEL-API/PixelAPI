package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type conversationAdminReplyTimeoutRepoStub struct {
	mu      sync.Mutex
	cutoffs []time.Time
	limits  []int
	content []string
	called  chan struct{}
}

func (s *conversationAdminReplyTimeoutRepoStub) SendAdminReplyTimeoutNotices(
	_ context.Context,
	cutoff time.Time,
	limit int,
	content string,
) (int, error) {
	s.mu.Lock()
	s.cutoffs = append(s.cutoffs, cutoff)
	s.limits = append(s.limits, limit)
	s.content = append(s.content, content)
	s.mu.Unlock()
	select {
	case s.called <- struct{}{}:
	default:
	}
	return 1, nil
}

func TestConversationAdminReplyTimeoutServiceRunOnceUsesConfiguredBoundary(t *testing.T) {
	repo := &conversationAdminReplyTimeoutRepoStub{}
	now := time.Date(2026, time.July, 28, 15, 0, 0, 0, time.UTC)
	svc := NewConversationAdminReplyTimeoutService(repo, 5*time.Minute)
	svc.now = func() time.Time { return now }

	svc.runOnce()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []time.Time{now.Add(-AdminReplyTimeout)}, repo.cutoffs)
	require.Equal(t, []int{100}, repo.limits)
	require.Equal(t, []string{AdminReplyTimeoutNoticeText}, repo.content)
}

func TestConversationAdminReplyTimeoutServiceStartsImmediatelyAndStopsIdempotently(t *testing.T) {
	repo := &conversationAdminReplyTimeoutRepoStub{called: make(chan struct{}, 1)}
	svc := NewConversationAdminReplyTimeoutService(repo, time.Hour)

	svc.Start()
	select {
	case <-repo.called:
	case <-time.After(time.Second):
		t.Fatal("timeout service did not run immediately")
	}

	svc.Stop()
	svc.Stop()
}
