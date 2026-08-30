package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

func newAgentIdentityTestKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal PKCS#8 key: %v", err)
	}
	return privateKey, base64.StdEncoding.EncodeToString(der)
}

func newAgentIdentityTestAccount(t *testing.T, id int64, runtimeID, taskID string) *Account {
	t.Helper()
	_, encodedKey := newAgentIdentityTestKey(t)
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode":         OpenAIAuthModeAgentIdentity,
			"agent_runtime_id":  runtimeID,
			"agent_private_key": encodedKey,
			"task_id":           taskID,
		},
	}
}

func TestBuildAgentAssertionSignsExactEnvelope(t *testing.T) {
	privateKey, _ := newAgentIdentityTestKey(t)
	now := time.Date(2026, 7, 16, 8, 9, 10, 0, time.FixedZone("UTC+8", 8*60*60))
	assertion, err := buildAgentAssertion(agentIdentityKey{
		runtimeID:  "runtime-1",
		taskID:     "task-1",
		privateKey: privateKey,
	}, now)
	if err != nil {
		t.Fatalf("build assertion: %v", err)
	}
	const prefix = "AgentAssertion "
	if !strings.HasPrefix(assertion, prefix) {
		t.Fatalf("assertion prefix = %q", assertion)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(assertion, prefix))
	if err != nil {
		t.Fatalf("decode assertion: %v", err)
	}
	var envelope struct {
		RuntimeID string `json:"agent_runtime_id"`
		TaskID    string `json:"task_id"`
		Timestamp string `json:"timestamp"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.RuntimeID != "runtime-1" || envelope.TaskID != "task-1" || envelope.Timestamp != "2026-07-16T00:09:10Z" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	payload := []byte(envelope.RuntimeID + ":" + envelope.TaskID + ":" + envelope.Timestamp)
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("private key did not expose an Ed25519 public key")
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		t.Fatal("assertion signature did not verify")
	}
}

func TestRegisterAgentIdentityTaskSupportsResponseFormsAndEscapesRuntimePath(t *testing.T) {
	privateKey, encodedKey := newAgentIdentityTestKey(t)
	encryptedTaskID := encryptAgentIdentityTaskIDForTest(t, privateKey, "task-encrypted")
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{name: "snake plaintext", response: `{"task_id":"task-snake"}`, want: "task-snake"},
		{name: "camel plaintext", response: `{"taskId":"task-camel"}`, want: "task-camel"},
		{name: "snake encrypted", response: fmt.Sprintf(`{"encrypted_task_id":%q}`, encryptedTaskID), want: "task-encrypted"},
		{name: "camel encrypted", response: fmt.Sprintf(`{"encryptedTaskId":%q}`, encryptedTaskID), want: "task-encrypted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.EscapedPath() != "/v1/agent/runtime%2Fwith%20space/task/register" {
					t.Errorf("escaped path = %q", r.URL.EscapedPath())
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			previousBaseURL := openAIAgentIdentityAuthAPIBaseURL
			openAIAgentIdentityAuthAPIBaseURL = server.URL
			defer func() { openAIAgentIdentityAuthAPIBaseURL = previousBaseURL }()

			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime/with space", "agent_private_key": encodedKey,
			}}
			got, err := registerAgentIdentityTask(context.Background(), account)
			if err != nil {
				t.Fatalf("register task: %v", err)
			}
			if got != test.want {
				t.Fatalf("task id = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRegisterAgentIdentityTaskRejectsIncompleteOrOversizedJSON(t *testing.T) {
	_, encodedKey := newAgentIdentityTestKey(t)
	responses := []string{
		`{"task_id":"task-1"} trailing`,
		`{"task_id":"task-1","taskId":"task-2"}`,
		`{"task_id":"task-1","encryptedTaskId":"AAAA"}`,
		`{"task_id":"` + strings.Repeat("x", agentIdentityTaskRegistrationMaxBody) + `"}`,
	}
	for index, response := range responses {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			previousBaseURL := openAIAgentIdentityAuthAPIBaseURL
			openAIAgentIdentityAuthAPIBaseURL = server.URL
			defer func() { openAIAgentIdentityAuthAPIBaseURL = previousBaseURL }()
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-1", "agent_private_key": encodedKey,
			}}
			if _, err := registerAgentIdentityTask(context.Background(), account); err == nil {
				t.Fatal("expected strict registration response rejection")
			}
		})
	}
}

func TestAgentIdentityTaskInvalidResponseRequiresCompleteExplicitJSONCode(t *testing.T) {
	valid := [][]byte{
		[]byte(`{"code":"invalid_task_id"}`),
		[]byte(`{"error":"task_not_found"}`),
		[]byte(`{"error":{"code":"task_expired","message":"expired"}}`),
	}
	for _, body := range valid {
		if !isAgentIdentityTaskInvalidHTTPResponse(http.StatusUnauthorized, body) {
			t.Fatalf("expected invalid task response for %s", body)
		}
	}
	invalid := [][]byte{
		[]byte(`{"error":{"message":"invalid task id"}}`),
		[]byte(`invalid_task_id`),
		[]byte(`{"error":{"code":"invalid_task_id"}`),
		[]byte(`{"error":{"code":"invalid_task_id"}} trailing`),
		[]byte(`{"error":{"code":"invalid_token"}}`),
	}
	for _, body := range invalid {
		if isAgentIdentityTaskInvalidHTTPResponse(http.StatusUnauthorized, body) {
			t.Fatalf("unexpected invalid task recovery for %s", body)
		}
	}
	if isAgentIdentityTaskInvalidHTTPResponse(http.StatusInternalServerError, valid[0]) {
		t.Fatal("5xx must not trigger Agent Identity task recovery")
	}
}

func TestAgentIdentityTaskLockRegistryCleansUpAfterWaiters(t *testing.T) {
	registry := &agentIdentityTaskLockRegistry{entries: make(map[int64]*agentIdentityTaskLockEntry)}
	unlockFirst, err := registry.lock(42)
	if err != nil {
		t.Fatalf("lock first: %v", err)
	}
	acquired := make(chan func(), 1)
	go func() {
		unlock, lockErr := registry.lock(42)
		if lockErr == nil {
			acquired <- unlock
		}
	}()
	time.Sleep(20 * time.Millisecond)
	if registry.size() != 1 {
		t.Fatalf("registry size while waiting = %d", registry.size())
	}
	unlockFirst()
	select {
	case unlockSecond := <-acquired:
		unlockSecond()
	case <-time.After(time.Second):
		t.Fatal("second waiter did not acquire lock")
	}
	if registry.size() != 0 {
		t.Fatalf("registry leaked entries: %d", registry.size())
	}
}

func TestEnsureAgentIdentityTaskUsesExpectedTaskCASAndInvalidatesWS(t *testing.T) {
	var registrations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrations.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"task-new"}`))
	}))
	defer server.Close()
	previousBaseURL := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	defer func() { openAIAgentIdentityAuthAPIBaseURL = previousBaseURL }()

	stored := newAgentIdentityTestAccount(t, 91, "runtime-91", "task-old")
	repo := &agentIdentityTaskRepo{account: stored}
	invalidator := &agentIdentityInvalidatorRecorder{}
	caller := cloneAgentIdentityAccountForTest(stored)
	if err := ensureAgentIdentityTaskForAccount(context.Background(), repo, invalidator, &sync.Mutex{}, caller, "task-old"); err != nil {
		t.Fatalf("recover task: %v", err)
	}
	if caller.GetCredential("task_id") != "task-new" || repo.account.GetCredential("task_id") != "task-new" {
		t.Fatalf("task was not persisted: caller=%q stored=%q", caller.GetCredential("task_id"), repo.account.GetCredential("task_id"))
	}
	if registrations.Load() != 1 || invalidator.accountID.Load() != 91 {
		t.Fatalf("registrations=%d invalidated=%d", registrations.Load(), invalidator.accountID.Load())
	}

	staleCaller := cloneAgentIdentityAccountForTest(stored)
	staleCaller.Credentials["task_id"] = "task-old"
	if err := ensureAgentIdentityTaskForAccount(context.Background(), repo, invalidator, &sync.Mutex{}, staleCaller, "task-old"); err != nil {
		t.Fatalf("stale caller recovery: %v", err)
	}
	if registrations.Load() != 1 || staleCaller.GetCredential("task_id") != "task-new" {
		t.Fatalf("stale caller caused duplicate registration: count=%d task=%q", registrations.Load(), staleCaller.GetCredential("task_id"))
	}
}

