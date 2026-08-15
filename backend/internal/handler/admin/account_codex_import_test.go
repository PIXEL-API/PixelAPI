package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// These tests intentionally describe the upstream Codex import contract. The
// existing /import-credentials profile has different raw-string semantics and
// must not be reused as the parser for this facade.
func TestParseCodexSessionImportEntriesSupportsUpstreamFormats(t *testing.T) {
	tokenJSON := buildCodexImportTestJWT(t, time.Now().Add(time.Hour), map[string]any{
		"email": "json@example.com",
	})
	req := CodexSessionImportRequest{
		Content: fmt.Sprintf("raw-access-1\n{\"accessToken\":%q}\n[%q,[%q]]", tokenJSON, "raw-access-2", "raw-access-3"),
		Contents: []string{
			"{\"access_token\":\"stream-access-1\"}{\"accessToken\":\"stream-access-2\"}",
		},
	}

	entries, err := parseCodexSessionImportEntries(req)
	require.NoError(t, err)
	require.Len(t, entries, 6)
	for i, entry := range entries {
		require.Equal(t, i+1, entry.Index, "content and contents must share one continuous index")
	}

	wants := []string{"raw-access-1", tokenJSON, "raw-access-2", "raw-access-3", "stream-access-1", "stream-access-2"}
	for i, want := range wants {
		item, normalizeErr := normalizeCodexImportEntry(entries[i])
		require.NoError(t, normalizeErr)
		require.Equal(t, want, item.Credentials["access_token"])
	}
	require.Equal(t, "json@example.com", mustNormalizeCodexImportEntry(t, entries[1]).Email)
}

func TestParseCodexSessionImportEntriesFallsBackToMixedLineMode(t *testing.T) {
	req := CodexSessionImportRequest{Content: "{\"accessToken\":\"json-line-token\"}\nraw-line-token"}

	entries, err := parseCodexSessionImportEntries(req)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "json-line-token", mustNormalizeCodexImportEntry(t, entries[0]).Credentials["access_token"])
	require.Equal(t, "raw-line-token", mustNormalizeCodexImportEntry(t, entries[1]).Credentials["access_token"])
}

func TestNormalizeCodexSessionJSONExtractsCredentialsAndIgnoresSessionToken(t *testing.T) {
	accessToken := buildCodexImportTestJWT(t, time.Now().Add(time.Hour), map[string]any{
		"email": "claim@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-from-claim",
			"chatgpt_user_id":    "user-from-claim",
			"chatgpt_plan_type":  "plus",
		},
	})
	raw := map[string]any{
		"user": map[string]any{
			"id":    "user-from-json",
			"email": "json@example.com",
		},
		"account": map[string]any{
			"id":       "acct-from-json",
			"planType": "free",
		},
		"accessToken":  accessToken,
		"sessionToken": "must-not-be-persisted",
		"expires":      "2026-08-05T13:40:42.836Z",
		"expiresAt":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
	}

	item, err := normalizeCodexImportEntry(codexImportEntry{Index: 1, Value: raw})
	require.NoError(t, err)
	require.Equal(t, accessToken, item.Credentials["access_token"])
	require.Equal(t, "json@example.com", item.Credentials["email"])
	require.Equal(t, "acct-from-json", item.Credentials["chatgpt_account_id"])
	require.Equal(t, "user-from-json", item.Credentials["chatgpt_user_id"])
	require.Equal(t, "free", item.Credentials["plan_type"])
	require.NotContains(t, item.Credentials, "session_token")
	require.NotContains(t, item.Credentials, "sessionToken")
	require.Equal(t, true, item.Extra["session_token_present"])
	require.Equal(t, "2026-08-05T13:40:42Z", item.Extra["session_expires_at"])
	require.NotEmpty(t, item.WarningTexts, "ignored sessionToken must be reported")
}

