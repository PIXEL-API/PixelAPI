package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

const (
	ownedAgentIdentityPrivateGroupID    int64 = 9001
	ownedAgentIdentityPlusPublicGroupID int64 = 9101
	ownedAgentIdentityTeamPublicGroupID int64 = 9102
)

type ownedAgentIdentityRepoStub struct {
	ownedAccountDuplicateRepoStub
	accounts             map[int64]*Account
	nextID               int64
	createCount          int
	updateCount          int
	conflictOnNextCreate bool
	updateErr            error
	bindGroupsErr        error
}

func newOwnedAgentIdentityRepoStub() *ownedAgentIdentityRepoStub {
	return &ownedAgentIdentityRepoStub{
		accounts: map[int64]*Account{},
		nextID:   1,
	}
}

func cloneOwnedAgentIdentityTestAccount(account *Account) *Account {
	if account == nil {
		return nil
	}
	clone := *account
	clone.Credentials = mergeAccountMap(account.Credentials, nil)
	clone.Extra = mergeAccountMap(account.Extra, nil)
	clone.GroupIDs = append([]int64(nil), account.GroupIDs...)
	if account.OwnerUserID != nil {
		ownerUserID := *account.OwnerUserID
		clone.OwnerUserID = &ownerUserID
	}
	if account.ProxyID != nil {
		proxyID := *account.ProxyID
		clone.ProxyID = &proxyID
	}
	if account.ExpiresAt != nil {
		expiresAt := *account.ExpiresAt
		clone.ExpiresAt = &expiresAt
	}
	if account.ExternalPlacement != nil {
		placement := *account.ExternalPlacement
		clone.ExternalPlacement = &placement
	}
	return &clone
}

func (s *ownedAgentIdentityRepoStub) Create(_ context.Context, account *Account) error {
	s.createCount++
	if account.ID <= 0 {
		account.ID = s.nextID
		s.nextID++
	}
	s.accounts[account.ID] = cloneOwnedAgentIdentityTestAccount(account)
	if s.conflictOnNextCreate {
		s.conflictOnNextCreate = false
		return ErrOwnedAccountAlreadyExists
	}
	return nil
}

func (s *ownedAgentIdentityRepoStub) Update(_ context.Context, account *Account) error {
	s.updateCount++
	if s.updateErr != nil {
		return s.updateErr
	}
	updated := cloneOwnedAgentIdentityTestAccount(account)
	if existing := s.accounts[account.ID]; existing != nil {
		// GroupIDs model the account_groups relation, which AccountRepository.Update
		// does not persist. BindGroups is the only operation that changes it.
		updated.GroupIDs = append([]int64(nil), existing.GroupIDs...)
	}
	s.accounts[account.ID] = updated
	return nil
}

func (s *ownedAgentIdentityRepoStub) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	if s.bindGroupsErr != nil {
		return s.bindGroupsErr
	}
	account := s.accounts[accountID]
	if account != nil {
		account.GroupIDs = append([]int64(nil), groupIDs...)
	}
	return nil
}

func (s *ownedAgentIdentityRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	account := s.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	return cloneOwnedAgentIdentityTestAccount(account), nil
}

func (s *ownedAgentIdentityRepoStub) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	accounts := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account := s.accounts[id]; account != nil {
			accounts = append(accounts, cloneOwnedAgentIdentityTestAccount(account))
		}
	}
	return accounts, nil
}

func (s *ownedAgentIdentityRepoStub) ListOwnedWithFilters(
	_ context.Context,
	ownerUserID int64,
	params pagination.PaginationParams,
	platform, accountType, _ string,
	_ string,
	_, _ int64,
	_ string,
) ([]Account, *pagination.PaginationResult, error) {
	accounts := make([]Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		if account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID {
			continue
		}
		if platform != "" && account.Platform != platform {
			continue
		}
		if accountType != "" && account.Type != accountType {
			continue
		}
		accounts = append(accounts, *cloneOwnedAgentIdentityTestAccount(account))
	}
	start := params.Offset()
	if start >= len(accounts) {
		return []Account{}, &pagination.PaginationResult{Total: int64(len(accounts))}, nil
	}
	end := start + params.Limit()
	if end > len(accounts) {
		end = len(accounts)
	}
	return accounts[start:end], &pagination.PaginationResult{Total: int64(len(accounts))}, nil
}

