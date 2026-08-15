package service

import "strings"

const (
	VideoBillingResolution480P  = "480p"
	VideoBillingResolution720P  = "720p"
	VideoBillingResolution1080P = "1080p"

	VideoBillingMinDurationSeconds     = 1
	VideoBillingMaxDurationSeconds     = 15
	VideoBillingDefaultDurationSeconds = 8
)

// NormalizeVideoBillingDurationSecondsOrDefault aligns billing with xAI's 1-15 second range.
// Missing or invalid non-positive values use xAI's default duration of eight seconds.
func NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds int) int {
	if durationSeconds <= 0 {
		return VideoBillingDefaultDurationSeconds
	}
	if durationSeconds < VideoBillingMinDurationSeconds {
		return VideoBillingMinDurationSeconds
	}
	if durationSeconds > VideoBillingMaxDurationSeconds {
		return VideoBillingMaxDurationSeconds
	}
	return durationSeconds
}

func NormalizeVideoBillingResolutionOrDefault(resolution string) string {
	if normalized, ok := NormalizeVideoBillingResolution(resolution); ok {
		return normalized
	}
	return VideoBillingResolution480P
}

// NormalizeVideoBillingResolution 仅接受 xAI 视频计费支持的分辨率及既有别名。
// 与带默认值的版本分离，避免管理端配置把拼写错误静默保存为 480p 价格。
func NormalizeVideoBillingResolution(resolution string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "480", "480p", "sd":
		return VideoBillingResolution480P, true
	case "720", "720p", "hd":
		return VideoBillingResolution720P, true
	case "1080", "1080p", "full_hd", "full-hd", "fhd":
		return VideoBillingResolution1080P, true
	default:
		return "", false
	}
}
