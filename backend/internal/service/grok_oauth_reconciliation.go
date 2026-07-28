package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	defaultGrokOAuthReconcilePageSize = 50
	maxGrokOAuthReconcilePageSize     = 500
	maxGrokOAuthReconcileWindow       = 24 * time.Hour

	GrokOAuthReconcileReasonMissingRefreshToken = "missing_refresh_token"
	GrokOAuthReconcileReasonMissingAccessToken  = "missing_access_token"
	GrokOAuthReconcileReasonMissingExpiry       = "missing_expiry"
	GrokOAuthReconcileReasonInvalidExpiry       = "invalid_expiry"
	GrokOAuthReconcileReasonNearExpiry          = "near_expiry"
	GrokOAuthReconcileReasonCredentialRejected  = "credential_rejected"

	GrokOAuthReconcileActionBlock   = "block_account"
	GrokOAuthReconcileActionRefresh = "refresh_credentials"

	GrokOAuthReconcileOutcomePlanned = "planned"
	GrokOAuthReconcileOutcomeApplied = "applied"
	GrokOAuthReconcileOutcomeSkipped = "skipped"
	GrokOAuthReconcileOutcomeFailed  = "failed"
	GrokOAuthReconcileOutcomePartial = "partial"
)

var (
	ErrGrokOAuthReconcileMode = infraerrors.BadRequest(
		"GROK_OAUTH_RECONCILE_MODE_INVALID",
		"apply requires dry_run=false and apply=true",
	)
	ErrGrokOAuthReconcileCursor = infraerrors.BadRequest(
		"GROK_OAUTH_RECONCILE_CURSOR_INVALID",
		"after_id must be non-negative",
	)
	ErrGrokOAuthReconcileLimit = infraerrors.BadRequest(
		"GROK_OAUTH_RECONCILE_LIMIT_INVALID",
		"limit is outside the allowed reconciliation page range",
	)
	ErrGrokOAuthReconcileWindow = infraerrors.BadRequest(
		"GROK_OAUTH_RECONCILE_WINDOW_INVALID",
		"refresh_window_seconds is outside the allowed range",
	)
)

// GrokOAuthReconciler is the narrow admin-facing reconciliation port.
type GrokOAuthReconciler interface {
	ReconcileGrokOAuth(ctx context.Context, input GrokOAuthReconcileInput) (*GrokOAuthReconcileResult, error)
}

// GrokOAuthReconcileCandidatePager is intentionally reconciliation-specific.
// It keeps structurally invalid OAuth rows discoverable without broadening the
// normal background refresh candidate contract.
type GrokOAuthReconcileCandidatePager interface {
	ListGrokOAuthReconcileCandidatePage(
		ctx context.Context,
		afterID int64,
		limit int,
	) (*GrokOAuthReconcileCandidatePage, error)
}

type GrokOAuthReconcileCandidatePage struct {
	Accounts    []Account
	NextAfterID int64
	HasMore     bool
}

type GrokOAuthReconcileInput struct {
	DryRun        bool
	Apply         bool
	AfterID       int64
	Limit         int
	RefreshWindow time.Duration
}

// GrokOAuthReconcileItem is metadata-only. Credentials, provider response
// bodies, account identity fields, and raw errors must never cross this API.
type GrokOAuthReconcileItem struct {
	AccountID int64  `json:"account_id"`
	Reason    string `json:"reason"`
	Action    string `json:"action"`
	Outcome   string `json:"outcome"`
}

type GrokOAuthReconcileResult struct {
	DryRun       bool                     `json:"dry_run"`
	Scanned      int                      `json:"scanned"`
	Actionable   int                      `json:"actionable"`
	WouldBlock   int                      `json:"would_block"`
	WouldRefresh int                      `json:"would_refresh"`
	Blocked      int                      `json:"blocked"`
	Refreshed    int                      `json:"refreshed"`
	Skipped      int                      `json:"skipped"`
	Failed       int                      `json:"failed"`
	Partial      int                      `json:"partial"`
	Items        []GrokOAuthReconcileItem `json:"items"`
	NextAfterID  int64                    `json:"next_after_id"`
	HasMore      bool                     `json:"has_more"`
}

