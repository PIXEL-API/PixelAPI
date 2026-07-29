package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type accountShareModerationClaimRepoStub struct {
	*accountShareModeRepoStub

	pending       []AccountShareReview
	claimLimits   []int
	attemptCalls  []int64
	completeCalls []int64
}

func (r *accountShareModerationClaimRepoStub) BeginReviewModerationAttempt(
	_ context.Context,
	reviewID int64,
	_ int,
) (bool, error) {
	r.attemptCalls = append(r.attemptCalls, reviewID)
	return true, nil
}

type accountShareModerationRoundTripperFunc func(*http.Request) (*http.Response, error)

type accountShareReviewListRepoStub struct {
	*accountShareModeRepoStub
	reviews        []AccountShareReview
	canViewDetails bool
}

func (r *accountShareReviewListRepoStub) ListListingReviews(
	context.Context,
	int64,
	bool,
	int64,
	pagination.PaginationParams,
) ([]AccountShareReview, *pagination.PaginationResult, error) {
	return append([]AccountShareReview(nil), r.reviews...), &pagination.PaginationResult{
		Total:    int64(len(r.reviews)),
		Page:     1,
		PageSize: 20,
		Pages:    1,
	}, nil
}

func (r *accountShareReviewListRepoStub) CanViewListingReviewDetails(
	context.Context,
	int64,
	bool,
	int64,
) (bool, error) {
	return r.canViewDetails, nil
}

