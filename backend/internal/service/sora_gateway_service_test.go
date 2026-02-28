//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var _ SoraClient = (*stubSoraClientForPoll)(nil)

type stubSoraClientForPoll struct {
	imageStatus *SoraImageTaskStatus
	videoStatus *SoraVideoTaskStatus
	imageCalls  int
	videoCalls  int
	enhanced    string
	enhanceErr  error
	storyboard  bool
	videoReq    SoraVideoRequest
	parseErr    error
	postCalls   int
	deleteCalls int
}

func (s *stubSoraClientForPoll) Enabled() bool { return true }
func (s *stubSoraClientForPoll) UploadImage(ctx context.Context, account *Account, data []byte, filename string) (string, error) {
	return "", nil
}
func (s *stubSoraClientForPoll) CreateImageTask(ctx context.Context, account *Account, req SoraImageRequest) (string, error) {
	return "task-image", nil
}
func (s *stubSoraClientForPoll) CreateVideoTask(ctx context.Context, account *Account, req SoraVideoRequest) (string, error) {
	s.videoReq = req
	return "task-video", nil
}
func (s *stubSoraClientForPoll) CreateStoryboardTask(ctx context.Context, account *Account, req SoraStoryboardRequest) (string, error) {
	s.storyboard = true
	return "task-video", nil
}
func (s *stubSoraClientForPoll) UploadCharacterVideo(ctx context.Context, account *Account, data []byte) (string, error) {
	return "cameo-1", nil
}
func (s *stubSoraClientForPoll) GetCameoStatus(ctx context.Context, account *Account, cameoID string) (*SoraCameoStatus, error) {
	return &SoraCameoStatus{
		Status:          "finalized",
		StatusMessage:   "Completed",
		DisplayNameHint: "Character",
		UsernameHint:    "user.character",
		ProfileAssetURL: "https://example.com/avatar.webp",
	}, nil
}
func (s *stubSoraClientForPoll) DownloadCharacterImage(ctx context.Context, account *Account, imageURL string) ([]byte, error) {
	return []byte("avatar"), nil
}
func (s *stubSoraClientForPoll) UploadCharacterImage(ctx context.Context, account *Account, data []byte) (string, error) {
	return "asset-pointer", nil
}
func (s *stubSoraClientForPoll) FinalizeCharacter(ctx context.Context, account *Account, req SoraCharacterFinalizeRequest) (string, error) {
	return "character-1", nil
}
func (s *stubSoraClientForPoll) SetCharacterPublic(ctx context.Context, account *Account, cameoID string) error {
	return nil
}
func (s *stubSoraClientForPoll) DeleteCharacter(ctx context.Context, account *Account, characterID string) error {
	return nil
}
func (s *stubSoraClientForPoll) PostVideoForWatermarkFree(ctx context.Context, account *Account, generationID string) (string, error) {
	s.postCalls++
	return "s_post", nil
}
func (s *stubSoraClientForPoll) DeletePost(ctx context.Context, account *Account, postID string) error {
	s.deleteCalls++
	return nil
}
func (s *stubSoraClientForPoll) GetWatermarkFreeURLCustom(ctx context.Context, account *Account, parseURL, parseToken, postID string) (string, error) {
	if s.parseErr != nil {
		return "", s.parseErr
	}
	return "https://example.com/no-watermark.mp4", nil
}
func (s *stubSoraClientForPoll) EnhancePrompt(ctx context.Context, account *Account, prompt, expansionLevel string, durationS int) (string, error) {
	if s.enhanced != "" {
		return s.enhanced, s.enhanceErr
	}
	return "enhanced prompt", s.enhanceErr
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

func TestSoraGatewayService_ForwardPromptEnhance(t *testing.T) {
	client := &stubSoraClientForPoll{
		enhanced: "cinematic prompt",
	}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				PollIntervalSeconds: 1,
				MaxPollAttempts:     1,
			},
		},
	}
	svc := NewSoraGatewayService(client, nil, nil, cfg)
	account := &Account{
		ID:       1,
		Platform: PlatformSora,
		Status:   StatusActive,
	}
	body := []byte(`{"model":"prompt-enhance-short-10s","messages":[{"role":"user","content":"cat running"}],"stream":false}`)

	result, err := svc.Forward(context.Background(), nil, account, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "prompt", result.MediaType)
	require.Equal(t, "prompt-enhance-short-10s", result.Model)
}

