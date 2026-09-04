package service

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type openAIHTTPContinuationTestCache struct {
	accountBindings map[string]int64
	stringBindings  map[string]string

	getStringErr  error
	setAccountErr error
	bindStringErr error
}

func (c *openAIHTTPContinuationTestCache) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (c *openAIHTTPContinuationTestCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	if accountID, ok := c.accountBindings[c.key(groupID, sessionHash)]; ok {
		return accountID, nil
	}
	return 0, redis.Nil
}

func (c *openAIHTTPContinuationTestCache) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, _ time.Duration) error {
	if c.setAccountErr != nil {
		return c.setAccountErr
	}
	if c.accountBindings == nil {
		c.accountBindings = make(map[string]int64)
	}
	c.accountBindings[c.key(groupID, sessionHash)] = accountID
	return nil
}

func (c *openAIHTTPContinuationTestCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *openAIHTTPContinuationTestCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.accountBindings, c.key(groupID, sessionHash))
	return nil
}

func (c *openAIHTTPContinuationTestCache) GetSessionString(_ context.Context, groupID int64, sessionHash string) (string, error) {
	if c.getStringErr != nil {
		return "", c.getStringErr
	}
	if value, ok := c.stringBindings[c.key(groupID, sessionHash)]; ok {
		return value, nil
	}
	return "", ErrGatewaySessionStringNotFound
}

func (c *openAIHTTPContinuationTestCache) SetSessionString(_ context.Context, groupID int64, sessionHash, value string, _ time.Duration) error {
	if c.stringBindings == nil {
		c.stringBindings = make(map[string]string)
	}
	c.stringBindings[c.key(groupID, sessionHash)] = value
	return nil
}

func (c *openAIHTTPContinuationTestCache) DeleteSessionString(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.stringBindings, c.key(groupID, sessionHash))
	return nil
}

func (c *openAIHTTPContinuationTestCache) BindSessionStringImmutable(
	_ context.Context,
	groupID int64,
	sessionHash, value string,
	_ time.Duration,
) (string, error) {
	if c.bindStringErr != nil {
		return "", c.bindStringErr
	}
	if c.stringBindings == nil {
		c.stringBindings = make(map[string]string)
	}
	key := c.key(groupID, sessionHash)
	if stored, ok := c.stringBindings[key]; ok {
		return stored, nil
	}
	c.stringBindings[key] = value
	return value, nil
}

func newOpenAIHTTPContinuationTestService(cache GatewayCache) (*OpenAIGatewayService, OpenAIWSStateStore) {
	store := NewOpenAIWSStateStore(cache)
	return &OpenAIGatewayService{
		cache:              cache,
		openaiWSStateStore: store,
	}, store
}

func TestOpenAIHTTPContinuationOwnerIsImmutable(t *testing.T) {
	ctx := context.Background()
	cache := &openAIHTTPContinuationTestCache{}
	svc, store := newOpenAIHTTPContinuationTestService(cache)

	const (
		groupID    = int64(22)
		userID     = int64(1001)
		apiKeyID   = int64(2001)
		accountID  = int64(3001)
		responseID = "resp_http_immutable"
	)
	wantOwner := OpenAIHTTPResponseOwner{
		Version:   openAIHTTPResponseOwnerVersion,
		UserID:    userID,
		APIKeyID:  apiKeyID,
		GroupID:   groupID,
		AccountID: accountID,
	}

	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, groupID, responseID, userID, apiKeyID, accountID))
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, groupID, responseID, userID, apiKeyID, accountID), "idempotent rebinding must succeed")

	err := svc.BindOpenAIHTTPResponseOwner(ctx, groupID, responseID, userID, apiKeyID, accountID+1)
	require.ErrorContains(t, err, "openai HTTP response owner conflict")

	gotOwner, found, err := store.GetHTTPResponseOwnerStrict(ctx, groupID, responseID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantOwner, gotOwner, "a conflicting write must not replace the original owner")
}

