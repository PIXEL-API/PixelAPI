package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

const (
	grokCredentialMutationTimeout   = 5 * time.Second
	grokPaymentRequiredErrorMessage = "grok payment required (402): account billing or spending limit requires manual intervention"

	GrokCredentialUnavailableClientMessage = "No healthy Grok OAuth account is currently available"

	GrokCredentialReasonRevoked          GatewayFailureReason = "grok_oauth_credential_revoked"
	GrokCredentialReasonMissing          GatewayFailureReason = "grok_oauth_credentials_missing"
	GrokCredentialReasonEntitlement      GatewayFailureReason = "grok_oauth_entitlement_action_required"
	GrokCredentialReasonProxyInvalid     GatewayFailureReason = "grok_oauth_proxy_invalid"
	GrokCredentialReasonRefreshTransient GatewayFailureReason = "grok_oauth_refresh_transient"
	GrokCredentialReasonProviderConfig   GatewayFailureReason = "grok_oauth_provider_config"
	GrokCredentialReasonProviderDown     GatewayFailureReason = "grok_oauth_provider_unavailable"
	GrokCredentialReasonAccountChanged   GatewayFailureReason = "grok_oauth_account_state_changed"
	GrokCredentialReasonStateUpdate      GatewayFailureReason = "grok_oauth_account_state_update_failed"
)

var (
	errOAuthRefreshAccountRereadFailed = errors.New("oauth refresh account reread failed")
	errOAuthRefreshAccountStateChanged = errors.New("oauth refresh account state changed")
	errOAuthRefreshCredentialPersist   = errors.New("oauth refresh credential persistence failed")
	errGrokConditionalStateUnsupported = errors.New("grok conditional account state repository is unavailable")
)

type providerConfigurationRefreshError struct{ err error }

func (e *providerConfigurationRefreshError) Error() string {
	if e == nil || e.err == nil {
		return "provider refresh configuration is unavailable"
	}
	return e.err.Error()
}
func (e *providerConfigurationRefreshError) Unwrap() error { return e.err }

type providerCycleContainmentRefreshError struct{ err error }

func (e *providerCycleContainmentRefreshError) Error() string {
	if e == nil || e.err == nil {
		return "provider refresh cycle is contained"
	}
	return e.err.Error()
}
func (e *providerCycleContainmentRefreshError) Unwrap() error { return e.err }

type grokCredentialFailureClass struct {
	scope     GatewayFailureScope
	reason    GatewayFailureReason
	action    NextAccountAction
	permanent bool
	transient bool
	message   string
}

// GrokCredentialMutationSnapshot is the credential identity observed by the
// failing request. Conditional repository updates prevent stale failures from
// quarantining a concurrently refreshed account.
type GrokCredentialMutationSnapshot struct {
	CredentialsJSON string
	ProxyID         *int64
	ProxyUpdatedAt  *time.Time
}

type grokCredentialFailureSnapshotError struct {
	cause    error
	snapshot GrokCredentialMutationSnapshot
}

func (e *grokCredentialFailureSnapshotError) Error() string { return e.cause.Error() }
func (e *grokCredentialFailureSnapshotError) Unwrap() error { return e.cause }

func withGrokCredentialFailureSnapshot(err error, account *Account) error {
	if err == nil || account == nil || !account.IsGrokOAuth() {
		return err
	}
	return withGrokCredentialFailureMutationSnapshot(err, grokCredentialMutationSnapshot(account))
}

func withGrokCredentialFailureMutationSnapshot(err error, snapshot GrokCredentialMutationSnapshot) error {
	if err == nil {
		return nil
	}
	var existing *grokCredentialFailureSnapshotError
	if errors.As(err, &existing) {
		return err
	}
	return &grokCredentialFailureSnapshotError{cause: err, snapshot: cloneGrokCredentialMutationSnapshot(snapshot)}
}

func grokCredentialMutationSnapshotFromError(err error, fallback *Account) GrokCredentialMutationSnapshot {
	var snapshotErr *grokCredentialFailureSnapshotError
	if errors.As(err, &snapshotErr) && snapshotErr != nil {
		return cloneGrokCredentialMutationSnapshot(snapshotErr.snapshot)
	}
	return grokCredentialMutationSnapshot(fallback)
}

func cloneGrokCredentialMutationSnapshot(snapshot GrokCredentialMutationSnapshot) GrokCredentialMutationSnapshot {
	cloned := GrokCredentialMutationSnapshot{CredentialsJSON: snapshot.CredentialsJSON}
	if snapshot.ProxyID != nil {
		proxyID := *snapshot.ProxyID
		cloned.ProxyID = &proxyID
	}
	if snapshot.ProxyUpdatedAt != nil {
		updatedAt := *snapshot.ProxyUpdatedAt
		cloned.ProxyUpdatedAt = &updatedAt
	}
	return cloned
}

