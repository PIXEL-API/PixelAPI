import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

// 用户侧「账号模式切换」失败时，前端走
// extractI18nErrorMessage(err, t, 'userAccounts.externalPlacement.errors', ...)，
// 按后端 reason 码查表。少一个码就退回后端英文原文——线上真出过：账号调度关着切公共号池，
// 用户只看到 "public account validation failed"，完全不知道要去打开调度开关。
//
// 下面这张表是 AccountService.ConvertOwnedExternalPlacement 在用户侧可达的全部拒绝原因：
// - backend/internal/service/account_service.go  ConvertOwnedExternalPlacement
// - backend/internal/service/account_share_mode.go  投放相关错误定义
// 后端新增拒绝原因时，这个用例会先红。
const CONVERT_REASON_CODES = [
  'OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED',
  'OWNED_ACCOUNT_PUBLIC_POOL_UNAVAILABLE',
  'OWNED_ACCOUNT_PUBLIC_POLICY_UNAVAILABLE',
  'ACCOUNT_SHARE_ROOM_ACCOUNT_ATTACHED',
  'ACCOUNT_EXTERNAL_PLACEMENT_BUSY',
  'ACCOUNT_EXTERNAL_PLACEMENT_CONFLICT',
  'ACCOUNT_EXTERNAL_PLACEMENT_INVALID',
  'ACCOUNT_SHARE_ROOM_UNKNOWN_LEVEL',
  'ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE',
  // 次级校验 / 基础设施拒绝，同样会从 convert 接口透出
  'OWNED_ACCOUNT_TYPE_NOT_ALLOWED',
  'OWNED_AGENT_IDENTITY_CREDENTIALS_INVALID',
  'OWNED_ACCOUNT_CREDENTIALS_INVALID',
  'OWNED_ACCOUNT_CREDENTIALS_NOT_ALLOWED',
  'OWNED_ACCOUNT_GROUP_PLATFORM_MISMATCH',
  'OWNED_ACCOUNT_GROUP_VALIDATION_UNAVAILABLE',
  'OWNED_ACCOUNT_SHARE_MODE_BOUNDARY_UNAVAILABLE',
  'ACCOUNT_EXTERNAL_PLACEMENT_IDEMPOTENCY_CONFLICT',
  'ACCOUNT_SHARE_ROOM_OWNER_MISMATCH',
  // 私人群缺失/订阅异常（getPrivateGroupForOwnedAccount 与 repo 层
  // accountOwnerPrivateGroupIDInTx 可达），切换任意模式都会被拒
  'ACCOUNT_SHARE_PRIVATE_GROUP_UNAVAILABLE',
  'USER_PRIVATE_GROUP_PLATFORM_UNSUPPORTED',
  'SUBSCRIPTION_EXPIRED',
  'GROUP_NOT_ALLOWED'
] as const

describe('external placement convert error i18n', () => {
  it.each(CONVERT_REASON_CODES)('maps %s in both locales', (code) => {
    const zhMessage = (zh.userAccounts.externalPlacement.errors as Record<string, string>)[code]
    const enMessage = (en.userAccounts.externalPlacement.errors as Record<string, string>)[code]

    expect(zhMessage, `zh 缺少 ${code} 的文案`).toBeTruthy()
    expect(enMessage, `en is missing a message for ${code}`).toBeTruthy()
    // 占位没填、直接把错误码当文案抄进去，等于没修
    expect(zhMessage).not.toBe(code)
    expect(enMessage).not.toBe(code)
    // 中文表里混进英文原文（例如直接粘贴后端 message）同样会让用户看不懂
    expect(zhMessage).toMatch(/[一-龥]/)
  })

  it('keeps the unschedulable pre-block hint in both locales', () => {
    // 选择器提前禁用「公共号池」时显示的原因，见 EditAccountModal 的
    // placementTargetDisabledReasons
    expect(zh.userAccounts.externalPlacement.publicPoolUnschedulableHint).toBeTruthy()
    expect(en.userAccounts.externalPlacement.publicPoolUnschedulableHint).toBeTruthy()
    expect(zh.userAccounts.externalPlacement.publicPoolUnschedulableHint).toMatch(/[一-龥]/)
  })
})