func TestOpenAIHTTPContinuationOwnerIsIsolatedByRouteGroup(t *testing.T) {
	ctx := context.Background()
	cache := &openAIHTTPContinuationTestCache{}
	svc, store := newOpenAIHTTPContinuationTestService(cache)

	const responseID = "resp_http_group_isolation"
	ownerA := OpenAIHTTPResponseOwner{Version: openAIHTTPResponseOwnerVersion, UserID: 1001, APIKeyID: 2001, GroupID: 22, AccountID: 3001}
	ownerB := OpenAIHTTPResponseOwner{Version: openAIHTTPResponseOwnerVersion, UserID: 1002, APIKeyID: 2002, GroupID: 33, AccountID: 3002}

	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, ownerA.GroupID, responseID, ownerA.UserID, ownerA.APIKeyID, ownerA.AccountID))
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, ownerB.GroupID, responseID, ownerB.UserID, ownerB.APIKeyID, ownerB.AccountID))

	gotA, found, err := store.GetHTTPResponseOwnerStrict(ctx, ownerA.GroupID, responseID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, gotA)

	gotB, found, err := store.GetHTTPResponseOwnerStrict(ctx, ownerB.GroupID, responseID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerB, gotB)

	_, found, err = store.GetHTTPResponseOwnerStrict(ctx, 44, responseID)
	require.NoError(t, err)
	require.False(t, found, "an unbound route group must not see another group's owner")
}

func TestOpenAIHTTPContinuationAllowsSameUserWithDifferentAPIKey(t *testing.T) {
	ctx := context.Background()
	cache := &openAIHTTPContinuationTestCache{}
	svc, store := newOpenAIHTTPContinuationTestService(cache)

	const (
		groupID     = int64(22)
		userID      = int64(1001)
		creatorKey  = int64(2001)
		continueKey = int64(2002)
		accountID   = int64(3001)
		responseID  = "resp_http_same_user_new_key"
	)
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, groupID, responseID, userID, creatorKey, accountID))

	resolvedRoute, owned, err := svc.ResolveOpenAIHTTPContinuationRoute(ctx, []int64{11, groupID}, responseID, userID, continueKey)
	require.NoError(t, err)
	require.True(t, owned)
	require.Equal(t, OpenAIHTTPContinuationRoute{GroupID: groupID, AccountID: accountID}, resolvedRoute)

	owner, found, err := store.GetHTTPResponseOwnerStrict(ctx, groupID, responseID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, creatorKey, owner.APIKeyID, "continuing with another key must not rewrite the immutable creator audit field")
}

func TestOpenAIHTTPContinuationRejectsDifferentUser(t *testing.T) {
	ctx := context.Background()
	cache := &openAIHTTPContinuationTestCache{}
	svc, store := newOpenAIHTTPContinuationTestService(cache)

	const (
		groupID    = int64(22)
		ownerUser  = int64(1001)
		otherUser  = int64(1002)
		apiKeyID   = int64(2001)
		accountID  = int64(3001)
		responseID = "resp_http_other_user"
	)
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, groupID, responseID, ownerUser, apiKeyID, accountID))

	resolvedRoute, owned, err := svc.ResolveOpenAIHTTPContinuationRoute(ctx, []int64{groupID}, responseID, otherUser, apiKeyID)
	require.NoError(t, err)
	require.False(t, owned, "sharing the creator API key ID must not bypass the user boundary")
	require.Zero(t, resolvedRoute)

	boundAccountID, err := store.GetResponseAccountStrict(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Zero(t, boundAccountID, "a rejected continuation must not restore mutable routing state")
}

func TestOpenAIHTTPContinuationRejectsUnknownResponse(t *testing.T) {
	cache := &openAIHTTPContinuationTestCache{}
	svc, _ := newOpenAIHTTPContinuationTestService(cache)

	resolvedRoute, owned, err := svc.ResolveOpenAIHTTPContinuationRoute(
		context.Background(),
		[]int64{22, 33},
		"resp_http_unknown",
		1001,
		2001,
	)
	require.NoError(t, err)
	require.False(t, owned)
	require.Zero(t, resolvedRoute)
}

