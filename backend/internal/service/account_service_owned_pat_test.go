package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validatedOwnedPATInfo(token, userID, accountID, planType string) *OpenAITokenInfo {
	return &OpenAITokenInfo{
		AccessToken:                  token,
		AuthMode:                     OpenAIAuthModePersonalAccessToken,
		Email:                        userID + "@example.com",
		ChatGPTUserID:                userID,
		ChatGPTAccountID:             accountID,
		PlanType:                     planType,
		ChatGPTAccountFedRAMP:        false,
		personalAccessTokenValidated: true,
	}
}

func ownedPATImportRequest(level string) CreateAccountRequest {
	return CreateAccountRequest{
		Name:         "Codex PAT import",
		Platform:     PlatformOpenAI,
		AccountLevel: level,
		Type:         AccountTypeOAuth,
		ShareMode:    AccountShareModePrivate,
		Concurrency:  3,
		Priority:     1,
		Credentials: map[string]any{
			"access_token":    "at-test-caller-must-not-win",
			"refresh_token":   "caller-must-not-survive",
			"chatgpt_user_id": "caller-user",
		},
		Extra: map[string]any{"email": "validated@example.com"},
	}
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenCreatesCanonicalAccount(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	req := ownedPATImportRequest(AccountLevelPlus)

	result, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		req,
		validatedOwnedPATInfo("at-test-created", "pat-user", "team-a", "plus"),
	)

	require.NoError(t, err)
	require.False(t, result.Updated)
	require.Equal(t, "at-test-created", result.Account.GetCredential("access_token"))
	require.Equal(t, "pat-user", result.Account.GetChatGPTUserID())
	require.Equal(t, OpenAIAuthModePersonalAccessToken, result.Account.GetCredential("auth_mode"))
	require.Equal(t, "personal_access_token", result.Account.GetCredential("openai_auth_mode"))
	require.Equal(t, "Bearer", result.Account.GetCredential("token_type"))
	require.NotContains(t, result.Account.Credentials, "refresh_token")
	require.Nil(t, result.Account.ExpiresAt)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, result.Account.GroupIDs)
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenRejectsUntrustedTokenInfo(t *testing.T) {
	tests := []struct {
		name string
		info *OpenAITokenInfo
	}{
		{name: "nil"},
		{name: "manually constructed", info: &OpenAITokenInfo{AccessToken: "at-test-forged", AuthMode: OpenAIAuthModePersonalAccessToken}},
		{name: "wrong auth mode", info: &OpenAITokenInfo{AccessToken: "at-test-oauth", AuthMode: "oauth", personalAccessTokenValidated: true}},
		{name: "wrong prefix", info: &OpenAITokenInfo{AccessToken: "oauth-token", AuthMode: OpenAIAuthModePersonalAccessToken, personalAccessTokenValidated: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newOwnedAgentIdentityRepoStub()
			svc, _ := newOwnedAgentIdentityService(repo)

			result, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
				context.Background(),
				101,
				ownedPATImportRequest(AccountLevelPlus),
				test.info,
			)

			require.Nil(t, result)
			require.ErrorIs(t, err, ErrOwnedPersonalAccessTokenValidationRequired)
			require.Zero(t, repo.createCount)
			require.Zero(t, repo.updateCount)
		})
	}
}