func (s *ownedAgentIdentityRepoStub) GetOwnedOpenAIAgentIdentityByChatGPTAccountID(
	_ context.Context,
	ownerUserID int64,
	chatGPTAccountID string,
) (*Account, error) {
	chatGPTAccountID = strings.TrimSpace(chatGPTAccountID)
	for _, account := range s.accounts {
		if account.OwnerUserID == nil || *account.OwnerUserID != ownerUserID || !account.IsOpenAIAgentIdentity() {
			continue
		}
		if strings.TrimSpace(account.GetChatGPTAccountID()) == chatGPTAccountID {
			return cloneOwnedAgentIdentityTestAccount(account), nil
		}
	}
	return nil, nil
}

type recordingAgentIdentityWSInvalidator struct {
	accountIDs []int64
}

type ownedAgentIdentityPlacementRepoStub struct {
	AccountShareModeRepository
	AccountShareRoomRepository
	accountRepo       *ownedAgentIdentityRepoStub
	beginDrain        bool
	restoreDrainCalls int
	conversionResult  *ConvertAccountExternalPlacementResult
}

func (s *ownedAgentIdentityPlacementRepoStub) HasRoomAccount(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func (s *ownedAgentIdentityPlacementRepoStub) BeginExternalPlacementDrain(_ context.Context, _ int64, accountID int64) (bool, error) {
	if !s.beginDrain {
		return false, nil
	}
	account := s.accountRepo.accounts[accountID]
	if account != nil && account.ExternalPlacement != nil {
		account.ExternalPlacement.State = "draining"
	}
	return true, nil
}

func (s *ownedAgentIdentityPlacementRepoStub) RestoreExternalPlacementAfterDrain(_ context.Context, _ int64, accountID int64) error {
	s.restoreDrainCalls++
	account := s.accountRepo.accounts[accountID]
	if account != nil && account.ExternalPlacement != nil && account.ExternalPlacement.State == "draining" {
		account.ExternalPlacement.State = "active"
	}
	return nil
}

func (s *ownedAgentIdentityPlacementRepoStub) ConvertExternalPlacement(_ context.Context, input ConvertAccountExternalPlacementInput) (*ConvertAccountExternalPlacementResult, error) {
	if s.conversionResult != nil {
		return s.conversionResult, nil
	}
	account, err := s.accountRepo.GetByID(context.Background(), input.AccountID)
	if err != nil {
		return nil, err
	}
	previous := cloneOwnedAgentIdentityTestAccount(account).ExternalPlacement
	if previous == nil {
		previous = &AccountExternalPlacement{Target: AccountExternalPlacementPrivate, State: "active"}
	}
	if err := s.accountRepo.BindGroups(context.Background(), input.AccountID, input.GroupIDs); err != nil {
		if input.Target == AccountExternalPlacementPublicPool {
			return nil, fmt.Errorf("bind public account groups: %w", err)
		}
		return nil, fmt.Errorf("bind groups: %w", err)
	}
	account.GroupIDs = append([]int64(nil), input.GroupIDs...)
	account.ShareMode = AccountShareModePrivate
	account.ShareStatus = AccountShareStatusApproved
	account.ErrorMessage = ""
	account.ExternalPlacement = &AccountExternalPlacement{
		Target:  AccountExternalPlacementPrivate,
		State:   "active",
		Version: 1,
	}
	if input.Target == AccountExternalPlacementPublicPool {
		account.ShareMode = AccountShareModePublic
		account.ExternalPlacement.Target = AccountExternalPlacementPublicPool
	}
	if err := s.accountRepo.Update(context.Background(), account); err != nil {
		if input.Target == AccountExternalPlacementPublicPool {
			return nil, fmt.Errorf("update account public share status: %w", err)
		}
		return nil, fmt.Errorf("update account placement status: %w", err)
	}
	return &ConvertAccountExternalPlacementResult{
		AccountID: input.AccountID,
		Previous:  previous,
		Current:   account.ExternalPlacement,
	}, nil
}

func (r *recordingAgentIdentityWSInvalidator) InvalidateAgentIdentityWSConnections(accountID int64) {
	r.accountIDs = append(r.accountIDs, accountID)
}

func newOwnedAgentIdentityService(repo *ownedAgentIdentityRepoStub) (*AccountService, *recordingAgentIdentityWSInvalidator) {
	invalidator := &recordingAgentIdentityWSInvalidator{}
	placementRepo := &ownedAgentIdentityPlacementRepoStub{accountRepo: repo}
	return &AccountService{
		accountRepo: repo,
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: ownedAgentIdentityPlusPublicGroupID, Name: "PLUS共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelPlus},
				{ID: ownedAgentIdentityTeamPublicGroupID, Name: "TEAM共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelTeam},
			},
		},
		accountSharePolicyRepo: &ownedPublicSharePolicyRepoStub{
			policy: &AccountSharePolicy{ID: 1, OwnerShareRatio: 0.7, Enabled: true},
		},
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: ownedAgentIdentityPrivateGroupID, Name: "OpenAI private", Platform: PlatformOpenAI, Status: StatusActive},
		},
		accountShareModeRepo:       placementRepo,
		accountShareRoomRepo:       placementRepo,
		agentIdentityWSInvalidator: invalidator,
	}, invalidator
}

