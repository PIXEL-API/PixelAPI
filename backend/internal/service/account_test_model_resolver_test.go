//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// catalogStub 实现 PricedModelCatalog 窄接口，供 resolver 单测使用。
type catalogStub struct {
	selectable func(ctx context.Context, query PricedModelQuery) ([]string, error)
	priced     func(ctx context.Context, query PricedModelQuery, modelID string) (bool, error)
}

func (s *catalogStub) ListPricedModelIDs(_ context.Context, _ []string) ([]string, error) {
	return nil, nil
}

func (s *catalogStub) ListSelectablePricedModelIDs(ctx context.Context, query PricedModelQuery) ([]string, error) {
	if s.selectable != nil {
		return s.selectable(ctx, query)
	}
	return nil, nil
}

func (s *catalogStub) IsModelPriced(ctx context.Context, query PricedModelQuery, modelID string) (bool, error) {
	if s.priced != nil {
		return s.priced(ctx, query, modelID)
	}
	return false, nil
}

func ownedGrokAccount(mapping map[string]any) *Account {
	ownerID := int64(1)
	credentials := map[string]any{}
	if mapping != nil {
		credentials["model_mapping"] = mapping
	}
	return &Account{
		Platform:    PlatformGrok,
		OwnerUserID: &ownerID,
		Credentials: credentials,
	}
}

func TestResolveTestModels_OwnedAccountIntersection(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5", "grok-4.7"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5", "grok-4.6": "grok-4.6"})

	models, err := resolver.ResolveTestModels(context.Background(), account)

	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "grok-4.5", models[0].ID)
}

func TestResolveTestModels_OwnedAccountWhitelistMissing(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := ownedGrokAccount(nil) // 空白名单

	_, err := resolver.ResolveTestModels(context.Background(), account)

	require.ErrorIs(t, err, ErrAccountTestModelWhitelistMissing)
}

func TestResolveTestModels_OwnedAccountNoPricedIntersection(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := ownedGrokAccount(map[string]any{"grok-9.9": "grok-9.9"}) // 白名单全未定价

	_, err := resolver.ResolveTestModels(context.Background(), account)

	require.ErrorIs(t, err, ErrAccountTestModelNoPricedIntersection)
}

func TestResolveTestModels_PlatformAccountNoMappingUsesCatalog(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5", "grok-4.6"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := &Account{Platform: PlatformGrok} // 平台账号，无 mapping

	models, err := resolver.ResolveTestModels(context.Background(), account)

	require.NoError(t, err)
	require.Len(t, models, 2)
}

func TestResolveTestModels_PlatformAccountExplicitMappingFilters(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5", "grok-4.6", "grok-3-mini"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := &Account{
		Platform:    PlatformGrok,
		Credentials: map[string]any{"model_mapping": map[string]any{"grok-4.*": "grok-4.*"}},
	}

	models, err := resolver.ResolveTestModels(context.Background(), account)

	require.NoError(t, err)
	require.Len(t, models, 2) // grok-4.5、grok-4.6 命中通配符，grok-3-mini 不命中
}

func TestResolveTestModels_CatalogEmpty(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return nil, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := &Account{Platform: PlatformGrok}

	_, err := resolver.ResolveTestModels(context.Background(), account)

	require.ErrorIs(t, err, ErrAccountTestModelCatalogEmpty)
}

func TestResolveTestModels_UnsupportedPlatform(t *testing.T) {
	resolver := NewAccountTestModelResolver(&catalogStub{})
	account := &Account{Platform: "bedrock"}

	_, err := resolver.ResolveTestModels(context.Background(), account)

	require.ErrorIs(t, err, ErrAccountTestUnsupportedPlatform)
}

func TestResolveTestModels_ImageOnlyFiltered(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"gpt-image-1", "grok-imagine"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)
	account := &Account{Platform: PlatformOpenAI}

	_, err := resolver.ResolveTestModels(context.Background(), account)

	require.ErrorIs(t, err, ErrAccountTestProtocolNoModels)
}

func TestResolveTestModels_NilCatalog(t *testing.T) {
	resolver := NewAccountTestModelResolver(nil)

	_, err := resolver.ResolveTestModels(context.Background(), &Account{Platform: PlatformGrok})

	require.ErrorIs(t, err, ErrOwnedAccountModelCatalogUnavailable)
}

func TestResolveBatchTestModels_CommonIntersection(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5", "grok-4.6", "grok-3-mini"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)

	accounts := []*Account{
		ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5", "grok-4.6": "grok-4.6"}),
		ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5", "grok-3-mini": "grok-3-mini"}),
	}

	models, err := resolver.ResolveBatchTestModels(context.Background(), accounts)

	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "grok-4.5", models[0].ID)
}

func TestResolveBatchTestModels_OrderIndependent(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5", "grok-4.6"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)

	accountA := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5", "grok-4.6": "grok-4.6"})
	accountB := ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5"})

	models1, err := resolver.ResolveBatchTestModels(context.Background(), []*Account{accountA, accountB})
	require.NoError(t, err)
	models2, err := resolver.ResolveBatchTestModels(context.Background(), []*Account{accountB, accountA})
	require.NoError(t, err)

	require.Len(t, models1, 1)
	require.Equal(t, models1[0].ID, models2[0].ID)
}

func TestResolveBatchTestModels_PlatformMismatch(t *testing.T) {
	resolver := NewAccountTestModelResolver(&catalogStub{})

	openAIAccount := &Account{
		Platform:    PlatformOpenAI,
		OwnerUserID: adminOwnedTestOwnerID(),
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}},
	}

	_, err := resolver.ResolveBatchTestModels(context.Background(), []*Account{
		ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5"}),
		openAIAccount,
	})

	require.ErrorIs(t, err, ErrAccountTestUnsupportedPlatform)
}

func TestResolveBatchTestModels_AccountWhitelistMissing(t *testing.T) {
	catalog := &catalogStub{selectable: func(_ context.Context, _ PricedModelQuery) ([]string, error) {
		return []string{"grok-4.5"}, nil
	}}
	resolver := NewAccountTestModelResolver(catalog)

	_, err := resolver.ResolveBatchTestModels(context.Background(), []*Account{
		ownedGrokAccount(map[string]any{"grok-4.5": "grok-4.5"}),
		ownedGrokAccount(nil), // 空白名单
	})

	require.ErrorIs(t, err, ErrAccountTestModelWhitelistMissing)
}

func TestResolveBatchTestModels_Empty(t *testing.T) {
	resolver := NewAccountTestModelResolver(&catalogStub{})

	_, err := resolver.ResolveBatchTestModels(context.Background(), nil)

	require.ErrorIs(t, err, ErrAccountTestBatchEmpty)
}
