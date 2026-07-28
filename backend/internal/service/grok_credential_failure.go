package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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
	var existing *grokCredentialFailureSnapshotError
	if errors.As(err, &existing) {
		return err
	}
	return &grokCredentialFailureSnapshotError{cause: err, snapshot: grokCredentialMutationSnapshot(account)}
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
		class = s.applyGrokCredentialFailureState(ctx, account, class)
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

func (s *OpenAIGatewayService) applyGrokCredentialFailureState(ctx context.Context, account *Account, class grokCredentialFailureClass) grokCredentialFailureClass {
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
	snapshot := grokCredentialMutationSnapshot(account)

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
	}
	return snapshot
}

func grokCredentialProxyIDsEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