func TestAccountServiceCreateAndGenericImportRejectUnvalidatedPersonalAccessToken(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*AccountService, CreateAccountRequest) error
	}{
		{
			name: "create",
			run: func(svc *AccountService, req CreateAccountRequest) error {
				_, err := svc.CreateOwned(context.Background(), 101, req)
				return err
			},
		},
		{
			name: "generic import",
			run: func(svc *AccountService, req CreateAccountRequest) error {
				_, err := svc.ImportOwnedWithResult(context.Background(), 101, req)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newOwnedAgentIdentityRepoStub()
			svc, _ := newOwnedAgentIdentityService(repo)
			req := ownedPATImportRequest(AccountLevelPlus)
			req.Credentials = BuildOpenAIPersonalAccessTokenCredentials(
				validatedOwnedPATInfo("at-test-unvalidated", "pat-user", "team-a", "plus"),
			)

			err := test.run(svc, req)

			require.ErrorIs(t, err, ErrOwnedPersonalAccessTokenValidationRequired)
			require.Zero(t, repo.createCount)
			require.Zero(t, repo.updateCount)
		})
	}
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenUpdatesAndPreservesLocalSettings(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelTeam),
		validatedOwnedPATInfo("at-test-old", "pat-user", "team-a", "team"),
	)
	require.NoError(t, err)

	stored := repo.accounts[created.Account.ID]
	stored.Name = "preserved local name"
	stored.Concurrency = 11
	stored.Priority = 37
	stored.Credentials["model_mapping"] = map[string]any{"gpt-5": "gpt-5-custom"}
	stored.Credentials["refresh_token"] = "historical-refresh"
	stored.Credentials["id_token"] = "historical-id"
	stored.Credentials["expires_at"] = "2026-01-01T00:00:00Z"
	stored.Credentials["client_id"] = "historical-client"
	stored.Extra["local_setting"] = "keep"
	expiresAt := time.Now().Add(time.Hour)
	stored.ExpiresAt = &expiresAt

	updated, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-new", "pat-user", "team-a", "plus"),
	)

	require.NoError(t, err)
	require.True(t, updated.Updated)
	require.Len(t, repo.accounts, 1)
	require.Equal(t, "preserved local name", updated.Account.Name)
	require.Equal(t, 11, updated.Account.Concurrency)
	require.Equal(t, 37, updated.Account.Priority)
	require.Equal(t, "at-test-new", updated.Account.GetCredential("access_token"))
	require.Equal(t, AccountLevelPlus, updated.Account.AccountLevel)
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-custom"}, updated.Account.Credentials["model_mapping"])
	require.Equal(t, "keep", updated.Account.Extra["local_setting"])
	require.Nil(t, updated.Account.ExpiresAt)
	for _, key := range openAIPersonalAccessTokenOAuthCredentialKeys {
		require.NotContains(t, updated.Account.Credentials, key)
	}
}

func TestAccountServiceUpdateOwnedPersonalAccessTokenModelWhitelistOnly(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	svc.pricedModelCatalog = &ownedPricedModelCatalogStub{modelsByPlatform: map[string][]string{
		PlatformOpenAI: []string{"gpt-5.2", "gpt-5.4"},
	}}
	created, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-model-edit", "pat-user", "team-a", "plus"),
	)
	require.NoError(t, err)
	storedAccessToken := created.Account.GetCredential("access_token")

	modelMapping := map[string]any{"gpt-5.4": "gpt-5.4"}
	updated, err := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{
		Credentials: &map[string]any{"model_mapping": modelMapping},
	})

	require.NoError(t, err)
	require.Equal(t, modelMapping, updated.Credentials["model_mapping"])
	require.Equal(t, storedAccessToken, updated.GetCredential("access_token"))
	require.True(t, updated.IsOpenAIPersonalAccessToken())
}

