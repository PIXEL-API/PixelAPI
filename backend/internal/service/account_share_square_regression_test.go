//go:build unit

// 账号广场三个线上故障的回归测试：
//   1. 「不能修改广场配置」—— UpdateListing 的房间容量校验与编辑锁前置判定误伤房主保存；
//   2. 「广场用过的号不能删号」—— 删除拦截的自动退房重试范围过宽，会把账号不可逆地
//      摘出房间却仍然删不掉。
//
// 这些用例刻意复用 account_share_mode_test.go 的 accountShareModeRepoStub 与
// account_service_delete_test.go 的 accountRepoStub / detachRoomRepoStub / roomAccountBlocked，
// 不另起一套桩，保证与既有单测口径一致。

package service

import (
	"context"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// accountShareSquareRoomStateRepoStub 在基础仓储桩之上补出 GetRoomManagementState，
// 让 roomConfiguredConcurrencyCeiling 能走到「配置并发」这条主路径。
// 基础桩没有这个方法，roomManagementStateRepository() 会失败并回退到 listing.AccountConcurrency，
// 两条路径都需要被覆盖。
type accountShareSquareRoomStateRepoStub struct {
	*accountShareModeRepoStub
	state         *AccountShareRoomManagementState
	stateErr      error
	stateCalls    int
	stateListings []int64
}

func (r *accountShareSquareRoomStateRepoStub) GetRoomManagementState(
	_ context.Context,
	_ int64,
	_ bool,
	listingID int64,
) (*AccountShareRoomManagementState, error) {
	r.stateCalls++
	r.stateListings = append(r.stateListings, listingID)
	if r.stateErr != nil {
		return nil, r.stateErr
	}
	return r.state, nil
}

var _ AccountShareModeRepository = (*accountShareSquareRoomStateRepoStub)(nil)
var _ accountShareRoomManagementStateRepository = (*accountShareSquareRoomStateRepoStub)(nil)

func accountShareSquareUpdatedListing() *AccountShareListing {
	return &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, RowVersion: 2}
}

// 守的 bug：房间账号一进额度保护（listing.AccountConcurrency 被 SQL 按健康度过滤成 0），
// 房主连改个房间名都会被打成 400 ACCOUNT_SHARE_MODE_INVALID_CONCURRENCY。
// 编辑弹窗是整表单提交，per_user_concurrency 永远随请求带上，所以只有它真的变了才该校验容量。
func TestAccountShareSquareRegressionUnchangedPerUserConcurrencySkipsRoomCapacityCheck(t *testing.T) {
	t.Run("房间账号全部不可调度时房主仍能改房间名", func(t *testing.T) {
		repo := &accountShareModeRepoStub{
			// AccountConcurrency = 0 模拟房间内账号全部处于额度保护 / 限流，按健康度过滤后归零。
			listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 5, AccountConcurrency: 0},
			updateListing: accountShareSquareUpdatedListing(),
		}
		svc := &AccountShareModeService{repo: repo}
		name := "共享账号一"
		perUser := 5
		expectedVersion := int64(1)

		listing, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
			Name:               &name,
			PerUserConcurrency: &perUser,
			ExpectedVersion:    &expectedVersion,
			Reason:             "名称更清晰",
		})

		require.NotErrorIs(t, err, ErrAccountShareModeInvalidConcurrency)
		require.NoError(t, err)
		require.NotNil(t, listing)
		require.Equal(t, 1, repo.updateCalls)
		require.NotNil(t, repo.updateInput.Name)
		require.Equal(t, name, *repo.updateInput.Name)
	})

	t.Run("配置容量已低于历史取值时不改并发也能保存", func(t *testing.T) {
		base := &accountShareModeRepoStub{
			listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 5, AccountConcurrency: 4},
			updateListing: accountShareSquareUpdatedListing(),
		}
		repo := &accountShareSquareRoomStateRepoStub{
			accountShareModeRepoStub: base,
			// 房间账号被摘走后配置容量降到 3，低于房主历史设置的 5。
			state: &AccountShareRoomManagementState{ListingID: 7, ConfiguredTotalConcurrency: 3},
		}
		svc := &AccountShareModeService{repo: repo}
		name := "共享账号一"
		perUser := 5
		expectedVersion := int64(1)

		_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
			Name:               &name,
			PerUserConcurrency: &perUser,
			ExpectedVersion:    &expectedVersion,
			Reason:             "名称更清晰",
		})

		require.NotErrorIs(t, err, ErrAccountShareModeInvalidConcurrency)
		require.NoError(t, err)
		require.Equal(t, 1, base.updateCalls)
		// per_user_concurrency 没变时整个容量校验都不该被触发，连房间状态都不该查。
		require.Zero(t, repo.stateCalls, "unchanged per_user_concurrency must not trigger the room capacity lookup")
	})
}