type grokProxyVersionObservation struct {
	ProxyID              int64
	UpdatedAt            time.Time
	Status               string
	Platform             string
	RequiredAccountLevel string
}

type grokProxyVersionObservationRecorder struct {
	mu          sync.Mutex
	observation *grokProxyVersionObservation
}

type grokProxyVersionObservationContextKey struct{}

func withGrokProxyVersionObservation(ctx context.Context) (context.Context, *grokProxyVersionObservationRecorder) {
	if ctx == nil {
		ctx = context.Background()
	}
	recorder := &grokProxyVersionObservationRecorder{}
	return context.WithValue(ctx, grokProxyVersionObservationContextKey{}, recorder), recorder
}

func recordGrokProxyVersionObservation(ctx context.Context, proxy *Proxy) {
	if ctx == nil || proxy == nil || proxy.ID <= 0 || proxy.UpdatedAt.IsZero() {
		return
	}
	recorder, _ := ctx.Value(grokProxyVersionObservationContextKey{}).(*grokProxyVersionObservationRecorder)
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.observation = &grokProxyVersionObservation{
		ProxyID:              proxy.ID,
		UpdatedAt:            proxy.UpdatedAt,
		Status:               proxy.Status,
		Platform:             proxy.Platform,
		RequiredAccountLevel: proxy.RequiredAccountLevel,
	}
	recorder.mu.Unlock()
}

func (r *grokProxyVersionObservationRecorder) snapshot() *grokProxyVersionObservation {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.observation == nil {
		return nil
	}
	copy := *r.observation
	return &copy
}

type grokCredentialConditionalStateRepository interface {
	SetGrokCredentialErrorIfMatch(context.Context, int64, GrokCredentialMutationSnapshot, string) (bool, error)
	SetGrokCredentialTempUnschedulableIfMatch(context.Context, int64, GrokCredentialMutationSnapshot, time.Time, string) (bool, error)
}