func TestSoraGatewayService_ForwardStoryboardPrompt(t *testing.T) {
	client := &stubSoraClientForPoll{
		videoStatus: &SoraVideoTaskStatus{
			Status: "completed",
			URLs:   []string{"https://example.com/v.mp4"},
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
	svc := NewSoraGatewayService(client, nil, nil, cfg)
	account := &Account{ID: 1, Platform: PlatformSora, Status: StatusActive}
	body := []byte(`{"model":"sora2-landscape-10s","messages":[{"role":"user","content":"[5.0s]猫猫跳伞 [5.0s]猫猫落地"}],"stream":false}`)

	result, err := svc.Forward(context.Background(), nil, account, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, client.storyboard)
}

func TestSoraGatewayService_ForwardVideoCount(t *testing.T) {
	client := &stubSoraClientForPoll{
		videoStatus: &SoraVideoTaskStatus{
			Status: "completed",
			URLs:   []string{"https://example.com/v.mp4"},
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
	svc := NewSoraGatewayService(client, nil, nil, cfg)
	account := &Account{ID: 1, Platform: PlatformSora, Status: StatusActive}
	body := []byte(`{"model":"sora2-landscape-10s","messages":[{"role":"user","content":"cat running"}],"video_count":3,"stream":false}`)

	result, err := svc.Forward(context.Background(), nil, account, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 3, client.videoReq.VideoCount)
}

func TestSoraGatewayService_ForwardCharacterOnly(t *testing.T) {
	client := &stubSoraClientForPoll{}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				PollIntervalSeconds: 1,
				MaxPollAttempts:     1,
			},
		},
	}
	svc := NewSoraGatewayService(client, nil, nil, cfg)
	account := &Account{ID: 1, Platform: PlatformSora, Status: StatusActive}
	body := []byte(`{"model":"sora2-landscape-10s","video":"aGVsbG8=","stream":false}`)

	result, err := svc.Forward(context.Background(), nil, account, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "prompt", result.MediaType)
	require.Equal(t, 0, client.videoCalls)
}

func TestSoraGatewayService_ForwardWatermarkFallback(t *testing.T) {
	client := &stubSoraClientForPoll{
		videoStatus: &SoraVideoTaskStatus{
			Status:       "completed",
			URLs:         []string{"https://example.com/original.mp4"},
			GenerationID: "gen_1",
		},
		parseErr: errors.New("parse failed"),
	}
	cfg := &config.Config{
		Sora: config.SoraConfig{
			Client: config.SoraClientConfig{
				PollIntervalSeconds: 1,
				MaxPollAttempts:     1,
			},
		},
	}
	svc := NewSoraGatewayService(client, nil, nil, cfg)
	account := &Account{ID: 1, Platform: PlatformSora, Status: StatusActive}
	body := []byte(`{"model":"sora2-landscape-10s","messages":[{"role":"user","content":"cat running"}],"stream":false,"watermark_free":true,"watermark_parse_method":"custom","watermark_parse_url":"https://parser.example.com","watermark_parse_token":"token","watermark_fallback_on_failure":true}`)

	result, err := svc.Forward(context.Background(), nil, account, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://example.com/original.mp4", result.MediaURL)
	require.Equal(t, 1, client.postCalls)
	require.Equal(t, 0, client.deleteCalls)
}

func TestSoraGatewayService_ForwardWatermarkCustomSuccessAndDelete(t *testing.T) {
	client := &stubSoraClientForPoll{
		videoStatus: &SoraVideoTaskStatus{
			Status:       "completed",
			URLs:         []string{"https://example.com/original.mp4"},
			GenerationID: "gen_1",
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
	svc := NewSoraGatewayService(client, nil, nil, cfg)
	account := &Account{ID: 1, Platform: PlatformSora, Status: StatusActive}
	body := []byte(`{"model":"sora2-landscape-10s","messages":[{"role":"user","content":"cat running"}],"stream":false,"watermark_free":true,"watermark_parse_method":"custom","watermark_parse_url":"https://parser.example.com","watermark_parse_token":"token","watermark_delete_post":true}`)

	result, err := svc.Forward(context.Background(), nil, account, body, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "https://example.com/no-watermark.mp4", result.MediaURL)
	require.Equal(t, 1, client.postCalls)
	require.Equal(t, 1, client.deleteCalls)
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

	status, err := service.pollVideoTaskDetailed(context.Background(), nil, &Account{ID: 1}, "task", false)
	require.Error(t, err)
	require.Nil(t, status)
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

func TestSoraGatewayService_WriteSoraError_StreamEscapesJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	svc.writeSoraError(c, http.StatusBadGateway, "upstream_error", "invalid \"prompt\"\nline2", true)

	body := rec.Body.String()
	require.Contains(t, body, "event: error\n")
	require.Contains(t, body, "data: [DONE]\n\n")

	lines := strings.Split(body, "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	require.Equal(t, "event: error", lines[0])
	require.True(t, strings.HasPrefix(lines[1], "data: "))

	data := strings.TrimPrefix(lines[1], "data: ")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(data), &parsed))
	errObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "upstream_error", errObj["type"])
	require.Equal(t, "invalid \"prompt\"\nline2", errObj["message"])
}

func TestSoraGatewayService_HandleSoraRequestError_FailoverHeadersCloned(t *testing.T) {
	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	sourceHeaders := http.Header{}
	sourceHeaders.Set("cf-ray", "9d01b0e9ecc35829-SEA")

	err := svc.handleSoraRequestError(
		context.Background(),
		&Account{ID: 1, Platform: PlatformSora},
		&SoraUpstreamError{
			StatusCode: http.StatusForbidden,
			Message:    "forbidden",
			Headers:    sourceHeaders,
			Body:       []byte(`<!DOCTYPE html><title>Just a moment...</title>`),
		},
		"sora2-landscape-10s",
		nil,
		false,
	)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.NotNil(t, failoverErr.ResponseHeaders)
	require.Equal(t, "9d01b0e9ecc35829-SEA", failoverErr.ResponseHeaders.Get("cf-ray"))

	sourceHeaders.Set("cf-ray", "mutated-after-return")
	require.Equal(t, "9d01b0e9ecc35829-SEA", failoverErr.ResponseHeaders.Get("cf-ray"))
}

func TestShouldFailoverUpstreamError(t *testing.T) {
	svc := NewSoraGatewayService(nil, nil, nil, &config.Config{})
	require.True(t, svc.shouldFailoverUpstreamError(401))
	require.True(t, svc.shouldFailoverUpstreamError(404))
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

func TestDecodeBase64WithLimit_ExceedLimit(t *testing.T) {
	data, err := decodeBase64WithLimit("aGVsbG8=", 3)
	require.Error(t, err)
	require.Nil(t, data)
}

func TestParseSoraWatermarkOptions_NumericBool(t *testing.T) {
	body := map[string]any{
		"watermark_free":                float64(1),
		"watermark_fallback_on_failure": float64(0),
	}
	opts := parseSoraWatermarkOptions(body)
	require.True(t, opts.Enabled)
	require.False(t, opts.FallbackOnFailure)
}

func TestParseSoraVideoCount(t *testing.T) {
	require.Equal(t, 1, parseSoraVideoCount(nil))
	require.Equal(t, 2, parseSoraVideoCount(map[string]any{"video_count": float64(2)}))
	require.Equal(t, 3, parseSoraVideoCount(map[string]any{"videos": "5"}))
	require.Equal(t, 1, parseSoraVideoCount(map[string]any{"n_variants": 0}))
}
