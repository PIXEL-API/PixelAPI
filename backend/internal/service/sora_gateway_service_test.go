//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

var _ SoraClient = (*stubSoraClientForPoll)(nil)

type stubSoraClientForPoll struct {
	imageStatus *SoraImageTaskStatus
	videoStatus *SoraVideoTaskStatus
	imageCalls  int
	videoCalls  int
}

func (s *stubSoraClientForPoll) Enabled() bool { return true }
func (s *stubSoraClientForPoll) UploadImage(ctx context.Context, account *Account, data []byte, filename string) (string, error) {
	return "", nil
}
func (s *stubSoraClientForPoll) CreateImageTask(ctx context.Context, account *Account, req SoraImageRequest) (string, error) {
	return "task-image", nil
}
func (s *stubSoraClientForPoll) CreateVideoTask(ctx context.Context, account *Account, req SoraVideoRequest) (string, error) {
	return "task-video", nil
}
func (s *stubSoraClientForPoll) GetImageTask(ctx context.Context, account *Account, taskID string) (*SoraImageTaskStatus, error) {
	s.imageCalls++
	return s.imageStatus, nil
}
func (s *stubSoraClientForPoll) GetVideoTask(ctx context.Context, account *Account, taskID string) (*SoraVideoTaskStatus, error) {
	s.videoCalls++
	return s.videoStatus, nil
}

func TestSoraGatewayService_PollImageTaskCompleted(t *testing.T) {
	client := &stubSoraClientForPoll{
		imageStatus: &SoraImageTaskStatus{
			Status: "completed",
			URLs:   []string{"https://example.com/a.png"},
		},
	}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				PollIntervalSeconds: 1,
				MaxPollAttempts:     1,
			},
		},
	}
	service := NewSoraGatewayService(client, nil, nil, cfg)

	urls, err := service.pollImageTask(context.Background(), nil, &Account{ID: 1}, "task", false)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/a.png"}, urls)
	require.Equal(t, 1, client.imageCalls)
}

func TestSoraGatewayService_PollVideoTaskFailed(t *testing.T) {
	client := &stubSoraClientForPoll{
		videoStatus: &SoraVideoTaskStatus{
			Status:   "failed",
			ErrorMsg: "reject",
		},
	}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				PollIntervalSeconds: 1,
				MaxPollAttempts:     1,
			},
		},
	}
	service := NewSoraGatewayService(client, nil, nil, cfg)

	urls, err := service.pollVideoTask(context.Background(), nil, &Account{ID: 1}, "task", false)
	require.Error(t, err)
	require.Empty(t, urls)
	require.Contains(t, err.Error(), "reject")
	require.Equal(t, 1, client.videoCalls)
}

func TestSoraGatewayService_BuildSoraMediaURLSigned(t *testing.T) {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			SoraMediaSigningKey:          "test-key",
			SoraMediaSignedURLTTLSeconds: 600,
		},
	}
	service := NewSoraGatewayService(nil, nil, nil, cfg)

	url := service.buildSoraMediaURL("/image/2025/01/01/a.png", "")
	require.Contains(t, url, "/sora/media-signed")
	require.Contains(t, url, "expires=")
	require.Contains(t, url, "sig=")
}

func TestNormalizeSoraMediaURLs_Empty(t *testing.T) {
	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	result := svc.normalizeSoraMediaURLs(nil)
	require.Empty(t, result)

	result = svc.normalizeSoraMediaURLs([]string{})
	require.Empty(t, result)
}

func TestNormalizeSoraMediaURLs_HTTPUrls(t *testing.T) {
	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	urls := []string{"https://example.com/a.png", "http://example.com/b.mp4"}
	result := svc.normalizeSoraMediaURLs(urls)
	require.Equal(t, urls, result)
}

func TestNormalizeSoraMediaURLs_LocalPaths(t *testing.T) {
	cfg := &config.Config{}
	svc := NewSoraGatewayService(nil, nil, nil, cfg)
	urls := []string{"/image/2025/01/a.png", "video/2025/01/b.mp4"}
	result := svc.normalizeSoraMediaURLs(urls)
	require.Len(t, result, 2)
	require.Contains(t, result[0], "/sora/media")
	require.Contains(t, result[1], "/sora/media")
}

func TestNormalizeSoraMediaURLs_SkipsBlank(t *testing.T) {
	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	urls := []string{"https://example.com/a.png", "", "  ", "https://example.com/b.png"}
	result := svc.normalizeSoraMediaURLs(urls)
	require.Len(t, result, 2)
}

