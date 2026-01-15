package service

import "context"

type TokenCacheInvalidator interface {
	InvalidateToken(ctx context.Context, account *Account) error
}

type CompositeTokenCacheInvalidator struct {
	geminiCache GeminiTokenCache
}

func NewCompositeTokenCacheInvalidator(geminiCache GeminiTokenCache) *CompositeTokenCacheInvalidator {
	return &CompositeTokenCacheInvalidator{
		geminiCache: geminiCache,
	}
}

func (c *CompositeTokenCacheInvalidator) InvalidateToken(ctx context.Context, account *Account) error {
	if c == nil || c.geminiCache == nil || account == nil {
		return nil
	}
	if account.Type != AccountTypeOAuth {
		return nil
	}

	switch account.Platform {
	case PlatformGemini:
		return c.geminiCache.DeleteAccessToken(ctx, GeminiTokenCacheKey(account))
	case PlatformAntigravity:
		return c.geminiCache.DeleteAccessToken(ctx, AntigravityTokenCacheKey(account))
	default:
		return nil
	}
}