func (s *TokenRefreshService) ReconcileGrokOAuth(
	ctx context.Context,
	input GrokOAuthReconcileInput,
) (*GrokOAuthReconcileResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if input.Apply && input.DryRun {
		return nil, ErrGrokOAuthReconcileMode
	}
	if input.AfterID < 0 {
		return nil, ErrGrokOAuthReconcileCursor
	}
	limit := input.Limit
	if limit == 0 {
		limit = defaultGrokOAuthReconcilePageSize
	}
	if limit < 1 || limit > maxGrokOAuthReconcilePageSize {
		return nil, ErrGrokOAuthReconcileLimit
	}
	refreshWindow := input.RefreshWindow
	if refreshWindow == 0 {
		refreshWindow = grokTokenRefreshSkew
	}
	if refreshWindow < 0 || refreshWindow > maxGrokOAuthReconcileWindow {
		return nil, ErrGrokOAuthReconcileWindow
	}
	if refreshWindow < grokTokenRefreshSkew {
		refreshWindow = grokTokenRefreshSkew
	}

	if s == nil || s.accountRepo == nil {
		return nil, errors.New("Grok OAuth reconciliation account repository is not configured")
	}
	pager, ok := s.accountRepo.(GrokOAuthReconcileCandidatePager)
	if !ok {
		return nil, errors.New("Grok OAuth reconciliation candidate pager is not configured")
	}
	conditionalRepo, ok := s.accountRepo.(grokCredentialConditionalStateRepository)
	if input.Apply && !ok {
		return nil, errors.New("Grok OAuth reconciliation conditional mutation repository is not configured")
	}
	executor, ok := s.grokOAuthReconcileExecutor()
	if !ok {
		return nil, errors.New("Grok OAuth refresher is not registered")
	}
	if input.Apply && s.refreshAPI == nil {
		return nil, errors.New("Grok OAuth refresh coordinator is not configured")
	}

	page, err := pager.ListGrokOAuthReconcileCandidatePage(ctx, input.AfterID, limit)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, errors.New("Grok OAuth reconciliation repository returned a nil cursor page")
	}
	if !isStrictlyIncreasingGrokReconcilePage(page.Accounts, input.AfterID) {
		return nil, errors.New("Grok OAuth reconciliation repository returned an invalid cursor page")
	}
	if page.HasMore && page.NextAfterID <= input.AfterID {
		return nil, errors.New("Grok OAuth reconciliation repository returned invalid cursor metadata")
	}

	dryRun := !input.Apply
	result := &GrokOAuthReconcileResult{
		DryRun:  dryRun,
		Scanned: len(page.Accounts),
		Items:   make([]GrokOAuthReconcileItem, 0, len(page.Accounts)),
		HasMore: page.HasMore,
	}
	if page.HasMore {
		result.NextAfterID = page.NextAfterID
	}

	providerUnavailable := false
	for i := range page.Accounts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		account := &page.Accounts[i]
		reason, action, actionable := classifyGrokOAuthReconcileAccount(account, refreshWindow)
		if !actionable {
			result.Skipped++
			continue
		}

		result.Actionable++
		item := GrokOAuthReconcileItem{
			AccountID: account.ID,
			Reason:    reason,
			Action:    action,
			Outcome:   GrokOAuthReconcileOutcomePlanned,
		}
		if action == GrokOAuthReconcileActionBlock {
			result.WouldBlock++
		} else {
			result.WouldRefresh++
		}
		if dryRun {
			result.Items = append(result.Items, item)
			continue
		}

		switch action {
		case GrokOAuthReconcileActionBlock:
			item.Outcome = s.applyGrokOAuthReconcileStructuralBlock(
				ctx,
				conditionalRepo,
				account,
				refreshWindow,
				&item,
			)
		case GrokOAuthReconcileActionRefresh:
			if providerUnavailable {
				item.Outcome = GrokOAuthReconcileOutcomeSkipped
				break
			}
			item.Outcome, providerUnavailable = s.applyGrokOAuthReconcileRefresh(
				ctx,
				conditionalRepo,
				account,
				executor,
				refreshWindow,
				&item,
			)
		default:
			return nil, fmt.Errorf("unsupported Grok OAuth reconciliation action")
		}
		accumulateGrokOAuthReconcileOutcome(result, item.Action, item.Outcome)
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *TokenRefreshService) grokOAuthReconcileExecutor() (OAuthRefreshExecutor, bool) {
	if s == nil {
		return nil, false
	}
	for i, refresher := range s.refreshers {
		if _, ok := refresher.(*GrokTokenRefresher); !ok || i >= len(s.executors) {
			continue
		}
		executor := s.executors[i]
		if executor != nil {
			return executor, true
		}
	}
	return nil, false
}

