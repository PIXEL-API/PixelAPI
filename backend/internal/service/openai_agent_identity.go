package service

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	agentIdentityAuthAPIBaseURL          = "https://auth.openai.com/api/accounts"
	agentIdentityTaskRegistrationTimeout = 30 * time.Second
	agentIdentityTaskRegistrationMaxBody = 64 * 1024
	agentIdentityIdentifierMaxBytes      = 4096
)

var openAIAgentIdentityAuthAPIBaseURL = agentIdentityAuthAPIBaseURL

type agentIdentityWSConnectionInvalidator interface {
	InvalidateAgentIdentityWSConnections(accountID int64)
}

// AgentIdentityWSInvalidatorProxy breaks the construction cycle between the
// gateway connection pool and auxiliary services that may rotate a task. Wire
// creates one proxy first; ProvideOpenAIGatewayService binds its final target
// before any request can be served.
type AgentIdentityWSInvalidatorProxy struct {
	mu     sync.RWMutex
	target agentIdentityWSConnectionInvalidator
}

func NewAgentIdentityWSInvalidatorProxy() *AgentIdentityWSInvalidatorProxy {
	return &AgentIdentityWSInvalidatorProxy{}
}

func (p *AgentIdentityWSInvalidatorProxy) SetTarget(target agentIdentityWSConnectionInvalidator) {
	if p == nil || target == nil {
		panic("Agent Identity WS invalidator target is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.target != nil && p.target != target {
		panic("Agent Identity WS invalidator target is already configured")
	}
	p.target = target
}

func (p *AgentIdentityWSInvalidatorProxy) InvalidateAgentIdentityWSConnections(accountID int64) {
	if p == nil {
		panic("Agent Identity WS invalidator proxy is nil")
	}
	p.mu.RLock()
	target := p.target
	p.mu.RUnlock()
	if target == nil {
		panic("Agent Identity WS invalidator target is not configured")
	}
	target.InvalidateAgentIdentityWSConnections(accountID)
}

type agentIdentityKey struct {
	runtimeID  string
	privateKey ed25519.PrivateKey
	taskID     string
}

type agentIdentityTaskRegistrationResponse struct {
	TaskID               string `json:"task_id"`
	TaskIDCamel          string `json:"taskId"`
	EncryptedTaskID      string `json:"encrypted_task_id"`
	EncryptedTaskIDCamel string `json:"encryptedTaskId"`
}

type agentIdentityTaskRecoveredError struct{}

func (*agentIdentityTaskRecoveredError) Error() string {
	return "Agent Identity task recovered; retry with a fresh assertion"
}

// agentIdentityTaskLockRegistry shares one account lock across gateway, usage,
// quota, and account-test services. Entries are reference counted and removed
// after the last waiter leaves so account churn cannot grow the registry forever.
type agentIdentityTaskLockRegistry struct {
	mu      sync.Mutex
	entries map[int64]*agentIdentityTaskLockEntry
}

type agentIdentityTaskLockEntry struct {
	mu   sync.Mutex
	refs int
}

var sharedAgentIdentityTaskLocks = agentIdentityTaskLockRegistry{
	entries: make(map[int64]*agentIdentityTaskLockEntry),
}

func (r *agentIdentityTaskLockRegistry) lock(accountID int64) (func(), error) {
	if r == nil || accountID <= 0 {
		return nil, errors.New("agent identity account id is required for shared task locking")
	}
	r.mu.Lock()
	entry := r.entries[accountID]
	if entry == nil {
		entry = &agentIdentityTaskLockEntry{}
		r.entries[accountID] = entry
	}
	entry.refs++
	r.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		r.mu.Lock()
		entry.refs--
		if entry.refs == 0 && r.entries[accountID] == entry {
			delete(r.entries, accountID)
		}
		r.mu.Unlock()
	}, nil
}

func (r *agentIdentityTaskLockRegistry) size() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func parseAgentIdentityPrivateKey(encoded string) (ed25519.PrivateKey, error) {
	raw := strings.TrimSpace(encoded)
	if raw == "" {
		return nil, errors.New("agent identity private key is missing")
	}
	if strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return nil, errors.New("agent identity private key is not valid base64")
	}
	der, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil {
		return nil, errors.New("agent identity private key is not valid base64")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("agent identity private key is not valid PKCS#8")
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("agent identity private key must be Ed25519")
	}
	return privateKey, nil
}

