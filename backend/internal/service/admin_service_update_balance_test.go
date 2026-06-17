//go:build unit

package service

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	_ "modernc.org/sqlite"
)

type balanceUserRepoStub struct {
	*userRepoStub
	updateErr error
	updated   []*User
	ledger    []UserBalanceLedgerDeltaInput
}

func (s *balanceUserRepoStub) Update(ctx context.Context, user *User) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if user == nil {
		return nil
	}
	clone := *user
	s.updated = append(s.updated, &clone)
	if s.userRepoStub != nil {
		s.userRepoStub.user = &clone
	}
	return nil
}

func (s *balanceUserRepoStub) LockUserBalanceForUpdate(ctx context.Context, userID int64) (float64, error) {
	if s.userRepoStub == nil || s.userRepoStub.user == nil {
		return 0, ErrUserNotFound
	}
	return s.userRepoStub.user.Balance, nil
}

func (s *balanceUserRepoStub) ApplyBalanceLedgerDelta(ctx context.Context, input UserBalanceLedgerDeltaInput) (*UserBalanceLedgerAdjustmentResult, error) {
	if s.userRepoStub == nil || s.userRepoStub.user == nil {
		return nil, ErrUserNotFound
	}
	before := s.userRepoStub.user.Balance
	after := before + input.Delta
	if input.ClampZero && after < 0 {
		input.Delta = -before
		after = 0
	}
	s.userRepoStub.user.Balance = after
	s.ledger = append(s.ledger, input)
	return &UserBalanceLedgerAdjustmentResult{
		BalanceBefore: before,
		BalanceAfter:  after,
		Delta:         input.Delta,
	}, nil
}

type balanceRedeemRepoStub struct {
	*redeemRepoStub
	created []*RedeemCode
}

func (s *balanceRedeemRepoStub) Create(ctx context.Context, code *RedeemCode) error {
	if code == nil {
		return nil
	}
	clone := *code
	if clone.ID == 0 {
		clone.ID = int64(len(s.created) + 1)
		code.ID = clone.ID
	}
	s.created = append(s.created, &clone)
	return nil
}

type authCacheInvalidatorStub struct {
	userIDs  []int64
	groupIDs []int64
	keys     []string
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByKey(ctx context.Context, key string) {
	s.keys = append(s.keys, key)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	s.groupIDs = append(s.groupIDs, groupID)
}

func newAdminBalanceTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:admin_balance_test?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestAdminService_UpdateUserBalance_InvalidatesAuthCache(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
		entClient:            newAdminBalanceTestClient(t),
	}

	user, err := svc.UpdateUserBalance(context.Background(), 7, 5, "add", "manual top-up")
	require.NoError(t, err)
	require.Equal(t, 15.0, user.Balance)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Len(t, redeemRepo.created, 1)
	require.Len(t, repo.ledger, 1)
	require.Equal(t, UserBalanceLedgerReasonAdminAdjustment, repo.ledger[0].Reason)
	require.Equal(t, 5.0, repo.ledger[0].Delta)
	require.NotNil(t, repo.ledger[0].RefID)
	require.Equal(t, redeemRepo.created[0].ID, *repo.ledger[0].RefID)
	require.Equal(t, "manual top-up", repo.ledger[0].Metadata["notes"])
}

func TestAdminService_UpdateUserBalance_NoChangeNoInvalidate(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
		entClient:            newAdminBalanceTestClient(t),
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 10, "set", "")
	require.NoError(t, err)
	require.Empty(t, invalidator.userIDs)
	require.Empty(t, redeemRepo.created)
	require.Empty(t, repo.ledger)
}

func TestRedeemService_GenerateCodesRejectsNonPositivePointsValue(t *testing.T) {
	svc := &RedeemService{redeemRepo: &redeemRepoStub{}}

	_, err := svc.GenerateCodes(context.Background(), GenerateCodesRequest{
		Count: 1,
		Type:  RedeemTypePoints,
		Value: -1,
	})
	require.Error(t, err)

	_, err = svc.GenerateCodes(context.Background(), GenerateCodesRequest{
		Count: 1,
		Type:  RedeemTypePoints,
		Value: 0,
	})
	require.Error(t, err)
}