// 防止修过头：per_user_concurrency 真的调大且超过房间容量时，必须继续拒绝。
// 同时钉死上限来源是「配置并发」而不是按健康度过滤过的 listing.AccountConcurrency。
func TestAccountShareSquareRegressionRaisingPerUserConcurrencyBeyondRoomCapacityStillRejected(t *testing.T) {
	t.Run("没有房间状态仓储时回退到 listing.AccountConcurrency 兜底", func(t *testing.T) {
		repo := &accountShareModeRepoStub{
			listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 2, AccountConcurrency: 4},
			updateListing: accountShareSquareUpdatedListing(),
		}
		svc := &AccountShareModeService{repo: repo}
		perUser := 8
		expectedVersion := int64(1)

		_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
			PerUserConcurrency: &perUser,
			ExpectedVersion:    &expectedVersion,
			Reason:             "提高单用户并发",
		})

		require.ErrorIs(t, err, ErrAccountShareModeInvalidConcurrency)
		appErr := infraerrors.FromError(err)
		require.NotNil(t, appErr)
		require.Equal(t, "per_user_concurrency", appErr.Metadata["field"])
		require.Equal(t, "4", appErr.Metadata["maximum"])
		require.Zero(t, repo.updateCalls, "rejected capacity update must not reach the repository")
	})

	t.Run("超过房间配置容量被拒", func(t *testing.T) {
		base := &accountShareModeRepoStub{
			// AccountConcurrency = 0：账号临时不可调度，兜底值不可用，上限必须来自配置容量。
			listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 2, AccountConcurrency: 0},
			updateListing: accountShareSquareUpdatedListing(),
		}
		repo := &accountShareSquareRoomStateRepoStub{
			accountShareModeRepoStub: base,
			state:                    &AccountShareRoomManagementState{ListingID: 7, ConfiguredTotalConcurrency: 6},
		}
		svc := &AccountShareModeService{repo: repo}
		perUser := 9
		expectedVersion := int64(1)

		_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
			PerUserConcurrency: &perUser,
			ExpectedVersion:    &expectedVersion,
			Reason:             "提高单用户并发",
		})

		require.ErrorIs(t, err, ErrAccountShareModeInvalidConcurrency)
		appErr := infraerrors.FromError(err)
		require.NotNil(t, appErr)
		require.Equal(t, "6", appErr.Metadata["maximum"], "上限必须是配置并发，不是按健康度过滤过的 account_concurrency")
		require.Equal(t, 1, repo.stateCalls)
		require.Equal(t, []int64{7}, repo.stateListings)
		require.Zero(t, base.updateCalls)
	})

	t.Run("恰好等于配置容量放行且不受健康度过滤影响", func(t *testing.T) {
		base := &accountShareModeRepoStub{
			listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 2, AccountConcurrency: 0},
			updateListing: accountShareSquareUpdatedListing(),
		}
		repo := &accountShareSquareRoomStateRepoStub{
			accountShareModeRepoStub: base,
			state:                    &AccountShareRoomManagementState{ListingID: 7, ConfiguredTotalConcurrency: 6},
		}
		svc := &AccountShareModeService{repo: repo}
		perUser := 6
		expectedVersion := int64(1)

		_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
			PerUserConcurrency: &perUser,
			ExpectedVersion:    &expectedVersion,
			Reason:             "提高单用户并发",
		})

		require.NoError(t, err)
		require.Equal(t, 1, base.updateCalls)
		require.NotNil(t, base.updateInput.PerUserConcurrency)
		require.Equal(t, 6, *base.updateInput.PerUserConcurrency)
	})

	t.Run("全局硬上限仍然先行拦截", func(t *testing.T) {
		repo := &accountShareModeRepoStub{
			listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 2, AccountConcurrency: 4},
			updateListing: accountShareSquareUpdatedListing(),
		}
		svc := &AccountShareModeService{repo: repo}
		perUser := AccountShareModeMaxPerUserConcurrency + 1
		expectedVersion := int64(1)

		_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
			PerUserConcurrency: &perUser,
			ExpectedVersion:    &expectedVersion,
			Reason:             "提高单用户并发",
		})

		require.ErrorIs(t, err, ErrAccountShareModeInvalidConcurrency)
		require.Zero(t, repo.updateCalls)
		require.Empty(t, repo.getListingIDs, "全局上限应在读 listing 之前就拦下")
	})
}