func (s *TokenRefreshService) applyGrokOAuthReconcileStructuralBlock(
	ctx context.Context,
	repo grokCredentialConditionalStateRepository,
	account *Account,
	refreshWindow time.Duration,
	item *GrokOAuthReconcileItem,
) string {
	latest, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || latest == nil {
		return GrokOAuthReconcileOutcomeFailed
	}
	latestReason, latestAction, actionable := classifyGrokOAuthReconcileAccount(latest, refreshWindow)
	if !actionable || latestAction != GrokOAuthReconcileActionBlock {
		return GrokOAuthReconcileOutcomeSkipped
	}
	item.Reason = latestReason
	applied, err := repo.SetGrokCredentialErrorIfMatch(
		ctx,
		latest.ID,
		grokCredentialMutationSnapshot(latest),
		"Grok OAuth credential reconciliation: "+latestReason,
	)
	if err != nil {
		return GrokOAuthReconcileOutcomeFailed
	}
	if !applied {
		return GrokOAuthReconcileOutcomeSkipped
	}
	return s.finishGrokOAuthReconcileBlock(ctx, latest)
}

func (s *TokenRefreshService) applyGrokOAuthReconcileRefresh(
	ctx context.Context,
	repo grokCredentialConditionalStateRepository,
	account *Account,
	executor OAuthRefreshExecutor,
	refreshWindow time.Duration,
	item *GrokOAuthReconcileItem,
) (outcome string, providerUnavailable bool) {
	refreshResult, err := s.refreshGrokOAuthForReconcile(ctx, account, executor, refreshWindow)
	if err == nil {
		if refreshResult == nil {
			return GrokOAuthReconcileOutcomeFailed, true
		}
		if refreshResult.LockHeld || !refreshResult.Refreshed {
			return GrokOAuthReconcileOutcomeSkipped, false
		}
		if refreshResult.Account == nil {
			return GrokOAuthReconcileOutcomePartial, true
		}
		if postErr := s.postRefreshActionsLeased(ctx, refreshResult.Account, &ClusterLeaseGuard{}); postErr != nil {
			return GrokOAuthReconcileOutcomePartial, false
		}
		return GrokOAuthReconcileOutcomeApplied, false
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return GrokOAuthReconcileOutcomeFailed, false
	}

	observedAccount := account
	if refreshResult != nil && refreshResult.Account != nil {
		observedAccount = refreshResult.Account
	}
	class := classifyGrokCredentialFailure(observedAccount, err)
	if !class.permanent {
		return GrokOAuthReconcileOutcomeFailed, class.scope == GatewayFailureScopeProvider
	}

	item.Reason = GrokOAuthReconcileReasonCredentialRejected
	item.Action = GrokOAuthReconcileActionBlock
	applied, mutationErr := repo.SetGrokCredentialErrorIfMatch(
		ctx,
		observedAccount.ID,
		grokCredentialMutationSnapshot(observedAccount),
		"Grok OAuth credential reconciliation: "+GrokOAuthReconcileReasonCredentialRejected,
	)
	if mutationErr != nil {
		return GrokOAuthReconcileOutcomeFailed, true
	}
	if !applied {
		return GrokOAuthReconcileOutcomeSkipped, false
	}
	return s.finishGrokOAuthReconcileBlock(ctx, observedAccount), false
}