func TestConvertOwnedExternalPlacementRestoresDrainAfterHistoricalIdempotencyReplay(t *testing.T) {
	ownerUserID := int64(101)
	publicGroupID := ownedAgentIdentityPlusPublicGroupID
	repo := newOwnedAgentIdentityRepoStub()
	repo.accounts[1] = &Account{
		ID:           1,
		Name:         "Replay placement",
		OwnerUserID:  &ownerUserID,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials:  map[string]any{"access_token": "test-token"},
		Extra:        map[string]any{},
		ShareMode:    AccountShareModePublic,
		ShareStatus:  AccountShareStatusApproved,
		Concurrency:  3,
		Priority:     1,
		Status:       StatusActive,
		Schedulable:  true,
		GroupIDs:     []int64{ownedAgentIdentityPrivateGroupID, publicGroupID},
		ExternalPlacement: &AccountExternalPlacement{
			Target:        AccountExternalPlacementPublicPool,
			PublicGroupID: &publicGroupID,
			State:         "active",
			Version:       2,
		},
	}
	service, _ := newOwnedAgentIdentityService(repo)
	service.concurrencyService = &ConcurrencyService{}
	placementRepo := service.accountShareRoomRepo.(*ownedAgentIdentityPlacementRepoStub)
	placementRepo.beginDrain = true
	placementRepo.conversionResult = &ConvertAccountExternalPlacementResult{
		AccountID: 1,
		Previous: &AccountExternalPlacement{
			Target:  AccountExternalPlacementPublicPool,
			State:   "active",
			Version: 1,
		},
		Current: &AccountExternalPlacement{
			Target:  AccountExternalPlacementPrivate,
			State:   "active",
			Version: 1,
		},
	}

	result, err := service.ConvertOwnedExternalPlacement(
		context.Background(),
		ownerUserID,
		1,
		ConvertAccountExternalPlacementInput{
			Target:         AccountExternalPlacementPrivate,
			IdempotencyKey: "historical-private-request",
		},
	)

	require.NoError(t, err)
	require.Equal(t, AccountExternalPlacementPrivate, result.Current.Target)
	require.Equal(t, 1, placementRepo.restoreDrainCalls)
	require.Equal(t, AccountExternalPlacementPublicPool, repo.accounts[1].ExternalPlacement.Target)
	require.Equal(t, "active", repo.accounts[1].ExternalPlacement.State)
}

func ownedAgentIdentityImportRequest(t *testing.T, teamID, userID, runtimeID, planType string) CreateAccountRequest {
	t.Helper()
	expiresAt := time.Now().Add(24 * time.Hour)
	proxyID := int64(77)
	return CreateAccountRequest{
		Name:        "new import name",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		ShareMode:   AccountShareModePublic,
		ProxyID:     &proxyID,
		Concurrency: 3,
		Priority:    5,
		ExpiresAt:   &expiresAt,
		Credentials: map[string]any{
			"auth_mode":          OpenAIAuthModeAgentIdentity,
			"agent_runtime_id":   runtimeID,
			"agent_private_key":  testAgentIdentityPrivateKey(t),
			"chatgpt_account_id": teamID,
			"chatgpt_user_id":    userID,
			"plan_type":          planType,
		},
	}
}

func TestAccountServiceImportOwnedAgentIdentityCreatesPrivateOwnedAccount(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	req := ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "team")

	result, err := svc.ImportOwnedWithResult(context.Background(), 101, req)

	require.NoError(t, err)
	require.False(t, result.Updated)
	require.NotNil(t, result.Account.OwnerUserID)
	require.EqualValues(t, 101, *result.Account.OwnerUserID)
	require.Equal(t, AccountShareModePrivate, result.Account.ShareMode)
	require.Equal(t, AccountShareStatusApproved, result.Account.ShareStatus)
	require.Equal(t, AccountLevelTeam, result.Account.AccountLevel)
	require.Nil(t, result.Account.ProxyID)
	require.Nil(t, result.Account.ExpiresAt)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, result.Account.GroupIDs)
	require.Empty(t, invalidator.accountIDs)
}