func agentIdentityKeyFromAccount(account *Account) (agentIdentityKey, error) {
	if account == nil {
		return agentIdentityKey{}, errors.New("agent identity account is nil")
	}
	privateKey, err := parseAgentIdentityPrivateKey(account.GetCredential("agent_private_key"))
	if err != nil {
		return agentIdentityKey{}, err
	}
	runtimeID, err := normalizeAgentIdentityIdentifier("runtime id", account.GetCredential("agent_runtime_id"))
	if err != nil {
		return agentIdentityKey{}, err
	}
	taskID := strings.TrimSpace(account.GetCredential("task_id"))
	if taskID != "" {
		if _, err := normalizeAgentIdentityIdentifier("task id", taskID); err != nil {
			return agentIdentityKey{}, err
		}
	}
	return agentIdentityKey{runtimeID: runtimeID, privateKey: privateKey, taskID: taskID}, nil
}

func normalizeAgentIdentityIdentifier(label, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("agent identity %s is missing", label)
	}
	if len(value) > agentIdentityIdentifierMaxBytes {
		return "", fmt.Errorf("agent identity %s exceeds %d bytes", label, agentIdentityIdentifierMaxBytes)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("agent identity %s contains control characters", label)
	}
	return value, nil
}

func buildAgentAssertion(key agentIdentityKey, now time.Time) (string, error) {
	runtimeID, err := normalizeAgentIdentityIdentifier("runtime id", key.runtimeID)
	if err != nil {
		return "", err
	}
	taskID, err := normalizeAgentIdentityIdentifier("task id", key.taskID)
	if err != nil {
		return "", err
	}
	if len(key.privateKey) != ed25519.PrivateKeySize {
		return "", errors.New("agent identity private key is invalid")
	}
	timestamp := now.UTC().Format(time.RFC3339)
	payload := []byte(runtimeID + ":" + taskID + ":" + timestamp)
	signature, err := key.privateKey.Sign(nil, payload, crypto.Hash(0))
	if err != nil {
		return "", errors.New("failed to sign agent identity assertion")
	}
	envelope := struct {
		RuntimeID string `json:"agent_runtime_id"`
		TaskID    string `json:"task_id"`
		Timestamp string `json:"timestamp"`
		Signature string `json:"signature"`
	}{
		RuntimeID: runtimeID,
		TaskID:    taskID,
		Timestamp: timestamp,
		Signature: base64.StdEncoding.EncodeToString(signature),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", errors.New("failed to serialize agent identity assertion")
	}
	return "AgentAssertion " + base64.RawURLEncoding.EncodeToString(encoded), nil
}

func signAgentTaskRegistration(key agentIdentityKey, now time.Time) (string, string, error) {
	runtimeID, err := normalizeAgentIdentityIdentifier("runtime id", key.runtimeID)
	if err != nil {
		return "", "", err
	}
	if len(key.privateKey) != ed25519.PrivateKeySize {
		return "", "", errors.New("agent identity private key is invalid")
	}
	timestamp := now.UTC().Format(time.RFC3339)
	signature, err := key.privateKey.Sign(nil, []byte(runtimeID+":"+timestamp), crypto.Hash(0))
	if err != nil {
		return "", "", errors.New("failed to sign agent identity task registration")
	}
	return timestamp, base64.StdEncoding.EncodeToString(signature), nil
}

func decryptAgentTaskID(key agentIdentityKey, encoded string) (string, error) {
	raw := strings.TrimSpace(encoded)
	if raw == "" || strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return "", errors.New("encrypted agent identity task id is not valid base64")
	}
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil {
		return "", errors.New("encrypted agent identity task id is not valid base64")
	}
	seed := key.privateKey.Seed()
	digest := sha512.Sum512(seed)
	var curvePrivate [32]byte
	copy(curvePrivate[:], digest[:32])
	curvePrivate[0] &= 248
	curvePrivate[31] &= 127
	curvePrivate[31] |= 64
	curvePublicBytes, err := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
	if err != nil {
		return "", errors.New("failed to derive agent identity decryption key")
	}
	var curvePublic [32]byte
	copy(curvePublic[:], curvePublicBytes)
	plaintext, ok := box.OpenAnonymous(nil, ciphertext, &curvePublic, &curvePrivate)
	if !ok {
		return "", errors.New("failed to decrypt encrypted agent identity task id")
	}
	return normalizeAgentIdentityIdentifier("task id", string(plaintext))
}

