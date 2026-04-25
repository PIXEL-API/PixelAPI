//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type affiliateSettingRepoStub struct {
	value string
	err   error
}

func (s *affiliateSettingRepoStub) Get(context.Context, string) (*Setting, error) { return nil, s.err }
func (s *affiliateSettingRepoStub) GetValue(context.Context, string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.value, nil
}
func (s *affiliateSettingRepoStub) Set(context.Context, string, string) error { return s.err }
func (s *affiliateSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return map[string]string{}, nil
}
func (s *affiliateSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return s.err
}
func (s *affiliateSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return map[string]string{}, nil
}
func (s *affiliateSettingRepoStub) Delete(context.Context, string) error { return s.err }

func TestAffiliateRebateRatePercentSemantics(t *testing.T) {
	t.Parallel()

	svc := &AffiliateService{settingRepo: &affiliateSettingRepoStub{value: "1"}}
	rate := svc.loadAffiliateRebateRatePercent(context.Background())
	require.Equal(t, 1.0, rate)

	svc.settingRepo = &affiliateSettingRepoStub{value: "0.2"}
	rate = svc.loadAffiliateRebateRatePercent(context.Background())
	require.Equal(t, 0.2, rate)
}

func TestMaskEmail(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a***@g***.com", maskEmail("alice@gmail.com"))
	require.Equal(t, "x***@d***", maskEmail("x@domain"))
	require.Equal(t, "", maskEmail(""))
}

func TestIsValidAffiliateCodeFormat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"valid canonical", "ABCDEFGHJKLM", true},
		{"valid all digits 2-9", "234567892345", true},
		{"valid mixed", "A2B3C4D5E6F7", true},
		{"too short", "ABCDEFGHJKL", false},
		{"too long", "ABCDEFGHJKLMN", false},
		{"contains excluded letter I", "IBCDEFGHJKLM", false},
		{"contains excluded letter O", "OBCDEFGHJKLM", false},
		{"contains excluded digit 0", "0BCDEFGHJKLM", false},
		{"contains excluded digit 1", "1BCDEFGHJKLM", false},
		{"lowercase rejected (caller must ToUpper first)", "abcdefghjklm", false},
		{"empty", "", false},
		{"12-byte utf8 non-ascii", "ÄÄÄÄÄÄ", false}, // 6×2 bytes = 12 bytes, bytes out of charset
		{"ascii punctuation", "ABCDEFGHJK.M", false},
		{"whitespace", "ABCDEFGHJK M", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isValidAffiliateCodeFormat(tc.in))
		})
	}
}
