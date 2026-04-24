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
