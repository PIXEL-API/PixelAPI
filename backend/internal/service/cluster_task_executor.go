package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrClusterTaskLeaseLost = errors.New("cluster task lease ownership was lost")

// ClusterLeaseGuard is passed to a leased task. Tasks that perform an
// irreversible external or shared-data side effect must call Check immediately
// before committing that side effect.
type ClusterLeaseGuard struct {
	executor     *ClusterTaskExecutor
	taskName     string
	fencingToken int64
	lost         *atomic.Bool
}

func (g *ClusterLeaseGuard) FencingToken() int64 {
	if g == nil {
		return 0
	}
	return g.fencingToken
}

func (g *ClusterLeaseGuard) Check(ctx context.Context) error {
	if g == nil || g.executor == nil || !g.executor.clusterMode {
		return nil
	}
	if g.executor.initErr != nil {
		return g.executor.initErr
	}
	if !g.executor.enabled() {
		return errors.New("cluster task lease guard is not ready")
	}
	if g.lost != nil && g.lost.Load() {
		return ErrClusterTaskLeaseLost
	}
	ok, err := g.executor.repo.RenewTaskLease(
		ctx,
		g.executor.deploymentID,
		g.taskName,
		g.executor.nodeID,
		g.executor.bootID,
		g.fencingToken,
		g.executor.leaseDuration,
	)
	if err != nil {
		return fmt.Errorf("validate task lease %s: %w", g.taskName, err)
	}
	if !ok {
		if g.lost != nil {
			g.lost.Store(true)
		}
		return ErrClusterTaskLeaseLost
	}
	return nil
}

type ClusterTaskExecutor struct {
	repo          ClusterRepository
	nodeState     *ClusterNodeState
	clusterMode   bool
	initErr       error
	deploymentID  string
	nodeID        string
	bootID        string
	leaseDuration time.Duration
	renewInterval time.Duration
}

func NewClusterTaskExecutor(cfg *config.Config, repo ClusterRepository, nodeState *ClusterNodeState) *ClusterTaskExecutor {
	executor := &ClusterTaskExecutor{repo: repo, nodeState: nodeState}
	if cfg == nil || !cfg.Cluster.Enabled {
		return executor
	}
	executor.clusterMode = true
	if repo == nil || nodeState == nil {
		executor.initErr = errors.New("cluster task executor requires repository and node state")
		return executor
	}
	executor.deploymentID, executor.nodeID, executor.bootID = nodeState.Identity()
	executor.leaseDuration = time.Duration(cfg.Cluster.TaskLeaseSeconds) * time.Second
	executor.renewInterval = time.Duration(cfg.Cluster.TaskRenewIntervalSeconds) * time.Second
	if executor.deploymentID == "" || executor.nodeID == "" || executor.bootID == "" {
		executor.initErr = errors.New("cluster task executor identity is incomplete")
	} else if executor.leaseDuration <= 0 || executor.renewInterval <= 0 ||
		executor.renewInterval >= executor.leaseDuration {
		executor.initErr = errors.New("cluster task executor lease configuration is invalid")
	}
	return executor
}

func (e *ClusterTaskExecutor) enabled() bool {
	return e != nil &&
		e.clusterMode &&
		e.initErr == nil &&
		e.repo != nil &&
		e.nodeState != nil &&
		e.deploymentID != "" &&
		e.nodeID != "" &&
		e.bootID != "" &&
		e.leaseDuration > 0 &&
		e.renewInterval > 0
}

// Run executes task only when this process owns its PostgreSQL-backed lease.
// The bool reports whether the callback ran; false with nil error means another
// healthy node owns the task or this node is draining.
func (e *ClusterTaskExecutor) Run(
	ctx context.Context,
	taskName string,
	task func(context.Context, *ClusterLeaseGuard) error,
) (bool, error) {
	if task == nil {
		return false, errors.New("cluster task callback is nil")
	}
	if e == nil {
		return false, errors.New("cluster task executor is nil")
	}
	if !e.clusterMode {
		return true, task(ctx, &ClusterLeaseGuard{})
	}
	if e.initErr != nil {
		return false, e.initErr
	}
	if !e.enabled() {
		return false, errors.New("cluster task executor is not ready")
	}
	if e.nodeState.IsDraining() {
		return false, nil
	}

	lease, acquired, err := e.repo.AcquireTaskLease(
		ctx,
		e.deploymentID,
		taskName,
		e.nodeID,
		e.bootID,
		e.leaseDuration,
	)
	if err != nil {
		return false, fmt.Errorf("acquire cluster task lease %s: %w", taskName, err)
	}
	if !acquired {
		return false, nil
	}

	startedAt := time.Now()
	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()
	var lost atomic.Bool
	guard := &ClusterLeaseGuard{
		executor:     e,
		taskName:     taskName,
		fencingToken: lease.FencingToken,
		lost:         &lost,
	}

	stopRenewal := make(chan struct{})
	renewalDone := make(chan struct{})
	go func() {
		defer close(renewalDone)
		ticker := time.NewTicker(e.renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-taskCtx.Done():
				return
			case <-stopRenewal:
				return
			case <-ticker.C:
				renewed, renewErr := e.repo.RenewTaskLease(
					taskCtx,
					e.deploymentID,
					taskName,
					e.nodeID,
					e.bootID,
					lease.FencingToken,
					e.leaseDuration,
				)
				if renewErr != nil || !renewed {
					lost.Store(true)
					cancelTask()
					return
				}
			}
		}
	}()

	taskErr := task(taskCtx, guard)
	close(stopRenewal)
	<-renewalDone
	if lost.Load() {
		return true, ErrClusterTaskLeaseLost
	}

	released, releaseErr := e.repo.ReleaseTaskLease(
		context.WithoutCancel(ctx),
		e.deploymentID,
		taskName,
		e.nodeID,
		e.bootID,
		lease.FencingToken,
		taskErr == nil,
		errorString(taskErr),
		time.Since(startedAt),
	)
	if releaseErr != nil {
		return true, fmt.Errorf("release cluster task lease %s: %w", taskName, releaseErr)
	}
	if !released {
		return true, ErrClusterTaskLeaseLost
	}
	return true, taskErr
}