// 守的 bug：service 层曾经有一条「合约字段必须带 edit_session_id」的前置判定，
// 它的条件与仓储 consumerSafeUpdate 免锁分支的进入条件逐字相同，等于把整条
// 「消费者安全更新」路径堵死 —— 房间一有人用，房主就永远保存不了配置。
// 现在裁决权归仓储（account_share_mode_repo.go 的 contractUpdate / consumerSafeUpdate），
// service 必须把空的 edit_session_id 原样转交下去。
func TestAccountShareSquareRegressionSessionlessContractUpdateIsDelegatedToRepository(t *testing.T) {
	seatLimit := 6
	rateMultiplier := 0.8
	hourlyRate := 1.5
	minBalance := 2.0
	waiverMinimum := 0.5
	models := []string{"gpt-5.5"}
	codexCLIOnly := true

	cases := []struct {
		name  string
		input UpdateAccountShareListingInput
	}{
		{"减少席位", UpdateAccountShareListingInput{SeatLimit: &seatLimit}},
		{"下调倍率", UpdateAccountShareListingInput{RateMultiplier: &rateMultiplier}},
		{"下调时租", UpdateAccountShareListingInput{HourlyRate: &hourlyRate}},
		{"调整最低余额", UpdateAccountShareListingInput{MinBalanceRequired: &minBalance}},
		{"调整免单门槛", UpdateAccountShareListingInput{HourlyFeeWaiverMinimum: &waiverMinimum}},
		{"新增可用模型", UpdateAccountShareListingInput{AllowedModels: &models}},
		{"限制 CodexCLI", UpdateAccountShareListingInput{CodexCLIOnly: &codexCLIOnly}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &accountShareModeRepoStub{
				listing:       &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42, PerUserConcurrency: 5, AccountConcurrency: 20},
				updateListing: accountShareSquareUpdatedListing(),
			}
			svc := &AccountShareModeService{repo: repo}
			expectedVersion := int64(1)

			input := tc.input
			input.ExpectedVersion = &expectedVersion
			input.Reason = "房主调整合约"
			// 刻意不带 EditSessionID：免锁保存恒不带 session。

			_, err := svc.UpdateListing(context.Background(), 42, false, 7, input)

			require.NotErrorIs(t, err, ErrAccountShareEditSessionRequired,
				"service 层不得再前置拒绝无编辑锁的合约更新，裁决权在仓储")
			require.NoError(t, err)
			require.Equal(t, 1, repo.updateCalls, "sessionless contract update must be forwarded to the repository")
			require.Empty(t, repo.updateInput.EditSessionID, "空 edit_session_id 必须原样转交给仓储")
		})
	}
}

