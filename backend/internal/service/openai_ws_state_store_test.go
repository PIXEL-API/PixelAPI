package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIWSRouteResolutionCache struct {
	stubGatewayCache
	accountErr error
	stringErr  error
}

type openAIWSOwnerBindErrorCache struct {
	stubGatewayCache
	bindErr error
}

func (c *openAIWSOwnerBindErrorCache) BindSessionStringImmutable(
	context.Context,
	int64,
	string,
	string,
	time.Duration,
) (string, error) {
	return "", c.bindErr
}

func (c *openAIWSRouteResolutionCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, c.accountErr
}

func (c *openAIWSRouteResolutionCache) GetSessionString(context.Context, int64, string) (string, error) {
	if c.stringErr != nil {
		return "", c.stringErr
	}
	return "", ErrGatewaySessionStringNotFound
}

func TestOpenAIWSStateStore_BindGetDeleteResponseAccount(t *testing.T) {
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.Background()
	groupID := int64(7)

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_abc", 101, time.Minute))

	accountID, err := store.GetResponseAccount(ctx, groupID, "resp_abc")
	require.NoError(t, err)
	require.Equal(t, int64(101), accountID)

	require.NoError(t, store.DeleteResponseAccount(ctx, groupID, "resp_abc"))
	accountID, err = store.GetResponseAccount(ctx, groupID, "resp_abc")
	require.NoError(t, err)
	require.Zero(t, accountID)
}

func TestOpenAIWSStateStore_ResponseAccountBindingIsolatedByGroup(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	ctx := context.Background()

	require.NoError(t, store.BindResponseAccount(ctx, 7, "resp_group_scoped", 101, time.Minute))

	accountID, err := store.GetResponseAccount(ctx, 8, "resp_group_scoped")
	require.NoError(t, err)
	require.Zero(t, accountID, "a local hot-cache hit must not leak across route groups")

	accountID, err = store.GetResponseAccount(ctx, 7, "resp_group_scoped")
	require.NoError(t, err)
	require.Equal(t, int64(101), accountID)

	require.NoError(t, store.DeleteResponseAccount(ctx, 8, "resp_group_scoped"))
	accountID, err = store.GetResponseAccount(ctx, 7, "resp_group_scoped")
	require.NoError(t, err)
	require.Equal(t, int64(101), accountID, "deleting another group's miss must not remove the owner group's binding")
}

func TestOpenAIWSStateStore_ResponseOwnerIsImmutableAndAPIKeyScoped(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	firstStore := NewOpenAIWSStateStore(cache)
	secondStore := NewOpenAIWSStateStore(cache)

	require.NoError(t, firstStore.BindResponseOwner(ctx, 77, 22, "resp_owner", 202, time.Minute))
	require.NoError(t, firstStore.BindResponseOwner(ctx, 77, 22, "resp_owner", 202, time.Minute), "same owner must be idempotent")
	require.ErrorContains(t, firstStore.BindResponseOwner(ctx, 77, 33, "resp_owner", 303, time.Minute), "owner conflict")

	owner, found, err := secondStore.GetResponseOwnerStrict(ctx, 77, "resp_owner")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, OpenAIWSResponseOwner{Version: 2, APIKeyID: 77, GroupID: 22, AccountID: 202}, owner)

	_, found, err = secondStore.GetResponseOwnerStrict(ctx, 78, "resp_owner")
	require.NoError(t, err)
	require.False(t, found, "another API key must not observe the owner")
}

func TestOpenAIWSStateStore_ResponseOwnerRequiresDurableCache(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	err := store.BindResponseOwner(context.Background(), 77, 22, "resp_owner", 202, time.Minute)
	require.ErrorContains(t, err, "durable response owner")

	_, found, err := store.GetResponseOwnerStrict(context.Background(), 77, "resp_owner")
	require.ErrorContains(t, err, "durable response owner")
	require.False(t, found)
}

