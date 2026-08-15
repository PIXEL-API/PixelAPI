package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokVideoE2EDurationFromCreatedAt(t *testing.T) {
	t.Parallel()
	created := time.Now().UTC().Add(-45 * time.Second)
	duration := GrokVideoE2EDuration(created.Format(time.RFC3339Nano), time.Now().UTC())
	require.GreaterOrEqual(t, duration, 44*time.Second)
	require.LessOrEqual(t, duration, 47*time.Second)
	require.Zero(t, GrokVideoE2EDuration("", time.Now()))
	require.Zero(t, GrokVideoE2EDuration("not-a-time", time.Now()))
	require.Zero(t, GrokVideoE2EDuration(
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), time.Now(),
	))
}

func TestIsGrokVideoStatusBillableRequiresOfficialDoneAndURL(t *testing.T) {
	t.Parallel()
	require.True(t, IsGrokVideoStatusBillable([]byte(`{
		"status":"done",
		"model":"grok-imagine-video-1.5",
		"video":{"url":"https://vidgen.x.ai/x.mp4","duration":8}
	}`)))

	for _, body := range [][]byte{
		nil,
		[]byte(`{"status":"pending","video":{"url":"https://vidgen.x.ai/x.mp4"}}`),
		[]byte(`{"status":"expired","video":{"url":"https://vidgen.x.ai/x.mp4"}}`),
		[]byte(`{"status":"failed","video":{"url":"https://vidgen.x.ai/x.mp4"}}`),
		[]byte(`{"status":"done"}`),
		[]byte(`{"url":"https://vidgen.x.ai/x.mp4"}`),
		[]byte(`{"status":"completed","video":{"url":"https://vidgen.x.ai/x.mp4"}}`),
	} {
		require.False(t, IsGrokVideoStatusBillable(body), string(body))
	}
}

func TestExtractGrokVideoBillingFromStatusUsesStatusDurationAndPendingResolution(t *testing.T) {
	t.Parallel()
	pending := &GrokVideoPendingBilling{
		Model:                "pending-model",
		BillingModel:         "pending-billing",
		UpstreamModel:        "pending-upstream",
		VideoResolution:      VideoBillingResolution720P,
		VideoDurationSeconds: 8,
	}
	result := ExtractGrokVideoBillingFromStatusBody([]byte(`{
		"status":"done",
		"model":"grok-imagine-video-1.5",
		"video":{"url":"https://vidgen.x.ai/signed.mp4","duration":12}
	}`), pending, "req-1")

	require.NotNil(t, result)
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "grok-imagine-video-1.5", result.Model)
	require.Equal(t, "pending-billing", result.BillingModel)
	require.Equal(t, "pending-upstream", result.UpstreamModel)
	require.Equal(t, VideoBillingResolution720P, result.VideoResolution)
	require.Equal(t, 12, result.VideoDurationSeconds)
}

func TestExtractGrokVideoBillingFromStatusFallsBackToPending(t *testing.T) {
	t.Parallel()
	pending := &GrokVideoPendingBilling{
		Model:                "create-model",
		BillingModel:         "create-billing",
		UpstreamModel:        "create-upstream",
		VideoResolution:      VideoBillingResolution1080P,
		VideoDurationSeconds: 10,
	}
	result := ExtractGrokVideoBillingFromStatusBody(
		[]byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/signed.mp4"}}`),
		pending,
		"req-2",
	)
	require.NotNil(t, result)
	require.Equal(t, "create-billing", result.BillingModel)
	require.Equal(t, "create-upstream", result.UpstreamModel)
	require.Equal(t, VideoBillingResolution1080P, result.VideoResolution)
	require.Equal(t, 10, result.VideoDurationSeconds)
}

func TestGrokMediaUsageVideoCreateDefersAndDoneStatusBills(t *testing.T) {
	t.Parallel()
	requestInfo := GrokMediaRequestInfo{
		Model: "grok-imagine-video", Resolution: "720p", DurationSeconds: 10,
	}
	created, err := grokMediaUsageFromResponse(
		GrokMediaEndpointVideosGenerations, requestInfo, []byte(`{"task_id":"v1"}`),
	)
	require.NoError(t, err)
	require.Equal(t, "v1", created.ResponseID)
	require.Zero(t, created.VideoCount)
	require.Equal(t, VideoBillingResolution720P, created.VideoResolution)
	require.Equal(t, 10, created.VideoDurationSeconds)

	done, err := grokMediaUsageFromResponse(
		GrokMediaEndpointVideoStatus,
		GrokMediaRequestInfo{},
		[]byte(`{"status":"done","model":"grok-imagine-video-1.5","video":{"url":"https://vidgen.x.ai/a.mp4","duration":9}}`),
	)
	require.NoError(t, err)
	require.Equal(t, 1, done.VideoCount)
	require.Equal(t, 9, done.VideoDurationSeconds)
	require.Equal(t, "grok-imagine-video-1.5", done.Model)
}

func TestStableGrokVideoBillingRequestID(t *testing.T) {
	t.Parallel()
	require.Empty(t, StableGrokVideoBillingRequestID(""))
	require.Equal(t, "grok-video:task-1", StableGrokVideoBillingRequestID("task-1"))
	require.Equal(t, "grok-video:task-1", StableGrokVideoBillingRequestID("grok-video:task-1"))
}