// squareDeletionBlocked 按 account_repo.go 的 conflictError 实际写出的 metadata 键名构造删除拦截错误。
func squareDeletionBlocked(blockerTypes string, extra map[string]string) error {
	metadata := map[string]string{
		"account_id":    "55",
		"blocker_types": blockerTypes,
	}
	for k, v := range extra {
		metadata[k] = v
	}
	return ErrAccountDeletionBlocked.WithMetadata(metadata)
}

// 守两个方向相反的 bug。
//
// 方向一（不能太松）：force 删除曾对任何含 room_account 的拦截都自动退房重试。
// 若同时存在退房解不掉的占用（queued/ending 的 membership、挂在非 active membership 上的
// 未闭合 binding、未结算计费），退房会成功、删除仍失败 —— 账号被不可逆地摘出房间却没删掉，
// 而且 room_account 拦截随之消失，用户下次再点删除连二次确认弹窗都不会再出现。
//
// 方向二（不能太紧）：退房**会**把 status='active' 的 membership 重绑到房间内的健康替补账号
// （account_share_room_repo.go 的 lockAccountShareMembershipsForAccountSetRebindInTx +
// UPDATE account_share_memberships SET account_id = <replacement>），所以「房间里有活跃租户」
// 恰恰是退房可解的主流场景。一刀切按 blocker_types 拒绝会把本来能删的号变成删不掉。
//
// 判据因此不看 blocker_types，只认仓储精确算出的 metadata.detach_resolvable。
func TestAccountShareSquareRegressionDeletionBlockerDetachResolvability(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		canResolve bool
	}{
		{
			name: "仅房间挂载可以退房重试",
			err: squareDeletionBlocked("room_account", map[string]string{
				"room_listing_ids":   "91",
				"room_account_count": "1",
				"room_listing_names": "OpenAI共享账号26",
				"detach_resolvable":  "true",
			}),
			canResolve: true,
		},
		{
			// 主流场景：房间里有活跃租户，但房内有健康替补账号，退房会重绑，删除随后成功。
			name: "活跃席位可被重绑时仍可退房重试",
			err: squareDeletionBlocked("room_account,live_membership", map[string]string{
				"room_listing_ids":              "91",
				"live_membership_count":         "2",
				"unresolvable_membership_count": "0",
				"unresolvable_binding_count":    "0",
				"detach_resolvable":             "true",
			}),
			canResolve: true,
		},
		{
			name: "排队或退租中的席位不可退房重试",
			err: squareDeletionBlocked("room_account,live_membership", map[string]string{
				"room_listing_ids":              "91",
				"live_membership_count":         "2",
				"unresolvable_membership_count": "1",
				"detach_resolvable":             "false",
			}),
			canResolve: false,
		},
		{
			name: "挂在非活跃席位上的未闭合绑定不可退房重试",
			err: squareDeletionBlocked("room_account,open_binding", map[string]string{
				"room_listing_ids":           "91",
				"open_binding_count":         "1",
				"unresolvable_binding_count": "1",
				"detach_resolvable":          "false",
			}),
			canResolve: false,
		},
		{
			name: "待结算计费不可退房重试",
			err: squareDeletionBlocked("room_account,pending_billing_intent", map[string]string{
				"room_listing_ids":             "91",
				"pending_billing_intent_count": "3",
				"detach_resolvable":            "false",
			}),
			canResolve: false,
		},
		{
			name: "没有房间可退时不退房",
			err: squareDeletionBlocked("live_membership", map[string]string{
				"live_membership_count": "2",
				"detach_resolvable":     "false",
			}),
			canResolve: false,
		},
		{
			// 判错的代价不对称：宁可让用户手动处理，也不能误判成可解而造成破坏性半失败。
			name: "metadata 缺 detach_resolvable 时按不可解处理",
			err: squareDeletionBlocked("room_account", map[string]string{
				"room_listing_ids": "91",
			}),
			canResolve: false,
		},
		{
			name: "非删除拦截类错误不参与退房判定",
			err: ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
				"blocker_types":     "room_account",
				"room_listing_ids":  "91",
				"detach_resolvable": "true",
			}),
			canResolve: false,
		},
		{
			name:       "普通错误不参与退房判定",
			err:        errors.New("boom"),
			canResolve: false,
		},
		{
			name:       "nil 错误不参与退房判定",
			err:        nil,
			canResolve: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.canResolve, canResolveDeletionBlockersByDetach(tc.err))
		})
	}
}