func TestOpenAIWSStateStore_ResponseOwnerPropagatesCacheAndFormatErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("bind error", func(t *testing.T) {
		bindErr := errors.New("immutable owner write unavailable")
		store := NewOpenAIWSStateStore(&openAIWSOwnerBindErrorCache{bindErr: bindErr})
		err := store.BindResponseOwner(ctx, 77, 22, "resp_bind_error", 202, time.Minute)
		require.ErrorIs(t, err, bindErr)
	})

	t.Run("read error", func(t *testing.T) {
		readErr := errors.New("owner cache read unavailable")
		store := NewOpenAIWSStateStore(&openAIWSRouteResolutionCache{stringErr: readErr})
		_, found, err := store.GetResponseOwnerStrict(ctx, 77, "resp_read_error")
		require.ErrorIs(t, err, readErr)
		require.False(t, found)
	})

	t.Run("corrupt record", func(t *testing.T) {
		cache := &stubGatewayCache{stringBindings: map[string]string{
			openAIWSResponseOwnerCacheKey(77, "resp_corrupt"): `{"v":99,"api_key_id":77,"group_id":22,"account_id":202}`,
		}}
		store := NewOpenAIWSStateStore(cache)
		_, found, err := store.GetResponseOwnerStrict(ctx, 77, "resp_corrupt")
		require.ErrorContains(t, err, "response owner record is invalid")
		require.False(t, found)
	})
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupUsesV2OwnerAcrossStores(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	writerStore := NewOpenAIWSStateStore(cache)
	readerStore := NewOpenAIWSStateStore(cache)
	require.NoError(t, writerStore.BindResponseOwner(ctx, 77, 22, "resp_v2_owner", 202, time.Minute))

	svc := &OpenAIGatewayService{openaiWSStateStore: readerStore}
	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(ctx, 77, "resp_v2_owner", "", []int64{11, 33})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(22), groupID, "removed owners remain authoritative so the handler can fail route pinning")

	accountID, getErr := readerStore.GetResponseAccountStrict(ctx, 22, "resp_v2_owner")
	require.NoError(t, getErr)
	require.Equal(t, int64(202), accountID, "v2 owner must prime the local compatibility index before scheduling")
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupRejectsLegacyOwner(t *testing.T) {
	ctx := context.Background()
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindResponseAccount(ctx, 22, "resp_fallback_group", 202, time.Minute))

	svc := &OpenAIGatewayService{openaiWSStateStore: store}
	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		0,
		"resp_fallback_group",
		"",
		[]int64{11, 22, 33},
	)
	require.ErrorIs(t, err, errOpenAIWSContinuationAccountUnresolved)
	require.False(t, found)
	require.Zero(t, groupID)

	groupID, found, err = svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		0,
		"resp_missing",
		"",
		[]int64{11, 22, 33},
	)
	require.NoError(t, err)
	require.False(t, found)
	require.Zero(t, groupID)
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupRejectsResponseSessionWithoutOwner(t *testing.T) {
	ctx := context.Background()
	sessionHash := "shared_session"
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindSessionResponse(ctx, 22, sessionHash, "resp_target", time.Minute))
	cache := &stubGatewayCache{sessionBindings: map[string]int64{"openai:" + sessionHash: 202}}

	svc := &OpenAIGatewayService{cache: cache, openaiWSStateStore: store}
	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		0,
		"resp_target",
		sessionHash,
		[]int64{11, 22},
	)
	require.ErrorIs(t, err, errOpenAIWSContinuationAccountUnresolved)
	require.False(t, found)
	require.Zero(t, groupID)
	boundAccountID, getErr := store.GetResponseAccountStrict(ctx, 22, "resp_target")
	require.NoError(t, getErr)
	require.Zero(t, boundAccountID, "mutable session stickiness must not manufacture historical response ownership")
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupDoesNotBindArbitraryPreviousResponse(t *testing.T) {
	ctx := context.Background()
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindSessionResponse(ctx, 22, "owner_session", "resp_latest", time.Minute))
	cache := &stubGatewayCache{sessionBindings: map[string]int64{"openai:owner_session": 202}}

	svc := &OpenAIGatewayService{cache: cache, openaiWSStateStore: store}
	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		0,
		"resp_arbitrary",
		"owner_session",
		[]int64{11, 22},
	)
	require.NoError(t, err)
	require.False(t, found)
	require.Zero(t, groupID)
	boundAccountID, getErr := store.GetResponseAccountStrict(ctx, 22, "resp_arbitrary")
	require.NoError(t, getErr)
	require.Zero(t, boundAccountID)
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupSupportsUngroupedNamespace(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	require.NoError(t, store.BindResponseOwner(ctx, 77, 0, "resp_ungrouped", 303, time.Minute))

	svc := &OpenAIGatewayService{openaiWSStateStore: store}
	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		77,
		"resp_ungrouped",
		"ungrouped_session",
		[]int64{0},
	)
	require.NoError(t, err)
	require.True(t, found)
	require.Zero(t, groupID)
	boundAccountID, getErr := store.GetResponseAccountStrict(ctx, 0, "resp_ungrouped")
	require.NoError(t, getErr)
	require.Equal(t, int64(303), boundAccountID)
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupRejectsSessionOnlyRoute(t *testing.T) {
	ctx := context.Background()
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindSessionResponse(ctx, 22, "owner_without_account", "resp_without_account", time.Minute))
	svc := &OpenAIGatewayService{cache: &stubGatewayCache{}, openaiWSStateStore: store}

	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		0,
		"resp_without_account",
		"different_request_session",
		[]int64{11, 22},
	)
	require.ErrorIs(t, err, errOpenAIWSContinuationAccountUnresolved)
	require.False(t, found)
	require.Zero(t, groupID)
	boundAccountID, getErr := store.GetResponseAccountStrict(ctx, 22, "resp_without_account")
	require.NoError(t, getErr)
	require.Zero(t, boundAccountID)
}