func buildAgentIdentityTaskRegistrationURL(baseURL, runtimeID string) (string, error) {
	runtimeID, err := normalizeAgentIdentityIdentifier("runtime id", runtimeID)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", errors.New("agent identity registration base URL is invalid")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errors.New("agent identity registration base URL has unsupported scheme")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("agent identity registration base URL must not contain credentials, query, or fragment")
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	escapedBasePath := strings.TrimRight(parsed.EscapedPath(), "/")
	parsed.Path = basePath + "/v1/agent/" + runtimeID + "/task/register"
	parsed.RawPath = escapedBasePath + "/v1/agent/" + url.PathEscape(runtimeID) + "/task/register"
	return parsed.String(), nil
}

func selectAgentIdentityTaskID(key agentIdentityKey, result agentIdentityTaskRegistrationResponse) (string, error) {
	plain, err := selectMatchingAgentIdentityField("task id", result.TaskID, result.TaskIDCamel)
	if err != nil {
		return "", err
	}
	encrypted, err := selectMatchingAgentIdentityField("encrypted task id", result.EncryptedTaskID, result.EncryptedTaskIDCamel)
	if err != nil {
		return "", err
	}
	if plain != "" && encrypted != "" {
		return "", errors.New("agent identity task registration returned ambiguous task ids")
	}
	if plain != "" {
		return normalizeAgentIdentityIdentifier("task id", plain)
	}
	if encrypted != "" {
		return decryptAgentTaskID(key, encrypted)
	}
	return "", errors.New("agent identity task registration response omitted task id")
}

func selectMatchingAgentIdentityField(label string, values ...string) (string, error) {
	selected := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if selected != "" && selected != value {
			return "", fmt.Errorf("agent identity task registration returned conflicting %s fields", label)
		}
		selected = value
	}
	return selected, nil
}

func registerAgentIdentityTask(ctx context.Context, account *Account) (string, error) {
	key, err := agentIdentityKeyFromAccount(account)
	if err != nil {
		return "", err
	}
	timestamp, signature, err := signAgentTaskRegistration(key, time.Now())
	if err != nil {
		return "", err
	}
	targetURL, err := buildAgentIdentityTaskRegistrationURL(openAIAgentIdentityAuthAPIBaseURL, key.runtimeID)
	if err != nil {
		return "", err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               agentIdentityTaskRegistrationTimeout,
		ResponseHeaderTimeout: 15 * time.Second,
	})
	if err != nil {
		return "", errors.New("invalid proxy configuration for agent identity task registration")
	}
	body, err := json.Marshal(map[string]string{"timestamp": timestamp, "signature": signature})
	if err != nil {
		return "", errors.New("failed to serialize agent identity task registration")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("failed to build agent identity task registration request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("agent identity task registration request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("agent identity task registration returned status %d", resp.StatusCode)
	}
	responseBody, err := readUpstreamResponseBodyLimited(resp.Body, agentIdentityTaskRegistrationMaxBody)
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			return "", errors.New("agent identity task registration response exceeds 64 KiB")
		}
		return "", errors.New("failed to read agent identity task registration response")
	}
	var result agentIdentityTaskRegistrationResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", errors.New("agent identity task registration response is not one complete JSON object")
	}
	return selectAgentIdentityTaskID(key, result)
}

func shouldSkipAgentIdentityTaskRegistration(currentTaskID, expectedTaskID string) bool {
	currentTaskID = strings.TrimSpace(currentTaskID)
	expectedTaskID = strings.TrimSpace(expectedTaskID)
	if expectedTaskID == "" {
		return currentTaskID != ""
	}
	return currentTaskID != "" && currentTaskID != expectedTaskID
}