// 端到端守方向一：detach_resolvable=false 时一次退房都不许发起，原始 409 必须原样回到前端。
func TestAccountShareSquareRegressionForceDeleteSkipsDetachOnMixedBlockers(t *testing.T) {
	ownerUserID := int64(9)
	mixed := squareDeletionBlocked("room_account,live_membership", map[string]string{
		"room_listing_ids":              "91",
		"room_account_count":            "1",
		"live_membership_count":         "2",
		"unresolvable_membership_count": "2",
		"detach_resolvable":             "false",
	})

	t.Run("单个删除", func(t *testing.T) {
		repo := &accountRepoStub{
			account:   &Account{ID: 55, OwnerUserID: &ownerUserID},
			deleteErr: mixed,
		}
		roomRepo := &detachRoomRepoStub{}
		svc := &AccountService{accountRepo: repo, accountShareRoomRepo: roomRepo}

		err := svc.DeleteOwned(context.Background(), ownerUserID, 55, true)

		require.ErrorIs(t, err, ErrAccountDeletionBlocked)
		appErr := infraerrors.FromError(err)
		require.NotNil(t, appErr)
		require.Equal(t, "room_account,live_membership", appErr.Metadata["blocker_types"])
		require.Empty(t, roomRepo.detachCalls,
			"混合拦截下退房只会把账号不可逆地摘出房间，删除依旧失败，绝不能自动发起")
		require.Empty(t, repo.ownedDeletedIDs)
	})

	t.Run("批量删除", func(t *testing.T) {
		repo := &accountRepoStub{
			accounts: []*Account{
				{ID: 55, OwnerUserID: &ownerUserID},
				{ID: 56, OwnerUserID: &ownerUserID},
			},
			deleteManyErr: mixed,
		}
		roomRepo := &detachRoomRepoStub{}
		svc := &AccountService{accountRepo: repo, accountShareRoomRepo: roomRepo}

		result, err := svc.BulkDeleteOwned(context.Background(), ownerUserID, []int64{55, 56}, true)

		require.Nil(t, result)
		require.ErrorIs(t, err, ErrAccountDeletionBlocked)
		require.Empty(t, roomRepo.detachCalls)
		require.Empty(t, repo.ownedDeletedIDs)
	})
}

// 反向守卫：纯 room_account 拦截时自动退房重试这条路必须依旧活着，
// 否则「广场用过的号不能删号」会以另一种方式复发。
func TestAccountShareSquareRegressionForceDeleteStillDetachesRoomOnlyBlocker(t *testing.T) {
	ownerUserID := int64(9)
	repo := &accountRepoStub{
		account:         &Account{ID: 55, OwnerUserID: &ownerUserID},
		ownedDeleteErrs: []error{roomAccountBlocked(55), nil},
	}
	roomRepo := &detachRoomRepoStub{}
	svc := &AccountService{accountRepo: repo, accountShareRoomRepo: roomRepo}

	err := svc.DeleteOwned(context.Background(), ownerUserID, 55, true)

	require.NoError(t, err)
	require.Len(t, roomRepo.detachCalls, 1)
	require.Equal(t, int64(91), roomRepo.detachCalls[0].ListingID)
	require.Equal(t, []int64{55}, repo.ownedDeletedIDs)
}
