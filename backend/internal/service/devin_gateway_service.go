package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	devinpkg "github.com/Wei-Shaw/sub2api/internal/pkg/devin"
)

// DevinGatewayService 编排 Devin 平台的上游调用：按账号构建/缓存
// Connect 客户端，封装模型目录、router 解析与流式 chat。
type DevinGatewayService struct {
	gatewayService *OpenAIGatewayService
	accountService *AccountService
	cfg            *config.Config

	// clients 按账号缓存上游客户端；指纹覆盖 token/base_url/代理，
	// 凭据或代理变更时自动重建。
	clients sync.Map // accountID → *devinClientEntry
}

type devinClientEntry struct {
	fingerprint string
	client      *devinpkg.Client
}

func NewDevinGatewayService(
	gatewayService *OpenAIGatewayService,
	accountService *AccountService,
	cfg *config.Config,
) *DevinGatewayService {
	return &DevinGatewayService{
		gatewayService: gatewayService,
		accountService: accountService,
		cfg:            cfg,
	}
}

// ClientForAccount 返回账号对应的上游客户端（凭据指纹变化时重建）。
func (s *DevinGatewayService) ClientForAccount(account *Account) (*devinpkg.Client, error) {
	if account == nil {
		return nil, fmt.Errorf("devin account is missing")
	}
	token := account.GetDevinToken()
	if token == "" {
		return nil, fmt.Errorf("devin account %d has no api_key credential", account.ID)
	}
	baseURL := account.GetDevinBaseURL()
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	fingerprint := devinClientFingerprint(token, baseURL, proxyURL)

	if cached, ok := s.clients.Load(account.ID); ok {
		entry := cached.(*devinClientEntry)
		if entry.fingerprint == fingerprint {
			return entry.client, nil
		}
	}
	client, err := devinpkg.NewClient(devinpkg.ClientConfig{
		BaseURL:  baseURL,
		Token:    token,
		ProxyURL: proxyURL,
	})
	if err != nil {
		return nil, err
	}
	if prev, loaded := s.clients.Swap(account.ID, &devinClientEntry{fingerprint: fingerprint, client: client}); loaded {
		prev.(*devinClientEntry).client.Close()
	}
	return client, nil
}

func devinClientFingerprint(token, baseURL, proxyURL string) string {
	sum := sha256.Sum256([]byte(token + "\x00" + baseURL + "\x00" + proxyURL))
	return hex.EncodeToString(sum[:])
}

// InvalidateClient 移除账号的缓存客户端（凭据/代理变更后调用）。
func (s *DevinGatewayService) InvalidateClient(accountID int64) {
	if cached, ok := s.clients.LoadAndDelete(accountID); ok {
		cached.(*devinClientEntry).client.Close()
	}
}

// DevinChatResult 是一次 chat 调用的编排结果。
type DevinChatResult struct {
	// Stream 是已打开的上游解码流；调用方负责 Close。
	Stream *devinpkg.ChatStream
	// UpstreamModel 是实际上游的 model uid（router 解析后值）。
	UpstreamModel string
}

// Chat 完成「router 解析 → 构建请求 → 打开上游流」的编排。
// model 为渠道映射后的模型名；req.SessionKey 为粘性会话种子。
func (s *DevinGatewayService) Chat(ctx context.Context, account *Account, req *devinpkg.ChatRequest, model string) (*DevinChatResult, error) {
	client, err := s.ClientForAccount(account)
	if err != nil {
		return nil, err
	}
	upstreamModel := strings.TrimSpace(model)
	if upstreamModel == "" {
		return nil, &devinpkg.Failure{StatusCode: 400, Code: "invalid_argument", ClientFault: true, Message: "model is required"}
	}
	// cascade_id 必须与 wire 上的派生值一致（assignment jwt 绑 cascade_id），
	// 用包内导出的派生函数保证同源。
	_, cascadeID := devinpkg.DeriveSessionIDs(req)
	resolved, jwt, err := client.ResolveModel(ctx, upstreamModel, cascadeID)
	if err != nil {
		return nil, err
	}
	upstreamModel = resolved
	req.Model = upstreamModel
	req.ModelAssignmentJWT = jwt

	protoReq, err := client.BuildRequest(req)
	if err != nil {
		return nil, err
	}
	stream, err := client.Chat(ctx, protoReq)
	if err != nil {
		return nil, err
	}
	stream.SetStopPatterns(req.StopSequences)
	return &DevinChatResult{Stream: stream, UpstreamModel: upstreamModel}, nil
}

// ListModels 选一个可调度账号拉取实时模型目录；无可用账号时报错。
func (s *DevinGatewayService) ListModels(ctx context.Context, groupID *int64) ([]devinpkg.ModelInfo, error) {
	ctx = context.WithValue(ctx, ctxkey.ForcePlatform, PlatformDevin)
	account, err := s.gatewayService.SelectAccountForModel(ctx, groupID, "", "")
	if err != nil {
		return nil, err
	}
	client, err := s.ClientForAccount(account)
	if err != nil {
		return nil, err
	}
	return client.ListModels(ctx)
}

// TestAccountConnection 用 GetCliModelConfigs 探测账号凭证有效性
// （不产生推理计费的最轻量调用）。
func (s *DevinGatewayService) TestAccountConnection(ctx context.Context, account *Account) error {
	client, err := s.ClientForAccount(account)
	if err != nil {
		return err
	}
	models, err := client.ListModels(ctx)
	if err != nil {
		return err
	}
	slog.Info("devin account test ok", "account_id", account.ID, "models", len(models))
	return nil
}

// MarkCredentialFailure 把凭证失效（unauthenticated）的账号标记为 error。
func (s *DevinGatewayService) MarkCredentialFailure(ctx context.Context, account *Account, failure *devinpkg.Failure) {
	if account == nil || failure == nil || !failure.CredentialFailed || s.accountService == nil {
		return
	}
	message := "Devin credential rejected by upstream: " + failure.Message
	if len(message) > 500 {
		message = message[:500]
	}
	if err := s.accountService.UpdateStatus(context.WithoutCancel(ctx), account.ID, StatusError, message); err != nil {
		slog.Warn("devin mark credential failure failed", "account_id", account.ID, "error", err)
		return
	}
	s.InvalidateClient(account.ID)
}
