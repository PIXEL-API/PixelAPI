package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyExpiryMetricsRepositoryStub struct {
	expiredCalls      int
	expiringSoonCalls int
	expired           int64
	expiringSoon      int64
	err               error
}

func (s *proxyExpiryMetricsRepositoryStub) CountExpired(context.Context) (int64, error) {
	s.expiredCalls++
	return s.expired, s.err
}

func (s *proxyExpiryMetricsRepositoryStub) CountExpiringSoon(context.Context, time.Time) (int64, error) {
	s.expiringSoonCalls++
	return s.expiringSoon, s.err
}

func TestOpsAlertEvaluatorExposesProxyExpiryMetrics(t *testing.T) {
	metrics := &proxyExpiryMetricsRepositoryStub{expired: 3, expiringSoon: 5}
	svc := NewOpsAlertEvaluatorService(nil, nil, nil, nil, nil, metrics)
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)

	expired, ok := svc.computeRuleMetric(
		context.Background(),
		&OpsAlertRule{MetricType: "proxy_expired_count"},
		nil,
		now.Add(-time.Minute),
		now,
		"",
		nil,
	)
	require.True(t, ok)
	require.Equal(t, float64(3), expired)
	require.Equal(t, 1, metrics.expiredCalls)

	expiringSoon, ok := svc.computeRuleMetric(
		context.Background(),
		&OpsAlertRule{MetricType: "proxy_expiring_soon_count"},
		nil,
		now.Add(-time.Minute),
		now,
		"",
		nil,
	)
	require.True(t, ok)
	require.Equal(t, float64(5), expiringSoon)
	require.Equal(t, 1, metrics.expiringSoonCalls)
}

func TestOpsAlertEvaluatorFailsClosedWhenProxyExpiryMetricsFail(t *testing.T) {
	metrics := &proxyExpiryMetricsRepositoryStub{err: errors.New("metrics unavailable")}
	svc := NewOpsAlertEvaluatorService(nil, nil, nil, nil, nil, metrics)

	for _, metricType := range []string{"proxy_expired_count", "proxy_expiring_soon_count"} {
		value, ok := svc.computeRuleMetric(
			context.Background(),
			&OpsAlertRule{MetricType: metricType},
			nil,
			time.Time{},
			time.Time{},
			"",
			nil,
		)
		require.False(t, ok)
		require.Zero(t, value)
	}
}
