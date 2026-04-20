//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ensureEmailCall struct {
	userID int64
	email  string
}

type replaceEmailCall struct {
	userID   int64
	oldEmail string
	newEmail string
}

type emailSyncUserRepoStub struct {
	*userRepoStub
	ensureCalls  []ensureEmailCall
	replaceCalls []replaceEmailCall
}

func (s *emailSyncUserRepoStub) EnsureEmailAuthIdentity(_ context.Context, userID int64, email string) error {
	s.ensureCalls = append(s.ensureCalls, ensureEmailCall{userID: userID, email: email})
	return nil
}

func (s *emailSyncUserRepoStub) ReplaceEmailAuthIdentity(_ context.Context, userID int64, oldEmail, newEmail string) error {
	s.replaceCalls = append(s.replaceCalls, replaceEmailCall{
		userID:   userID,
		oldEmail: oldEmail,
		newEmail: newEmail,
	})
	return nil
}

func (s *emailSyncUserRepoStub) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	return map[int64]*time.Time{}, nil
}

func (s *emailSyncUserRepoStub) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	return nil, nil
}

func TestAdminService_CreateUser_EnsuresEmailAuthIdentity(t *testing.T) {
	repo := &emailSyncUserRepoStub{userRepoStub: &userRepoStub{nextID: 55}}
	svc := &adminServiceImpl{userRepo: repo}

	user, err := svc.CreateUser(context.Background(), &CreateUserInput{
		Email:    "admin-created@example.com",
		Password: "strong-pass",
	})
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, []ensureEmailCall{{
		userID: 55,
		email:  "admin-created@example.com",
	}}, repo.ensureCalls)
	require.Empty(t, repo.replaceCalls)
}

func TestAdminService_UpdateUser_ReplacesEmailAuthIdentity(t *testing.T) {
	repo := &emailSyncUserRepoStub{
		userRepoStub: &userRepoStub{
			user: &User{
				ID:          91,
				Email:       "before@example.com",
				Role:        RoleUser,
				Status:      StatusActive,
				Concurrency: 3,
			},
		},
	}
	svc := &adminServiceImpl{userRepo: repo}

	updated, err := svc.UpdateUser(context.Background(), 91, &UpdateUserInput{
		Email: "after@example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, "after@example.com", updated.Email)
	require.Equal(t, []replaceEmailCall{{
		userID:   91,
		oldEmail: "before@example.com",
		newEmail: "after@example.com",
	}}, repo.replaceCalls)
	require.Empty(t, repo.ensureCalls)
}
