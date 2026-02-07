//go:build unit

package ip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		// 私有 IPv4
		{"10.x 私有地址", "10.0.0.1", true},
		{"10.x 私有地址段末", "10.255.255.255", true},
		{"172.16.x 私有地址", "172.16.0.1", true},
		{"172.31.x 私有地址", "172.31.255.255", true},
		{"192.168.x 私有地址", "192.168.1.1", true},
		{"127.0.0.1 本地回环", "127.0.0.1", true},
		{"127.x 回环段", "127.255.255.255", true},

		// 公网 IPv4
		{"8.8.8.8 公网 DNS", "8.8.8.8", false},
		{"1.1.1.1 公网", "1.1.1.1", false},
		{"172.15.255.255 非私有", "172.15.255.255", false},
		{"172.32.0.0 非私有", "172.32.0.0", false},
		{"11.0.0.1 公网", "11.0.0.1", false},

		// IPv6
		{"::1 IPv6 回环", "::1", true},
		{"fc00:: IPv6 私有", "fc00::1", true},
		{"fd00:: IPv6 私有", "fd00::1", true},
		{"2001:db8::1 IPv6 公网", "2001:db8::1", false},

		// 无效输入
		{"空字符串", "", false},
		{"非法字符串", "not-an-ip", false},
		{"不完整 IP", "192.168", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isPrivateIP(tc.ip)
			require.Equal(t, tc.expected, got, "isPrivateIP(%q)", tc.ip)
		})
	}
}