func TestAccountServiceImportOwnedAgentIdentityUpdatesSameTeamKeepsPrivateAccountPrivate(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	stored := repo.accounts[created.Account.ID]
	stored.Name = "preserved local name"
	stored.Concurrency = 11
	stored.Priority = 37
	stored.Credentials["task_id"] = "task-old"
	stored.Extra["local_setting"] = "keep"
	expiresAt := time.Now().Add(time.Hour)
	stored.ExpiresAt = &expiresAt

	updated, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-new", "plus"))

	require.NoError(t, err)
	require.True(t, updated.Updated)
	require.Len(t, repo.accounts, 1)
	require.Equal(t, "preserved local name", updated.Account.Name)
	require.Equal(t, 11, updated.Account.Concurrency)
	require.Equal(t, 37, updated.Account.Priority)
	require.Equal(t, "runtime-new", updated.Account.GetCredential("agent_runtime_id"))
	require.Empty(t, updated.Account.GetCredential("task_id"), "runtime rotation without a new task must clear the stale task")
	require.Equal(t, "keep", updated.Account.Extra["local_setting"])
	require.Equal(t, AccountShareModePrivate, updated.Account.ShareMode)
	require.Equal(t, AccountShareStatusApproved, updated.Account.ShareStatus)
	require.Equal(t, AccountLevelPlus, updated.Account.AccountLevel)
	require.Nil(t, updated.Account.ExpiresAt)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, updated.Account.GroupIDs)
	require.Equal(t, []int64{created.Account.ID}, invalidator.accountIDs)
}

func TestAccountServiceImportOwnedAgentIdentityRevalidatesChangedPublicAccountWithoutMakingItPrivate(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	pending, err := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	require.Equal(t, AccountShareModePublic, pending.ShareMode)
	require.Equal(t, AccountShareStatusPending, pending.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, pending.GroupIDs)

	approved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	require.Equal(t, AccountShareModePublic, approved.ShareMode)
	require.Equal(t, AccountShareStatusApproved, approved.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, approved.GroupIDs)

	updated, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-new", "plus"))

	require.NoError(t, err)
	require.True(t, updated.Updated)
	require.Equal(t, AccountShareModePublic, updated.Account.ShareMode, "re-importing an already public account must not silently make it private")
	require.Equal(t, AccountShareStatusPending, updated.Account.ShareStatus, "changed authentication material must be revalidated before returning to the public pool")
	require.Equal(t, AccountLevelPlus, updated.Account.AccountLevel)
	require.Equal(t, "runtime-new", updated.Account.GetCredential("agent_runtime_id"))
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, updated.Account.GroupIDs, "a public account awaiting revalidation must only remain in its owner's private group")
	require.Equal(t, []int64{created.Account.ID}, invalidator.accountIDs)

	reapproved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	require.Equal(t, AccountShareModePublic, reapproved.ShareMode)
	require.Equal(t, AccountShareStatusApproved, reapproved.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityPlusPublicGroupID}, reapproved.GroupIDs)
}

func TestAccountServiceImportOwnedAgentIdentityDropsHistoricalNonAllowlistedCredentials(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	req := ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "team")
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, req)
	require.NoError(t, err)

	stored := repo.accounts[created.Account.ID]
	stored.Credentials["task_id"] = "task-existing"
	stored.Credentials["accessToken"] = "legacy-access-token"
	stored.Credentials["metadata"] = map[string]any{"id_token": "legacy-nested-token"}
	stored.Credentials["base_url"] = "https://legacy.invalid"

	updated, err := svc.ImportOwnedWithResult(
		context.Background(),
		101,
		ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "plus"),
	)
	require.NoError(t, err)
	require.True(t, updated.Updated)
	require.Equal(t, "task-existing", updated.Account.GetCredential("task_id"))
	require.Equal(t, "plus", updated.Account.GetCredential("plan_type"))
	for _, field := range []string{"accessToken", "metadata", "base_url"} {
		require.NotContains(t, updated.Account.Credentials, field)
	}
	_, hasOAuthToken := findOAuthTokenCredentialContent(updated.Account.Credentials)
	require.False(t, hasOAuthToken)
	safetyCredentials := mergeAccountMap(updated.Account.Credentials, nil)
	removeImportMapField(safetyCredentials, "auth_mode")
	_, hasUnsafeField := findDisallowedOwnedAgentIdentityField(safetyCredentials)
	require.False(t, hasUnsafeField)
}

