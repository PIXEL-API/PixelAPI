package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type CyberPolicyBlockScope string

const (
	CyberPolicyBlockScopeNone         CyberPolicyBlockScope = ""
	CyberPolicyBlockScopeUserGroupDay CyberPolicyBlockScope = "user_group_day"
)

// CyberPolicyHitDecision describes the restriction caused by one unique
// upstream cyber_policy response. HitSequence is scoped to a user, the actual
// routed group, and the configured local calendar day.
type CyberPolicyHitDecision struct {
	HitSequence  int64
	Action       CyberPolicyBlockScope
	BlockedUntil time.Time
	Duplicate    bool
	// Enforced is set by OpenAIGatewayService after the effective group passes
	// runtime selection. It remains true when Redis is unavailable so callers can
	// distinguish an enforced group from an intentionally ignored group.
	Enforced bool
}

// CyberPolicyBlockState is the effective user-and-group restriction state.
type CyberPolicyBlockState struct {
	Blocked      bool
	Scope        CyberPolicyBlockScope
	RetryAfter   time.Duration
	BlockedUntil time.Time
}

// CyberPolicyIsolationStore atomically records, checks, and clears user-level
// group restrictions. upstreamAttemptID is required so repeated handling of the
// same routed HTTP attempt or logical WebSocket turn is idempotent.
type CyberPolicyIsolationStore interface {
	RecordHit(
		ctx context.Context,
		userID, effectiveGroupID int64,
		upstreamAttemptID string,
	) (CyberPolicyHitDecision, error)
	CheckBlock(
		ctx context.Context,
		userID, effectiveGroupID int64,
	) (CyberPolicyBlockState, error)
	ClearBlock(
		ctx context.Context,
		userID, effectiveGroupID int64,
	) (bool, error)
}

func (s *OpenAIGatewayService) cyberPolicyIsolationStore() CyberPolicyIsolationStore {
	if s == nil || s.cache == nil {
		return nil
	}
	store, ok := s.cache.(CyberPolicyIsolationStore)
	if !ok {
		return nil
	}
	return store
}

// IsCyberPolicyGroupEnforced reports whether the actual route group selected
// for an upstream attempt is configured for Cyber handling.
func (s *OpenAIGatewayService) IsCyberPolicyGroupEnforced(ctx context.Context, effectiveGroupID int64) bool {
	if s == nil || s.settingService == nil || effectiveGroupID <= 0 {
		return false
	}
	groupID := effectiveGroupID
	return s.settingService.IsOpenAICyberPolicyEnforcedGroup(ctx, &groupID)
}

// RecordCyberPolicyHit records one upstream attempt and returns the resulting
// action. Configuration and Redis failures are deliberately fail-open: callers
// receive a zero decision and normal traffic is not blocked by control-plane or
// cache outages.
func (s *OpenAIGatewayService) RecordCyberPolicyHit(
	ctx context.Context,
	userID, effectiveGroupID int64,
	upstreamAttemptID string,
) CyberPolicyHitDecision {
	return s.recordCyberPolicyHit(
		ctx,
		userID,
		effectiveGroupID,
		upstreamAttemptID,
		s.IsCyberPolicyGroupEnforced(ctx, effectiveGroupID),
	)
}

// RecordCyberPolicyHitForEnforcedAttempt records a hit using the route-policy
// decision captured before the upstream request started. This keeps one attempt
// internally consistent if an administrator changes the selected group list
// while the request is in flight.
func (s *OpenAIGatewayService) RecordCyberPolicyHitForEnforcedAttempt(
	ctx context.Context,
	userID, effectiveGroupID int64,
	upstreamAttemptID string,
) CyberPolicyHitDecision {
	return s.recordCyberPolicyHit(ctx, userID, effectiveGroupID, upstreamAttemptID, true)
}

func (s *OpenAIGatewayService) recordCyberPolicyHit(
	ctx context.Context,
	userID, effectiveGroupID int64,
	upstreamAttemptID string,
	enforced bool,
) CyberPolicyHitDecision {
	if !enforced {
		return CyberPolicyHitDecision{}
	}
	store := s.cyberPolicyIsolationStore()
	if store == nil {
		return CyberPolicyHitDecision{Enforced: true}
	}
	decision, err := store.RecordHit(ctx, userID, effectiveGroupID, upstreamAttemptID)
	if err != nil {
		logger.LegacyPrintf(
			"service.openai_gateway",
			"cyber policy restriction record failed: user_id=%d group_id=%d err=%v",
			userID,
			effectiveGroupID,
			err,
		)
		return CyberPolicyHitDecision{Enforced: true}
	}
	decision.Enforced = true
	return decision
}

// CheckCyberPolicyBlock checks only groups selected by the runtime policy.
// Redis errors fail open and are logged for operations visibility.
func (s *OpenAIGatewayService) CheckCyberPolicyBlock(
	ctx context.Context,
	userID, effectiveGroupID int64,
) CyberPolicyBlockState {
	if !s.IsCyberPolicyGroupEnforced(ctx, effectiveGroupID) {
		return CyberPolicyBlockState{}
	}
	store := s.cyberPolicyIsolationStore()
	if store == nil {
		return CyberPolicyBlockState{}
	}
	state, err := store.CheckBlock(ctx, userID, effectiveGroupID)
	if err != nil {
		logger.LegacyPrintf(
			"service.openai_gateway",
			"cyber policy restriction check failed: user_id=%d group_id=%d err=%v",
			userID,
			effectiveGroupID,
			err,
		)
		return CyberPolicyBlockState{}
	}
	return state
}

// GetCyberPolicyRestriction returns the stored restriction without applying the
// runtime group allow-list. Administrative reads must report cache failures
// instead of silently claiming that the user is unrestricted.
func (s *OpenAIGatewayService) GetCyberPolicyRestriction(
	ctx context.Context,
	userID, effectiveGroupID int64,
) (CyberPolicyBlockState, error) {
	store := s.cyberPolicyIsolationStore()
	if store == nil {
		return CyberPolicyBlockState{}, errors.New("cyber policy restriction store is unavailable")
	}
	state, err := store.CheckBlock(ctx, userID, effectiveGroupID)
	if err != nil {
		return CyberPolicyBlockState{}, fmt.Errorf("get cyber policy restriction: %w", err)
	}
	return state, nil
}

// ClearCyberPolicyRestriction removes today's exact user-and-group restriction.
// Seen-attempt markers remain until the end of the day so a replay of the same
// upstream attempt cannot recreate the restriction after an administrator has
// cleared it. A genuinely new cyber_policy hit restricts the user again.
func (s *OpenAIGatewayService) ClearCyberPolicyRestriction(
	ctx context.Context,
	userID, effectiveGroupID int64,
) (bool, error) {
	store := s.cyberPolicyIsolationStore()
	if store == nil {
		return false, errors.New("cyber policy restriction store is unavailable")
	}
	removed, err := store.ClearBlock(ctx, userID, effectiveGroupID)
	if err != nil {
		return false, fmt.Errorf("clear cyber policy restriction: %w", err)
	}
	return removed, nil
}
