package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// OpencodeUsageWindow 是 opencode 订阅用量窗口的解析结果。
//
// opencode 官方未文档化 GET /zen/go/v1/usage 的响应结构，这里采用防御式解析：
// 任一字段缺失或格式变化都不会报错，只会跳过对应窗口的更新。
type OpencodeUsageWindow struct {
	Window            string     // "5h" | "7d" | "30d"
	Percent           *float64   // 已用百分比 (0-100)
	ResetsAt          *time.Time // 重置时刻（绝对值）
	ResetAfterSeconds *int       // 距重置的剩余秒数（上游只给相对值时）
}

// OpencodeUsageSnapshot 是 GET /zen/go/v1/usage 的归一化结果。
type OpencodeUsageSnapshot struct {
	UpdatedAt string
	Window5h  *OpencodeUsageWindow
	Window7d  *OpencodeUsageWindow
	Window30d *OpencodeUsageWindow
}

func (s *OpencodeUsageSnapshot) hasAny() bool {
	if s == nil {
		return false
	}
	return s.Window5h != nil || s.Window7d != nil || s.Window30d != nil
}

// ParseOpencodeUsage 把 opencode usage 响应解析成归一化快照。
// 真实结构（已实测）为 usage.rolling / usage.weekly / usage.monthly，每个窗口含
// percent(0-100) 与 resetsAt(ISO-8601)。解析失败仅跳过对应窗口，不报错。
func ParseOpencodeUsage(body []byte) *OpencodeUsageSnapshot {
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return nil
	}

	snapshot := &OpencodeUsageSnapshot{UpdatedAt: time.Now().UTC().Format(time.RFC3339)}

	// 真实结构：rolling=5h、weekly=7d、monthly=30d。
	if usageRoot := root.Get("usage"); usageRoot.Exists() && usageRoot.IsObject() {
		assignOpencodeNamedWindow(snapshot, OpencodeQuotaWindow5h, usageRoot, "rolling")
		assignOpencodeNamedWindow(snapshot, OpencodeQuotaWindow7d, usageRoot, "weekly")
		assignOpencodeNamedWindow(snapshot, OpencodeQuotaWindow30d, usageRoot, "monthly")
		if snapshot.hasAny() {
			return snapshot
		}
	}

	// 回退：上游若返回 windows 数组（可能嵌套在 usage 下）。
	windowsArr := root.Get("windows")
	if !windowsArr.Exists() || !windowsArr.IsArray() {
		windowsArr = root.Get("usage.windows")
	}
	if windowsArr.Exists() && windowsArr.IsArray() {
		for _, raw := range windowsArr.Array() {
			win := opencodeWindowFromResult(raw)
			if win == nil {
				continue
			}
			if win.Window == "" {
				win.Window = opencodeWindowID(raw)
			}
			opencodeSnapshotAssign(snapshot, win)
		}
		if snapshot.hasAny() {
			return snapshot
		}
		return nil
	}

	// 回退：命名字段。
	for _, candidate := range []struct {
		window string
		keys   []string
	}{
		{OpencodeQuotaWindow5h, []string{"five_hour", "5h", "fiveHour", "5_hour"}},
		{OpencodeQuotaWindow7d, []string{"weekly", "7d", "seven_day", "7_day", "week"}},
		{OpencodeQuotaWindow30d, []string{"monthly", "30d", "thirty_day", "30_day", "month"}},
	} {
		for _, key := range candidate.keys {
			raw := root.Get(key)
			if !raw.Exists() || !raw.IsObject() {
				continue
			}
			if win := opencodeWindowFromResult(raw); win != nil {
				win.Window = candidate.window
				opencodeSnapshotAssign(snapshot, win)
				break
			}
		}
	}

	if snapshot.hasAny() {
		return snapshot
	}
	return nil
}

// assignOpencodeNamedWindow 从 usage 下的命名字段（rolling/weekly/monthly）解析单个窗口。
func assignOpencodeNamedWindow(snapshot *OpencodeUsageSnapshot, window string, parent gjson.Result, key string) {
	raw := parent.Get(key)
	if !raw.Exists() || !raw.IsObject() {
		return
	}
	win := &OpencodeUsageWindow{Window: window}
	if p := raw.Get("percent"); p.Exists() {
		v := p.Float()
		win.Percent = &v
	}
	if t := raw.Get("resetsAt"); t.Exists() {
		if parsed := opencodeParseTime(t.String()); parsed != nil {
			win.ResetsAt = parsed
		}
	}
	if win.Percent == nil && win.ResetsAt == nil {
		return
	}
	opencodeSnapshotAssign(snapshot, win)
}

func opencodeWindowID(r gjson.Result) string {
	for _, key := range []string{"window", "id", "name", "period"} {
		if v := r.Get(key); v.Exists() {
			id := strings.ToLower(strings.TrimSpace(v.String()))
			switch id {
			case "5h", "five_hour", "5_hour":
				return OpencodeQuotaWindow5h
			case "7d", "weekly", "seven_day", "7_day", "week":
				return OpencodeQuotaWindow7d
			case "30d", "monthly", "thirty_day", "30_day", "month":
				return OpencodeQuotaWindow30d
			}
		}
	}
	return ""
}