func TestOpenAIGatewayService_ValidateOpenAIWSContinuationAccount(t *testing.T) {
	ctx := context.Background()
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindResponseAccount(ctx, 22, "resp_validate", 202, time.Minute))
	svc := &OpenAIGatewayService{openaiWSStateStore: store}

	require.NoError(t, svc.ValidateOpenAIWSContinuationAccount(ctx, 0, 22, "resp_validate", 202))
	require.ErrorContains(t, svc.ValidateOpenAIWSContinuationAccount(ctx, 0, 22, "resp_validate", 203), "continuation account mismatch")
	require.NoError(t, svc.ValidateOpenAIWSContinuationAccount(ctx, 0, 22, "resp_missing", 202))
	require.NoError(t, store.BindSessionResponse(ctx, 22, "session_partial", "resp_partial", time.Minute))
	require.ErrorIs(t, svc.ValidateOpenAIWSContinuationAccount(ctx, 0, 22, "resp_partial", 202), errOpenAIWSContinuationAccountUnresolved)
	require.NoError(t, svc.ValidateOpenAIWSContinuationAccount(ctx, 0, 22, "", 203))
}

func TestOpenAIGatewayService_ValidateOpenAIWSContinuationAccountUsesV2Owner(t *testing.T) {
	ctx := context.Background()
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	require.NoError(t, store.BindResponseOwner(ctx, 77, 22, "resp_validate_v2", 202, time.Minute))
	svc := &OpenAIGatewayService{openaiWSStateStore: store}

	require.NoError(t, svc.ValidateOpenAIWSContinuationAccount(ctx, 77, 22, "resp_validate_v2", 202))
	mismatchErr := svc.ValidateOpenAIWSContinuationAccount(ctx, 77, 22, "resp_validate_v2", 203)
	require.True(t, IsOpenAIWSContinuationPermanentError(mismatchErr))

	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, newOpenAIWSContinuationClientCloseError(mismatchErr), &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
	require.Contains(t, closeErr.Reason(), "start a new conversation")
}