func TestNormalizeCodexSessionOrdinaryOAuthAllowsAuthModeMetadata(t *testing.T) {
	accessToken := buildCodexImportTestJWT(t, time.Now().Add(time.Hour), map[string]any{})

	item, err := normalizeCodexImportEntry(codexImportEntry{Index: 1, Value: map[string]any{
		"auth_mode":    "oauth",
		"access_token": accessToken,
	}})

	require.NoError(t, err)
	require.False(t, item.IsAgentIdentity)
	require.Equal(t, accessToken, item.Credentials["access_token"])
}

func TestParseCodexTimeValueSupportsRFC3339SecondsAndMilliseconds(t *testing.T) {
	want := time.Date(2026, time.August, 5, 13, 40, 42, 0, time.UTC)
	cases := []any{
		"2026-08-05T13:40:42Z",
		json.Number(fmt.Sprintf("%d", want.Unix())),
		json.Number(fmt.Sprintf("%d", want.UnixMilli())),
		fmt.Sprintf("%d", want.UnixMilli()),
	}
	for _, value := range cases {
		got, ok := parseCodexTimeValue(value)
		require.True(t, ok, "value=%v", value)
		require.Equal(t, want.Unix(), got.Unix(), "value=%v", value)
	}
}

func TestMergeCodexImportCredentialsPreservesRefreshFieldsForAccessOnlyUpdate(t *testing.T) {
	existing := map[string]any{
		"access_token":  "old-access-token",
		"refresh_token": "old-refresh-token",
		"client_id":     "old-client-id",
		"id_token":      "old-id-token",
		"model_mapping": map[string]any{"from": "existing"},
	}
	incoming := map[string]any{"access_token": "new-access-token"}

	merged := mergeCodexImportCredentials(existing, incoming, &codexImportAccount{AccessToken: "new-access-token"})

	require.Equal(t, "new-access-token", merged["access_token"])
	require.Equal(t, "old-refresh-token", merged["refresh_token"])
	require.Equal(t, "old-client-id", merged["client_id"])
	require.NotContains(t, merged, "id_token")
	require.Contains(t, merged, "model_mapping")
}

func TestCodexIdentityKeysProtectAccessOnlyAndTeamMemberBoundaries(t *testing.T) {
	accessOnly := buildCodexImportIdentityKeys("team-1", "user-1", "same@example.com", "access-1", "")
	require.Len(t, accessOnly, 1)
	require.True(t, strings.HasPrefix(accessOnly[0], "access:"))

	withRefresh := buildCodexImportIdentityKeys("team-1", "user-1", "same@example.com", "access-2", "refresh-2")
	require.Equal(t, "user:user-1", withRefresh[0])
	require.Equal(t, "account:team-1", withRefresh[len(withRefresh)-1])

	index := buildCodexAccountIndex([]service.Account{{
		ID: 10,
		Credentials: map[string]any{
			"chatgpt_account_id": "team-1",
			"chatgpt_user_id":    "user-1",
			"access_token":       "access-1",
			"refresh_token":      "refresh-1",
		},
	}})
	differentMember := buildCodexImportIdentityKeys("team-1", "user-2", "", "access-2", "refresh-2")
	matched, _ := index.Find(differentMember, "user-2")
	require.Nil(t, matched, "members in one Team workspace must not be merged")
}

