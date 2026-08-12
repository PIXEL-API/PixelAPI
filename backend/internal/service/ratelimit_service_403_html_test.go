//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type countingOpenAI403CounterCache struct {
	openAI403CounterCacheStub
	increments int
}

func (s *countingOpenAI403CounterCache) IncrementOpenAI403Count(ctx context.Context, accountID int64, window int) (int64, error) {
	s.increments++
	return s.openAI403CounterCacheStub.IncrementOpenAI403Count(ctx, accountID, window)
}

func TestRateLimitService_HandleUpstreamError_OpenAIHTML403SkipsAccountPenalty(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "doctype", body: "<!DOCTYPE html><html><body>403 Forbidden</body></html>"},
		{name: "html tag", body: "<html><body>403 Forbidden</body></html>"},
		{name: "leading whitespace", body: "\n\t  <!DOCTYPE HTML><html><body>Forbidden</body></html>"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			counter := &countingOpenAI403CounterCache{
				openAI403CounterCacheStub: openAI403CounterCacheStub{counts: []int64{1}},
			}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			service.SetOpenAI403CounterCache(counter)
			account := &Account{
				ID:       601,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
			}

			shouldDisable := service.HandleUpstreamError(
				context.Background(),
				account,
				http.StatusForbidden,
				http.Header{},
				[]byte(testCase.body),
			)

			require.False(t, shouldDisable)
			require.Equal(t, 0, repo.setErrorCalls)
			require.Equal(t, 0, repo.tempCalls)
			require.Equal(t, 0, counter.increments)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_OpenAIHTML403DoesNotEscalateAcrossRepeats(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &countingOpenAI403CounterCache{
		openAI403CounterCacheStub: openAI403CounterCacheStub{counts: []int64{1, 2, 3, 4, 5}},
	}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       602,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	for i := 0; i < openAI403DisableThreshold+2; i++ {
		require.False(t, service.HandleUpstreamError(
			context.Background(),
			account,
			http.StatusForbidden,
			http.Header{},
			[]byte("<!DOCTYPE html><html><body>403 Forbidden</body></html>"),
		))
	}

	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, 0, counter.increments)
}

func TestIsHTMLResponse(t *testing.T) {
	testCases := []struct {
		name string
		body string
		want bool
	}{
		{name: "doctype lower", body: "<!doctype html><html></html>", want: true},
		{name: "doctype upper", body: "<!DOCTYPE HTML>", want: true},
		{name: "bare html", body: "<html lang=\"en\">", want: true},
		{name: "leading whitespace", body: "\n\n   <html>", want: true},
		{name: "json error", body: `{"error":{"message":"forbidden"}}`, want: false},
		{name: "plain text", body: "Forbidden", want: false},
		{name: "empty", body: "", want: false},
		{name: "xml", body: `<?xml version="1.0"?><error/>`, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, isHTMLResponse([]byte(testCase.body)))
		})
	}
}
