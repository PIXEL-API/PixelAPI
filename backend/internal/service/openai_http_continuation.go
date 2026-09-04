package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	openAIHTTPResponseOwnerCachePrefix = "openai:http_response_owner:v1:"
	openAIHTTPResponseOwnerVersion     = 1
	openAIHTTPResponseOwnerContextKey  = "openai_http_response_owner"
)

var errOpenAIHTTPResponseOwnerInvalid = errors.New("openai HTTP response owner record is invalid")

// OpenAIHTTPResponseOwner is a durable, immutable authorization and routing
// record for an HTTP Responses continuation. GroupID and AccountID pin the
// upstream state, while UserID prevents a leaked response ID from crossing
// downstream tenants. APIKeyID is retained for audit.
type OpenAIHTTPResponseOwner struct {
	Version   int   `json:"v"`
	UserID    int64 `json:"user_id"`
	APIKeyID  int64 `json:"api_key_id"`
	GroupID   int64 `json:"group_id"`
	AccountID int64 `json:"account_id"`
}

// OpenAIHTTPContinuationRoute is the immutable upstream location of a response.
// Both fields must match before a continuation may be forwarded.
type OpenAIHTTPContinuationRoute struct {
	GroupID   int64
	AccountID int64
}

type openAIHTTPResponseRequestOwner struct {
	userID   int64
	apiKeyID int64
}

// SetOpenAIHTTPResponseOwner marks the authenticated downstream owner whose
// successful response IDs may be continued over HTTP.
func SetOpenAIHTTPResponseOwner(c *gin.Context, userID, apiKeyID int64) {
	if c == nil || userID <= 0 || apiKeyID <= 0 {
		return
	}
	c.Set(openAIHTTPResponseOwnerContextKey, openAIHTTPResponseRequestOwner{userID: userID, apiKeyID: apiKeyID})
}

func (s *defaultOpenAIWSStateStore) BindHTTPResponseOwner(
	ctx context.Context,
	groupID int64,
	responseID string,
	userID, apiKeyID, accountID int64,
	ttl time.Duration,
) error {
	if s == nil {
		return errors.New("openai HTTP response owner store is unavailable")
	}
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" || groupID < 0 || userID <= 0 || apiKeyID <= 0 || accountID <= 0 {
		return errors.New("invalid openai HTTP response owner binding")
	}
	if s.cache == nil {
		return errors.New("gateway cache is unavailable for durable HTTP response owner binding")
	}
	immutableCache, ok := s.cache.(openAIWSImmutableStringCache)
	if !ok || immutableCache == nil {
		return errors.New("gateway cache does not support immutable HTTP response owner bindings")
	}

	owner := OpenAIHTTPResponseOwner{
		Version:   openAIHTTPResponseOwnerVersion,
		UserID:    userID,
		APIKeyID:  apiKeyID,
		GroupID:   groupID,
		AccountID: accountID,
	}
	encoded, err := json.Marshal(owner)
	if err != nil {
		return fmt.Errorf("encode openai HTTP response owner: %w", err)
	}
	cacheCtx, cancel := withOpenAIWSStateStoreRedisTimeout(ctx)
	storedValue, err := immutableCache.BindSessionStringImmutable(
		cacheCtx,
		groupID,
		openAIHTTPResponseOwnerCacheKey(id),
		string(encoded),
		normalizeOpenAIWSTTL(ttl),
	)
	cancel()
	if err != nil {
		return fmt.Errorf("persist openai HTTP response owner: %w", err)
	}
	storedOwner, err := decodeOpenAIHTTPResponseOwner(storedValue, groupID)
	if err != nil {
		return err
	}
	if storedOwner != owner {
		return fmt.Errorf(
			"openai HTTP response owner conflict: response is owned by user %d API key %d group %d account %d",
			storedOwner.UserID,
			storedOwner.APIKeyID,
			storedOwner.GroupID,
			storedOwner.AccountID,
		)
	}
	return nil
}

func (s *defaultOpenAIWSStateStore) GetHTTPResponseOwnerStrict(
	ctx context.Context,
	groupID int64,
	responseID string,
) (OpenAIHTTPResponseOwner, bool, error) {
	if s == nil {
		return OpenAIHTTPResponseOwner{}, false, errors.New("openai HTTP response owner store is unavailable")
	}
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" || groupID < 0 {
		return OpenAIHTTPResponseOwner{}, false, nil
	}
	if s.cache == nil {
		return OpenAIHTTPResponseOwner{}, false, errors.New("gateway cache is unavailable for durable HTTP response owner lookup")
	}

	cacheCtx, cancel := withOpenAIWSStateStoreRedisTimeout(ctx)
	storedValue, err := s.cache.GetSessionString(cacheCtx, groupID, openAIHTTPResponseOwnerCacheKey(id))
	cancel()
	if err != nil {
		if errors.Is(err, ErrGatewaySessionStringNotFound) || errors.Is(err, redis.Nil) {
			return OpenAIHTTPResponseOwner{}, false, nil
		}
		return OpenAIHTTPResponseOwner{}, false, err
	}
	owner, err := decodeOpenAIHTTPResponseOwner(storedValue, groupID)
	if err != nil {
		return OpenAIHTTPResponseOwner{}, false, err
	}
	return owner, true, nil
}

func openAIHTTPResponseOwnerCacheKey(responseID string) string {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return openAIHTTPResponseOwnerCachePrefix + hex.EncodeToString(sum[:])
}

