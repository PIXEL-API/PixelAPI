package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerCyberPolicyCache struct {
	recordedUserID  int64
	recordedGroupID int64
}

func (c *handlerCyberPolicyCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, service.ErrGatewaySessionStringNotFound
}

func (c *handlerCyberPolicyCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (c *handlerCyberPolicyCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *handlerCyberPolicyCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *handlerCyberPolicyCache) GetSessionString(context.Context, int64, string) (string, error) {
	return "", service.ErrGatewaySessionStringNotFound
}

func (c *handlerCyberPolicyCache) SetSessionString(context.Context, int64, string, string, time.Duration) error {
	return nil
}

func (c *handlerCyberPolicyCache) DeleteSessionString(context.Context, int64, string) error {
	return nil
}

func (c *handlerCyberPolicyCache) RecordHit(
	_ context.Context,
	userID, effectiveGroupID int64,
	_ string,
) (service.CyberPolicyHitDecision, error) {
	c.recordedUserID = userID
	c.recordedGroupID = effectiveGroupID
	return service.CyberPolicyHitDecision{
		HitSequence:  1,
		Action:       service.CyberPolicyBlockScopeUserGroupDay,
		BlockedUntil: time.Now().Add(time.Hour),
	}, nil
}

func (c *handlerCyberPolicyCache) CheckBlock(
	_ context.Context,
	userID, effectiveGroupID int64,
) (service.CyberPolicyBlockState, error) {
	blocked := userID == c.recordedUserID && effectiveGroupID == c.recordedGroupID
	state := service.CyberPolicyBlockState{Blocked: blocked}
	if blocked {
		state.Scope = service.CyberPolicyBlockScopeUserGroupDay
	}
	return state, nil
}

func (c *handlerCyberPolicyCache) ClearBlock(context.Context, int64, int64) (bool, error) {
	return false, nil
}

func TestRecordCyberPolicyHitUsesUserIDSoAnotherAPIKeyCannotBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &handlerCyberPolicyCache{}
	gatewayService := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil,
		cache,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	h := &OpenAIGatewayHandler{gatewayService: gatewayService}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	service.BeginOpenAIUpstreamAttempt(c, "attempt-1", true)
	service.MarkOpsCyberPolicy(c, service.CyberPolicyMark{Message: "blocked"})

	groupID := int64(1198)
	firstKey := &service.APIKey{ID: 901, UserID: 445, GroupID: &groupID}
	secondKey := &service.APIKey{ID: 902, UserID: 445, GroupID: &groupID}
	otherUserKey := &service.APIKey{ID: 903, UserID: 446, GroupID: &groupID}

	hit, decision := h.recordCyberPolicyHitForAttempt(context.Background(), c, firstKey, "attempt-1")
	require.True(t, hit)
	require.Equal(t, service.CyberPolicyBlockScopeUserGroupDay, decision.Action)
	require.Equal(t, firstKey.UserID, cache.recordedUserID)
	require.NotEqual(t, firstKey.ID, cache.recordedUserID)

	secondKeyState, err := cache.CheckBlock(context.Background(), secondKey.UserID, groupID)
	require.NoError(t, err)
	require.True(t, secondKeyState.Blocked, "another key owned by the same user must share the restriction")
	otherUserState, err := cache.CheckBlock(context.Background(), otherUserKey.UserID, groupID)
	require.NoError(t, err)
	require.False(t, otherUserState.Blocked)
}
