package service

import "log/slog"

// 自有账号凭证安全扫描的作用域。
//
// accounts.credentials 与 accounts.extra 这两个 JSONB 里混着两类数据：
// 一类是账号所有者提交的鉴权凭据，另一类是服务端自己写入的运行状态
// （令牌刷新结果、配额/用量快照、限流簿记、探测结果、隐私标记等）。
// 凭证安全扫描本来是用来约束"用户提交了什么"的，早期实现却在每次所有者
// 更新时对库内完整对象重跑一遍，于是任何由系统或管理员写进去的值都会变成
// 所有者永久无法通过的 400——哪怕这次请求只是切一下调度开关。
//
// 作用域把契约改回它本来的样子：
//   - 新建 / 导入，以及账号即将对外提供服务的准入闸口，仍然全量扫描；
//   - 所有者更新只扫描本次请求相对库内快照新增或改动的部分。
type ownedSourceScanMode int

const (
	// ownedSourceScanFull 扫描整份 credentials/extra。
	ownedSourceScanFull ownedSourceScanMode = iota
	// ownedSourceScanDelta 只扫描相对库内快照发生变化的部分。
	ownedSourceScanDelta
)

type ownedAccountSourceScope struct {
	Mode              ownedSourceScanMode
	StoredCredentials map[string]any
	StoredExtra       map[string]any
}

func (s ownedAccountSourceScope) credentialsToScan(credentials map[string]any) map[string]any {
	if s.Mode != ownedSourceScanDelta {
		return credentials
	}
	return changedAccountMapSubset(s.StoredCredentials, credentials)
}

func (s ownedAccountSourceScope) extraToScan(extra map[string]any) map[string]any {
	if s.Mode != ownedSourceScanDelta {
		return extra
	}
	return changedAccountMapSubset(s.StoredExtra, extra)
}

// sanitizeOwnedAccountCredentialWrite 是系统侧凭证写入的收口点：丢弃后台写入者
// 新引入或改动的、自有账号安全扫描不接受的顶层凭证字段。
//
// 这是"系统写入永远不会把账号所有者锁在门外"的结构性保证。它只丢字段、从不让刷新
// 失败——续上令牌比留一条运维提示重要得多；已经被污染的历史数据也会在下一次刷新时
// 自愈。只处理顶层键即可：各平台的 Build*AccountCredentials 写的都是扁平标量。
func sanitizeOwnedAccountCredentialWrite(account *Account, next map[string]any) map[string]any {
	if account == nil || account.OwnerUserID == nil || len(next) == 0 {
		return next
	}
	for {
		delta := changedAccountMapSubset(account.Credentials, next)
		field, blocked := findDisallowedOwnedAccountField(delta)
		if !blocked {
			return next
		}
		if _, present := next[field]; !present {
			// 违规内容嵌在某个值内部而不是顶层键上，这里无法安全裁剪，
			// 交给上层扫描按原规则处理，避免静默吞掉。
			return next
		}
		slog.Warn("account_system_credential_write_dropped",
			"account_id", account.ID,
			"owner_user_id", *account.OwnerUserID,
			"platform", account.Platform,
			"field", field,
		)
		delete(next, field)
	}
}

// changedAccountMapSubset 返回 next 中相对 base 新增或改动的条目。
//
// 嵌套结构被保留，使依赖父级键名的规则（disallowedCredentialStringReason 的
// parentKey）仍然按原语义生效。base 里有而 next 里没有的键会被忽略：删除一个
// 字段永远不可能引入违规内容。切片整体比较，只要有差异就整体纳入扫描——偏保守，
// 方向是安全的。
func changedAccountMapSubset(base, next map[string]any) map[string]any {
	if len(next) == 0 {
		return nil
	}
	out := make(map[string]any, len(next))
	for key, value := range next {
		baseValue, exists := base[key]
		if !exists {
			out[key] = value
			continue
		}
		if nextMap, isMap := value.(map[string]any); isMap {
			if baseMap, baseIsMap := baseValue.(map[string]any); baseIsMap {
				if sub := changedAccountMapSubset(baseMap, nextMap); len(sub) > 0 {
					out[key] = sub
				}
				continue
			}
			out[key] = value
			continue
		}
		if !sameAccountJSONValue(baseValue, value) {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
