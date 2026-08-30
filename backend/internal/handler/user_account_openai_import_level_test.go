//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type ownedOpenAIImportCaptureRepo struct {
	service.AccountRepository
	created *service.Account
}

func (r *ownedOpenAIImportCaptureRepo) Create(_ context.Context, account *service.Account) error {
	if account.ID <= 0 {
		account.ID = 1
	}
	clone := *account
	clone.Credentials = make(map[string]any, len(account.Credentials))
	for key, value := range account.Credentials {
		clone.Credentials[key] = value
	}
	clone.Extra = make(map[string]any, len(account.Extra))
	for key, value := range account.Extra {
		clone.Extra[key] = value
	}
	r.created = &clone
	return nil
}

func (r *ownedOpenAIImportCaptureRepo) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	if r.created != nil && r.created.ID == accountID {
		r.created.GroupIDs = append([]int64(nil), groupIDs...)
	}
	return nil
}

func (r *ownedOpenAIImportCaptureRepo) ListOwnedWithFilters(
	context.Context,
	int64,
	pagination.PaginationParams,
	string,
	string,
	string,
	string,
	int64,
	int64,
	string,
) ([]service.Account, *pagination.PaginationResult, error) {
	return []service.Account{}, &pagination.PaginationResult{}, nil
}

type ownedOpenAIImportPrivateGroupProvisioner struct{}

func (ownedOpenAIImportPrivateGroupProvisioner) ProvisionUserPrivateGroups(context.Context, int64) error {
	return nil
}

func (ownedOpenAIImportPrivateGroupProvisioner) GetActiveUserPrivateGroup(
	context.Context,
	int64,
	string,
) (*service.Group, error) {
	return &service.Group{ID: 91, Status: service.StatusActive}, nil
}

type ownedOpenAIImportPricedModelCatalog struct{}

func (ownedOpenAIImportPricedModelCatalog) ListPricedModelIDs(context.Context, []string) ([]string, error) {
	return []string{"gpt-5"}, nil
}

type ownedOpenAIImportRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper ownedOpenAIImportRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func TestResolveOwnedOpenAIImportLevel(t *testing.T) {
	configs := service.DefaultOpenAIAccountLevelConfigs()

	tests := []struct {
		name            string
		probePlanType   string
		probeFailed     bool
		targetLevel     string
		wantLevel       string
		wantErrContains string
	}{
		{
			name:          "plus matches plus",
			probePlanType: "plus",
			targetLevel:   service.AccountLevelPlus,
			wantLevel:     service.AccountLevelPlus,
		},
		{
			name:          "free auto detects plus",
			probePlanType: "plus",
			targetLevel:   service.AccountLevelFree,
			wantLevel:     service.AccountLevelPlus,
		},
		{
			name:          "free auto detects pro",
			probePlanType: "pro",
			targetLevel:   service.AccountLevelFree,
			wantLevel:     service.AccountLevelPro,
		},
		{
			name:          "free auto detects team",
			probePlanType: "team",
			targetLevel:   service.AccountLevelFree,
			wantLevel:     service.AccountLevelTeam,
		},
		{
			name:          "free auto detects k12",
			probePlanType: "chatgpt-k12",
			targetLevel:   service.AccountLevelFree,
			wantLevel:     service.AccountLevelK12,
		},
		{
			name:            "plus cannot impersonate pro",
			probePlanType:   "plus",
			targetLevel:     service.AccountLevelPro,
			wantErrContains: "不符",
		},
		{
			name:            "pro cannot downgrade to plus",
			probePlanType:   "pro",
			targetLevel:     service.AccountLevelPlus,
			wantErrContains: "不符",
		},
		{
			name:            "unrecognized plan_type is rejected",
			probePlanType:   "some-new-plan",
			targetLevel:     service.AccountLevelPro,
			wantErrContains: "无法识别",
		},
		{
			name:          "free safely falls back for unrecognized plan_type",
			probePlanType: "some-new-plan",
			targetLevel:   service.AccountLevelFree,
			wantLevel:     service.AccountLevelFree,
		},
		{
			name:        "probe failed allows free",
			probeFailed: true,
			targetLevel: service.AccountLevelFree,
			wantLevel:   service.AccountLevelFree,
		},
		{
			name:            "probe failed rejects unknown",
			probeFailed:     true,
			targetLevel:     service.AccountLevelUnknown,
			wantErrContains: "无法验证",
		},
		{
			name:            "probe failed rejects pro",
			probeFailed:     true,
			targetLevel:     service.AccountLevelPro,
			wantErrContains: "无法验证",
		},
		{
			name:            "probe failed rejects plus",
			probeFailed:     true,
			targetLevel:     service.AccountLevelPlus,
			wantErrContains: "无法验证",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			level, err := resolveOwnedOpenAIImportLevel(test.probePlanType, test.probeFailed, test.targetLevel, configs)
			if test.wantErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.wantErrContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantLevel, level)
		})
	}
}

