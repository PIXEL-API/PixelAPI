package service

import "context"

type TokenCacheInvalidator interface {
	InvalidateToken(ctx context.Context, account *Account) error
}

type CompositeTokenCacheInvalidator struct {
	cache GeminiTokenCache // 统一使用一个缓存接口，通过缓存键前缀区分平台
}

func NewCompositeTokenCacheInvalidator(cache GeminiTokenCache) *CompositeTokenCacheInvalidator {
	return &CompositeTokenCacheInvalidator{
		cache: cache,
	}
}

func (c *CompositeTokenCacheInvalidator) InvalidateToken(ctx context.Context, account *Account) error {
	if c == nil || c.cache == nil || account == nil {
		return nil
	}
	if account.Type != AccountTypeOAuth {
		return nil
	}

	var cacheKey string
	switch account.Platform {
	case PlatformGemini:
		cacheKey = GeminiTokenCacheKey(account)
	case PlatformAntigravity:
		cacheKey = AntigravityTokenCacheKey(account)
	case PlatformOpenAI:
		cacheKey = OpenAITokenCacheKey(account)
	case PlatformAnthropic:
		cacheKey = ClaudeTokenCacheKey(account)
	default:
		return nil
	}
	return c.cache.DeleteAccessToken(ctx, cacheKey)
}
