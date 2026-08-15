package service

import "time"

// ResolveProxyFallbackTarget 计算过期代理的改投目标。
// change=false 表示保持原绑定；change=true 且 targetID=nil 表示改为直连。
func ResolveProxyFallbackTarget(start Proxy, byID map[int64]Proxy, now time.Time) (targetID *int64, change bool) {
	switch start.FallbackMode {
	case FallbackModeDirect:
		return nil, true
	case FallbackModeProxy:
		visited := map[int64]struct{}{start.ID: {}}
		currentID := start.BackupProxyID
		for currentID != nil {
			if _, seen := visited[*currentID]; seen {
				return nil, false
			}
			candidate, ok := byID[*currentID]
			if !ok {
				return nil, false
			}
			if candidate.Status == StatusActive && !candidate.IsExpired(now) {
				id := candidate.ID
				return &id, true
			}
			visited[*currentID] = struct{}{}
			switch candidate.FallbackMode {
			case FallbackModeDirect:
				return nil, true
			case FallbackModeProxy:
				currentID = candidate.BackupProxyID
			default:
				return nil, false
			}
		}
	}
	return nil, false
}

// CanAccountUseProxyFallback 复用本地代理可见性、平台/等级与容量规则，
// 防止到期 worker 绕过管理员和用户创建流程的既有安全边界。
func CanAccountUseProxyFallback(target Proxy, account Account, currentBindings int64, now time.Time) bool {
	if target.Status != StatusActive || target.IsExpired(now) {
		return false
	}
	if target.OwnerUserID != nil {
		if account.OwnerUserID == nil || *target.OwnerUserID != *account.OwnerUserID {
			return false
		}
	} else if !target.AllowsScope(account.Platform, account.AccountLevel) {
		return false
	}
	return target.MaxAccounts <= 0 || currentBindings < int64(target.MaxAccounts)
}