func TestAccountServiceImportOwnedAgentIdentityIsolatesOwnerAndTeam(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)

	for _, test := range []struct {
		ownerID int64
		teamID  string
	}{
		{ownerID: 101, teamID: "team-a"},
		{ownerID: 101, teamID: "team-b"},
		{ownerID: 202, teamID: "team-a"},
	} {
		result, err := svc.ImportOwnedWithResult(
			context.Background(),
			test.ownerID,
			ownedAgentIdentityImportRequest(t, test.teamID, "same-member", "same-runtime", "team"),
		)
		require.NoError(t, err)
		require.False(t, result.Updated)
	}

	require.Len(t, repo.accounts, 3)
}

func TestAccountServiceImportOwnedAgentIdentityConvergesAfterUniqueConflict(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	repo.conflictOnNextCreate = true
	svc, invalidator := newOwnedAgentIdentityService(repo)

	result, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-race", "member-a", "runtime-race", "team"))

	require.NoError(t, err)
	require.True(t, result.Updated)
	require.Len(t, repo.accounts, 1)
	require.Equal(t, []int64{result.Account.ID}, invalidator.accountIDs)
}

func TestValidateOwnedAgentIdentitySourceRejectsUnsafeOrMalformedCredentials(t *testing.T) {
	valid := ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "team").Credentials
	overlongIdentifier := strings.Repeat("x", agentIdentityIdentifierMaxBytes+1)

	tests := []struct {
		name        string
		platform    string
		credentials map[string]any
		extra       map[string]any
	}{
		{name: "wrong platform", platform: PlatformAnthropic, credentials: mergeAccountMap(valid, nil)},
		{name: "missing Team", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"chatgpt_account_id": ""})},
		{name: "bad private key", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"agent_private_key": "not-a-key"})},
		{name: "mixed access token", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"access_token": "oauth-token"})},
		{name: "runtime control character", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"agent_runtime_id": "runtime\x00bad"})},
		{name: "Team control character", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"chatgpt_account_id": "team\nbad"})},
		{name: "task id too long", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"task_id": overlongIdentifier})},
		{name: "user id too long", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"chatgpt_user_id": overlongIdentifier})},
		{name: "custom URL", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, map[string]any{"base_url": "https://evil.example"})},
		{name: "unsafe extra", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, nil), extra: map[string]any{"proxy_url": "https://evil.example"}},
		{name: "nested OAuth token in extra", platform: PlatformOpenAI, credentials: mergeAccountMap(valid, nil), extra: map[string]any{"metadata": []any{map[string]any{"idToken": "must-reject"}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOwnedAccountSourceForPlatform(test.platform, AccountTypeOAuth, test.credentials, test.extra)
			require.Error(t, err)
		})
	}
}

func TestAccountServiceOwnedAgentIdentityRejectsNestedOAuthTokensBeforeWrite(t *testing.T) {
	t.Run("create nested object", func(t *testing.T) {
		repo := newOwnedAgentIdentityRepoStub()
		svc, _ := newOwnedAgentIdentityService(repo)
		req := ownedAgentIdentityImportRequest(t, "team-create", "member-create", "runtime-create", "team")
		req.Credentials["metadata"] = map[string]any{
			" ACCESS_TOKEN ": "must-reject",
		}

		account, err := svc.CreateOwned(context.Background(), 101, req)

		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
		require.Nil(t, account, "a rejected credential payload must not be returned to the response DTO")
		require.Zero(t, repo.createCount)
		require.Zero(t, repo.updateCount)
		require.Empty(t, repo.accounts)
	})

	t.Run("import nested array", func(t *testing.T) {
		repo := newOwnedAgentIdentityRepoStub()
		svc, _ := newOwnedAgentIdentityService(repo)
		req := ownedAgentIdentityImportRequest(t, "team-import", "member-import", "runtime-import", "team")
		req.Credentials["metadata"] = []any{
			map[string]any{"RefreshToken": "must-reject"},
		}

		result, err := svc.ImportOwnedWithResult(context.Background(), 101, req)

		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
		require.Nil(t, result, "a rejected credential payload must not be returned to the response DTO")
		require.Zero(t, repo.createCount)
		require.Zero(t, repo.updateCount)
		require.Empty(t, repo.accounts)
	})

	t.Run("update nested array object", func(t *testing.T) {
		repo := newOwnedAgentIdentityRepoStub()
		svc, _ := newOwnedAgentIdentityService(repo)
		created, err := svc.ImportOwnedWithResult(
			context.Background(),
			101,
			ownedAgentIdentityImportRequest(t, "team-update", "member-update", "runtime-update", "team"),
		)
		require.NoError(t, err)
		require.NotNil(t, created)

		credentials := mergeAccountMap(created.Account.Credentials, map[string]any{
			"metadata": []any{
				map[string]any{" ID_TOKEN ": "must-reject"},
			},
		})
		account, err := svc.UpdateOwned(
			context.Background(),
			101,
			created.Account.ID,
			UpdateAccountRequest{Credentials: &credentials},
		)

		require.ErrorIs(t, err, ErrOwnedAccountCredentialsNotAllowed)
		require.Nil(t, account, "a rejected credential payload must not be returned to the response DTO")
		require.Zero(t, repo.updateCount)
		require.NotContains(t, repo.accounts[created.Account.ID].Credentials, "metadata")
	})
}

