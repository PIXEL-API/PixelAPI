//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- helpers ---

func testTimePtr(t time.Time) *time.Time { return &t }

func makeAccWithLoad(id int64, priority int, loadRate int, lastUsed *time.Time, accType string) accountWithLoad {
	return accountWithLoad{
		account: &Account{
			ID:          id,
			Priority:    priority,
			LastUsedAt:  lastUsed,
			Type:        accType,
			Schedulable: true,
			Status:      StatusActive,
		},
		loadInfo: &AccountLoadInfo{
			AccountID:          id,
			CurrentConcurrency: 0,
			LoadRate:           loadRate,
		},
	}
}

// --- sortAccountsByPriorityAndLastUsed ---

func TestSortAccountsByPriorityAndLastUsed_ByPriority(t *testing.T) {
	now := time.Now()
	accounts := []*Account{
		{ID: 1, Priority: 5, LastUsedAt: testTimePtr(now)},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 3, Priority: 3, LastUsedAt: testTimePtr(now)},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	require.Equal(t, int64(2), accounts[0].ID, "优先级最低的排第一")
	require.Equal(t, int64(3), accounts[1].ID)
	require.Equal(t, int64(1), accounts[2].ID)
}

func TestSortAccountsByPriorityAndLastUsed_SamePriorityByLastUsed(t *testing.T) {
	now := time.Now()
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now.Add(-1 * time.Hour))},
		{ID: 3, Priority: 1, LastUsedAt: nil},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	require.Equal(t, int64(3), accounts[0].ID, "nil LastUsedAt 排最前")
	require.Equal(t, int64(2), accounts[1].ID, "更早使用的排前面")
	require.Equal(t, int64(1), accounts[2].ID)
}

func TestSortAccountsByPriorityAndLastUsed_PreferOAuth(t *testing.T) {
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
		{ID: 2, Priority: 1, LastUsedAt: nil, Type: AccountTypeOAuth},
	}
	sortAccountsByPriorityAndLastUsed(accounts, true)
	require.Equal(t, int64(2), accounts[0].ID, "preferOAuth 时 OAuth 账号排前面")
}

func TestSortAccountsByPriorityAndLastUsed_StableSort(t *testing.T) {
	accounts := []*Account{
		{ID: 1, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
		{ID: 2, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
		{ID: 3, Priority: 1, LastUsedAt: nil, Type: AccountTypeAPIKey},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	// 稳定排序：相同键值的元素保持原始顺序
	require.Equal(t, int64(1), accounts[0].ID)
	require.Equal(t, int64(2), accounts[1].ID)
	require.Equal(t, int64(3), accounts[2].ID)
}

func TestSortAccountsByPriorityAndLastUsed_MixedPriorityAndTime(t *testing.T) {
	now := time.Now()
	accounts := []*Account{
		{ID: 1, Priority: 2, LastUsedAt: nil},
		{ID: 2, Priority: 1, LastUsedAt: testTimePtr(now)},
		{ID: 3, Priority: 1, LastUsedAt: testTimePtr(now.Add(-1 * time.Hour))},
		{ID: 4, Priority: 2, LastUsedAt: testTimePtr(now.Add(-2 * time.Hour))},
	}
	sortAccountsByPriorityAndLastUsed(accounts, false)
	// 优先级1排前：nil < earlier
	require.Equal(t, int64(3), accounts[0].ID, "优先级1 + 更早")
	require.Equal(t, int64(2), accounts[1].ID, "优先级1 + 现在")
	// 优先级2排后：nil < time
	require.Equal(t, int64(1), accounts[2].ID, "优先级2 + nil")
	require.Equal(t, int64(4), accounts[3].ID, "优先级2 + 有时间")
}

// --- selectByCallCount ---

func TestSelectByCallCount_Empty(t *testing.T) {
	result := selectByCallCount(nil, nil, false)
	require.Nil(t, result)
}

func TestSelectByCallCount_Single(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 50, nil, AccountTypeAPIKey),
	}
	result := selectByCallCount(accounts, map[int64]*ModelLoadInfo{1: {CallCount: 10}}, false)
	require.NotNil(t, result)
	require.Equal(t, int64(1), result.account.ID)
}

func TestSelectByCallCount_NilModelLoadFallsBackToLRU(t *testing.T) {
	now := time.Now()
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 50, testTimePtr(now), AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 50, testTimePtr(now.Add(-1*time.Hour)), AccountTypeAPIKey),
	}
	result := selectByCallCount(accounts, nil, false)
	require.NotNil(t, result)
	require.Equal(t, int64(2), result.account.ID, "nil modelLoadMap 应回退到 LRU 选择")
}

func TestSelectByCallCount_SelectsMinCallCount(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 50, nil, AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 50, nil, AccountTypeAPIKey),
		makeAccWithLoad(3, 1, 50, nil, AccountTypeAPIKey),
	}
	modelLoad := map[int64]*ModelLoadInfo{
		1: {CallCount: 100},
		2: {CallCount: 5},
		3: {CallCount: 50},
	}
	// 运行多次确认总是选调用次数最少的
	for i := 0; i < 10; i++ {
		result := selectByCallCount(accounts, modelLoad, false)
		require.NotNil(t, result)
		require.Equal(t, int64(2), result.account.ID, "应选择调用次数最少的账号")
	}
}

func TestSelectByCallCount_NewAccountUsesAverage(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 50, nil, AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 50, nil, AccountTypeAPIKey),
		makeAccWithLoad(3, 1, 50, nil, AccountTypeAPIKey),
	}
	// 账号1和2有调用记录，账号3是新账号（CallCount=0）
	// 平均调用次数 = (100 + 200) / 2 = 150
	// 新账号用平均值 150，比账号1(100)多，所以应选账号1
	modelLoad := map[int64]*ModelLoadInfo{
		1: {CallCount: 100},
		2: {CallCount: 200},
		// 3 没有记录
	}
	for i := 0; i < 10; i++ {
		result := selectByCallCount(accounts, modelLoad, false)
		require.NotNil(t, result)
		require.Equal(t, int64(1), result.account.ID, "新账号虚拟调用次数(150)高于账号1(100)，应选账号1")
	}
}

func TestSelectByCallCount_AllNewAccountsFallToAvgZero(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 50, nil, AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 50, nil, AccountTypeAPIKey),
	}
	// 所有账号都是新的，avgCallCount = 0，所有人 effectiveCallCount 都是 0
	modelLoad := map[int64]*ModelLoadInfo{}
	validIDs := map[int64]bool{1: true, 2: true}
	for i := 0; i < 10; i++ {
		result := selectByCallCount(accounts, modelLoad, false)
		require.NotNil(t, result)
		require.True(t, validIDs[result.account.ID], "所有新账号应随机选择")
	}
}

func TestSelectByCallCount_PreferOAuth(t *testing.T) {
	accounts := []accountWithLoad{
		makeAccWithLoad(1, 1, 50, nil, AccountTypeAPIKey),
		makeAccWithLoad(2, 1, 50, nil, AccountTypeOAuth),
	}
	// 两个账号调用次数相同
	modelLoad := map[int64]*ModelLoadInfo{
		1: {CallCount: 10},
		2: {CallCount: 10},
	}
	for i := 0; i < 10; i++ {
		result := selectByCallCount(accounts, modelLoad, true)
		require.NotNil(t, result)
		require.Equal(t, int64(2), result.account.ID, "调用次数相同时应优先选择 OAuth 账号")
	}
}