func TestAccountServiceUpdateOwnedPersonalAccessTokenRejectsCredentialChangesWithModelWhitelist(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	svc.pricedModelCatalog = &ownedPricedModelCatalogStub{modelsByPlatform: map[string][]string{
		PlatformOpenAI: []string{"gpt-5.4"},
	}}
	created, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-model-edit-guard", "pat-user", "team-a", "plus"),
	)
	require.NoError(t, err)

	for _, test := range []struct {
		name        string
		credentials map[string]any
	}{
		{
			name: "identity field",
			credentials: map[string]any{
				"email":         "attacker@example.com",
				"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"},
			},
		},
		{
			name: "sensitive token",
			credentials: map[string]any{
				"access_token":  "at-forged-token",
				"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, updateErr := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{
				Credentials: &test.credentials,
			})

			require.ErrorIs(t, updateErr, ErrOwnedPersonalAccessTokenValidationRequired)
			require.Equal(t, "at-test-model-edit-guard", repo.accounts[created.Account.ID].GetCredential("access_token"))
		})
	}
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenIsolatesOwnersAndUsers(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)

	for _, test := range []struct {
		ownerID int64
		userID  string
	}{
		{ownerID: 101, userID: "member-a"},
		{ownerID: 101, userID: "member-b"},
		{ownerID: 202, userID: "member-a"},
	} {
		result, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
			context.Background(),
			test.ownerID,
			ownedPATImportRequest(AccountLevelTeam),
			validatedOwnedPATInfo("at-test-"+test.userID, test.userID, "shared-team", "team"),
		)
		require.NoError(t, err)
		require.False(t, result.Updated)
	}

	require.Len(t, repo.accounts, 3)
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenDoesNotDowngradeOAuth(t *testing.T) {
	ownerUserID := int64(101)
	repo := newOwnedAgentIdentityRepoStub()
	repo.accounts[1] = &Account{
		ID:           1,
		Name:         "refresh OAuth",
		OwnerUserID:  &ownerUserID,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		Credentials: map[string]any{
			"access_token":    "oauth-access",
			"refresh_token":   "oauth-refresh",
			"chatgpt_user_id": "pat-user",
			"plan_type":       "plus",
		},
	}
	svc, _ := newOwnedAgentIdentityService(repo)

	result, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		ownerUserID,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-conflict", "pat-user", "team-a", "plus"),
	)

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrOwnedAccountAlreadyExists)
	require.Equal(t, "oauth-refresh", repo.accounts[1].GetCredential("refresh_token"))
	require.False(t, repo.accounts[1].IsOpenAIPersonalAccessToken())
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenConvergesAfterUniqueConflict(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	repo.conflictOnNextCreate = true
	svc, _ := newOwnedAgentIdentityService(repo)

	result, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-race", "pat-user", "team-a", "plus"),
	)

	require.NoError(t, err)
	require.True(t, result.Updated)
	require.Len(t, repo.accounts, 1)
	require.Equal(t, "at-test-race", result.Account.GetCredential("access_token"))
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenRevalidatesPublicAccount(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelTeam),
		validatedOwnedPATInfo("at-test-public-old", "pat-user", "team-a", "team"),
	)
	require.NoError(t, err)

	publicMode := AccountShareModePublic
	pending, err := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{ShareMode: &publicMode})
	require.NoError(t, err)
	_, err = svc.ApproveOwnedPublicShare(context.Background(), 101, pending.ID)
	require.NoError(t, err)

	updated, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-public-new", "pat-user", "team-a", "plus"),
	)

	require.NoError(t, err)
	require.True(t, updated.Updated)
	require.Equal(t, AccountShareModePublic, updated.Account.ShareMode)
	require.Equal(t, AccountShareStatusPending, updated.Account.ShareStatus)
	require.Equal(t, []int64{ownedAgentIdentityPrivateGroupID}, updated.Account.GroupIDs)
}

func TestAccountServiceImportOwnedValidatedPersonalAccessTokenPropagatesLookupFailure(t *testing.T) {
	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	lookupErr := errors.New("lookup failed")
	svc.accountRepo = &ownedPATLookupErrorRepo{ownedAgentIdentityRepoStub: repo, err: lookupErr}

	result, err := svc.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		101,
		ownedPATImportRequest(AccountLevelPlus),
		validatedOwnedPATInfo("at-test-lookup", "pat-user", "team-a", "plus"),
	)

	require.Nil(t, result)
	require.ErrorIs(t, err, lookupErr)
}

type ownedPATLookupErrorRepo struct {
	*ownedAgentIdentityRepoStub
	err error
}

func (s *ownedPATLookupErrorRepo) GetOwnedOpenAIPersonalAccessTokenByChatGPTUserID(context.Context, int64, string) (*Account, error) {
	return nil, s.err
}