func (s *TokenRefreshService) refreshGrokOAuthForReconcile(
	ctx context.Context,
	account *Account,
	executor OAuthRefreshExecutor,
	refreshWindow time.Duration,
) (*OAuthRefreshResult, error) {
	maxAttempts := 1
	if s.cfg != nil && s.cfg.MaxRetries > maxAttempts {
		maxAttempts = s.cfg.MaxRetries
	}
	var lastResult *OAuthRefreshResult
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.refreshAPI.RefreshIfNeeded(ctx, account, executor, refreshWindow)
		lastResult, lastErr = result, err
		if err == nil {
			return result, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		observedAccount := account
		if result != nil && result.Account != nil {
			observedAccount = result.Account
		}
		if !classifyGrokCredentialFailure(observedAccount, err).transient || attempt == maxAttempts {
			return result, err
		}
		backoff := time.Second
		if s.cfg != nil && s.cfg.RetryBackoffSeconds > 0 {
			backoff = time.Duration(s.cfg.RetryBackoffSeconds) * time.Second
		}
		shift := attempt - 1
		if shift > 6 {
			shift = 6
		}
		backoff *= time.Duration(1 << shift)
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return lastResult, ctx.Err()
		}
	}
	return lastResult, lastErr
}

func (s *TokenRefreshService) finishGrokOAuthReconcileBlock(ctx context.Context, account *Account) string {
	if account == nil {
		return GrokOAuthReconcileOutcomePartial
	}
	account.Status = StatusError
	account.Schedulable = false
	if s.cacheInvalidator == nil {
		return GrokOAuthReconcileOutcomePartial
	}
	if err := s.cacheInvalidator.InvalidateToken(ctx, account); err != nil {
		return GrokOAuthReconcileOutcomePartial
	}
	return GrokOAuthReconcileOutcomeApplied
}

func accumulateGrokOAuthReconcileOutcome(result *GrokOAuthReconcileResult, action, outcome string) {
	if outcome == GrokOAuthReconcileOutcomeApplied || outcome == GrokOAuthReconcileOutcomePartial {
		switch action {
		case GrokOAuthReconcileActionBlock:
			result.Blocked++
		case GrokOAuthReconcileActionRefresh:
			result.Refreshed++
		}
	}
	switch outcome {
	case GrokOAuthReconcileOutcomeSkipped:
		result.Skipped++
	case GrokOAuthReconcileOutcomeFailed:
		result.Failed++
	case GrokOAuthReconcileOutcomePartial:
		result.Partial++
	}
}

func classifyGrokOAuthReconcileAccount(
	account *Account,
	refreshWindow time.Duration,
) (reason, action string, actionable bool) {
	if account == nil || !account.IsGrokOAuth() || account.Status != StatusActive {
		return "", "", false
	}
	if strings.TrimSpace(account.GetGrokRefreshToken()) == "" {
		return GrokOAuthReconcileReasonMissingRefreshToken, GrokOAuthReconcileActionBlock, true
	}
	if strings.TrimSpace(account.GetGrokAccessToken()) == "" {
		return GrokOAuthReconcileReasonMissingAccessToken, GrokOAuthReconcileActionRefresh, true
	}
	rawExpiry := strings.TrimSpace(account.GetCredential("expires_at"))
	if rawExpiry == "" {
		return GrokOAuthReconcileReasonMissingExpiry, GrokOAuthReconcileActionRefresh, true
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return GrokOAuthReconcileReasonInvalidExpiry, GrokOAuthReconcileActionRefresh, true
	}
	if time.Until(*expiresAt) <= refreshWindow {
		return GrokOAuthReconcileReasonNearExpiry, GrokOAuthReconcileActionRefresh, true
	}
	return "", "", false
}

func isStrictlyIncreasingGrokReconcilePage(accounts []Account, afterID int64) bool {
	previousID := afterID
	for i := range accounts {
		if accounts[i].ID <= previousID {
			return false
		}
		previousID = accounts[i].ID
	}
	return true
}