func setGrokPaymentRequiredErrorIfMatch(
	ctx context.Context,
	accountRepo AccountRepository,
	account *Account,
) (bool, error) {
	if accountRepo == nil || account == nil || account.Platform != PlatformGrok {
		return false, errGrokConditionalStateUnsupported
	}
	// Pool-mode accounts represent an upstream-managed credential pool. A 402
	// from one pooled credential must not permanently poison the local wrapper
	// account; pool health and rotation remain owned by that upstream.
	if account.IsPoolMode() {
		return false, nil
	}
	repo, ok := accountRepo.(grokCredentialConditionalStateRepository)
	if !ok {
		return false, errGrokConditionalStateUnsupported
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	return repo.SetGrokCredentialErrorIfMatch(
		stateCtx,
		account.ID,
		grokCredentialMutationSnapshot(account),
		grokPaymentRequiredErrorMessage,
	)
}

// GetRequestCredential provides a safe request-path credential entry for
// callers that can use it directly, such as WebSocket setup.
func (s *OpenAIGatewayService) GetRequestCredential(ctx context.Context, c *gin.Context, account *Account) (string, string, error) {
	token, kind, err := s.GetAccessToken(ctx, account)
	if err == nil {
		return token, kind, nil
	}
	return "", "", s.NormalizeGrokCredentialFailure(ctx, c, account, err)
}

// NormalizeGrokCredentialFailure turns raw Grok OAuth token failures into a
// failover contract while conditionally quarantining only the credential
// version that actually failed.
func (s *OpenAIGatewayService) NormalizeGrokCredentialFailure(ctx context.Context, c *gin.Context, account *Account, err error) error {
	if err == nil || account == nil || !account.IsGrokOAuth() {
		return err
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var existing *UpstreamFailoverError
	if errors.As(err, &existing) {
		return err
	}

	class := classifyGrokCredentialFailure(account, err)
	if class.permanent || class.transient {
		class = s.applyGrokCredentialFailureState(ctx, account, err, class)
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:  PlatformGrok,
		AccountID: account.ID,
		Kind:      "credential_failover",
		Message:   class.message,
	})
	return &UpstreamFailoverError{
		StatusCode:        http.StatusServiceUnavailable,
		Stage:             GatewayFailureStageAccountAuth,
		Scope:             class.scope,
		Reason:            class.reason,
		NextAccountAction: class.action,
		ClientStatusCode:  http.StatusServiceUnavailable,
		ClientMessage:     GrokCredentialUnavailableClientMessage,
	}
}

func classifyGrokCredentialFailure(account *Account, err error) grokCredentialFailureClass {
	stableReason := strings.ToLower(strings.TrimSpace(infraerrors.Reason(err)))
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	contains := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(stableReason, value) || strings.Contains(message, value) {
				return true
			}
		}
		return false
	}
	var providerConfigErr *providerConfigurationRefreshError
	var containmentErr *providerCycleContainmentRefreshError

	switch {
	case errors.Is(err, errGrokOAuthRefreshTokenMissing), errors.Is(err, errGrokOAuthAccessTokenMissing), errors.Is(err, errGrokOAuthAccessTokenExpired):
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonMissing, action: NextAccountRetry, permanent: true, message: "Grok OAuth credentials are missing or expired"}
	case contains("invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_reused", "refresh_token_invalidated", "app_session_terminated"):
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonRevoked, action: NextAccountRetry, permanent: true, message: "Grok OAuth credentials require account action"}
	case contains("grok_oauth_entitlement_denied", "entitlement_denied", "access_denied", "subscription required", "no active grok subscription"):
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonEntitlement, action: NextAccountRetry, permanent: true, message: "Grok OAuth entitlement requires account action"}
	case errors.Is(err, errGrokOAuthConfiguredProxyMiss), contains("grok_oauth_proxy_not_found"):
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonProxyInvalid, action: NextAccountRetry, permanent: true, message: "Grok OAuth account proxy configuration is invalid"}
	case account != nil && account.ProxyID != nil && isGrokProxyAuthenticationFailure(err):
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonProxyInvalid, action: NextAccountRetry, permanent: true, message: "Grok OAuth account proxy authentication failed"}
	case errors.Is(err, errOAuthRefreshAccountStateChanged):
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonAccountChanged, action: NextAccountRetry, message: "Grok OAuth account eligibility changed"}
	case errors.As(err, &containmentErr):
		return grokCredentialFailureClass{scope: GatewayFailureScopeProvider, reason: GrokCredentialReasonProviderDown, action: NextAccountStop, message: "Grok OAuth provider state is temporarily unavailable"}
	case errors.As(err, &providerConfigErr):
		return grokCredentialFailureClass{scope: GatewayFailureScopeProvider, reason: GrokCredentialReasonProviderConfig, action: NextAccountStop, message: "Grok OAuth provider configuration is unavailable"}
	case errors.Is(err, errGrokOAuthRefreshNotConfigured), contains("invalid_client", "unauthorized_client", "invalid_scope", "unknown scope", "grok oauth service is not configured"):
		return grokCredentialFailureClass{scope: GatewayFailureScopeProvider, reason: GrokCredentialReasonProviderConfig, action: NextAccountStop, message: "Grok OAuth provider configuration is unavailable"}
	case errors.Is(err, errOAuthRefreshAccountRereadFailed), errors.Is(err, errOAuthRefreshCredentialPersist), contains("grok_oauth_proxy_lookup_failed", "grok_oauth_request_failed"):
		return grokCredentialFailureClass{scope: GatewayFailureScopeProvider, reason: GrokCredentialReasonProviderDown, action: NextAccountStop, message: "Grok OAuth provider is temporarily unavailable"}
	case contains("status 429", "status 500", "status 502", "status 503", "status 504") && (account == nil || account.ProxyID == nil):
		return grokCredentialFailureClass{scope: GatewayFailureScopeProvider, reason: GrokCredentialReasonProviderDown, action: NextAccountStop, message: "Grok OAuth provider is temporarily unavailable"}
	default:
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonRefreshTransient, action: NextAccountRetry, transient: true, message: "Grok OAuth credential refresh is temporarily unavailable"}
	}
}

// isGrokProxyAuthenticationFailure recognizes stable HTTP/SOCKS proxy
// credential failures. Callers must additionally require an account-bound
// proxy so an xAI/OpenID authentication error cannot be mislabeled as proxy
// configuration. No proxy URL or credentials are extracted or logged here.
func isGrokProxyAuthenticationFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"proxy authentication required",
		"proxy authentication failed",
		"proxy authorization required",
		"status 407",
		"http 407",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return (strings.Contains(message, "proxyconnect") || strings.Contains(message, "socks")) &&
		strings.Contains(message, "authentication failed")
}