func TestBuildSoraContent_Image(t *testing.T) {
	content := buildSoraContent("image", []string{"https://a.com/1.png", "https://a.com/2.png"})
	require.Contains(t, content, "![image](https://a.com/1.png)")
	require.Contains(t, content, "![image](https://a.com/2.png)")
}

func TestBuildSoraContent_Video(t *testing.T) {
	content := buildSoraContent("video", []string{"https://a.com/v.mp4"})
	require.Contains(t, content, "<video src='https://a.com/v.mp4'")
}

func TestBuildSoraContent_VideoEmpty(t *testing.T) {
	content := buildSoraContent("video", nil)
	require.Empty(t, content)
}

func TestBuildSoraContent_Prompt(t *testing.T) {
	content := buildSoraContent("prompt", nil)
	require.Empty(t, content)
}

func TestSoraImageSizeFromModel(t *testing.T) {
	require.Equal(t, "360", soraImageSizeFromModel("gpt-image"))
	require.Equal(t, "540", soraImageSizeFromModel("gpt-image-landscape"))
	require.Equal(t, "540", soraImageSizeFromModel("gpt-image-portrait"))
	require.Equal(t, "540", soraImageSizeFromModel("something-landscape"))
	require.Equal(t, "360", soraImageSizeFromModel("unknown-model"))
}

func TestFirstMediaURL(t *testing.T) {
	require.Equal(t, "", firstMediaURL(nil))
	require.Equal(t, "", firstMediaURL([]string{}))
	require.Equal(t, "a", firstMediaURL([]string{"a", "b"}))
}

func TestSoraProErrorMessage(t *testing.T) {
	require.Contains(t, soraProErrorMessage("sora2pro-hd", ""), "Pro-HD")
	require.Contains(t, soraProErrorMessage("sora2pro", ""), "Pro")
	require.Empty(t, soraProErrorMessage("sora-basic", ""))
}

func TestShouldFailoverUpstreamError(t *testing.T) {
	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	require.True(t, svc.shouldFailoverUpstreamError(401))
	require.True(t, svc.shouldFailoverUpstreamError(429))
	require.True(t, svc.shouldFailoverUpstreamError(500))
	require.True(t, svc.shouldFailoverUpstreamError(502))
	require.False(t, svc.shouldFailoverUpstreamError(200))
	require.False(t, svc.shouldFailoverUpstreamError(400))
}

func TestWithSoraTimeout_NilService(t *testing.T) {
	var svc *SoraGatewayService
	ctx, cancel := svc.withSoraTimeout(context.Background(), false)
	require.NotNil(t, ctx)
	require.Nil(t, cancel)
}

func TestWithSoraTimeout_ZeroTimeout(t *testing.T) {
	cfg := &config.Config{}
	svc := NewSoraGatewayService(nil, nil, nil, cfg)
	ctx, cancel := svc.withSoraTimeout(context.Background(), false)
	require.NotNil(t, ctx)
	require.Nil(t, cancel)
}

func TestWithSoraTimeout_PositiveTimeout(t *testing.T) {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			SoraRequestTimeoutSeconds: 30,
		},
	}
	svc := NewSoraGatewayService(nil, nil, nil, cfg)
	ctx, cancel := svc.withSoraTimeout(context.Background(), false)
	require.NotNil(t, ctx)
	require.NotNil(t, cancel)
	cancel()
}

func TestPollInterval(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				PollIntervalSeconds: 5,
			},
		},
	}
	svc := NewSoraGatewayService(nil, nil, nil, cfg)
	require.Equal(t, 5*time.Second, svc.pollInterval())

	// 默认值
	svc2 := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	require.True(t, svc2.pollInterval() > 0)
}

func TestPollMaxAttempts(t *testing.T) {
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				MaxPollAttempts: 100,
			},
		},
	}
	svc := NewSoraGatewayService(nil, nil, nil, cfg)
	require.Equal(t, 100, svc.pollMaxAttempts())

	// 默认值
	svc2 := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	require.True(t, svc2.pollMaxAttempts() > 0)
}

func TestDecodeSoraImageInput_BlockPrivateURL(t *testing.T) {
	_, _, err := decodeSoraImageInput(context.Background(), "http://127.0.0.1/internal.png")
	require.Error(t, err)
}

func TestDecodeSoraImageInput_DataURL(t *testing.T) {
	encoded := "data:image/png;base64,aGVsbG8="
	data, filename, err := decodeSoraImageInput(context.Background(), encoded)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.Contains(t, filename, ".png")
}
