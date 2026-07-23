package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type apiKeyCreateRepoStub struct {
	APIKeyRepository
	created *APIKey
}

func (s *apiKeyCreateRepoStub) Create(_ context.Context, key *APIKey) error {
	clone := *key
	if key.ExpiresAt != nil {
		expiresAt := *key.ExpiresAt
		clone.ExpiresAt = &expiresAt
	}
	s.created = &clone
	return nil
}

type apiKeyCreateUserRepoStub struct {
	UserRepository
	user *User
}

func (s *apiKeyCreateUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	clone := *s.user
	return &clone, nil
}

type apiKeyCreateGroupRepoStub struct {
	GroupRepository
	group *Group
}

func (s *apiKeyCreateGroupRepoStub) GetByID(context.Context, int64) (*Group, error) {
	clone := *s.group
	return &clone, nil
}

func TestAPIKeyServiceCreatePreservesExactExpiration(t *testing.T) {
	groupID := int64(9)
	expiresAt := time.Date(2099, time.March, 3, 21, 6, 7, 123456789, time.UTC)
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

	created, err := svc.Create(context.Background(), 42, CreateAPIKeyRequest{
		Name:      "precise expiration",
		GroupID:   &groupID,
		ExpiresAt: &expiresAt,
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.NotNil(t, created.ExpiresAt)
	require.Equal(t, expiresAt, *created.ExpiresAt)
	require.Equal(t, expiresAt, *repo.created.ExpiresAt)
}

func TestAPIKeyServiceCreateRejectsInvalidExactExpirationBeforeRepositoryAccess(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	legacyDays := 30

	tests := []struct {
		name string
		req  CreateAPIKeyRequest
		want error
	}{
		{
			name: "past expires_at",
			req:  CreateAPIKeyRequest{ExpiresAt: &past},
			want: ErrAPIKeyExpirationNotFuture,
		},
		{
			name: "conflicting expiration fields",
			req:  CreateAPIKeyRequest{ExpiresAt: &past, ExpiresInDays: &legacyDays},
			want: ErrAPIKeyExpirationConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &APIKeyService{}

			_, err := svc.Create(context.Background(), 42, tt.req)

			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestResolveCreateAPIKeyExpirationKeepsLegacyDaysCompatibility(t *testing.T) {
	now := time.Date(2026, time.July, 23, 8, 9, 10, 11, time.UTC)
	days := 30

	expiresAt, err := resolveCreateAPIKeyExpiration(CreateAPIKeyRequest{ExpiresInDays: &days}, now)

	require.NoError(t, err)
	require.NotNil(t, expiresAt)
	require.Equal(t, now.AddDate(0, 0, days), *expiresAt)
}
