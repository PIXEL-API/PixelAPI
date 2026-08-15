package service

import (
	"context"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestBuildGrokVoiceURLUsesOfficialAPIForCLIProxy(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": xai.DefaultCLIBaseURL,
		},
	}
	service := &OpenAIGatewayService{}
	got, err := buildGrokVoiceURL(context.Background(), account, service, "tts")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/tts", got)

	got, err = buildGrokVoiceURL(context.Background(), account, service, "realtime")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/realtime", got)
}

func TestValidateGrokVoiceEndpointAllowsOnlyDocumentedSubresources(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{
		"tts", "stt", "custom-voices", "custom-voices/voice-1", "custom-voices/voice-1/audio",
	} {
		got, _, err := validateGrokVoiceEndpoint(endpoint)
		require.NoError(t, err, endpoint)
		require.Equal(t, endpoint, got)
	}
	for _, endpoint := range []string{
		"", "unknown", "tts/extra", "custom-voices/../audio", "custom-voices/v/metadata",
	} {
		_, _, err := validateGrokVoiceEndpoint(endpoint)
		require.Error(t, err, endpoint)
	}
}

func TestGrokRealtimeAudioObservation(t *testing.T) {
	t.Parallel()
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"session.created"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio_transcript.delta","delta":"hi"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":""}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":"abc"}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"input_audio_buffer.append","audio":"abc"}`)))

	errCh := make(chan error, 1)
	var observed atomic.Bool
	go func() {
		observed.Store(true)
		errCh <- io.EOF
	}()
	got, err := awaitGrokRealtimeAudioObserved(errCh, &observed)
	require.ErrorIs(t, err, io.EOF)
	require.True(t, got)
}

func TestEstimateGrokVoiceAudioUsage(t *testing.T) {
	t.Parallel()
	tts := estimateGrokVoiceAudioUsage(
		"tts", []byte(`{"input":"你好 Grok"}`), nil, time.Second,
	)
	require.NotNil(t, tts)
	require.Equal(t, "tts", tts.Mode)
	require.InDelta(t, float64(len([]rune("你好 Grok")))/1_000_000, tts.DurationOrUnits, 1e-12)

	stt := estimateGrokVoiceAudioUsage(
		"stt", []byte("audio"), []byte(`{"duration":90}`), 10*time.Second,
	)
	require.NotNil(t, stt)
	require.Equal(t, "stt", stt.Mode)
	require.InDelta(t, 90.0/3600.0, stt.DurationOrUnits, 1e-12)

	require.Nil(t, estimateGrokVoiceAudioUsage("custom-voices", nil, nil, 0))
}

func TestStableGrokAudioBillingRequestIDs(t *testing.T) {
	t.Parallel()
	require.Equal(t, "grok_audio:upstream-1", StableGrokAudioBillingRequestID("upstream-1"))
	require.Equal(t, "grok_audio:upstream-1", StableGrokAudioBillingRequestID("grok_audio:upstream-1"))
	require.NotEqual(t, StableGrokAudioBillingRequestID(""), StableGrokAudioBillingRequestID(""))
	require.Equal(t, "grok_realtime:session-1", StableGrokRealtimeBillingRequestID("session-1"))
	require.NotEqual(t, StableGrokRealtimeBillingRequestID(""), StableGrokRealtimeBillingRequestID(""))
}

func TestValidateGrokAudioBillingPriceFailsClosedBeforeForward(t *testing.T) {
	t.Parallel()
	valid := 0.05
	free := 0.0
	invalid := math.NaN()

	for _, endpoint := range []string{"tts", "stt", "realtime"} {
		require.Error(t, ValidateGrokAudioBillingPrice(&Group{}, endpoint), endpoint)
	}
	require.NoError(t, ValidateGrokAudioBillingPrice(&Group{}, "custom-voices/voice-1/audio"))
	require.NoError(t, ValidateGrokAudioBillingPrice(&Group{AudioTTSPricePerMillionChars: &valid}, "tts"))
	require.NoError(t, ValidateGrokAudioBillingPrice(&Group{AudioSTTPricePerHour: &free}, "stt"), "explicit zero means free")
	require.NoError(t, ValidateGrokAudioBillingPrice(&Group{AudioRealtimePricePerMin: &valid}, "realtime"))
	require.Error(t, ValidateGrokAudioBillingPrice(&Group{AudioRealtimePricePerMin: &invalid}, "realtime"))
	require.Error(t, ValidateGrokAudioBillingPrice(&Group{}, "unknown"))
}

func TestGrokAccountProxyURLFailsClosedWhenBindingCannotResolve(t *testing.T) {
	t.Parallel()
	proxyID := int64(9)
	_, err := grokAccountProxyURL(&Account{ProxyID: &proxyID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy binding")
}
