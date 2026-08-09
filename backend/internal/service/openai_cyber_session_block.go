package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type CyberPolicyBlockScope string

const (
	CyberPolicyBlockScopeNone             CyberPolicyBlockScope = ""
	CyberPolicyBlockScopeSession          CyberPolicyBlockScope = "session"
	CyberPolicyBlockScopeAPIKeyGroupShort CyberPolicyBlockScope = "api_key_group_short"
	CyberPolicyBlockScopeAPIKeyGroupDay   CyberPolicyBlockScope = "api_key_group_day"

	CyberPolicyFirstHitBlockDuration  = 5 * time.Minute
	CyberPolicySecondHitBlockDuration = 15 * time.Minute
)

// CyberPolicyHitDecision describes the isolation action caused by one unique
// upstream cyber_policy response. HitSequence is scoped to an API key, its
// effective group, and the configured local calendar day.
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

// CyberPolicyBlockState is the effective pre-request isolation state. A missing
// explicit session hash still checks API-key/group short and day blocks.
type CyberPolicyBlockState struct {
	Blocked      bool
	Scope        CyberPolicyBlockScope
	RetryAfter   time.Duration
	BlockedUntil time.Time
}

// CyberPolicyIsolationStore atomically records and checks group-aware cyber
// isolation. upstreamAttemptID is required so repeated handling of the same
// routed HTTP attempt or logical WebSocket turn cannot advance the daily hit
// sequence more than once.
type CyberPolicyIsolationStore interface {
	RecordHit(
		ctx context.Context,
		apiKeyID, effectiveGroupID int64,
		sessionHash, upstreamAttemptID string,
	) (CyberPolicyHitDecision, error)
	CheckBlock(
		ctx context.Context,
		apiKeyID, effectiveGroupID int64,
		sessionHash string,
	) (CyberPolicyBlockState, error)
}

// CyberPolicyGroupSessionHash returns a privacy-preserving session fingerprint
// scoped to the API key and the effective route group. Empty means that no
// explicit session signal was supplied and group-level short-block fallback
// must be used by RecordHit.
func CyberPolicyGroupSessionHash(apiKeyID, effectiveGroupID int64, c *gin.Context, body []byte) string {
	if apiKeyID <= 0 || effectiveGroupID <= 0 {
		return ""
	}
	raw := cyberPolicyExplicitSessionID(c, body)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("k%d:g%d:%s", apiKeyID, effectiveGroupID, raw)))
	return hex.EncodeToString(sum[:])
}

func cyberPolicyExplicitSessionID(c *gin.Context, body []byte) string {
	if raw := explicitOpenAISessionID(c, body); raw != "" {
		return raw
	}
	if !isOpenAIMessagesRequest(c) || len(body) == 0 {
		return ""
	}

	raw := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if raw == "" {
		return ""
	}
	if parsed := ParseMetadataUserID(raw); parsed != nil {
		if sessionID := strings.TrimSpace(parsed.SessionID); sessionID != "" {
			return sessionID
		}
	}
	// Third-party Anthropic-compatible clients may use an opaque stable value
	// instead of the Claude Code metadata format. Preserve that compatibility
	// without promoting metadata.user_id to every OpenAI endpoint.
	return raw
}

func isOpenAIMessagesRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	requestPath := strings.TrimSuffix(strings.TrimSpace(c.Request.URL.Path), "/")
	return requestPath == "/v1/messages" || strings.HasSuffix(requestPath, "/v1/messages")
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
	apiKeyID, effectiveGroupID int64,
	sessionHash, upstreamAttemptID string,
) CyberPolicyHitDecision {
	return s.recordCyberPolicyHit(
		ctx,
		apiKeyID,
		effectiveGroupID,
		sessionHash,
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
	apiKeyID, effectiveGroupID int64,
	sessionHash, upstreamAttemptID string,
) CyberPolicyHitDecision {
	return s.recordCyberPolicyHit(ctx, apiKeyID, effectiveGroupID, sessionHash, upstreamAttemptID, true)
}

func (s *OpenAIGatewayService) recordCyberPolicyHit(
	ctx context.Context,
	apiKeyID, effectiveGroupID int64,
	sessionHash, upstreamAttemptID string,
	enforced bool,
) CyberPolicyHitDecision {
	if !enforced {
		return CyberPolicyHitDecision{}
	}
	store := s.cyberPolicyIsolationStore()
	if store == nil {
		return CyberPolicyHitDecision{Enforced: true}
	}
	decision, err := store.RecordHit(ctx, apiKeyID, effectiveGroupID, sessionHash, upstreamAttemptID)
	if err != nil {
		logger.LegacyPrintf(
			"service.openai_gateway",
			"cyber policy isolation record failed: api_key_id=%d group_id=%d err=%v",
			apiKeyID,
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
	apiKeyID, effectiveGroupID int64,
	sessionHash string,
) CyberPolicyBlockState {
	if !s.IsCyberPolicyGroupEnforced(ctx, effectiveGroupID) {
		return CyberPolicyBlockState{}
	}
	store := s.cyberPolicyIsolationStore()
	if store == nil {
		return CyberPolicyBlockState{}
	}
	state, err := store.CheckBlock(ctx, apiKeyID, effectiveGroupID, sessionHash)
	if err != nil {
		logger.LegacyPrintf(
			"service.openai_gateway",
			"cyber policy isolation check failed: api_key_id=%d group_id=%d err=%v",
			apiKeyID,
			effectiveGroupID,
			err,
		)
		return CyberPolicyBlockState{}
	}
	return state
}