func (s *OpenAIGatewayService) applyGrokCredentialFailureState(ctx context.Context, account *Account, failure error, class grokCredentialFailureClass) grokCredentialFailureClass {
	if s == nil || s.accountRepo == nil {
		return grokCredentialStateUpdateFailure()
	}
	repo, ok := s.accountRepo.(grokCredentialConditionalStateRepository)
	if !ok {
		return grokCredentialStateUpdateFailure()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stateCtx, cancel := context.WithTimeout(ctx, grokCredentialMutationTimeout)
	defer cancel()
	snapshot := grokCredentialMutationSnapshotFromError(failure, account)

	var updated bool
	var err error
	if class.permanent {
		updated, err = repo.SetGrokCredentialErrorIfMatch(stateCtx, account.ID, snapshot, string(class.reason))
	} else {
		until := time.Now().Add(tokenRefreshTempUnschedDuration)
		updated, err = repo.SetGrokCredentialTempUnschedulableIfMatch(stateCtx, account.ID, snapshot, until, string(class.reason))
		if err == nil && updated {
			s.BlockAccountScheduling(account, until, string(class.reason))
		}
	}
	if err != nil {
		return grokCredentialStateUpdateFailure()
	}
	if !updated {
		return grokCredentialFailureClass{scope: GatewayFailureScopeAccount, reason: GrokCredentialReasonAccountChanged, action: NextAccountRetry, message: "Grok OAuth account eligibility changed"}
	}
	if class.permanent {
		s.BlockAccountScheduling(account, time.Time{}, string(class.reason))
		if s.grokTokenProvider != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
			_ = s.grokTokenProvider.InvalidateToken(cleanupCtx, account)
			cleanupCancel()
		}
	}
	return class
}

func grokCredentialStateUpdateFailure() grokCredentialFailureClass {
	return grokCredentialFailureClass{scope: GatewayFailureScopeProvider, reason: GrokCredentialReasonStateUpdate, action: NextAccountStop, message: "Grok OAuth account state could not be updated safely"}
}

func grokCredentialMutationSnapshot(account *Account) GrokCredentialMutationSnapshot {
	if account == nil {
		return GrokCredentialMutationSnapshot{CredentialsJSON: "null"}
	}
	credentialsJSON := "null"
	if encoded, err := json.Marshal(account.Credentials); err == nil {
		credentialsJSON = string(encoded)
	}
	snapshot := GrokCredentialMutationSnapshot{CredentialsJSON: credentialsJSON}
	if account.ProxyID != nil {
		proxyID := *account.ProxyID
		snapshot.ProxyID = &proxyID
		if account.Proxy != nil && account.Proxy.ID == proxyID && !account.Proxy.UpdatedAt.IsZero() {
			updatedAt := account.Proxy.UpdatedAt
			snapshot.ProxyUpdatedAt = &updatedAt
		}
	}
	return snapshot
}

func grokCredentialMutationSnapshotWithObservedProxy(account *Account, proxy *Proxy) GrokCredentialMutationSnapshot {
	snapshot := grokCredentialMutationSnapshot(account)
	snapshot.ProxyUpdatedAt = nil
	if proxy == nil || proxy.ID <= 0 || proxy.UpdatedAt.IsZero() {
		return snapshot
	}
	proxyID := proxy.ID
	snapshot.ProxyID = &proxyID
	updatedAt := proxy.UpdatedAt
	snapshot.ProxyUpdatedAt = &updatedAt
	return snapshot
}

type grokAccountSchedulingBlockCleaner interface {
	ClearAccountSchedulingBlock(accountID int64)
}

// GrokSchedulingBlockCleanerProxy breaks the RateLimitService -> gateway
// construction cycle. It only clears this process' short scheduling bridge;
// cross-instance propagation requires a separate cluster mechanism.
type GrokSchedulingBlockCleanerProxy struct {
	mu     sync.RWMutex
	target grokAccountSchedulingBlockCleaner
}

func NewGrokSchedulingBlockCleanerProxy() *GrokSchedulingBlockCleanerProxy {
	return &GrokSchedulingBlockCleanerProxy{}
}

func (p *GrokSchedulingBlockCleanerProxy) SetTarget(target grokAccountSchedulingBlockCleaner) {
	if p == nil || target == nil {
		panic("Grok scheduling block cleaner target is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.target != nil && p.target != target {
		panic("Grok scheduling block cleaner target is already configured")
	}
	p.target = target
}

func (p *GrokSchedulingBlockCleanerProxy) ClearAccountSchedulingBlock(accountID int64) {
	if p == nil || accountID <= 0 {
		return
	}
	p.mu.RLock()
	target := p.target
	p.mu.RUnlock()
	if target != nil {
		target.ClearAccountSchedulingBlock(accountID)
	}
}

func grokCredentialProxyIDsEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