func TestAccountServiceUpdateOwnedAgentIdentityInvalidatesWSWhenAuthMaterialChanges(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	credentials := map[string]any{
		"auth_mode":          OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":   "runtime-new",
		"task_id":            "task-new",
		"chatgpt_account_id": "team-a",
		"chatgpt_user_id":    "member-a",
		"plan_type":          "team",
	}
	updated, err := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{Credentials: &credentials})

	require.NoError(t, err)
	require.Equal(t, "runtime-new", updated.GetCredential("agent_runtime_id"))
	require.Equal(t, "task-new", updated.GetCredential("task_id"))
	require.Equal(t, []int64{created.Account.ID}, invalidator.accountIDs)
}

func TestAccountServiceUpdateOwnedPublicAgentIdentityFailsClosedWhenPrivateGroupBindingFails(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	approved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, approved.GroupIDs)

	repo.updateCount = 0
	invalidator.accountIDs = nil
	repo.bindGroupsErr = errors.New("injected group binding failure")
	credentials := mergeAccountMap(approved.Credentials, map[string]any{
		"agent_runtime_id": "runtime-new",
		"task_id":          "task-new",
	})

	updated, err := svc.UpdateOwned(
		context.Background(),
		101,
		created.Account.ID,
		UpdateAccountRequest{Credentials: &credentials},
	)

	require.ErrorContains(t, err, "bind groups")
	require.Nil(t, updated)
	require.Equal(t, 0, repo.updateCount)
	require.Empty(t, invalidator.accountIDs)
	stored := repo.accounts[created.Account.ID]
	require.Equal(t, "runtime-old", stored.GetCredential("agent_runtime_id"))
	require.Equal(t, AccountShareModePublic, stored.ShareMode)
	require.Equal(t, AccountShareStatusApproved, stored.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, stored.GroupIDs)
	require.True(t, stored.IsVisibleToConsumer(202), "the failed conversion must leave the previously approved placement unchanged")
}

func TestAccountServiceUpdateOwnedPublicAgentIdentityRevocationFailsClosedWhenGroupBindingFails(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	approved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, approved.GroupIDs)

	repo.updateCount = 0
	repo.bindGroupsErr = errors.New("injected group binding failure")
	privateMode := AccountShareModePrivate
	updated, err := svc.UpdateOwned(
		context.Background(),
		101,
		created.Account.ID,
		UpdateAccountRequest{ShareMode: &privateMode},
	)

	require.ErrorContains(t, err, "bind groups")
	require.Nil(t, updated)
	require.Equal(t, 1, repo.updateCount)
	require.Equal(t, []int64{created.Account.ID}, invalidator.accountIDs)
	stored := repo.accounts[created.Account.ID]
	require.Equal(t, AccountShareModePrivate, stored.ShareMode)
	require.Equal(t, AccountShareStatusApproved, stored.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, stored.GroupIDs)
	require.False(t, stored.IsVisibleToConsumer(202), "private status must block stale public-group membership")
}

func TestAccountServiceUpdateOwnedPublicAgentIdentityToPrivateInvalidatesWS(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	_, err = svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	invalidator.accountIDs = nil

	privateMode := AccountShareModePrivate
	updated, err := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &privateMode})

	require.NoError(t, err)
	require.Equal(t, AccountShareModePrivate, updated.ShareMode)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, updated.GroupIDs)
	require.Equal(t, []int64{created.Account.ID}, invalidator.accountIDs)
}