func TestImportCodexSessionsUpdatesExistingAndPreservesRefreshToken(t *testing.T) {
	existingToken := buildCodexAccessToken(t, "workspace-1", "user-1", "same-token", time.Now().Add(time.Hour))
	svc := newCodexImportMemoryAdminService([]service.Account{{
		ID:       13,
		Name:     "existing",
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "workspace-1",
			"chatgpt_user_id":    "user-1",
			"access_token":       existingToken,
			"refresh_token":      "refresh-old",
			"client_id":          "client-old",
		},
	}})
	handler := newCodexImportTestHandler(svc)

	result, err := handler.importCodexSessions(context.Background(), CodexSessionImportRequest{
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, []codexImportEntry{{Index: 1, Value: map[string]any{"access_token": existingToken}}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Zero(t, result.Created)
	require.Equal(t, 1, result.Updated)
	require.Zero(t, result.Skipped)
	require.Zero(t, result.Failed)
	require.Len(t, result.Items, 1)
	require.Equal(t, "updated", result.Items[0].Action)
	require.Equal(t, int64(13), result.Items[0].AccountID)
	require.Len(t, svc.updatedAccounts, 1)
	require.Equal(t, "refresh-old", svc.updatedAccounts[0].input.Credentials["refresh_token"])
	require.Equal(t, "client-old", svc.updatedAccounts[0].input.Credentials["client_id"])
}

func TestImportCodexSessionsUpdateExistingFalseCreatesCopy(t *testing.T) {
	existingToken := buildCodexAccessToken(t, "workspace-1", "user-1", "same-token", time.Now().Add(time.Hour))
	svc := newCodexImportMemoryAdminService([]service.Account{{
		ID: 21, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": existingToken},
	}})
	handler := newCodexImportTestHandler(svc)
	updateExisting := false

	result, err := handler.importCodexSessions(context.Background(), CodexSessionImportRequest{
		UpdateExisting:       &updateExisting,
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, []codexImportEntry{{Index: 1, Value: existingToken}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Zero(t, result.Updated)
	require.Len(t, svc.createdAccounts, 1)
}

func TestImportCodexSessionsNeverUpdatesUserOwnedAccount(t *testing.T) {
	accessToken := buildCodexAccessToken(t, "workspace-1", "user-1", "owned-token", time.Now().Add(time.Hour))
	ownerUserID := int64(77)
	svc := newCodexImportMemoryAdminService([]service.Account{{
		ID:          24,
		Name:        "user-owned-account",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		OwnerUserID: &ownerUserID,
		Credentials: map[string]any{"access_token": accessToken},
	}})

	result, err := newCodexImportTestHandler(svc).importCodexSessions(context.Background(), CodexSessionImportRequest{
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, []codexImportEntry{{Index: 1, Value: accessToken}})

	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Zero(t, result.Updated)
	require.Empty(t, svc.updatedAccounts)
	require.Len(t, svc.createdAccounts, 1)
	require.Nil(t, svc.createdAccounts[0].OwnerUserID)
}

func TestImportCodexSessionsReturnsPartialItemResults(t *testing.T) {
	svc := newCodexImportMemoryAdminService(nil)
	svc.failCreateAt = 2
	svc.createFailure = errors.New("injected create failure")
	handler := newCodexImportTestHandler(svc)
	entries := []codexImportEntry{
		{Index: 1, Value: buildCodexAccessToken(t, "workspace-1", "user-1", "token-1", time.Now().Add(time.Hour))},
		{Index: 2, Value: buildCodexAccessToken(t, "workspace-2", "user-2", "token-2", time.Now().Add(time.Hour))},
		{Index: 3, Value: buildCodexAccessToken(t, "workspace-3", "user-3", "token-3", time.Now().Add(time.Hour))},
	}

	result, err := handler.importCodexSessions(context.Background(), CodexSessionImportRequest{
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, entries)

	require.NoError(t, err, "an item failure must not discard the rest of the batch")
	require.Equal(t, 3, result.Total)
	require.Equal(t, 2, result.Created)
	require.Zero(t, result.Updated)
	require.Zero(t, result.Skipped)
	require.Equal(t, 1, result.Failed)
	require.Len(t, result.Items, 3)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "failed", result.Items[1].Action)
	require.Equal(t, result.Total, result.Created+result.Updated+result.Skipped+result.Failed)
}

func TestImportCodexSessionsSkipsBatchDuplicate(t *testing.T) {
	token := buildCodexAccessToken(t, "workspace-1", "user-1", "same-token", time.Now().Add(time.Hour))
	svc := newCodexImportMemoryAdminService(nil)
	handler := newCodexImportTestHandler(svc)

	result, err := handler.importCodexSessions(context.Background(), CodexSessionImportRequest{
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, []codexImportEntry{{Index: 1, Value: token}, {Index: 2, Value: token}})

	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Equal(t, 1, result.Created)
	require.Equal(t, 1, result.Skipped)
	require.Equal(t, "skipped", result.Items[1].Action)
	require.Equal(t, result.Total, result.Created+result.Updated+result.Skipped+result.Failed)
}

type codexImportMemoryAdminService struct {
	*stubAdminService
	nextID          int64
	createCalls     int
	failCreateAt    int
	createFailure   error
	updatedAccounts []struct {
		id    int64
		input *service.UpdateAccountInput
	}
}

func newCodexImportMemoryAdminService(accounts []service.Account) *codexImportMemoryAdminService {
	stub := newStubAdminService()
	stub.accounts = append([]service.Account(nil), accounts...)
	return &codexImportMemoryAdminService{stubAdminService: stub, nextID: 100}
}

func (s *codexImportMemoryAdminService) CreateAccount(ctx context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	s.createCalls++
	if s.failCreateAt > 0 && s.createCalls == s.failCreateAt {
		return nil, s.createFailure
	}
	s.createdAccounts = append(s.createdAccounts, input)
	account := service.Account{
		ID:          s.nextID,
		Name:        input.Name,
		Platform:    input.Platform,
		Type:        input.Type,
		Status:      service.StatusActive,
		Credentials: cloneCodexImportTestMap(input.Credentials),
		Extra:       cloneCodexImportTestMap(input.Extra),
	}
	s.nextID++
	s.accounts = append(s.accounts, account)
	return &account, nil
}

func (s *codexImportMemoryAdminService) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updatedAccounts = append(s.updatedAccounts, struct {
		id    int64
		input *service.UpdateAccountInput
	}{id: id, input: input})
	for idx := range s.accounts {
		if s.accounts[idx].ID == id {
			s.accounts[idx].Credentials = cloneCodexImportTestMap(input.Credentials)
			s.accounts[idx].Extra = cloneCodexImportTestMap(input.Extra)
			return &s.accounts[idx], nil
		}
	}
	return &service.Account{ID: id, Status: service.StatusActive, Credentials: cloneCodexImportTestMap(input.Credentials)}, nil
}

func (s *codexImportMemoryAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	for idx := range s.accounts {
		if s.accounts[idx].ID == id {
			return &s.accounts[idx], nil
		}
	}
	return s.stubAdminService.GetAccount(ctx, id)
}

func newCodexImportTestHandler(svc service.AdminService) *AccountHandler {
	return NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func mustNormalizeCodexImportEntry(t *testing.T, entry codexImportEntry) *codexImportAccount {
	t.Helper()
	item, err := normalizeCodexImportEntry(entry)
	require.NoError(t, err)
	return item
}

func buildCodexAccessToken(t *testing.T, accountID, userID, jti string, exp time.Time) string {
	t.Helper()
	claims := map[string]any{
		"sub":                         userID,
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	}
	if jti != "" {
		claims["jti"] = jti
	}
	return buildCodexImportTestJWT(t, exp, claims)
}

func buildCodexImportTestJWT(t *testing.T, exp time.Time, extraClaims map[string]any) string {
	t.Helper()
	headerBytes, err := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	require.NoError(t, err)
	claims := map[string]any{"sub": "user-from-sub", "exp": exp.Unix(), "iat": time.Now().Unix()}
	for key, value := range extraClaims {
		claims[key] = value
	}
	claimBytes, err := json.Marshal(claims)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(claimBytes) + "."
}

func cloneCodexImportTestMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func codexImportBoolPtr(value bool) *bool { return &value }