func TestOpenAIGatewayService_RequireOpenAIWSContinuationAccount(t *testing.T) {
	ctx := context.Background()
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindResponseAccount(ctx, 22, "resp_share", 202, time.Minute))
	svc := &OpenAIGatewayService{openaiWSStateStore: store}

	require.NoError(t, svc.RequireOpenAIWSContinuationAccount(ctx, 0, 22, "resp_share", 202))
	require.ErrorContains(t, svc.RequireOpenAIWSContinuationAccount(ctx, 0, 22, "resp_share", 203), "continuation account mismatch")
	require.ErrorIs(t, svc.RequireOpenAIWSContinuationAccount(ctx, 0, 22, "resp_share_missing", 202), errOpenAIWSContinuationAccountUnresolved)
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupRejectsAmbiguousSession(t *testing.T) {
	ctx := context.Background()
	store := NewOpenAIWSStateStore(nil)
	require.NoError(t, store.BindSessionResponse(ctx, 11, "shared_session", "resp_group_11", time.Minute))
	require.NoError(t, store.BindSessionResponse(ctx, 22, "shared_session", "resp_group_22", time.Minute))

	svc := &OpenAIGatewayService{openaiWSStateStore: store}
	groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(
		ctx,
		0,
		"",
		"shared_session",
		[]int64{11, 22},
	)
	require.ErrorContains(t, err, "session state exists in multiple route groups")
	require.False(t, found)
	require.Zero(t, groupID)
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupRejectsAmbiguousResponseOwnership(t *testing.T) {
	ctx := context.Background()

	t.Run("response account", func(t *testing.T) {
		store := NewOpenAIWSStateStore(nil)
		require.NoError(t, store.BindResponseAccount(ctx, 11, "resp_ambiguous_account", 101, time.Minute))
		require.NoError(t, store.BindResponseAccount(ctx, 22, "resp_ambiguous_account", 202, time.Minute))

		svc := &OpenAIGatewayService{openaiWSStateStore: store}
		groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(ctx, 0, "resp_ambiguous_account", "", []int64{11, 22})
		require.ErrorContains(t, err, "response account exists in multiple route groups")
		require.False(t, found)
		require.Zero(t, groupID)
	})

	t.Run("response session is not account ownership", func(t *testing.T) {
		store := NewOpenAIWSStateStore(nil)
		require.NoError(t, store.BindSessionResponse(ctx, 11, "owner_11", "resp_ambiguous_session", time.Minute))
		require.NoError(t, store.BindSessionResponse(ctx, 22, "owner_22", "resp_ambiguous_session", time.Minute))

		svc := &OpenAIGatewayService{openaiWSStateStore: store}
		groupID, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(ctx, 0, "resp_ambiguous_session", "", []int64{11, 22})
		require.ErrorContains(t, err, "response session exists in multiple route groups")
		require.False(t, found)
		require.Zero(t, groupID)
	})
}

func TestOpenAIGatewayService_ResolveContinuationRouteGroupPropagatesCacheErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("response binding lookup", func(t *testing.T) {
		lookupErr := errors.New("response binding cache unavailable")
		store := NewOpenAIWSStateStore(&openAIWSRouteResolutionCache{accountErr: lookupErr})
		svc := &OpenAIGatewayService{openaiWSStateStore: store}

		_, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(ctx, 0, "resp_lookup_error", "", []int64{11})
		require.ErrorIs(t, err, lookupErr)
		require.False(t, found)
	})

	t.Run("session binding lookup", func(t *testing.T) {
		lookupErr := errors.New("session binding cache unavailable")
		store := NewOpenAIWSStateStore(&openAIWSRouteResolutionCache{stringErr: lookupErr})
		svc := &OpenAIGatewayService{openaiWSStateStore: store}

		_, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(ctx, 0, "", "session_lookup_error", []int64{11})
		require.ErrorIs(t, err, lookupErr)
		require.False(t, found)
	})

	t.Run("response session lookup", func(t *testing.T) {
		lookupErr := errors.New("response session cache unavailable")
		store := NewOpenAIWSStateStore(&openAIWSRouteResolutionCache{stringErr: lookupErr})
		svc := &OpenAIGatewayService{openaiWSStateStore: store}

		_, found, err := svc.ResolveOpenAIWSContinuationRouteGroup(ctx, 0, "resp_session_lookup_error", "", []int64{11})
		require.ErrorIs(t, err, lookupErr)
		require.False(t, found)
	})
}