func TestAccountServiceAutoRepairOwnedAgentIdentitySuspensionInvalidatesWS(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, invalidator := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	_, err = svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	groupRepo := svc.groupRepo.(*ownedPublicShareGroupRepoStub)
	groupRepo.groups = append(groupRepo.groups, Group{ID: 9100, Name: "FREE共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelFree})
	account := repo.accounts[created.Account.ID]
	now := time.Now().UTC()
	account.Extra = mergeAccountMap(account.Extra, map[string]any{
		"quota_weekly_limit":    50.0,
		"codex_7d_used_percent": 100.0,
		"codex_7d_reset_at":     now.Add(24 * time.Hour).Format(time.RFC3339),
	})
	invalidator.accountIDs = nil

	updated, repaired, err := svc.AutoRepairSuspectedOpenAIFreeAccount(context.Background(), created.Account.ID, 60, "quota proof")

	require.NoError(t, err)
	require.True(t, repaired)
	require.Equal(t, AccountShareStatusSuspended, updated.ShareStatus)
	require.Equal(t, []int64{created.Account.ID}, invalidator.accountIDs)
}

func TestAccountServiceUpdateOwnedAgentIdentityFailsBeforeWriteWithoutWSInvalidator(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-old", "team"))
	require.NoError(t, err)
	svc.agentIdentityWSInvalidator = nil

	credentials := map[string]any{
		"auth_mode":          OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":   "runtime-new",
		"chatgpt_account_id": "team-a",
		"chatgpt_user_id":    "member-a",
	}
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{Credentials: &credentials})

	require.ErrorIs(t, err, ErrOwnedAgentIdentityWSInvalidatorUnavailable)
	require.Zero(t, repo.updateCount)
	require.Equal(t, "runtime-old", repo.accounts[created.Account.ID].GetCredential("agent_runtime_id"))
}

func TestOwnedAgentIdentityCanEnterPublicShareAfterApproval(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "team"))
	require.NoError(t, err)
	require.Equal(t, AccountShareModePrivate, created.Account.ShareMode, "Agent Identity imports must default to private")
	require.Equal(t, AccountShareStatusApproved, created.Account.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, created.Account.GroupIDs)

	publicMode := AccountShareModePublic
	pending, err := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	require.Equal(t, AccountShareModePublic, pending.ShareMode)
	require.Equal(t, AccountShareStatusPending, pending.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, pending.GroupIDs, "an unverified account must not enter the public pool")

	approved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	require.Equal(t, AccountShareModePublic, approved.ShareMode)
	require.Equal(t, AccountShareStatusApproved, approved.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, approved.GroupIDs)
}

func TestAccountServiceApproveOwnedPublicShareKeepsPendingWhenGroupBindingFails(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	repo.updateCount = 0
	repo.bindGroupsErr = errors.New("injected group binding failure")

	approved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)

	require.Nil(t, approved)
	require.ErrorContains(t, err, "bind public account groups")
	require.Zero(t, repo.updateCount, "approval must not be persisted before group binding succeeds")
	stored := repo.accounts[created.Account.ID]
	require.Equal(t, AccountShareModePublic, stored.ShareMode)
	require.Equal(t, AccountShareStatusPending, stored.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, stored.GroupIDs)
	require.False(t, stored.IsVisibleToConsumer(202))
}

func TestAccountServiceApproveOwnedPublicShareRetriesAfterStatusUpdateFailure(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-a", "member-a", "runtime-a", "team"))
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	repo.updateCount = 0
	repo.updateErr = errors.New("injected status update failure")

	approved, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)

	require.Nil(t, approved)
	require.ErrorContains(t, err, "update account public share status")
	require.Equal(t, 1, repo.updateCount)
	stored := repo.accounts[created.Account.ID]
	require.Equal(t, AccountShareModePublic, stored.ShareMode)
	require.Equal(t, AccountShareStatusPending, stored.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, stored.GroupIDs)
	require.False(t, stored.IsVisibleToConsumer(202), "pending status must block a partially bound account")

	repo.updateErr = nil
	retried, err := svc.ApproveOwnedPublicShare(context.Background(), 101, created.Account.ID)
	require.NoError(t, err)
	require.Equal(t, AccountShareStatusApproved, retried.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID, ownedAgentIdentityTeamPublicGroupID}, retried.GroupIDs)
	require.True(t, retried.IsVisibleToConsumer(202))
}