func opencodeWindowFromResult(r gjson.Result) *OpencodeUsageWindow {
	win := &OpencodeUsageWindow{}

	for _, key := range []string{"percent", "used_percent", "usedPercent", "utilization", "used"} {
		if v := r.Get(key); v.Exists() {
			p := v.Float()
			win.Percent = &p
			break
		}
	}
	for _, key := range []string{"resets_at", "resetsAt", "reset_at", "resetAt"} {
		if v := r.Get(key); v.Exists() {
			if t := opencodeParseTime(v.String()); t != nil {
				win.ResetsAt = t
				break
			}
		}
	}
	for _, key := range []string{"reset_after_seconds", "resets_in_seconds", "resetsInSeconds", "reset_in"} {
		if v := r.Get(key); v.Exists() {
			s := int(v.Int())
			win.ResetAfterSeconds = &s
			break
		}
	}

	if win.Percent == nil && win.ResetsAt == nil && win.ResetAfterSeconds == nil {
		return nil
	}
	return win
}

func opencodeParseTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return &t
		}
	}
	return nil
}

func opencodeSnapshotAssign(snapshot *OpencodeUsageSnapshot, win *OpencodeUsageWindow) {
	if snapshot == nil || win == nil {
		return
	}
	switch win.Window {
	case OpencodeQuotaWindow5h:
		snapshot.Window5h = win
	case OpencodeQuotaWindow7d:
		snapshot.Window7d = win
	case OpencodeQuotaWindow30d:
		snapshot.Window30d = win
	}
}

// buildOpencodeUsageExtraUpdates 把解析后的快照归一化到账号 extra 的
// opencode_* 键，供调度守卫与用量展示读取。仅写出成功解析的窗口。
func buildOpencodeUsageExtraUpdates(snapshot *OpencodeUsageSnapshot, fallbackNow time.Time) map[string]any {
	if snapshot == nil || !snapshot.hasAny() {
		return nil
	}

	baseTime := fallbackNow
	if snapshot.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, snapshot.UpdatedAt); err == nil {
			baseTime = t
		}
	}

	updates := make(map[string]any)
	opencodeApplyWindowUpdates(updates, baseTime, OpencodeQuotaWindow5h, snapshot.Window5h)
	opencodeApplyWindowUpdates(updates, baseTime, OpencodeQuotaWindow7d, snapshot.Window7d)
	opencodeApplyWindowUpdates(updates, baseTime, OpencodeQuotaWindow30d, snapshot.Window30d)
	if len(updates) == 0 {
		return nil
	}
	updates["opencode_usage_updated_at"] = baseTime.Format(time.RFC3339)
	return updates
}

func opencodeApplyWindowUpdates(updates map[string]any, baseTime time.Time, window string, w *OpencodeUsageWindow) {
	if w == nil {
		return
	}
	if w.Percent != nil {
		updates["opencode_"+window+"_used_percent"] = *w.Percent
	}
	resetAt := w.ResetsAt
	if resetAt == nil && w.ResetAfterSeconds != nil && *w.ResetAfterSeconds >= 0 {
		computed := baseTime.Add(time.Duration(*w.ResetAfterSeconds) * time.Second)
		resetAt = &computed
	}
	if resetAt != nil {
		updates["opencode_"+window+"_reset_at"] = resetAt.Format(time.RFC3339)
	}
}

// buildOpencodeUsageProgressFromExtra 从账号 extra 构建单窗口用量进度，用于列表展示。
func buildOpencodeUsageProgressFromExtra(extra map[string]any, window string, now time.Time) *UsageProgress {
	if len(extra) == 0 {
		return nil
	}

	var usedPercentKey, resetAtKey string
	switch window {
	case OpencodeQuotaWindow5h:
		usedPercentKey, resetAtKey = "opencode_5h_used_percent", "opencode_5h_reset_at"
	case OpencodeQuotaWindow7d:
		usedPercentKey, resetAtKey = "opencode_7d_used_percent", "opencode_7d_reset_at"
	case OpencodeQuotaWindow30d:
		usedPercentKey, resetAtKey = "opencode_30d_used_percent", "opencode_30d_reset_at"
	default:
		return nil
	}

	usedRaw, ok := extra[usedPercentKey]
	if !ok {
		return nil
	}

	progress := &UsageProgress{Utilization: parseExtraFloat64(usedRaw)}
	if resetAtRaw, ok := extra[resetAtKey]; ok {
		if resetAt, err := parseTime(fmt.Sprint(resetAtRaw)); err == nil {
			progress.ResetsAt = &resetAt
			progress.RemainingSeconds = int(time.Until(resetAt).Seconds())
			if progress.RemainingSeconds < 0 {
				progress.RemainingSeconds = 0
			}
		}
	}

	// 窗口已过期（resetAt 在 now 之前）→ 额度已重置，归零。
	if progress.ResetsAt != nil && !now.Before(*progress.ResetsAt) {
		progress.Utilization = 0
		progress.WindowStart = nil
	}
	return progress
}