func TestPersistAgentIdentityCredentialsFailsClosedWithoutNarrowCapability(t *testing.T) {
	account := &Account{
		ID:          91,
		Credentials: map[string]any{"task_id": "task-old"},
	}
	repo := &agentIdentityRepoWithoutCredentialUpdater{}

	err := persistAgentIdentityCredentials(context.Background(), repo, account, map[string]any{"task_id": "task-new"})

	if !errors.Is(err, ErrAccountMutationGuardUnavailable) {
		t.Fatalf("expected missing narrow capability error, got %v", err)
	}
	if got := account.GetCredential("task_id"); got != "task-old" {
		t.Fatalf("failed persistence mutated caller snapshot: task_id=%q", got)
	}
}

// Deliberately omits UpdateCredentials so the persistence helper's fail-closed
// behavior is tested without accidentally exercising the broad Update path.
type agentIdentityRepoWithoutCredentialUpdater struct {
	AccountRepository
}

func TestBuildOpenAIAuthenticationHeadersAndRedaction(t *testing.T) {
	account := newAgentIdentityTestAccount(t, 0, "runtime-secret", "task-secret")
	service := &OpenAIGatewayService{}
	headers, err := service.buildOpenAIAuthenticationHeaders(context.Background(), account, "")
	if err != nil {
		t.Fatalf("build Agent Identity auth: %v", err)
	}
	if !strings.HasPrefix(headers.Get("Authorization"), "AgentAssertion ") {
		t.Fatalf("authorization = %q", headers.Get("Authorization"))
	}
	body := []byte(`{"message":"runtime-secret task-secret ` + account.GetCredential("agent_private_key") + ` AgentAssertion abc.def"}`)
	redacted := string(service.redactAgentIdentitySensitiveBody(context.Background(), account, body))
	for _, secret := range []string{"runtime-secret", "task-secret", account.GetCredential("agent_private_key"), "abc.def"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted body leaked %q: %s", secret, redacted)
		}
	}
}