// 公共号池账号切回「仅本人」：即使账号正被公共调度（在途请求 > 0）也应成功。
// 修复前 ensureOwnedAccountExternalPlacementIdle 会以 ACCOUNT_EXTERNAL_PLACEMENT_BUSY
// 拒绝——公共号池账号被公共流量占用时 CurrentConcurrency 几乎恒 > 0，
// 用户永远切不回仅本人。切回私有无需排空：repo 层在同一事务原子改 placement 与分组。
func TestConvertOwnedExternalPlacementPublicPoolToPrivateSkipsIdleGuard(t *testing.T) {
	ownerUserID := int64(101)
	publicGroupID := ownedAgentIdentityPlusPublicGroupID
	repo := newOwnedAgentIdentityRepoStub()
	repo.accounts[1] = &Account{
		ID:           1,
		Name:         "Shared pool account",
		OwnerUserID:  &ownerUserID,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials:  map[string]any{"access_token": "test-token"},
		Extra:        map[string]any{},
		ShareMode:    AccountShareModePublic,
		ShareStatus:  AccountShareStatusApproved,
		Concurrency:  3,
		Priority:     1,
		Status:       StatusActive,
		Schedulable:  true,
		GroupIDs:     []int64{ownedAgentIdentityPrivateGroupID, publicGroupID},
		ExternalPlacement: &AccountExternalPlacement{
			Target:        AccountExternalPlacementPublicPool,
			PublicGroupID: &publicGroupID,
			State:         "active",
			Version:       2,
		},
	}
	svc, _ := newOwnedAgentIdentityService(repo)
	// 模拟账号正被公共调度：Redis 槽位里有在途请求。
	svc.concurrencyService = &ConcurrencyService{cache: &accountShareRuntimeLoadCacheStub{
		loads: map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 2, WaitingCount: 0},
		},
	}}
	placementRepo := svc.accountShareRoomRepo.(*ownedAgentIdentityPlacementRepoStub)
	placementRepo.beginDrain = true

	result, err := svc.ConvertOwnedExternalPlacement(
		context.Background(),
		ownerUserID,
		1,
		ConvertAccountExternalPlacementInput{
			Target:         AccountExternalPlacementPrivate,
			IdempotencyKey: "public-to-private-with-inflight",
		},
	)

	require.NoError(t, err)
	require.Equal(t, AccountExternalPlacementPrivate, result.Current.Target)
	require.Equal(t, 1, placementRepo.restoreDrainCalls)
	stored := repo.accounts[1]
	require.Equal(t, AccountShareModePrivate, stored.ShareMode)
	require.Equal(t, AccountExternalPlacementPrivate, stored.ExternalPlacement.Target)
	require.Equal(t, "active", stored.ExternalPlacement.State)
}

// 非 private 方向仍保留排空守卫。真实私有账号没有 placement 行（生产里
// account_external_placements 的 CHECK 只允许 public_pool/room，private 时行会被
// DELETE），BeginExternalPlacementDrain 对无行账号直接短路返回 drained=false，
// 首投放天然不走在途检查——这条是改动前就有的行为，本测试刻意不背书。
// 这里覆盖的是「已有 placement 行的账号」在非 private 方向上仍会被在途请求拒绝：
// 房间账号转投公共号池（有 room 行、有在途请求）必须被 ErrAccountExternalPlacementBusy
// 拦下，防止把还在跑请求的账号挪进公共调度。
func TestConvertOwnedExternalPlacementRoomToPublicPoolStillRejectsInflight(t *testing.T) {
	ownerUserID := int64(101)
	repo := newOwnedAgentIdentityRepoStub()
	repo.accounts[1] = &Account{
		ID:           1,
		Name:         "Room account moving to public pool",
		OwnerUserID:  &ownerUserID,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials:  map[string]any{"access_token": "test-token"},
		Extra:        map[string]any{},
		ShareMode:    AccountShareModePrivate,
		ShareStatus:  AccountShareStatusApproved,
		Concurrency:  3,
		Priority:     1,
		Status:       StatusActive,
		Schedulable:  true,
		GroupIDs:     []int64{ownedAgentIdentityPrivateGroupID},
		ExternalPlacement: &AccountExternalPlacement{
			Target: AccountExternalPlacementRoom,
			State:  "active",
			Version: 1,
		},
	}
	svc, _ := newOwnedAgentIdentityService(repo)
	svc.concurrencyService = &ConcurrencyService{cache: &accountShareRuntimeLoadCacheStub{
		loads: map[int64]*AccountLoadInfo{
			1: {AccountID: 1, CurrentConcurrency: 1, WaitingCount: 0},
		},
	}}
	placementRepo := svc.accountShareRoomRepo.(*ownedAgentIdentityPlacementRepoStub)
	placementRepo.beginDrain = true

	_, err := svc.ConvertOwnedExternalPlacement(
		context.Background(),
		ownerUserID,
		1,
		ConvertAccountExternalPlacementInput{
			Target:         AccountExternalPlacementPublicPool,
			IdempotencyKey: "room-to-public-with-inflight",
		},
	)

	require.ErrorIs(t, err, ErrAccountExternalPlacementBusy)
	// 被拒后 drain 必须恢复，账号 placement 保持 active，不能卡在 draining。
	require.Equal(t, 1, placementRepo.restoreDrainCalls)
	require.Equal(t, "active", repo.accounts[1].ExternalPlacement.State)
}
