//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// candidateSamplingSettingRepoStub implements just SettingRepository.GetMultiple;
// the embedded interface is nil (other methods are unused on this path).
type candidateSamplingSettingRepoStub struct {
	SettingRepository
	values map[string]string
	calls  int
}

func (s *candidateSamplingSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	s.calls++
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := s.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

// resetCandidateSamplingSettingCache clears the package-level cache so tests are
// deterministic regardless of ordering.
func resetCandidateSamplingSettingCache() {
	schedulerCandidateSamplingSettingSF.Forget(schedulerCandidateSamplingSettingSFKey)
	schedulerCandidateSamplingSettingCache.Store((*cachedSchedulerCandidateSamplingSetting)(nil))
}

func TestCandidateSamplingSettings_ReadsAndClamps(t *testing.T) {
	resetCandidateSamplingSettingCache()
	repo := &candidateSamplingSettingRepoStub{values: map[string]string{
		SettingKeySchedulerCandidateSamplingEnabled:   "true",
		SettingKeySchedulerCandidateSamplingLimit:     "2000", // over max -> clamp to 1024
		SettingKeySchedulerCandidateSamplingThreshold: "8000",
	}}
	svc := &SchedulerSnapshotService{settingRepo: repo}

	enabled, limit, threshold := svc.candidateSamplingSettings(context.Background())
	require.True(t, enabled)
	require.Equal(t, maxSchedulerCandidateSamplingLimit, limit)
	require.Equal(t, 8000, threshold)
}

func TestCandidateSamplingSettings_CachedNoRepeatReads(t *testing.T) {
	resetCandidateSamplingSettingCache()
	repo := &candidateSamplingSettingRepoStub{values: map[string]string{
		SettingKeySchedulerCandidateSamplingEnabled: "true",
	}}
	svc := &SchedulerSnapshotService{settingRepo: repo}

	for i := 0; i < 5; i++ {
		enabled, _, _ := svc.candidateSamplingSettings(context.Background())
		require.True(t, enabled)
	}
	require.Equal(t, 1, repo.calls, "settings should be cached, not read per call")
}

func TestCandidateSamplingSettings_DefaultsWhenNoRepo(t *testing.T) {
	resetCandidateSamplingSettingCache()
	svc := &SchedulerSnapshotService{}

	enabled, limit, threshold := svc.candidateSamplingSettings(context.Background())
	require.False(t, enabled, "disabled by default")
	require.Equal(t, defaultSchedulerCandidateSamplingLimit, limit)
	require.Equal(t, defaultSchedulerCandidateSamplingThreshold, threshold)
}