func (f accountShareModerationRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (r *accountShareModerationClaimRepoStub) ClaimPendingReviewModerations(
	_ context.Context,
	_ time.Time,
	limit int,
) ([]AccountShareReview, error) {
	r.claimLimits = append(r.claimLimits, limit)
	if len(r.pending) == 0 {
		return nil, nil
	}
	review := r.pending[0]
	r.pending = r.pending[1:]
	return []AccountShareReview{review}, nil
}

func (r *accountShareModerationClaimRepoStub) CompleteReviewModeration(
	_ context.Context,
	reviewID int64,
	_ AccountShareReviewModerationResult,
) error {
	r.completeCalls = append(r.completeCalls, reviewID)
	return nil
}

func TestAccountShareReviewModerationClaimsOnlyTheJobBeingStarted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"decision":"pass","reason":""}`}},
			},
		})
	}))
	defer server.Close()

	repo := &accountShareModerationClaimRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		pending: []AccountShareReview{
			{ID: 1, Score: 8, Comment: "稳定"},
			{ID: 2, Score: 9, Comment: "好用"},
		},
	}
	service := &AccountShareModeService{
		repo:             repo,
		reviewHTTPClient: server.Client(),
		reviewSettingRepo: &accountShareReviewSettingRepoStub{values: map[string]string{
			SettingKeyAccountShareCommentReviewEnabled: "true",
			SettingKeyAccountShareCommentReviewURL:     server.URL,
			SettingKeyAccountShareCommentReviewAPIKey:  "review-key",
			SettingKeyAccountShareCommentReviewModel:   "review-model",
		}},
	}

	err := service.processReviewModerationOnceLeased(context.Background(), nil)

	require.NoError(t, err)
	require.Equal(t, []int{1, 1, 1}, repo.claimLimits)
	require.Equal(t, []int64{1, 2}, repo.attemptCalls)
	require.Equal(t, []int64{1, 2}, repo.completeCalls)
}

func TestAccountShareReviewModerationCancellationDoesNotClaimRemainingJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: accountShareModerationRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})}

	repo := &accountShareModerationClaimRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		pending: []AccountShareReview{
			{ID: 1, Score: 8, Comment: "稳定"},
			{ID: 2, Score: 9, Comment: "好用"},
		},
	}
	service := &AccountShareModeService{
		repo:             repo,
		reviewHTTPClient: client,
		reviewSettingRepo: &accountShareReviewSettingRepoStub{values: map[string]string{
			SettingKeyAccountShareCommentReviewEnabled: "true",
			SettingKeyAccountShareCommentReviewURL:     "https://moderation.example/v1/chat/completions",
			SettingKeyAccountShareCommentReviewAPIKey:  "review-key",
			SettingKeyAccountShareCommentReviewModel:   "review-model",
		}},
	}

	err := service.processReviewModerationOnceLeased(ctx, nil)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []int{1}, repo.claimLimits)
	require.Len(t, repo.pending, 1)
	require.Equal(t, int64(2), repo.pending[0].ID)
	require.Equal(t, []int64{1}, repo.attemptCalls)
	require.Empty(t, repo.completeCalls)
}

func TestAccountShareReviewModerationDoesNotFollowRedirectOrExposeResponseBody(t *testing.T) {
	var redirectedCalls atomic.Int64
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("redirected Authorization = %q, want empty", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer redirected.Close()

	const sensitiveBody = "upstream-secret-detail"
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer review-key" {
			t.Errorf("initial Authorization = %q, want Bearer review-key", got)
		}
		w.Header().Set("Location", redirected.URL)
		w.WriteHeader(http.StatusFound)
		_, _ = io.WriteString(w, sensitiveBody)
	}))
	defer redirector.Close()

	service := &AccountShareModeService{reviewHTTPClient: redirector.Client()}
	_, err := service.callAccountShareCommentReviewModel(
		context.Background(),
		accountShareCommentReviewConfig{
			URL:    redirector.URL,
			APIKey: "review-key",
			Model:  "review-model",
		},
		&AccountShareReview{Score: 8, Comment: "稳定"},
	)

	require.ErrorContains(t, err, "non-success status 302")
	require.False(t, strings.Contains(err.Error(), sensitiveBody))
	require.Zero(t, redirectedCalls.Load())
}

func TestAnonymizePublicAccountShareReviewRemovesConsumerAndAccountIdentity(t *testing.T) {
	review := &AccountShareReview{
		AccountIdentityID:   11,
		AccountID:           12,
		MembershipID:        13,
		ConsumerUserID:      14,
		ConsumerUsername:    "真实用户",
		AccountName:         "真实账号",
		CommentRejectReason: "内部审核信息",
	}

	anonymizePublicAccountShareReview(review)

	require.Zero(t, review.AccountIdentityID)
	require.Zero(t, review.AccountID)
	require.Zero(t, review.MembershipID)
	require.Zero(t, review.ConsumerUserID)
	require.Equal(t, "匿名用户", review.ConsumerUsername)
	require.Empty(t, review.AccountName)
	require.Empty(t, review.CommentRejectReason)
}

func TestListListingReviewsAnonymizesPublicViewerButKeepsAuthorizedDetails(t *testing.T) {
	source := AccountShareReview{
		ID:                1,
		AccountIdentityID: 11,
		ListingID:         2,
		AccountID:         12,
		MembershipID:      13,
		OwnerUserID:       20,
		ConsumerUserID:    14,
		ConsumerUsername:  "真实用户",
		AccountName:       "真实账号",
		Comment:           "稳定",
		CommentStatus:     AccountShareReviewCommentStatusApproved,
	}
	for _, test := range []struct {
		name           string
		canViewDetails bool
		wantAnonymous  bool
	}{
		{name: "public", canViewDetails: false, wantAnonymous: true},
		{name: "authorized", canViewDetails: true, wantAnonymous: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &accountShareReviewListRepoStub{
				accountShareModeRepoStub: &accountShareModeRepoStub{},
				reviews:                  []AccountShareReview{source},
				canViewDetails:           test.canViewDetails,
			}
			service := &AccountShareModeService{repo: repo}

			reviews, _, err := service.ListListingReviews(
				context.Background(),
				99,
				false,
				2,
				pagination.PaginationParams{Page: 1, PageSize: 20},
			)

			require.NoError(t, err)
			require.Len(t, reviews, 1)
			if test.wantAnonymous {
				require.Equal(t, "匿名用户", reviews[0].ConsumerUsername)
				require.Zero(t, reviews[0].ConsumerUserID)
				require.Zero(t, reviews[0].MembershipID)
				require.Zero(t, reviews[0].AccountIdentityID)
				require.Zero(t, reviews[0].AccountID)
				require.Empty(t, reviews[0].AccountName)
			} else {
				require.Equal(t, source, reviews[0])
			}
		})
	}
}
