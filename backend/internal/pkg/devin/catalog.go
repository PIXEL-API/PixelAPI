package devin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/Wei-Shaw/sub2api/internal/pkg/devinproto"
)

// ListModels 拉取账号可见的模型目录（GetCliModelConfigs），带 TTL 缓存
// 与失败冷却：冷却期内有旧值回旧值，否则回上次错误。
// 并发 miss 收敛为单次上游调用（singleflight）：拉取方持有锁外 fetch
// channel，等待者吃自己的 ctx 可随时退出。
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	for {
		c.modelsMu.Lock()
		if c.models != nil && time.Now().Before(c.modelsExpiry) {
			models := c.models
			c.modelsMu.Unlock()
			return models, nil
		}
		if time.Now().Before(c.modelsRetryUntil) {
			if c.models != nil {
				models := c.models
				c.modelsMu.Unlock()
				return models, nil
			}
			err := c.modelsErr
			c.modelsMu.Unlock()
			return nil, err
		}
		if fetch := c.modelsFetch; fetch != nil {
			c.modelsMu.Unlock()
			select {
			case <-fetch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		c.modelsFetch = make(chan struct{})
		c.modelsMu.Unlock()

		// 目录是 client 级共享状态，一个调用方断连不该掐死全体等待者的拉取。
		models, err := c.fetchModelCatalog(context.WithoutCancel(ctx))

		c.modelsMu.Lock()
		done := c.modelsFetch
		c.modelsFetch = nil
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				c.modelsRetryUntil = time.Now().Add(modelsErrorCooldown)
				c.modelsErr = err
			}
			stale := c.models
			c.modelsMu.Unlock()
			close(done)
			if stale != nil {
				return stale, nil
			}
			return nil, err
		}
		c.models = models
		c.modelsExpiry = time.Now().Add(modelsCacheTTL)
		c.modelsRetryUntil = time.Time{}
		c.modelsErr = nil
		c.modelsMu.Unlock()
		close(done)
		return models, nil
	}
}

// fetchModelCatalog 执行一次 GetCliModelConfigs 并整形目录
// （去重、剥 disabled、提取能力位）。
func (c *Client) fetchModelCatalog(ctx context.Context) ([]ModelInfo, error) {
	resp, err := c.api.GetCliModelConfigs(ctx, connect.NewRequest(&devinproto.GetCliModelConfigsRequest{
		Metadata: c.metadata(0),
	}))
	if err != nil {
		return nil, fmt.Errorf("devin GetCliModelConfigs: %w", err)
	}
	configs := resp.Msg.GetClientModelConfigs()
	models := make([]ModelInfo, 0, len(configs))
	seen := make(map[string]struct{}, len(configs))
	for _, cfg := range configs {
		if cfg.GetDisabled() {
			continue
		}
		uid := cfg.GetModelUid()
		if uid == "" && cfg.GetModelOrAlias() != nil {
			uid = cfg.GetModelOrAlias().GetModelUid()
		}
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		info := ModelInfo{
			ID:            uid,
			OwnedBy:       "devin",
			ContextTokens: int(cfg.GetMaxTokens()),
		}
		if provider := cfg.GetProvider().String(); provider != "" {
			if i := strings.LastIndex(provider, "_"); i >= 0 && i+1 < len(provider) {
				info.OwnedBy = strings.ToLower(provider[i+1:])
			}
		}
		if cfg.GetSupportsImages() {
			info.SupportsImages = true
		}
		if modelInfo := cfg.GetModelInfo(); modelInfo != nil {
			info.MaxOutputTokens = int(modelInfo.GetMaxOutputTokens())
			info.IsModelRouter = modelInfo.GetIsModelRouter()
			if info.ContextTokens == 0 {
				info.ContextTokens = int(modelInfo.GetMaxTokens())
			}
			if features := modelInfo.GetModelFeatures(); features != nil {
				info.SupportsToolCalls = features.GetSupportsToolCalls()
				info.SupportsParallelToolCalls = features.GetSupportsParallelToolCalls()
				info.SupportsThinking = features.GetSupportsThinking()
				if !info.SupportsImages {
					info.SupportsImages = features.GetSupportsImages()
				}
			}
		}
		models = append(models, info)
	}
	return models, nil
}

// ResolveModel 把请求的模型 uid 解析为真实上游模型：router uid 经
// AssignModel 得到真实模型 + assignment jwt（jwt 绑 cascade_id，按
// (router, cascade) 缓存，同会话内复用省去每请求一次解析往返）。
// 非 router 模型原样返回；catalog 未收录的模型名也原样放行（上游裁决）。
func (c *Client) ResolveModel(ctx context.Context, model string, cascadeID string) (resolved, assignmentJWT string, err error) {
	models, listErr := c.ListModels(ctx)
	if listErr == nil {
		for _, m := range models {
			if m.ID == model && m.IsModelRouter {
				assignment, assignErr := c.assignModel(ctx, model, cascadeID)
				if assignErr != nil {
					return "", "", assignErr
				}
				return assignment.modelUID, assignment.jwt, nil
			}
		}
		return model, "", nil
	}
	// 目录拉取失败的降级路径：尝试 AssignModel——router 名能正常解析；
	// 非 router 名返回 invalid_argument/not_found 类客户端错误时按原样透传，
	// 传输/鉴权类错误向上抛，由调用方按错误分类处理。
	assignment, assignErr := c.assignModel(ctx, model, cascadeID)
	if assignErr == nil {
		return assignment.modelUID, assignment.jwt, nil
	}
	if failure := Classify(assignErr); failure != nil && failure.ClientFault {
		return model, "", nil
	}
	return "", "", assignErr
}

// assignModel 调上游 AssignModel；结果按 (router|cascade) 缓存，
// 容量封顶防 map 无界增长。
func (c *Client) assignModel(ctx context.Context, routerUID, cascadeID string) (resolvedAssignment, error) {
	key := routerUID + "|" + cascadeID
	c.assignmentsMu.Lock()
	cached, ok := c.assignments[key]
	c.assignmentsMu.Unlock()
	if ok {
		return cached, nil
	}
	resp, err := c.api.AssignModel(ctx, connect.NewRequest(&devinproto.AssignModelRequest{
		Metadata:       c.metadata(metadataFingerprintBytes),
		ModelRouterUid: proto.String(routerUID),
		CascadeId:      proto.String(cascadeID),
	}))
	if err != nil {
		failure := Classify(err)
		failure.Message = fmt.Sprintf("AssignModel(%s): %s", routerUID, failure.Message)
		return resolvedAssignment{}, failure
	}
	assignment := resp.Msg.GetAssignment()
	resolved := strings.TrimSpace(assignment.GetModelUid())
	if resolved == "" || assignment.GetAssignmentJwt() == "" {
		return resolvedAssignment{}, &Failure{StatusCode: 502, Code: "invalid_argument",
			Message: fmt.Sprintf("AssignModel(%s) returned empty assignment", routerUID)}
	}
	result := resolvedAssignment{modelUID: resolved, jwt: assignment.GetAssignmentJwt()}
	c.assignmentsMu.Lock()
	if len(c.assignments) >= 4096 {
		c.assignments = make(map[string]resolvedAssignment)
	}
	c.assignments[key] = result
	c.assignmentsMu.Unlock()
	return result, nil
}