func persistAgentIdentityCredentials(ctx context.Context, repo AccountRepository, account *Account, credentials map[string]any) error {
	if account == nil {
		return errors.New("agent identity account is nil")
	}
	nextCredentials := cloneCredentials(credentials)
	if account.ID <= 0 {
		account.Credentials = nextCredentials
		return nil
	}
	if repo == nil {
		return errors.New("account repository is required to persist agent identity task")
	}
	if updater, ok := any(repo).(accountCredentialsUpdater); ok {
		if err := updater.UpdateCredentials(ctx, account.ID, nextCredentials); err != nil {
			return fmt.Errorf("persist agent identity task: %w", err)
		}
	} else {
		return ErrAccountMutationGuardUnavailable.WithMetadata(map[string]string{
			"account_id": fmt.Sprintf("%d", account.ID),
			"operation":  "persist_agent_identity_credentials",
			"stage":      "missing_narrow_capability",
		})
	}
	account.Credentials = nextCredentials
	return nil
}

func ensureAgentIdentityTaskForAccount(ctx context.Context, repo AccountRepository, wsInvalidator agentIdentityWSConnectionInvalidator, fallbackMu *sync.Mutex, account *Account, expectedTaskID string) error {
	if account == nil || !account.IsOpenAIAgentIdentity() {
		return nil
	}
	if shouldSkipAgentIdentityTaskRegistration(account.GetCredential("task_id"), expectedTaskID) {
		return nil
	}

	var unlock func()
	if account.ID > 0 {
		var err error
		unlock, err = sharedAgentIdentityTaskLocks.lock(account.ID)
		if err != nil {
			return err
		}
	} else {
		if fallbackMu == nil {
			return errors.New("agent identity task lock is unavailable")
		}
		fallbackMu.Lock()
		unlock = fallbackMu.Unlock
	}
	defer unlock()

	credentialAccount := account
	if account.ID > 0 {
		if repo == nil {
			return errors.New("account repository is required to refresh agent identity task state")
		}
		refreshed, err := repo.GetByID(ctx, account.ID)
		if err != nil {
			return fmt.Errorf("reload agent identity account: %w", err)
		}
		if refreshed == nil || !refreshed.IsOpenAIAgentIdentity() {
			return errors.New("agent identity credentials are unavailable")
		}
		credentialAccount = refreshed
	}
	currentTaskID := strings.TrimSpace(credentialAccount.GetCredential("task_id"))
	if shouldSkipAgentIdentityTaskRegistration(currentTaskID, expectedTaskID) {
		account.Credentials = cloneCredentials(credentialAccount.Credentials)
		return nil
	}

	newTaskID, err := registerAgentIdentityTask(ctx, credentialAccount)
	if err != nil {
		return err
	}
	credentials := cloneCredentials(credentialAccount.Credentials)
	credentials["task_id"] = newTaskID
	if err := persistAgentIdentityCredentials(ctx, repo, credentialAccount, credentials); err != nil {
		return err
	}
	account.Credentials = cloneCredentials(credentialAccount.Credentials)
	if wsInvalidator != nil && credentialAccount.ID > 0 {
		wsInvalidator.InvalidateAgentIdentityWSConnections(credentialAccount.ID)
	}
	return nil
}

func (s *OpenAIGatewayService) ensureAgentIdentityTask(ctx context.Context, account *Account, expectedTaskID string) error {
	if s == nil {
		return errors.New("openAI gateway service is nil")
	}
	return ensureAgentIdentityTaskForAccount(ctx, s.accountRepo, s, &s.agentIdentityTaskMu, account, expectedTaskID)
}

var agentIdentityTaskInvalidCodes = map[string]struct{}{
	"invalid_task_id": {},
	"task_not_found":  {},
	"task_expired":    {},
}

func isAgentIdentityTaskInvalidHTTPResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusUnauthorized || len(body) == 0 || len(body) > agentIdentityTaskRegistrationMaxBody {
		return false
	}
	var envelope struct {
		Code  string          `json:"code"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	if isAgentIdentityTaskInvalidCode(envelope.Code) {
		return true
	}
	if len(envelope.Error) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Error), []byte("null")) {
		return false
	}
	var errorCode string
	if err := json.Unmarshal(envelope.Error, &errorCode); err == nil {
		return isAgentIdentityTaskInvalidCode(errorCode)
	}
	var nested struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(envelope.Error, &nested) == nil && isAgentIdentityTaskInvalidCode(nested.Code)
}

func isAgentIdentityTaskInvalidCode(code string) bool {
	_, ok := agentIdentityTaskInvalidCodes[strings.ToLower(strings.TrimSpace(code))]
	return ok
}

type agentIdentityTaskRecoveryContextKey struct{}

func markAgentIdentityTaskRecoveryTried(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, agentIdentityTaskRecoveryContextKey{}, true)
}

func agentIdentityTaskRecoveryWasTried(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tried, _ := ctx.Value(agentIdentityTaskRecoveryContextKey{}).(bool)
	return tried
}

func isAgentIdentityTaskInvalidWSDialError(err *openAIWSDialError) bool {
	return err != nil && isAgentIdentityTaskInvalidHTTPResponse(err.StatusCode, err.ResponseBody)
}

func (s *OpenAIGatewayService) buildOpenAIAuthenticationHeaders(ctx context.Context, account *Account, token string) (http.Header, error) {
	if account == nil {
		return nil, errors.New("account is nil")
	}
	if account.IsOpenAIAgentIdentity() {
		return buildAgentIdentityAuthenticationHeaders(ctx, s.accountRepo, s, &s.agentIdentityTaskMu, account)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("openAI authentication token is missing")
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers, nil
}

func buildAgentIdentityAuthenticationHeaders(ctx context.Context, repo AccountRepository, wsInvalidator agentIdentityWSConnectionInvalidator, taskMu *sync.Mutex, account *Account) (http.Header, error) {
	if account == nil || !account.IsOpenAIAgentIdentity() {
		return nil, errors.New("agent identity account is required")
	}
	if err := ensureAgentIdentityTaskForAccount(ctx, repo, wsInvalidator, taskMu, account, ""); err != nil {
		return nil, err
	}
	key, err := agentIdentityKeyFromAccount(account)
	if err != nil {
		return nil, err
	}
	assertion, err := buildAgentAssertion(key, time.Now())
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("Authorization", assertion)
	return headers, nil
}

func (s *OpenAIGatewayService) refreshOpenAIAgentIdentityHeaders(ctx context.Context, account *Account, headers http.Header) (http.Header, error) {
	refreshed, _, err := s.refreshOpenAIAgentIdentityHeadersWithTask(ctx, account, headers)
	return refreshed, err
}

func (s *OpenAIGatewayService) refreshOpenAIAgentIdentityHeadersWithTask(ctx context.Context, account *Account, headers http.Header) (http.Header, string, error) {
	refreshed := cloneHeader(headers)
	if account == nil || !account.IsOpenAIAgentIdentity() {
		return refreshed, "", nil
	}
	credentialAccount := account
	if account.ID > 0 {
		if s == nil || s.accountRepo == nil {
			return nil, "", errors.New("account repository is required to refresh agent identity WS credentials")
		}
		latest, err := s.accountRepo.GetByID(ctx, account.ID)
		if err != nil {
			return nil, "", fmt.Errorf("reload agent identity WS credentials: %w", err)
		}
		if latest == nil || !latest.IsOpenAIAgentIdentity() {
			return nil, "", errors.New("agent identity WS credentials are unavailable")
		}
		credentialAccount = latest
	}
	if refreshed == nil {
		refreshed = make(http.Header)
	}
	authHeaders, err := buildAgentIdentityAuthenticationHeaders(ctx, s.accountRepo, s, &s.agentIdentityTaskMu, credentialAccount)
	if err != nil {
		return nil, "", err
	}
	refreshed.Set("Authorization", authHeaders.Get("Authorization"))
	return refreshed, strings.TrimSpace(credentialAccount.GetCredential("task_id")), nil
}

type agentIdentityWSHeaderState struct {
	mu     sync.Mutex
	taskID string
}

func (s *agentIdentityWSHeaderState) setTaskID(taskID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.taskID = strings.TrimSpace(taskID)
	s.mu.Unlock()
}

func (s *agentIdentityWSHeaderState) expectedTaskID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskID
}

func (s *OpenAIGatewayService) agentIdentityWSHeadersFactory(account *Account) (func(context.Context, http.Header) (http.Header, error), *agentIdentityWSHeaderState) {
	if account == nil || !account.IsOpenAIAgentIdentity() {
		return nil, nil
	}
	state := &agentIdentityWSHeaderState{}
	return func(ctx context.Context, base http.Header) (http.Header, error) {
		refreshed, taskID, err := s.refreshOpenAIAgentIdentityHeadersWithTask(ctx, account, base)
		if err == nil {
			state.setTaskID(taskID)
		}
		return refreshed, err
	}, state
}

func (s *OpenAIGatewayService) recoverAgentIdentityTask(ctx context.Context, account *Account, expectedTaskID string) error {
	return s.ensureAgentIdentityTask(ctx, account, expectedTaskID)
}

func (s *OpenAIGatewayService) isAgentIdentityAccount(_ context.Context, account *Account) bool {
	return account != nil && account.IsOpenAIAgentIdentity()
}

func (s *OpenAIGatewayService) InvalidateAgentIdentityWSConnections(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.getOpenAIWSConnPool().ClearAccount(accountID)
}

type agentIdentitySensitiveValuesContextKey struct{}

func withAgentIdentitySensitiveValues(ctx context.Context, values ...string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	existing, _ := ctx.Value(agentIdentitySensitiveValuesContextKey{}).([]string)
	merged := append([]string(nil), existing...)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		duplicate := false
		for _, current := range merged {
			if current == value {
				duplicate = true
				break
			}
		}
		if !duplicate {
			merged = append(merged, value)
		}
	}
	if len(merged) == len(existing) {
		return ctx
	}
	return context.WithValue(ctx, agentIdentitySensitiveValuesContextKey{}, merged)
}

func agentIdentitySensitiveValuesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	values, _ := ctx.Value(agentIdentitySensitiveValuesContextKey{}).([]string)
	return values
}

func redactAgentIdentitySensitiveBodyForAccount(account *Account, body []byte, additionalSensitiveValues ...string) []byte {
	if account == nil || !account.IsOpenAIAgentIdentity() || len(body) == 0 {
		return body
	}
	redacted := string(body)
	for _, key := range []string{
		"agent_private_key",
		"agent_runtime_id",
		"task_id",
		"access_token",
		"refresh_token",
		"id_token",
		"api_key",
		"session_key",
		"cookie",
	} {
		if value := strings.TrimSpace(account.GetCredential(key)); value != "" {
			redacted = strings.ReplaceAll(redacted, value, "[redacted]")
		}
	}
	for _, value := range additionalSensitiveValues {
		if value = strings.TrimSpace(value); value != "" {
			redacted = strings.ReplaceAll(redacted, value, "[redacted]")
		}
	}
	const assertionPrefix = "AgentAssertion "
	for offset := 0; offset < len(redacted); {
		relativeStart := strings.Index(redacted[offset:], assertionPrefix)
		if relativeStart < 0 {
			break
		}
		valueStart := offset + relativeStart + len(assertionPrefix)
		end := valueStart
		for end < len(redacted) && !strings.ContainsRune(" \t\r\n\"',}", rune(redacted[end])) {
			end++
		}
		redacted = redacted[:valueStart] + "[redacted]" + redacted[end:]
		offset = valueStart + len("[redacted]")
	}
	return []byte(redacted)
}

func (s *OpenAIGatewayService) redactAgentIdentitySensitiveBody(ctx context.Context, account *Account, body []byte, additionalSensitiveValues ...string) []byte {
	if !s.isAgentIdentityAccount(ctx, account) {
		return body
	}
	contextValues := agentIdentitySensitiveValuesFromContext(ctx)
	values := make([]string, 0, len(contextValues)+len(additionalSensitiveValues))
	values = append(values, contextValues...)
	values = append(values, additionalSensitiveValues...)
	return redactAgentIdentitySensitiveBodyForAccount(account, body, values...)
}