func TestReplaceOpenAIImportPlanDeclarations(t *testing.T) {
	req := service.CreateAccountRequest{
		Credentials: map[string]any{
			"access_token":      "trusted-access-token",
			"plan_type":         "team",
			"chatgpt_plan_type": "pro",
			"subscription_plan": "plus",
		},
		Extra: map[string]any{
			"plan_type":         "pro",
			"chatgpt_plan_type": "team",
			"subscription_plan": "plus",
			"local_setting":     "keep",
		},
	}

	replaceOpenAIImportPlanDeclarations(&req, "")

	for _, key := range openAIImportPlanDeclarationKeys {
		require.NotContains(t, req.Credentials, key)
		require.NotContains(t, req.Extra, key)
	}
	require.Equal(t, "trusted-access-token", req.Credentials["access_token"])
	require.Equal(t, "keep", req.Extra["local_setting"])

	replaceOpenAIImportPlanDeclarations(&req, "  k12  ")
	require.Equal(t, "k12", req.Credentials["plan_type"])
	for _, key := range []string{"chatgpt_plan_type", "subscription_plan"} {
		require.NotContains(t, req.Credentials, key)
		require.NotContains(t, req.Extra, key)
	}
}

func TestCreateOwnedOpenAIOAuthImportPassesDetectedK12LevelAndEnrichedCredentialsToService(t *testing.T) {
	repo := &ownedOpenAIImportCaptureRepo{}
	accountService := service.NewAccountService(repo, nil, nil, nil, nil)
	accountService.SetUserPrivateGroupProvisioner(ownedOpenAIImportPrivateGroupProvisioner{})
	accountService.SetPricedModelCatalog(ownedOpenAIImportPricedModelCatalog{})

	var probedAuthorization string
	openAIOAuthService := service.NewOpenAIOAuthService(nil, nil)
	openAIOAuthService.SetPrivacyClientFactory(func(string) (*req.Client, error) {
		client := req.C()
		client.GetClient().Transport = ownedOpenAIImportRoundTripper(func(request *http.Request) (*http.Response, error) {
			probedAuthorization = request.Header.Get("Authorization")
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     header,
				Body: io.NopCloser(strings.NewReader(
					`{"accounts":{"school-workspace":{"account":{"plan_type":"chatgpt-k12","is_default":true}}}}`,
				)),
				Request: request,
			}, nil
		})
		return client, nil
	})
	t.Cleanup(openAIOAuthService.Stop)

	handler := NewUserAccountHandler(accountService, nil, nil, nil, nil, nil, openAIOAuthService, nil, nil)
	source := service.AccountCredentialImportSource{
		Kind:     service.AccountCredentialImportKindOAuthCredentials,
		Platform: service.PlatformOpenAI,
		Name:     "K12 teacher",
		Credentials: map[string]any{
			"access_token":      "access-token",
			"id_token":          testUserK12CredentialImportIDToken(t),
			"model_mapping":     map[string]any{"gpt-5": "gpt-5"},
			"plan_type":         "team",
			"chatgpt_plan_type": "pro",
			"subscription_plan": "plus",
		},
		Extra: map[string]any{
			"plan_type":         "plus",
			"chatgpt_plan_type": "team",
			"subscription_plan": "pro",
			"local_setting":     "keep",
		},
	}
	expiresAt := time.Now().Add(time.Hour).Unix()
	defaults := importUserAccountCredentialsRequest{
		Platform:     service.PlatformOpenAI,
		AccountLevel: service.AccountLevelFree,
		ShareMode:    service.AccountShareModePrivate,
		Concurrency:  1,
		Priority:     1,
		ExpiresAt:    &expiresAt,
	}

	outcome, err := handler.createOwnedAccountFromCredentialImportSource(
		context.Background(),
		42,
		source,
		defaults,
		1,
	)

	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.NotNil(t, repo.created)
	require.Equal(t, "Bearer access-token", probedAuthorization)
	require.Equal(t, service.AccountLevelK12, repo.created.AccountLevel)
	require.Equal(t, "access-token", repo.created.Credentials["access_token"])
	require.Equal(t, "teacher-a", repo.created.Credentials["chatgpt_user_id"])
	require.Equal(t, "school-workspace", repo.created.Credentials["chatgpt_account_id"])
	require.Equal(t, "chatgpt-k12", repo.created.Credentials["plan_type"])
	require.NotContains(t, repo.created.Credentials, "chatgpt_plan_type")
	require.NotContains(t, repo.created.Credentials, "subscription_plan")
	require.NotContains(t, repo.created.Extra, "plan_type")
	require.NotContains(t, repo.created.Extra, "chatgpt_plan_type")
	require.NotContains(t, repo.created.Extra, "subscription_plan")
	require.Equal(t, "keep", repo.created.Extra["local_setting"])
}

func TestValidateResolvedOpenAIImportProxy(t *testing.T) {
	configs := service.DefaultOpenAIAccountLevelConfigs()
	proxyID := int64(7)

	require.Error(t, validateResolvedOpenAIImportProxy(service.AccountLevelPro, nil, configs, false))
	require.NoError(t, validateResolvedOpenAIImportProxy(service.AccountLevelPro, &proxyID, configs, false))
	require.NoError(t, validateResolvedOpenAIImportProxy(service.AccountLevelPro, nil, configs, true))
	require.Error(t, validateResolvedOpenAIImportProxy(service.AccountLevelTeam, &proxyID, configs, false))
	require.NoError(t, validateResolvedOpenAIImportProxy(service.AccountLevelTeam, nil, configs, false))
}