func TestAgentIdentityWSHeadersFactoryReloadsTaskWithoutMutatingSharedSnapshot(t *testing.T) {
	stored := newAgentIdentityTestAccount(t, 103, "runtime-103", "task-current")
	repo := &agentIdentityTaskRepo{account: stored}
	snapshot := cloneAgentIdentityAccountForTest(stored)
	snapshot.Credentials["task_id"] = "task-stale"
	service := &OpenAIGatewayService{accountRepo: repo}
	factory, state := service.agentIdentityWSHeadersFactory(snapshot)
	if factory == nil || state == nil {
		t.Fatal("Agent Identity websocket header factory was not created")
	}
	headers, err := factory(context.Background(), http.Header{"X-Test": []string{"preserved"}})
	if err != nil {
		t.Fatalf("build websocket headers: %v", err)
	}
	if headers.Get("X-Test") != "preserved" || !strings.HasPrefix(headers.Get("Authorization"), "AgentAssertion ") {
		t.Fatalf("unexpected websocket headers: %#v", headers)
	}
	if state.expectedTaskID() != "task-current" {
		t.Fatalf("dial state task = %q", state.expectedTaskID())
	}
	if snapshot.GetCredential("task_id") != "task-stale" {
		t.Fatalf("shared account snapshot was mutated: %q", snapshot.GetCredential("task_id"))
	}

	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(headers.Get("Authorization"), "AgentAssertion "))
	if err != nil {
		t.Fatalf("decode websocket assertion: %v", err)
	}
	var envelope struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode websocket assertion envelope: %v", err)
	}
	if envelope.TaskID != "task-current" {
		t.Fatalf("websocket assertion task = %q", envelope.TaskID)
	}
}

func encryptAgentIdentityTaskIDForTest(t *testing.T, privateKey ed25519.PrivateKey, taskID string) string {
	t.Helper()
	digest := sha512.Sum512(privateKey.Seed())
	var curvePrivate [32]byte
	copy(curvePrivate[:], digest[:32])
	curvePrivate[0] &= 248
	curvePrivate[31] &= 127
	curvePrivate[31] |= 64
	publicBytes, err := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
	if err != nil {
		t.Fatalf("derive Curve25519 public key: %v", err)
	}
	var publicKey [32]byte
	copy(publicKey[:], publicBytes)
	ciphertext, err := box.SealAnonymous(nil, []byte(taskID), &publicKey, rand.Reader)
	if err != nil {
		t.Fatalf("encrypt task id: %v", err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func cloneAgentIdentityAccountForTest(account *Account) *Account {
	if account == nil {
		return nil
	}
	clone := *account
	clone.Credentials = cloneCredentials(account.Credentials)
	return &clone
}

type agentIdentityTaskRepo struct {
	AccountRepository
	mu        sync.Mutex
	account   *Account
	updateErr error
}

func (r *agentIdentityTaskRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil || r.account.ID != id {
		return nil, errors.New("account not found")
	}
	return cloneAgentIdentityAccountForTest(r.account), nil
}

func (r *agentIdentityTaskRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	if r.account == nil || r.account.ID != id {
		return errors.New("account not found")
	}
	r.account.Credentials = cloneCredentials(credentials)
	return nil
}

type agentIdentityInvalidatorRecorder struct {
	accountID atomic.Int64
}

func (r *agentIdentityInvalidatorRecorder) InvalidateAgentIdentityWSConnections(accountID int64) {
	r.accountID.Store(accountID)
}