func TestOpenAIWSStateStore_ResponseConnTTL(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	store.BindResponseConn("resp_conn", "conn_1", 30*time.Millisecond)

	connID, ok := store.GetResponseConn("resp_conn")
	require.True(t, ok)
	require.Equal(t, "conn_1", connID)

	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetResponseConn("resp_conn")
	require.False(t, ok)
}

func TestOpenAIWSStateStore_SessionTurnStateTTL(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	store.BindSessionTurnState(9, "session_hash_1", "turn_state_1", 30*time.Millisecond)

	state, ok := store.GetSessionTurnState(9, "session_hash_1")
	require.True(t, ok)
	require.Equal(t, "turn_state_1", state)

	// group 隔离
	_, ok = store.GetSessionTurnState(10, "session_hash_1")
	require.False(t, ok)

	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetSessionTurnState(9, "session_hash_1")
	require.False(t, ok)
}

func TestOpenAIWSStateStore_SessionConnTTL(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	store.BindSessionConn(9, "session_hash_conn_1", "conn_1", 30*time.Millisecond)

	connID, ok := store.GetSessionConn(9, "session_hash_conn_1")
	require.True(t, ok)
	require.Equal(t, "conn_1", connID)

	// group 隔离
	_, ok = store.GetSessionConn(10, "session_hash_conn_1")
	require.False(t, ok)

	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetSessionConn(9, "session_hash_conn_1")
	require.False(t, ok)
}