func decodeOpenAIHTTPResponseOwner(raw string, expectedGroupID int64) (OpenAIHTTPResponseOwner, error) {
	var owner OpenAIHTTPResponseOwner
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &owner); err != nil {
		return OpenAIHTTPResponseOwner{}, fmt.Errorf("%w: decode: %v", errOpenAIHTTPResponseOwnerInvalid, err)
	}
	if owner.Version != openAIHTTPResponseOwnerVersion || owner.UserID <= 0 || owner.APIKeyID <= 0 ||
		owner.GroupID != expectedGroupID || owner.GroupID < 0 || owner.AccountID <= 0 {
		return OpenAIHTTPResponseOwner{}, errOpenAIHTTPResponseOwnerInvalid
	}
	return owner, nil
}

// BindOpenAIHTTPResponseOwner is exposed for focused ownership tests and
// administrative compatibility checks. Normal traffic is bound automatically
// by bindHTTPResponseAccount after a successful upstream response.
func (s *OpenAIGatewayService) BindOpenAIHTTPResponseOwner(
	ctx context.Context,
	groupID int64,
	responseID string,
	userID, apiKeyID, accountID int64,
) error {
	if s == nil {
		return errors.New("openai gateway service is unavailable")
	}
	return s.getOpenAIWSStateStore().BindHTTPResponseOwner(
		ctx,
		groupID,
		responseID,
		userID,
		apiKeyID,
		accountID,
		s.openAIWSResponseStickyTTL(),
	)
}

// ResolveOpenAIHTTPContinuationRoute authorizes a response ID and restores its
// immutable upstream account binding before account selection. Missing,
// mismatched, corrupt, and ambiguous ownership all fail closed.
func (s *OpenAIGatewayService) ResolveOpenAIHTTPContinuationRoute(
	ctx context.Context,
	candidateGroupIDs []int64,
	responseID string,
	userID, apiKeyID int64,
) (OpenAIHTTPContinuationRoute, bool, error) {
	if s == nil || strings.TrimSpace(responseID) == "" || userID <= 0 || apiKeyID <= 0 {
		return OpenAIHTTPContinuationRoute{}, false, nil
	}
	store := s.getOpenAIWSStateStore()
	if store == nil {
		return OpenAIHTTPContinuationRoute{}, false, errors.New("openai HTTP response owner store is unavailable")
	}
	if len(candidateGroupIDs) == 0 {
		candidateGroupIDs = []int64{0}
	}

	seenGroups := make(map[int64]struct{}, len(candidateGroupIDs))
	var matched *OpenAIHTTPResponseOwner
	for _, groupID := range candidateGroupIDs {
		if groupID < 0 {
			continue
		}
		if _, seen := seenGroups[groupID]; seen {
			continue
		}
		seenGroups[groupID] = struct{}{}
		owner, found, err := store.GetHTTPResponseOwnerStrict(ctx, groupID, responseID)
		if err != nil {
			return OpenAIHTTPContinuationRoute{}, false, fmt.Errorf("lookup openai HTTP response owner in group %d: %w", groupID, err)
		}
		if !found {
			continue
		}
		if owner.UserID != userID {
			return OpenAIHTTPContinuationRoute{}, false, nil
		}
		if matched != nil {
			return OpenAIHTTPContinuationRoute{}, false, errors.New("openai HTTP response owner exists in multiple route groups")
		}
		ownerCopy := owner
		matched = &ownerCopy
	}
	if matched == nil {
		return OpenAIHTTPContinuationRoute{}, false, nil
	}
	if err := store.BindResponseAccount(
		ctx,
		matched.GroupID,
		responseID,
		matched.AccountID,
		s.openAIWSResponseStickyTTL(),
	); err != nil {
		return OpenAIHTTPContinuationRoute{}, false, fmt.Errorf("restore openai HTTP response account binding: %w", err)
	}
	return OpenAIHTTPContinuationRoute{
		GroupID:   matched.GroupID,
		AccountID: matched.AccountID,
	}, true, nil
}

func (s *OpenAIGatewayService) ValidateOpenAIHTTPResponseOwner(
	ctx context.Context,
	groupID int64,
	responseID string,
	userID, apiKeyID int64,
) (bool, error) {
	_, owned, err := s.ResolveOpenAIHTTPContinuationRoute(
		ctx,
		[]int64{groupID},
		responseID,
		userID,
		apiKeyID,
	)
	return owned, err
}

func (s *OpenAIGatewayService) bindHTTPResponseAccount(ctx context.Context, c *gin.Context, account *Account, responseID string) {
	if s == nil || account == nil || !account.IsOpenAIApiKey() || account.ID <= 0 {
		return
	}
	responseID = strings.TrimSpace(responseID)
	if responseID == "" || c == nil {
		return
	}
	rawOwner, ok := c.Get(openAIHTTPResponseOwnerContextKey)
	owner, ok := rawOwner.(openAIHTTPResponseRequestOwner)
	if !ok || owner.userID <= 0 || owner.apiKeyID <= 0 {
		return
	}

	groupID := getOpenAIEffectiveGroupID(c)
	store := s.getOpenAIWSStateStore()
	ttl := s.openAIWSResponseStickyTTL()
	if err := store.BindHTTPResponseOwner(ctx, groupID, responseID, owner.userID, owner.apiKeyID, account.ID, ttl); err != nil {
		logger.L().Warn(
			"openai.http_bind_response_owner_failed",
			zap.Int64("group_id", groupID),
			zap.Int64("account_id", account.ID),
			zap.Int64("user_id", owner.userID),
			zap.Int64("api_key_id", owner.apiKeyID),
			zap.String("response_id", truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen)),
			zap.Error(err),
		)
		return
	}
	logOpenAIWSBindResponseAccountWarn(
		groupID,
		account.ID,
		responseID,
		store.BindResponseAccount(ctx, groupID, responseID, account.ID, ttl),
	)
}
