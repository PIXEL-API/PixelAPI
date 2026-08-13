//go:build e2e

package integration

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

type contractAuthData struct {
	AccessToken string `json:"access_token"`
	User        struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
}

type contractGroupData struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type contractAPIKeyData struct {
	ID      int64  `json:"id"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	GroupID *int64 `json:"group_id"`
}

type contractAPIKeyListData struct {
	Items []contractAPIKeyData `json:"items"`
	Total int                  `json:"total"`
}

func contractLogin(t *testing.T, email, password string) contractAuthData {
	t.Helper()
	path := "/api/v1/auth/login"
	resp, body := doJSONRequest(t, http.MethodPost, path, map[string]string{
		"email":    email,
		"password": password,
	}, "", nil)
	requireHTTPStatus(t, http.MethodPost, path, resp, body, http.StatusOK)

	var auth contractAuthData
	decodeEnvelopeData(t, body, &auth)
	if auth.AccessToken == "" || auth.User.ID <= 0 {
		t.Fatalf("login returned incomplete authentication data: %s", body)
	}
	return auth
}

// TestContractRegistrationLoginAndAPIKeyLifecycle is intentionally provider-free.
// It runs against an isolated PostgreSQL/Redis/application stack created by
// scripts/e2e-test.sh and treats every contract step as required: no Skip can
// turn a broken registration, login, JWT, API-key, or cache-invalidation path
// into a green test.
func TestContractRegistrationLoginAndAPIKeyLifecycle(t *testing.T) {
	requireContractMode(t)

	adminEmail := getEnv("ADMIN_EMAIL", "contract-admin@test.local")
	adminPassword := getEnv("ADMIN_PASSWORD", "")
	if adminPassword == "" {
		t.Fatal("ADMIN_PASSWORD is required for contract E2E")
	}
	admin := contractLogin(t, adminEmail, adminPassword)
	if admin.User.Role != "admin" {
		t.Fatalf("bootstrap login role=%q, want admin", admin.User.Role)
	}
	settingsPath := "/api/v1/admin/settings"
	resp, body := doJSONRequest(t, http.MethodPut, settingsPath, map[string]bool{
		"registration_enabled": true,
	}, admin.AccessToken, nil)
	requireHTTPStatus(t, http.MethodPut, settingsPath, resp, body, http.StatusOK)
	var settings struct {
		RegistrationEnabled bool `json:"registration_enabled"`
	}
	decodeEnvelopeData(t, body, &settings)
	if !settings.RegistrationEnabled {
		t.Fatalf("contract setup did not enable registration: %s", body)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	groupName := "contract-group-" + suffix
	groupPath := "/api/v1/admin/groups"
	resp, body = doJSONRequest(t, http.MethodPost, groupPath, map[string]any{
		"name":            groupName,
		"platform":        "anthropic",
		"rate_multiplier": 1,
	}, admin.AccessToken, nil)
	requireHTTPStatus(t, http.MethodPost, groupPath, resp, body, http.StatusOK)
	var group contractGroupData
	decodeEnvelopeData(t, body, &group)
	if group.ID <= 0 || group.Name != groupName || group.Platform != "anthropic" {
		t.Fatalf("created group does not match request: %s", body)
	}
	t.Cleanup(func() {
		path := groupPath + "/" + strconv.FormatInt(group.ID, 10)
		cleanupResp, cleanupBody := doJSONRequest(t, http.MethodDelete, path, nil, admin.AccessToken, nil)
		if cleanupResp.StatusCode != http.StatusOK && cleanupResp.StatusCode != http.StatusNotFound {
			t.Errorf("cleanup group HTTP %d: %s", cleanupResp.StatusCode, cleanupBody)
		}
	})

	userEmail := "contract-user-" + suffix + "@test.local"
	userPassword := "ContractTest@12345"
	registerPath := "/api/v1/auth/register"
	resp, body = doJSONRequest(t, http.MethodPost, registerPath, map[string]string{
		"email":    userEmail,
		"password": userPassword,
	}, "", nil)
	requireHTTPStatus(t, http.MethodPost, registerPath, resp, body, http.StatusOK)
	var registration contractAuthData
	decodeEnvelopeData(t, body, &registration)
	if registration.AccessToken == "" || registration.User.Email != userEmail {
		t.Fatalf("registration returned incomplete authentication data: %s", body)
	}

	user := contractLogin(t, userEmail, userPassword)
	if user.User.ID != registration.User.ID || user.User.Email != userEmail {
		t.Fatalf("login identity does not match registered identity: registration=%+v login=%+v", registration.User, user.User)
	}

	mePath := "/api/v1/auth/me"
	resp, body = doJSONRequest(t, http.MethodGet, mePath, nil, user.AccessToken, nil)
	requireHTTPStatus(t, http.MethodGet, mePath, resp, body, http.StatusOK)
	var currentUser struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	decodeEnvelopeData(t, body, &currentUser)
	if currentUser.ID != user.User.ID || currentUser.Email != userEmail {
		t.Fatalf("current-user contract mismatch: %s", body)
	}

	availableGroupsPath := "/api/v1/groups/available"
	resp, body = doJSONRequest(t, http.MethodGet, availableGroupsPath, nil, user.AccessToken, nil)
	requireHTTPStatus(t, http.MethodGet, availableGroupsPath, resp, body, http.StatusOK)
	var availableGroups []contractGroupData
	decodeEnvelopeData(t, body, &availableGroups)
	foundGroup := false
	for _, available := range availableGroups {
		if available.ID == group.ID {
			foundGroup = true
			break
		}
	}
	if !foundGroup {
		t.Fatalf("new public group %d is missing from user available groups: %s", group.ID, body)
	}

	apiKeyPath := "/api/v1/keys"
	apiKeyName := "contract-key-" + suffix
	resp, body = doJSONRequest(t, http.MethodPost, apiKeyPath, map[string]any{
		"name":     apiKeyName,
		"group_id": group.ID,
	}, user.AccessToken, map[string]string{
		"Idempotency-Key": "contract-create-api-key-" + suffix,
	})
	requireHTTPStatus(t, http.MethodPost, apiKeyPath, resp, body, http.StatusOK)
	var apiKey contractAPIKeyData
	decodeEnvelopeData(t, body, &apiKey)
	if apiKey.ID <= 0 || apiKey.Key == "" || apiKey.Name != apiKeyName || apiKey.GroupID == nil || *apiKey.GroupID != group.ID {
		t.Fatalf("created API key does not match contract: %s", body)
	}
	safeLogKey(t, "contract API key", apiKey.Key)

	apiKeyByIDPath := apiKeyPath + "/" + strconv.FormatInt(apiKey.ID, 10)
	apiKeyDeleted := false
	t.Cleanup(func() {
		if apiKeyDeleted {
			return
		}
		cleanupResp, cleanupBody := doJSONRequest(t, http.MethodDelete, apiKeyByIDPath, nil, user.AccessToken, nil)
		if cleanupResp.StatusCode != http.StatusOK && cleanupResp.StatusCode != http.StatusNotFound {
			t.Errorf("cleanup API key HTTP %d: %s", cleanupResp.StatusCode, cleanupBody)
		}
	})

	resp, body = doJSONRequest(t, http.MethodGet, apiKeyByIDPath, nil, user.AccessToken, nil)
	requireHTTPStatus(t, http.MethodGet, apiKeyByIDPath, resp, body, http.StatusOK)
	var fetched contractAPIKeyData
	decodeEnvelopeData(t, body, &fetched)
	if fetched.ID != apiKey.ID || fetched.Key != apiKey.Key {
		t.Fatalf("API key get contract mismatch: %s", body)
	}

	resp, body = doJSONRequest(t, http.MethodGet, apiKeyPath, nil, user.AccessToken, nil)
	requireHTTPStatus(t, http.MethodGet, apiKeyPath, resp, body, http.StatusOK)
	var listed contractAPIKeyListData
	decodeEnvelopeData(t, body, &listed)
	if listed.Total < 1 {
		t.Fatalf("API key list did not include created key: %s", body)
	}
	foundKey := false
	for _, item := range listed.Items {
		if item.ID == apiKey.ID && item.Key == apiKey.Key {
			foundKey = true
			break
		}
	}
	if !foundKey {
		t.Fatalf("API key %d missing from list: %s", apiKey.ID, body)
	}

	// /v1/usage intentionally performs authentication without billing
	// enforcement, so a fresh zero-balance user can prove the key is usable
	// without requiring a real provider account or artificial wallet credit.
	usagePath := "/v1/usage"
	resp, body = doJSONRequest(t, http.MethodGet, usagePath, nil, apiKey.Key, nil)
	requireHTTPStatus(t, http.MethodGet, usagePath, resp, body, http.StatusOK)

	resp, body = doJSONRequest(t, http.MethodDelete, apiKeyByIDPath, nil, user.AccessToken, nil)
	requireHTTPStatus(t, http.MethodDelete, apiKeyByIDPath, resp, body, http.StatusOK)
	apiKeyDeleted = true

	// Deletion must invalidate both L1 and Redis authentication caches. A 200
	// here would prove that the lifecycle endpoint deleted the row but left the
	// gateway credential usable.
	resp, body = doJSONRequest(t, http.MethodGet, usagePath, nil, apiKey.Key, nil)
	requireHTTPStatus(t, http.MethodGet, usagePath, resp, body, http.StatusUnauthorized)
}
