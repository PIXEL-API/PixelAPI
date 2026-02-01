//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

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
