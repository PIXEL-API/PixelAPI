package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestAccountShareQuotaUsageJSONContractUsesSnakeCase(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(AccountShareQuotaUsage{
		LiveRooms:           1,
		RoomCreates24Hours:  2,
		OwnerRoomAccounts:   3,
		LargestRoomAccounts: 4,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"live_rooms": 1,
		"room_creates_24_hours": 2,
		"owner_room_accounts": 3,
		"largest_room_accounts": 4
	}`, string(payload))
}

type accountShareQuotaAdminRepositoryStub struct {
	AccountShareModeRepository
	latest        *AccountShareQuotaPolicy
	state         *AccountShareQuotaAdminState
	appended      *AccountShareQuotaPolicy
	appendInput   AppendAccountShareQuotaPolicyInput
	applyOwnerIDs []int64
	applyErrors   map[int64]error
}

func (r *accountShareQuotaAdminRepositoryStub) ResolveAccountShareQuota(
	context.Context,
	int64,
	time.Time,
) (*AccountShareResolvedQuota, error) {
	if r.state == nil {
		return nil, nil
	}
	resolved := r.state.EffectiveQuota
	return &resolved, nil
}

func (r *accountShareQuotaAdminRepositoryStub) GetLatestAccountShareQuotaPolicy(
	context.Context,
	string,
	*int64,
) (*AccountShareQuotaPolicy, error) {
	return r.latest, nil
}

func (r *accountShareQuotaAdminRepositoryStub) GetAccountShareQuotaAdminState(
	context.Context,
	int64,
	time.Time,
) (*AccountShareQuotaAdminState, error) {
	return r.state, nil
}

func (r *accountShareQuotaAdminRepositoryStub) AppendAccountShareQuotaPolicyRevision(
	_ context.Context,
	input AppendAccountShareQuotaPolicyInput,
) (*AccountShareQuotaPolicy, error) {
	r.appendInput = input
	if r.appended != nil {
		return r.appended, nil
	}
	return &AccountShareQuotaPolicy{
		ID:           99,
		ScopeType:    input.ScopeType,
		OwnerUserID:  input.OwnerUserID,
		Version:      input.ExpectedVersion + 1,
		Status:       input.Status,
		OverrideKind: input.OverrideKind,
		Limits:       input.Limits,
		EffectiveAt:  input.EffectiveAt,
		ExpiresAt:    input.ExpiresAt,
		Reason:       input.Reason,
	}, nil
}

func (r *accountShareQuotaAdminRepositoryStub) ListAccountShareQuotaPolicyRevisions(
	context.Context,
	string,
	*int64,
	pagination.PaginationParams,
) ([]AccountShareQuotaPolicy, int64, error) {
	return nil, 0, nil
}

func (r *accountShareQuotaAdminRepositoryStub) ListAccountShareGrandfatherCandidates(
	context.Context,
	time.Time,
	pagination.PaginationParams,
) ([]AccountShareGrandfatherCandidate, int64, error) {
	return nil, 0, nil
}

func (r *accountShareQuotaAdminRepositoryStub) ApplyAccountShareGrandfatherCandidate(
	_ context.Context,
	input ApplyAccountShareGrandfatherCandidateInput,
) (*AccountShareGrandfatherBatchItemResult, error) {
	r.applyOwnerIDs = append(r.applyOwnerIDs, input.Item.OwnerUserID)
	if err := r.applyErrors[input.Item.OwnerUserID]; err != nil {
		return nil, err
	}
	return &AccountShareGrandfatherBatchItemResult{
		OwnerUserID:   input.Item.OwnerUserID,
		Status:        "applied",
		PolicyID:      input.Item.OwnerUserID + 100,
		PolicyVersion: input.Item.ExpectedVersion + 1,
		ExpiresAt:     &input.ExpiresAt,
	}, nil
}

func TestAccountShareQuotaAdminMutationsRequirePermissionConfirmationAndReason(t *testing.T) {
	t.Parallel()

	limits := DefaultAccountShareQuotaLimits()
	repo := &accountShareQuotaAdminRepositoryStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	_, err := svc.UpdateAccountShareGlobalQuotaForAdmin(
		context.Background(),
		42,
		false,
		UpdateAccountShareGlobalQuotaInput{},
	)
	require.ErrorIs(t, err, ErrAccountShareQuotaAdminRequired)

	_, err = svc.UpdateAccountShareGlobalQuotaForAdmin(
		context.Background(),
		42,
		true,
		UpdateAccountShareGlobalQuotaInput{
			Limits:          limits,
			ExpectedVersion: 1,
			Reason:          "raise capacity",
			Confirmed:       false,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareQuotaConfirmationRequired)

	_, err = svc.UpdateAccountShareGlobalQuotaForAdmin(
		context.Background(),
		42,
		true,
		UpdateAccountShareGlobalQuotaInput{
			Limits:          limits,
			ExpectedVersion: 1,
			Confirmed:       true,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareQuotaReasonRequired)
}

func TestAccountShareQuotaAdminGlobalUpdateAppendsDefaultRevision(t *testing.T) {
	t.Parallel()

	repo := &accountShareQuotaAdminRepositoryStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	limits := AccountShareQuotaLimits{
		MaxLiveRooms:            8,
		MaxRoomCreates24Hours:   9,
		MaxAccountsPerRoom:      30,
		MaxRoomAccountsPerOwner: 200,
	}

	got, err := svc.UpdateAccountShareGlobalQuotaForAdmin(
		context.Background(),
		42,
		true,
		UpdateAccountShareGlobalQuotaInput{
			Limits:          limits,
			ExpectedVersion: 3,
			Reason:          "运营容量评估后调整",
			Confirmed:       true,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, AccountShareQuotaScopeGlobal, repo.appendInput.ScopeType)
	require.Nil(t, repo.appendInput.OwnerUserID)
	require.Equal(t, AccountShareQuotaPolicyKindDefault, repo.appendInput.OverrideKind)
	require.Equal(t, AccountShareQuotaPolicyStatusActive, repo.appendInput.Status)
	require.Equal(t, int64(3), repo.appendInput.ExpectedVersion)
	require.Equal(t, limits, repo.appendInput.Limits)
	require.Equal(t, int64(42), repo.appendInput.ActorUserID)
}

func TestAccountShareQuotaOwnerOverrideRequiresFiniteValidity(t *testing.T) {
	t.Parallel()

	repo := &accountShareQuotaAdminRepositoryStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	limits := DefaultAccountShareQuotaLimits()
	effectiveAt := time.Now().UTC().Add(time.Hour)
	expiredBeforeStart := effectiveAt.Add(-time.Minute)

	_, err := svc.UpsertAccountShareOwnerQuotaForAdmin(
		context.Background(),
		42,
		true,
		77,
		UpsertAccountShareOwnerQuotaInput{
			Limits:          limits,
			EffectiveAt:     &effectiveAt,
			ExpiresAt:       &expiredBeforeStart,
			ExpectedVersion: 0,
			Reason:          "temporary override",
			Confirmed:       true,
		},
	)
	require.ErrorIs(t, err, ErrAccountShareQuotaInvalid)

	validExpiry := effectiveAt.Add(24 * time.Hour)
	_, err = svc.UpsertAccountShareOwnerQuotaForAdmin(
		context.Background(),
		42,
		true,
		77,
		UpsertAccountShareOwnerQuotaInput{
			Limits:          limits,
			EffectiveAt:     &effectiveAt,
			ExpiresAt:       &validExpiry,
			ExpectedVersion: 0,
			Reason:          "temporary override",
			Confirmed:       true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, AccountShareQuotaScopeOwner, repo.appendInput.ScopeType)
	require.Equal(t, int64(77), *repo.appendInput.OwnerUserID)
	require.Equal(t, AccountShareQuotaPolicyKindManual, repo.appendInput.OverrideKind)
	require.False(t, repo.appendInput.DeriveGrandfather)
}

func TestAccountShareQuotaGrandfatherAndRevokeKeepExplicitAuditSemantics(t *testing.T) {
	t.Parallel()

	expiry := time.Now().UTC().Add(7 * 24 * time.Hour)
	repo := &accountShareQuotaAdminRepositoryStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	_, err := svc.GrandfatherAccountShareOwnerQuotaForAdmin(
		context.Background(),
		42,
		true,
		77,
		GrandfatherAccountShareOwnerQuotaInput{
			ExpiresAt:       &expiry,
			ExpectedVersion: 0,
			Reason:          "保留迁移前历史房间并只允许收缩",
			Confirmed:       true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, AccountShareQuotaPolicyKindGrandfather, repo.appendInput.OverrideKind)
	require.True(t, repo.appendInput.DeriveGrandfather)

	repo.latest = &AccountShareQuotaPolicy{
		ID:           7,
		ScopeType:    AccountShareQuotaScopeOwner,
		OwnerUserID:  ptrInt64ForQuotaTest(77),
		Version:      1,
		Status:       AccountShareQuotaPolicyStatusActive,
		OverrideKind: AccountShareQuotaPolicyKindGrandfather,
		Limits:       DefaultAccountShareQuotaLimits(),
	}
	_, err = svc.RevokeAccountShareOwnerQuotaForAdmin(
		context.Background(),
		42,
		true,
		77,
		RevokeAccountShareOwnerQuotaInput{
			ExpectedVersion: 1,
			Reason:          "历史容量已收缩到全局默认",
			Confirmed:       true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, AccountShareQuotaPolicyStatusRevoked, repo.appendInput.Status)
	require.Equal(t, AccountShareQuotaPolicyKindGrandfather, repo.appendInput.OverrideKind)
	require.Nil(t, repo.appendInput.ExpiresAt)
	require.False(t, repo.appendInput.DeriveGrandfather)
}

func TestBatchGrandfatherAccountShareQuotaSortsAndDeduplicatesOwners(t *testing.T) {
	t.Parallel()
	repo := &accountShareQuotaAdminRepositoryStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	expiresAt := time.Now().UTC().Add(time.Hour)
	results, err := svc.BatchGrandfatherAccountShareQuotaForAdmin(
		context.Background(), 900, true,
		BatchGrandfatherAccountShareQuotaInput{
			ExpiresAt: &expiresAt,
			Reason:    "历史超限冻结",
			Confirmed: true,
			Items: []AccountShareGrandfatherCandidateItem{
				{OwnerUserID: 9, ExpectedVersion: 2, PreviewFingerprint: "candidate-9"},
				{OwnerUserID: 3, ExpectedVersion: 0, PreviewFingerprint: "candidate-3"},
				{OwnerUserID: 9, ExpectedVersion: 2, PreviewFingerprint: "candidate-9"},
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 9}, repo.applyOwnerIDs)
	require.Len(t, results, 2)
	require.Equal(t, int64(3), results[0].OwnerUserID)
	require.Equal(t, int64(9), results[1].OwnerUserID)
}

func TestBatchGrandfatherAccountShareQuotaRejectsConflictingDuplicateOwner(t *testing.T) {
	t.Parallel()

	repo := &accountShareQuotaAdminRepositoryStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	expiresAt := time.Now().UTC().Add(time.Hour)
	_, err := svc.BatchGrandfatherAccountShareQuotaForAdmin(
		context.Background(), 900, true,
		BatchGrandfatherAccountShareQuotaInput{
			ExpiresAt: &expiresAt,
			Reason:    "历史超限冻结",
			Confirmed: true,
			Items: []AccountShareGrandfatherCandidateItem{
				{OwnerUserID: 9, ExpectedVersion: 2, PreviewFingerprint: "candidate-9"},
				{OwnerUserID: 9, ExpectedVersion: 3, PreviewFingerprint: "candidate-9-new"},
			},
		},
	)
	require.ErrorIs(t, err, ErrAccountShareQuotaInvalid)
	require.Contains(t, err.Error(), "items/duplicate_owner")
	require.Empty(t, repo.applyOwnerIDs)
}

func TestBatchGrandfatherAccountShareQuotaReturnsPerOwnerInfrastructureFailure(t *testing.T) {
	t.Parallel()

	repo := &accountShareQuotaAdminRepositoryStub{
		applyErrors: map[int64]error{
			9: ErrAccountShareQuotaConfigurationUnavailable,
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	expiresAt := time.Now().UTC().Add(time.Hour)
	results, err := svc.BatchGrandfatherAccountShareQuotaForAdmin(
		context.Background(), 900, true,
		BatchGrandfatherAccountShareQuotaInput{
			ExpiresAt: &expiresAt,
			Reason:    "历史超限冻结",
			Confirmed: true,
			Items: []AccountShareGrandfatherCandidateItem{
				{OwnerUserID: 3, PreviewFingerprint: "candidate-3"},
				{OwnerUserID: 9, PreviewFingerprint: "candidate-9"},
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []int64{3, 9}, repo.applyOwnerIDs)
	require.Len(t, results, 2)
	require.Equal(t, "applied", results[0].Status)
	require.Equal(t, "failed", results[1].Status)
	require.Equal(t, "ACCOUNT_SHARE_QUOTA_CONFIGURATION_UNAVAILABLE", results[1].ResultCode)
	require.Zero(t, results[1].PolicyID)
}

func TestAccountShareGrandfatherBatchResultJSONIsCompactAndUsesResultCode(t *testing.T) {
	t.Parallel()

	results := make([]AccountShareGrandfatherBatchItemResult, 0, AccountShareGrandfatherBatchMaximumItems)
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	for ownerUserID := int64(1); ownerUserID <= AccountShareGrandfatherBatchMaximumItems; ownerUserID++ {
		results = append(results, AccountShareGrandfatherBatchItemResult{
			OwnerUserID:   ownerUserID,
			Status:        "applied",
			ResultCode:    "ACCOUNT_SHARE_QUOTA_APPLIED",
			Message:       "grandfather quota policy applied",
			PolicyID:      ownerUserID + 1000,
			PolicyVersion: 2,
			ExpiresAt:     &expiresAt,
		})
	}
	payload, err := json.Marshal(results)
	require.NoError(t, err)
	require.Less(t, len(payload), 64*1024)
	require.Contains(t, string(payload), `"result_code":"ACCOUNT_SHARE_QUOTA_APPLIED"`)
	require.NotContains(t, string(payload), `"code":`)
	require.NotContains(t, string(payload), `"policy":`)
	require.NotContains(t, string(payload), `"reason":`)
}

func ptrInt64ForQuotaTest(value int64) *int64 {
	return &value
}
