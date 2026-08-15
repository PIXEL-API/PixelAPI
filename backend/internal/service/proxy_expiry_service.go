package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ProxyExpirySweeper 是周期任务唯一需要的写能力。
// 保持最窄接口，避免 worker 被指标或查询能力耦合。
type ProxyExpirySweeper interface {
	SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error)
}

type ProxyExpiryMetricsRepository interface {
	CountExpired(ctx context.Context) (int64, error)
	CountExpiringSoon(ctx context.Context, now time.Time) (int64, error)
}

// ProxyExpiryRepository 聚合运行时解析和运维指标所需的代理生命周期能力，
// 不并入通用 ProxyRepository，避免扩大所有既有 fake 与调用方的实现面。
type ProxyExpiryRepository interface {
	ProxyExpirySweeper
	ProxyExpiryMetricsRepository
	ListAllForFallback(ctx context.Context) ([]Proxy, error)
}

// ProxyExpiryService 周期扫描到期代理；构造函数不会隐式启动写任务。
type ProxyExpiryService struct {
	repo      ProxyExpirySweeper
	interval  time.Duration
	stopCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

func NewProxyExpiryService(repo ProxyExpirySweeper, interval time.Duration) *ProxyExpiryService {
	return &ProxyExpiryService{repo: repo, interval: interval, stopCh: make(chan struct{})}
}

func (s *ProxyExpiryService) Start() {
	if s == nil || s.repo == nil || s.interval <= 0 {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runOnce()
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.runOnce()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

func (s *ProxyExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *ProxyExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	changed, err := s.repo.SweepExpiredProxies(ctx, time.Now().UTC())
	if err != nil {
		slog.Error("proxy_expiry_sweep_failed", "error", err)
		return
	}
	if changed > 0 {
		slog.Info("proxy_expiry_sweep_completed", "changed_accounts", changed)
	}
}
