package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"golang.org/x/sync/singleflight"
)

// settingCacheTTL 是 settings 读穿缓存的有效期。写路径只失效本进程缓存，
// 多实例部署下其他实例最多陈旧一个 TTL，settings 均为开关/阈值类配置，可接受。
const settingCacheTTL = 5 * time.Second

type cachedSettingEntry struct {
	// setting 为 nil 表示 negative cache（键不存在，Get 返回 ErrSettingNotFound）
	setting *service.Setting
	// valueOnly 表示条目来自 GetMultiple，仅有 Key/Value；Get 需要完整行时回源
	valueOnly bool
	expiresAt time.Time
}

// cachedSettingRepository 以短 TTL 进程内缓存包裹底层 settings 仓储，
// 网关热路径的高频设置读取不再每次落库。
type cachedSettingRepository struct {
	inner service.SettingRepository
	ttl   time.Duration
	now   func() time.Time

	mu      sync.RWMutex
	entries map[string]cachedSettingEntry

	sf singleflight.Group
}

func newCachedSettingRepository(inner service.SettingRepository) *cachedSettingRepository {
	return &cachedSettingRepository{
		inner:   inner,
		ttl:     settingCacheTTL,
		now:     time.Now,
		entries: make(map[string]cachedSettingEntry),
	}
}

func (r *cachedSettingRepository) lookup(key string) (cachedSettingEntry, bool) {
	r.mu.RLock()
	entry, ok := r.entries[key]
	r.mu.RUnlock()
	if !ok || r.now().After(entry.expiresAt) {
		return cachedSettingEntry{}, false
	}
	return entry, true
}

func (r *cachedSettingRepository) store(key string, setting *service.Setting, valueOnly bool) {
	entry := cachedSettingEntry{valueOnly: valueOnly, expiresAt: r.now().Add(r.ttl)}
	if setting != nil {
		clone := *setting
		entry.setting = &clone
	}
	r.mu.Lock()
	r.entries[key] = entry
	r.mu.Unlock()
}

func (r *cachedSettingRepository) invalidate(keys ...string) {
	r.mu.Lock()
	for _, key := range keys {
		delete(r.entries, key)
	}
	r.mu.Unlock()
}

func (r *cachedSettingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	if entry, ok := r.lookup(key); ok && (entry.setting == nil || !entry.valueOnly) {
		if entry.setting == nil {
			return nil, service.ErrSettingNotFound
		}
		clone := *entry.setting
		return &clone, nil
	}

	result, err, _ := r.sf.Do(key, func() (any, error) {
		if entry, ok := r.lookup(key); ok && (entry.setting == nil || !entry.valueOnly) {
			if entry.setting == nil {
				return nil, service.ErrSettingNotFound
			}
			return entry.setting, nil
		}
		setting, err := r.inner.Get(ctx, key)
		if err != nil {
			if errors.Is(err, service.ErrSettingNotFound) {
				r.store(key, nil, false)
			}
			return nil, err
		}
		r.store(key, setting, false)
		return setting, nil
	})
	if err != nil {
		return nil, err
	}
	setting, ok := result.(*service.Setting)
	if !ok || setting == nil {
		return nil, service.ErrSettingNotFound
	}
	clone := *setting
	return &clone, nil
}

func (r *cachedSettingRepository) GetValue(ctx context.Context, key string) (string, error) {
	if entry, ok := r.lookup(key); ok {
		if entry.setting == nil {
			return "", service.ErrSettingNotFound
		}
		return entry.setting.Value, nil
	}
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *cachedSettingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	missing := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		entry, ok := r.lookup(key)
		if !ok {
			missing = append(missing, key)
			continue
		}
		if entry.setting != nil {
			result[key] = entry.setting.Value
		}
	}
	if len(missing) == 0 {
		return result, nil
	}

	fetched, err := r.inner.GetMultiple(ctx, missing)
	if err != nil {
		return nil, err
	}
	for _, key := range missing {
		value, ok := fetched[key]
		if !ok {
			// 底层未返回的 key 与单键 miss 一样写 negative cache
			r.store(key, nil, false)
			continue
		}
		result[key] = value
		r.store(key, &service.Setting{Key: key, Value: value}, true)
	}
	return result, nil
}

func (r *cachedSettingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	return r.inner.GetAll(ctx)
}

func (r *cachedSettingRepository) Set(ctx context.Context, key, value string) error {
	if err := r.inner.Set(ctx, key, value); err != nil {
		return err
	}
	r.invalidate(key)
	return nil
}

func (r *cachedSettingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	if err := r.inner.SetMultiple(ctx, settings); err != nil {
		return err
	}
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	r.invalidate(keys...)
	return nil
}

func (r *cachedSettingRepository) Delete(ctx context.Context, key string) error {
	if err := r.inner.Delete(ctx, key); err != nil {
		return err
	}
	r.invalidate(key)
	return nil
}
