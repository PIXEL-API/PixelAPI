//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type emailSyncMockUserRepo struct {
	*mockUserRepo
	ensureCalls  []ensureEmailCall
	replaceCalls []replaceEmailCall
}

func (m *emailSyncMockUserRepo) EnsureEmailAuthIdentity(_ context.Context, userID int64, email string) error {
	m.ensureCalls = append(m.ensureCalls, ensureEmailCall{userID: userID, email: email})
	return nil
}

func (m *emailSyncMockUserRepo) ReplaceEmailAuthIdentity(_ context.Context, userID int64, oldEmail, newEmail string) error {
	m.replaceCalls = append(m.replaceCalls, replaceEmailCall{
		userID:   userID,
		oldEmail: oldEmail,
		newEmail: newEmail,
	})
	return nil
}

func (m *emailSyncMockUserRepo) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	return map[int64]*time.Time{}, nil
}

func (m *emailSyncMockUserRepo) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	return nil, nil
}

func TestUpdateProfile_ReplacesEmailAuthIdentityWhenEmailChanges(t *testing.T) {
	repo := &emailSyncMockUserRepo{
		mockUserRepo: &mockUserRepo{
			getByIDUser: &User{
				ID:          19,
				Email:       "profile-before@example.com",
				Username:    "tester",
				Concurrency: 2,
			},
		},
	}
	svc := NewUserService(repo, nil, nil, nil)

	newEmail := "profile-after@example.com"
	updated, err := svc.UpdateProfile(context.Background(), 19, UpdateProfileRequest{
		Email: &newEmail,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, newEmail, updated.Email)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, []replaceEmailCall{{
		userID:   19,
		oldEmail: "profile-before@example.com",
		newEmail: "profile-after@example.com",
	}}, repo.replaceCalls)
	require.Empty(t, repo.ensureCalls)
}