func TestOpenAIHTTPContinuationPropagatesCacheErrors(t *testing.T) {
	t.Run("immutable owner write", func(t *testing.T) {
		wantErr := errors.New("immutable owner cache write failed")
		cache := &openAIHTTPContinuationTestCache{bindStringErr: wantErr}
		svc, _ := newOpenAIHTTPContinuationTestService(cache)

		err := svc.BindOpenAIHTTPResponseOwner(context.Background(), 22, "resp_http_bind_error", 1001, 2001, 3001)
		require.ErrorIs(t, err, wantErr)
	})

	t.Run("owner lookup", func(t *testing.T) {
		wantErr := errors.New("owner cache lookup failed")
		cache := &openAIHTTPContinuationTestCache{getStringErr: wantErr}
		svc, _ := newOpenAIHTTPContinuationTestService(cache)

		resolvedRoute, owned, err := svc.ResolveOpenAIHTTPContinuationRoute(
			context.Background(),
			[]int64{22},
			"resp_http_lookup_error",
			1001,
			2001,
		)
		require.ErrorIs(t, err, wantErr)
		require.False(t, owned)
		require.Zero(t, resolvedRoute)
	})

	t.Run("mutable route repair", func(t *testing.T) {
		ctx := context.Background()
		wantErr := errors.New("mutable account cache write failed")
		cache := &openAIHTTPContinuationTestCache{}
		svc, _ := newOpenAIHTTPContinuationTestService(cache)
		require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, 22, "resp_http_repair_error", 1001, 2001, 3001))
		cache.setAccountErr = wantErr

		resolvedRoute, owned, err := svc.ResolveOpenAIHTTPContinuationRoute(
			ctx,
			[]int64{22},
			"resp_http_repair_error",
			1001,
			2001,
		)
		require.ErrorIs(t, err, wantErr)
		require.False(t, owned)
		require.Zero(t, resolvedRoute)
	})
}

func TestOpenAIHTTPContinuationBindsEffectiveRouteGroup(t *testing.T) {
	ctx := context.Background()
	cache := &openAIHTTPContinuationTestCache{}
	svc, store := newOpenAIHTTPContinuationTestService(cache)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	defaultGroupID := int64(11)
	effectiveGroupID := int64(22)
	c.Set("api_key", &APIKey{GroupID: &defaultGroupID})
	setOpenAIEffectiveGroupID(c, &effectiveGroupID)
	SetOpenAIHTTPResponseOwner(c, 1001, 2001)

	const responseID = "resp_http_effective_group"
	svc.bindHTTPResponseAccount(ctx, c, &Account{
		ID:       3001,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}, responseID)

	owner, found, err := store.GetHTTPResponseOwnerStrict(ctx, effectiveGroupID, responseID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, effectiveGroupID, owner.GroupID)
	require.Equal(t, int64(3001), owner.AccountID)

	_, found, err = store.GetHTTPResponseOwnerStrict(ctx, defaultGroupID, responseID)
	require.NoError(t, err)
	require.False(t, found, "the API key's default group must not shadow the selected effective route")

	boundAccountID, err := store.GetResponseAccountStrict(ctx, effectiveGroupID, responseID)
	require.NoError(t, err)
	require.Equal(t, int64(3001), boundAccountID)

	boundAccountID, err = store.GetResponseAccountStrict(ctx, defaultGroupID, responseID)
	require.NoError(t, err)
	require.Zero(t, boundAccountID, "mutable routing state must also use the effective route group")
}

func TestOpenAIHTTPContinuationRepairsMutableAccountFromImmutableOwner(t *testing.T) {
	ctx := context.Background()
	cache := &openAIHTTPContinuationTestCache{}
	svc, store := newOpenAIHTTPContinuationTestService(cache)

	const (
		groupID      = int64(22)
		userID       = int64(1001)
		apiKeyID     = int64(2001)
		ownerAccount = int64(3001)
		staleAccount = int64(3999)
		responseID   = "resp_http_mutable_repair"
	)
	require.NoError(t, svc.BindOpenAIHTTPResponseOwner(ctx, groupID, responseID, userID, apiKeyID, ownerAccount))
	require.NoError(t, store.BindResponseAccount(ctx, groupID, responseID, staleAccount, time.Hour))

	boundAccountID, err := store.GetResponseAccountStrict(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Equal(t, staleAccount, boundAccountID)

	resolvedRoute, owned, err := svc.ResolveOpenAIHTTPContinuationRoute(ctx, []int64{groupID}, responseID, userID, apiKeyID)
	require.NoError(t, err)
	require.True(t, owned)
	require.Equal(t, OpenAIHTTPContinuationRoute{GroupID: groupID, AccountID: ownerAccount}, resolvedRoute)

	readerStore := NewOpenAIWSStateStore(cache)
	boundAccountID, err = readerStore.GetResponseAccountStrict(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Equal(t, ownerAccount, boundAccountID, "a fresh reader must observe the durable mutable repair")

	owner, found, err := readerStore.GetHTTPResponseOwnerStrict(ctx, groupID, responseID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, OpenAIHTTPResponseOwner{
		Version:   openAIHTTPResponseOwnerVersion,
		UserID:    userID,
		APIKeyID:  apiKeyID,
		GroupID:   groupID,
		AccountID: ownerAccount,
	}, owner, "repairing mutable routing must not alter the immutable owner")
}
