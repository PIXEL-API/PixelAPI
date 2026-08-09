package service

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const opsCyberPolicyKey = "ops_cyber_policy"
const opsCyberPolicyAttemptKey = "ops_cyber_policy_attempt"
const openAICyberPolicyDefaultMessage = "Request blocked by upstream cyber-security policy"

type openAICyberPolicyAttempt struct {
	ID       string
	Enforced bool
}

type CyberPolicyMark struct {
	Code              string
	Message           string
	Body              string
	UpstreamAttemptID string
	UpstreamStatus    int
	UpstreamInTok     int
	UpstreamOutTok    int
}

// BeginOpenAIUpstreamAttempt binds both the server-generated identifier and the
// effective route's Cyber enforcement decision to the next HTTP attempt or
// logical WebSocket turn. Transport-level retries within the same WebSocket
// turn deliberately reuse the identifier so one client turn cannot advance the
// daily cyber-policy count more than once.
func BeginOpenAIUpstreamAttempt(c *gin.Context, upstreamAttemptID string, enforced bool) {
	if c == nil {
		return
	}
	c.Set(opsCyberPolicyAttemptKey, openAICyberPolicyAttempt{
		ID:       strings.TrimSpace(upstreamAttemptID),
		Enforced: enforced,
	})
	// A mark belongs to exactly one routed attempt. Clearing it here prevents an
	// earlier selected group from changing the ordinary error path of a later
	// unselected group.
	c.Set(opsCyberPolicyKey, nil)
}

func currentOpenAIUpstreamAttempt(c *gin.Context) openAICyberPolicyAttempt {
	if c == nil {
		return openAICyberPolicyAttempt{}
	}
	v, ok := c.Get(opsCyberPolicyAttemptKey)
	if !ok {
		return openAICyberPolicyAttempt{}
	}
	attempt, _ := v.(openAICyberPolicyAttempt)
	attempt.ID = strings.TrimSpace(attempt.ID)
	return attempt
}

func currentOpenAIUpstreamAttemptID(c *gin.Context) string {
	return currentOpenAIUpstreamAttempt(c).ID
}

// IsOpenAICyberPolicyEnforcedForCurrentAttempt reports whether the actual
// routed group selected for the current attempt is configured for Cyber
// handling. Missing attempt state is intentionally not enforced: every gateway
// entry point must bind the effective group before forwarding upstream.
func IsOpenAICyberPolicyEnforcedForCurrentAttempt(c *gin.Context) bool {
	attempt := currentOpenAIUpstreamAttempt(c)
	return attempt.ID != "" && attempt.Enforced
}

func MarkOpsCyberPolicy(c *gin.Context, mark CyberPolicyMark) {
	if c == nil || !IsOpenAICyberPolicyEnforcedForCurrentAttempt(c) {
		return
	}
	attemptID := currentOpenAIUpstreamAttemptID(c)
	if existing := GetOpsCyberPolicy(c); existing != nil &&
		(attemptID == "" || strings.TrimSpace(existing.UpstreamAttemptID) == attemptID) {
		return
	}
	mark.Code = "cyber_policy"
	mark.Message = strings.TrimSpace(mark.Message)
	mark.Body = strings.TrimSpace(mark.Body)
	mark.UpstreamAttemptID = attemptID
	c.Set(opsCyberPolicyKey, &mark)

	// A cyber_policy response can arrive in-band on an HTTP 200 SSE/WebSocket
	// transport. Persist explicit upstream error context here so every enforced
	// protocol reaches the Ops logger without relying on protocol-specific error
	// branches. The stable prefix also makes these records directly searchable.
	// Do not append an event here: ordinary HTTP error paths already append their
	// account-scoped event, and doing both would count one upstream response twice.
	opsMessage := "cyber_policy: " + openAICyberPolicyClientMessage(mark.Message)
	setOpsUpstreamError(c, mark.UpstreamStatus, opsMessage, mark.Body)
}

func GetOpsCyberPolicy(c *gin.Context) *CyberPolicyMark {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(opsCyberPolicyKey); ok {
		if m, ok := v.(*CyberPolicyMark); ok && m != nil {
			return m
		}
	}
	return nil
}

// GetOpsCyberPolicyForAttempt returns a mark only when it belongs to the
// supplied server-generated attempt. A stale mark from an earlier account or
// group retry must never be attributed to the current route.
func GetOpsCyberPolicyForAttempt(c *gin.Context, upstreamAttemptID string) *CyberPolicyMark {
	upstreamAttemptID = strings.TrimSpace(upstreamAttemptID)
	if upstreamAttemptID == "" {
		return nil
	}
	mark := GetOpsCyberPolicy(c)
	if mark == nil || strings.TrimSpace(mark.UpstreamAttemptID) != upstreamAttemptID {
		return nil
	}
	return mark
}

func markOpsCyberPolicyPayload(c *gin.Context, payload []byte, upstreamStatus, upstreamInTok, upstreamOutTok int) bool {
	hit, code, message := detectOpenAICyberPolicyForCurrentAttempt(c, payload)
	if !hit {
		return false
	}
	MarkOpsCyberPolicy(c, CyberPolicyMark{
		Code:           code,
		Message:        message,
		Body:           truncateString(string(payload), 4096),
		UpstreamStatus: upstreamStatus,
		UpstreamInTok:  upstreamInTok,
		UpstreamOutTok: upstreamOutTok,
	})
	return true
}

func detectOpenAICyberPolicyForCurrentAttempt(c *gin.Context, payload []byte) (bool, string, string) {
	if !IsOpenAICyberPolicyEnforcedForCurrentAttempt(c) {
		return false, "", ""
	}
	return detectOpenAICyberPolicy(payload)
}

func openAICyberPolicyClientMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return openAICyberPolicyDefaultMessage
	}
	return msg
}

func detectOpenAICyberPolicy(payload []byte) (bool, string, string) {
	code := gjson.GetBytes(payload, "error.code").String()
	if code == "" {
		code = gjson.GetBytes(payload, "response.error.code").String()
	}
	if !strings.EqualFold(strings.TrimSpace(code), "cyber_policy") {
		return false, "", ""
	}
	msg := gjson.GetBytes(payload, "error.message").String()
	if msg == "" {
		msg = gjson.GetBytes(payload, "response.error.message").String()
	}
	return true, "cyber_policy", strings.TrimSpace(msg)
}