func TestOpenAIWSStateStore_BindSessionResponse(t *testing.T) {
	cache := &stubGatewayCache{stringBindings: map[string]string{}}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.Background()
	groupID := int64(19)
	sessionHash := "session_hash_chain_1"
	responseID := "resp_chain_5"

	require.NoError(t, store.BindSessionResponse(ctx, groupID, sessionHash, responseID, time.Minute))

	latest, err := store.GetSessionLatestResponse(ctx, groupID, sessionHash)
	require.NoError(t, err)
	require.Equal(t, responseID, latest)

	ownerSession, err := store.GetResponseSession(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Equal(t, sessionHash, ownerSession)

	cachedLatest := cache.stringBindings[openAIWSSessionLatestResponseCacheKey(sessionHash)]
	cachedOwner := cache.stringBindings[openAIWSResponseSessionCacheKey(responseID)]
	require.Equal(t, responseID, cachedLatest)
	require.Equal(t, sessionHash, cachedOwner)
}

func TestOpenAIWSStateStore_GetSessionLatestResponse_NoStaleAfterCacheMiss(t *testing.T) {
	cache := &stubGatewayCache{stringBindings: map[string]string{}}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.Background()
	groupID := int64(22)
	sessionHash := "session_hash_latest_stale"

	require.NoError(t, store.BindSessionResponse(ctx, groupID, sessionHash, "resp_latest_1", time.Minute))
	latest, err := store.GetSessionLatestResponse(ctx, groupID, sessionHash)
	require.NoError(t, err)
	require.Equal(t, "resp_latest_1", latest)

	delete(cache.stringBindings, openAIWSSessionLatestResponseCacheKey(sessionHash))
	latest, err = store.GetSessionLatestResponse(ctx, groupID, sessionHash)
	require.NoError(t, err)
	require.Empty(t, latest, "多实例 latest_response_id 读取应以 Redis 为准，避免本机旧值误纠偏")
}

func TestOpenAIWSStateStore_BindSessionResponse_LocalFallback(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	ctx := context.Background()
	groupID := int64(20)

	require.NoError(t, store.BindSessionResponse(ctx, groupID, "session_hash_local", "resp_local", time.Minute))

	latest, err := store.GetSessionLatestResponse(ctx, groupID, "session_hash_local")
	require.NoError(t, err)
	require.Equal(t, "resp_local", latest)

	ownerSession, err := store.GetResponseSession(ctx, groupID, "resp_local")
	require.NoError(t, err)
	require.Equal(t, "session_hash_local", ownerSession)
}

func TestOpenAIGatewayService_RepairOpenAIWSPreviousResponseIDForSession_UsesPreviousOwnerSession(t *testing.T) {
	ctx := context.Background()
	groupID := int64(21)
	cache := &stubGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	store := svc.getOpenAIWSStateStore()

	require.NoError(t, store.BindSessionResponse(ctx, groupID, "session_chain_owner", "resp_chain_3", time.Minute))
	require.NoError(t, store.BindSessionResponse(ctx, groupID, "session_chain_owner", "resp_chain_5", time.Minute))

	payload := []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_chain_3","input":"sixth"}`)
	repairedPayload, repairedPreviousResponseID, repaired := svc.RepairOpenAIWSPreviousResponseIDForSession(
		ctx,
		groupID,
		"different_content_hash_session",
		payload,
		true,
	)

	require.True(t, repaired)
	require.Equal(t, "resp_chain_5", repairedPreviousResponseID)
	require.Equal(t, "resp_chain_5", gjson.GetBytes(repairedPayload, "previous_response_id").String())
}

func TestOpenAIWSStateStore_GetResponseAccount_NoStaleAfterCacheMiss(t *testing.T) {
	cache := &stubGatewayCache{sessionBindings: map[string]int64{}}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.Background()
	groupID := int64(17)
	responseID := "resp_cache_stale"
	cacheKey := openAIWSResponseAccountCacheKey(responseID)

	cache.sessionBindings[cacheKey] = 501
	accountID, err := store.GetResponseAccount(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Equal(t, int64(501), accountID)

	delete(cache.sessionBindings, cacheKey)
	accountID, err = store.GetResponseAccount(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Zero(t, accountID, "上游缓存失效后不应继续命中本地陈旧映射")
}

func TestOpenAIWSStateStore_MaybeCleanupRemovesExpiredIncrementally(t *testing.T) {
	raw := NewOpenAIWSStateStore(nil)
	store, ok := raw.(*defaultOpenAIWSStateStore)
	require.True(t, ok)

	expiredAt := time.Now().Add(-time.Minute)
	total := 2048
	store.responseToConnMu.Lock()
	for i := 0; i < total; i++ {
		store.responseToConn[fmt.Sprintf("resp_%d", i)] = openAIWSConnBinding{
			connID:    "conn_incremental",
			expiresAt: expiredAt,
		}
	}
	store.responseToConnMu.Unlock()

	store.lastCleanupUnixNano.Store(time.Now().Add(-2 * openAIWSStateStoreCleanupInterval).UnixNano())
	store.maybeCleanup()

	store.responseToConnMu.RLock()
	remainingAfterFirst := len(store.responseToConn)
	store.responseToConnMu.RUnlock()
	require.Less(t, remainingAfterFirst, total, "单轮 cleanup 应至少有进展")
	require.Greater(t, remainingAfterFirst, 0, "增量清理不要求单轮清空全部键")

	for i := 0; i < 8; i++ {
		store.lastCleanupUnixNano.Store(time.Now().Add(-2 * openAIWSStateStoreCleanupInterval).UnixNano())
		store.maybeCleanup()
	}

	store.responseToConnMu.RLock()
	remaining := len(store.responseToConn)
	store.responseToConnMu.RUnlock()
	require.Zero(t, remaining, "多轮 cleanup 后应逐步清空全部过期键")
}

func TestEnsureBindingCapacity_EvictsOneWhenMapIsFull(t *testing.T) {
	bindings := map[string]int{
		"a": 1,
		"b": 2,
	}

	ensureBindingCapacity(bindings, "c", 2)
	bindings["c"] = 3

	require.Len(t, bindings, 2)
	require.Equal(t, 3, bindings["c"])
}

func TestEnsureBindingCapacity_DoesNotEvictWhenUpdatingExistingKey(t *testing.T) {
	bindings := map[string]int{
		"a": 1,
		"b": 2,
	}

	ensureBindingCapacity(bindings, "a", 2)
	bindings["a"] = 9

	require.Len(t, bindings, 2)
	require.Equal(t, 9, bindings["a"])
}

type openAIWSStateStoreTimeoutProbeCache struct {
	setHasDeadline    bool
	getHasDeadline    bool
	deleteHasDeadline bool
	setStringDeadline bool
	getStringDeadline bool
	setDeadlineDelta  time.Duration
	getDeadlineDelta  time.Duration
	delDeadlineDelta  time.Duration
}

func (c *openAIWSStateStoreTimeoutProbeCache) GetSessionAccountID(ctx context.Context, _ int64, _ string) (int64, error) {
	if deadline, ok := ctx.Deadline(); ok {
		c.getHasDeadline = true
		c.getDeadlineDelta = time.Until(deadline)
	}
	return 123, nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) SetSessionAccountID(ctx context.Context, _ int64, _ string, _ int64, _ time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok {
		c.setHasDeadline = true
		c.setDeadlineDelta = time.Until(deadline)
	}
	return errors.New("set failed")
}

func (c *openAIWSStateStoreTimeoutProbeCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) DeleteSessionAccountID(ctx context.Context, _ int64, _ string) error {
	if deadline, ok := ctx.Deadline(); ok {
		c.deleteHasDeadline = true
		c.delDeadlineDelta = time.Until(deadline)
	}
	return nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) GetSessionString(ctx context.Context, _ int64, _ string) (string, error) {
	if _, ok := ctx.Deadline(); ok {
		c.getStringDeadline = true
	}
	return "", errors.New("not found")
}

func (c *openAIWSStateStoreTimeoutProbeCache) SetSessionString(ctx context.Context, _ int64, _ string, _ string, _ time.Duration) error {
	if _, ok := ctx.Deadline(); ok {
		c.setStringDeadline = true
	}
	return nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) DeleteSessionString(context.Context, int64, string) error {
	return nil
}

func TestOpenAIWSStateStore_RedisOpsUseShortTimeout(t *testing.T) {
	probe := &openAIWSStateStoreTimeoutProbeCache{}
	store := NewOpenAIWSStateStore(probe)
	ctx := context.Background()
	groupID := int64(5)

	err := store.BindResponseAccount(ctx, groupID, "resp_timeout_probe", 11, time.Minute)
	require.Error(t, err)

	accountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_timeout_probe")
	require.NoError(t, getErr)
	require.Equal(t, int64(11), accountID, "本地缓存命中应优先返回已绑定账号")

	require.NoError(t, store.DeleteResponseAccount(ctx, groupID, "resp_timeout_probe"))

	require.True(t, probe.setHasDeadline, "SetSessionAccountID 应携带独立超时上下文")
	require.True(t, probe.deleteHasDeadline, "DeleteSessionAccountID 应携带独立超时上下文")
	require.False(t, probe.getHasDeadline, "GetSessionAccountID 本用例应由本地缓存命中，不触发 Redis 读取")
	require.Greater(t, probe.setDeadlineDelta, 2*time.Second)
	require.LessOrEqual(t, probe.setDeadlineDelta, 3*time.Second)
	require.Greater(t, probe.delDeadlineDelta, 2*time.Second)
	require.LessOrEqual(t, probe.delDeadlineDelta, 3*time.Second)

	probe2 := &openAIWSStateStoreTimeoutProbeCache{}
	store2 := NewOpenAIWSStateStore(probe2)
	accountID2, err2 := store2.GetResponseAccount(ctx, groupID, "resp_cache_only")
	require.NoError(t, err2)
	require.Equal(t, int64(123), accountID2)
	require.True(t, probe2.getHasDeadline, "GetSessionAccountID 在缓存未命中时应携带独立超时上下文")
	require.Greater(t, probe2.getDeadlineDelta, 2*time.Second)
	require.LessOrEqual(t, probe2.getDeadlineDelta, 3*time.Second)
}

func TestWithOpenAIWSStateStoreRedisTimeout_WithParentContext(t *testing.T) {
	ctx, cancel := withOpenAIWSStateStoreRedisTimeout(context.Background())
	defer cancel()
	require.NotNil(t, ctx)
	_, ok := ctx.Deadline()
	require.True(t, ok, "应附加短超时")
}
