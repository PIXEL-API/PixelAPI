//go:build unit

package service

import (
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// 投放守卫的核心契约：只看"值真的变了"，并且把敏感字段拆成处置方式不同的两组。
// 旧实现按"payload 里出现了哪些字段"判定，导致管理端整表单提交的编辑弹窗
// 保存任何字段都会被打回。

func TestClassifyAccountMutationRecordsSensitiveFieldsSeparately(t *testing.T) {
	before := &Account{
		ID:          7,
		Name:        "before",
		Concurrency: 10,
		Priority:    1,
	}
	after := &Account{
		ID:          7,
		Name:        "after",
		Concurrency: 3,
		Priority:    9,
	}

	diff := ClassifyAccountMutation(before, after, nil, nil)

	require.True(t, diff.Sensitive)
	require.Equal(t, []string{"concurrency", "name", "priority"}, diff.ChangedFields)
	// 改名和调优先级不影响在用消费者，只有降并发是敏感的。
	require.Equal(t, []string{"concurrency"}, diff.SensitiveFields)
}

func TestClassifyAccountPlacementImpactSeparatesHardLockedFromForceable(t *testing.T) {
	owner := int64(5112)
	before := &Account{ID: 7, OwnerUserID: &owner, AccountLevel: "plus", Concurrency: 10}
	after := &Account{ID: 7, OwnerUserID: &owner, AccountLevel: "pro", Concurrency: 3}

	impact := ClassifyAccountPlacementImpact(ClassifyAccountMutation(before, after, nil, nil))

	// account_level 被数据库触发器锁死，强制确认也绕不过去，只能先转出投放。
	require.Equal(t, []string{"account_level"}, impact.ConversionFields)
	// 降并发影响在用消费者，但可以在填写理由后强制修改。
	require.Equal(t, []string{"concurrency"}, impact.ForceFields)
	require.True(t, impact.RequiresConversion())
	require.True(t, impact.RequiresForce())
}

func TestClassifyAccountPlacementImpactIgnoresUnchangedFields(t *testing.T) {
	owner := int64(5112)
	account := func() *Account {
		return &Account{
			ID:           7,
			Name:         "heavy",
			OwnerUserID:  &owner,
			AccountLevel: "plus",
			Concurrency:  30,
			Credentials:  map[string]any{"access_token": "tok"},
			Extra:        map[string]any{"grok_client_tool_cache": true},
		}
	}
	before := account()
	after := account()
	// 只改了并发数（而且是调高），其余字段原样回传——这正是管理端编辑弹窗的形态。
	after.Concurrency = 50

	impact := ClassifyAccountPlacementImpact(
		ClassifyAccountMutation(before, after, []int64{3, 9}, []int64{9, 3}),
	)

	require.False(t, impact.RequiresConversion())
	require.False(t, impact.RequiresForce())
}

func TestClassifyAccountPlacementImpactTreatsModelRoutingAsNonIdentity(t *testing.T) {
	before := &Account{ID: 7, Credentials: map[string]any{
		"access_token":  "tok",
		"model_mapping": map[string]any{"grok-4.5": "grok-4.5"},
	}}
	after := &Account{ID: 7, Credentials: map[string]any{
		"access_token":  "tok",
		"model_mapping": map[string]any{"grok-4.5": "grok-4.3"},
	}}

	diff := ClassifyAccountMutation(before, after, nil, nil)
	require.True(t, diff.Sensitive, "凭证整体仍算敏感变更")

	impact := ClassifyAccountPlacementImpact(diff)
	// 只动模型映射不改变消费者用的是哪个上游账号，不该要求强制确认。
	require.False(t, impact.RequiresForce())
	require.False(t, impact.RequiresConversion())
}

func TestClassifyAccountPlacementImpactTreatsCredentialRotationAsForceable(t *testing.T) {
	before := &Account{ID: 7, Credentials: map[string]any{"access_token": "old"}}
	after := &Account{ID: 7, Credentials: map[string]any{"access_token": "new"}}

	impact := ClassifyAccountPlacementImpact(ClassifyAccountMutation(before, after, nil, nil))

	require.Equal(t, []string{"credentials"}, impact.ForceFields)
	require.False(t, impact.RequiresConversion())
}

func TestClassifyAccountPlacementImpactIgnoresSystemDrivenShareStatus(t *testing.T) {
	before := &Account{ID: 7, ShareMode: AccountShareModePublic, ShareStatus: AccountShareStatusApproved}
	after := &Account{ID: 7, ShareMode: AccountShareModePublic, ShareStatus: AccountShareStatusPending}

	diff := ClassifyAccountMutation(before, after, nil, nil)
	require.Contains(t, diff.SensitiveFields, "share_status")

	impact := ClassifyAccountPlacementImpact(diff)
	// 改凭证/等级后系统会自动把公共池账号打回 pending 重验。那是系统的自我保护，
	// 不该要求管理员为它填写"修改原因"。
	require.False(t, impact.RequiresForce())
	require.False(t, impact.RequiresConversion())
}

func TestClassifyAccountPlacementImpactRequiresConversionForShareMode(t *testing.T) {
	before := &Account{ID: 7, ShareMode: AccountShareModePublic}
	after := &Account{ID: 7, ShareMode: AccountShareModePrivate}

	impact := ClassifyAccountPlacementImpact(ClassifyAccountMutation(before, after, nil, nil))

	// share_mode 是投放目标的投影，改它等于换投放，必须走转换接口。
	require.Equal(t, []string{"share_mode"}, impact.ConversionFields)
}

func TestAccountPlacementConversionRequiredCarriesActionableMetadata(t *testing.T) {
	roomID := int64(42)
	account := &Account{
		ID: 706602,
		ExternalPlacement: &AccountExternalPlacement{
			Target:  AccountExternalPlacementRoom,
			RoomID:  &roomID,
			Version: 7,
		},
	}

	err := AccountPlacementConversionRequired(account, []string{"account_level", "owner_user_id"})

	appErr := infraerrors.FromError(err)
	require.NotNil(t, appErr)
	require.Equal(t, "OWNED_ACCOUNT_PLACEMENT_CONVERSION_REQUIRED", appErr.Reason)
	require.Equal(t, "convert_external_placement", appErr.Metadata["required_action"])
	require.Equal(t, "account_level,owner_user_id", appErr.Metadata["changed_fields"])
	require.Equal(t, "706602", appErr.Metadata["account_id"])
	require.Equal(t, AccountExternalPlacementRoom, appErr.Metadata["placement_target"])
	require.Equal(t, "42", appErr.Metadata["room_id"])
	require.Equal(t, "7", appErr.Metadata["placement_version"])
}
