package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestValidateCreateAPIKeyRequestRejectsInvalidNumericLimits(t *testing.T) {
	tests := []struct {
		name string
		req  CreateAPIKeyRequest
		want string
	}{
		{
			name: "nan quota",
			req:  CreateAPIKeyRequest{Quota: math.NaN()},
			want: "finite and non-negative",
		},
		{
			name: "infinite 5h rate limit",
			req:  CreateAPIKeyRequest{RateLimit5h: math.Inf(1)},
			want: "finite and non-negative",
		},
		{
			name: "negative 1d rate limit",
			req:  CreateAPIKeyRequest{RateLimit1d: -1},
			want: "finite and non-negative",
		},
		{
			name: "negative 7d rate limit",
			req:  CreateAPIKeyRequest{RateLimit7d: -1},
			want: "finite and non-negative",
		},
		{
			name: "zero expires_in_days",
			req:  CreateAPIKeyRequest{ExpiresInDays: apiKeyServiceIntPtr(0)},
			want: "expires_in_days must be greater than zero",
		},
		{
			name: "negative expires_in_days",
			req:  CreateAPIKeyRequest{ExpiresInDays: apiKeyServiceIntPtr(-1)},
			want: "expires_in_days must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateAPIKeyRequest(tt.req)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateCreateAPIKeyRequestAcceptsValidNumericLimits(t *testing.T) {
	req := CreateAPIKeyRequest{
		Quota:         0,
		RateLimit5h:   0,
		RateLimit1d:   123456.789,
		RateLimit7d:   999999.5,
		ExpiresInDays: apiKeyServiceIntPtr(7),
	}

	require.NoError(t, validateCreateAPIKeyRequest(req))
}

func TestValidateUpdateAPIKeyRequestRejectsInvalidNumericLimits(t *testing.T) {
	tests := []struct {
		name string
		req  UpdateAPIKeyRequest
		want string
	}{
		{
			name: "negative quota",
			req:  UpdateAPIKeyRequest{Quota: apiKeyServiceFloat64Ptr(-1)},
			want: "finite and non-negative",
		},
		{
			name: "nan 5h rate limit",
			req:  UpdateAPIKeyRequest{RateLimit5h: apiKeyServiceFloat64Ptr(math.NaN())},
			want: "finite and non-negative",
		},
		{
			name: "infinite 1d rate limit",
			req:  UpdateAPIKeyRequest{RateLimit1d: apiKeyServiceFloat64Ptr(math.Inf(1))},
			want: "finite and non-negative",
		},
		{
			name: "negative 7d rate limit",
			req:  UpdateAPIKeyRequest{RateLimit7d: apiKeyServiceFloat64Ptr(-1)},
			want: "finite and non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpdateAPIKeyRequest(tt.req)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateUpdateAPIKeyRequestAcceptsValidNumericLimits(t *testing.T) {
	req := UpdateAPIKeyRequest{
		Quota:       apiKeyServiceFloat64Ptr(0),
		RateLimit5h: apiKeyServiceFloat64Ptr(0),
		RateLimit1d: apiKeyServiceFloat64Ptr(123456.789),
		RateLimit7d: apiKeyServiceFloat64Ptr(999999.5),
	}

	require.NoError(t, validateUpdateAPIKeyRequest(req))
}

func TestAPIKeyServiceCreateRejectsInvalidLimitsBeforeRepositoryWrite(t *testing.T) {
	groupID := int64(9)

	tests := []struct {
		name string
		req  CreateAPIKeyRequest
		want string
	}{
		{
			name: "nan quota",
			req: CreateAPIKeyRequest{
				Name:    "invalid quota",
				GroupID: &groupID,
				Quota:   math.NaN(),
			},
			want: "finite and non-negative",
		},
		{
			name: "zero expires_in_days",
			req: CreateAPIKeyRequest{
				Name:          "invalid expiry",
				GroupID:       &groupID,
				ExpiresInDays: apiKeyServiceIntPtr(0),
			},
			want: "expires_in_days must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &apiKeyCreateRepoStub{}
			svc := &APIKeyService{
				apiKeyRepo: repo,
				userRepo:   &apiKeyCreateUserRepoStub{user: &User{ID: 42}},
				groupRepo: &apiKeyCreateGroupRepoStub{group: &Group{
					ID:               groupID,
					Status:           StatusActive,
					Scope:            GroupScopePublic,
					SubscriptionType: SubscriptionTypeStandard,
				}},
				cfg: &config.Config{},
			}

			_, err := svc.Create(context.Background(), 42, tt.req)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, repo.created)
		})
	}
}

func TestAPIKeyServiceUpdateRejectsInvalidLimitsBeforeRepositoryWrite(t *testing.T) {
	tests := []struct {
		name string
		req  UpdateAPIKeyRequest
		want string
	}{
		{
			name: "negative quota",
			req:  UpdateAPIKeyRequest{Quota: apiKeyServiceFloat64Ptr(-1)},
			want: "finite and non-negative",
		},
		{
			name: "nan 5h rate limit",
			req:  UpdateAPIKeyRequest{RateLimit5h: apiKeyServiceFloat64Ptr(math.NaN())},
			want: "finite and non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &apiKeyRepoStub{apiKey: &APIKey{
				ID:     42,
				UserID: 7,
				Key:    "k",
				Status: StatusAPIKeyActive,
			}}
			svc := &APIKeyService{
				apiKeyRepo: repo,
				userRepo:   &apiKeyUpdateUserRepoStub{user: &User{ID: 7}},
			}

			_, err := svc.Update(context.Background(), 42, 7, tt.req)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, repo.updatedKeys)
		})
	}
}

func apiKeyServiceFloat64Ptr(v float64) *float64 {
	return &v
}

func apiKeyServiceIntPtr(v int) *int {
	return &v
}
