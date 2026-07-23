package service

import (
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

// ClusterNodeState is created before runtime services. It owns the immutable
// process identity and the node-local draining gate, which prevents dependency
// cycles between background task providers and ClusterRuntime.
type ClusterNodeState struct {
	enabled      bool
	deploymentID string
	nodeID       string
	bootID       string
	draining     atomic.Bool
}

func NewClusterNodeState(cfg *config.Config) *ClusterNodeState {
	state := &ClusterNodeState{bootID: uuid.NewString()}
	if cfg == nil {
		return state
	}
	state.enabled = cfg.Cluster.Enabled
	state.deploymentID = cfg.Cluster.DeploymentID
	state.nodeID = cfg.Cluster.NodeID
	return state
}

func (s *ClusterNodeState) Enabled() bool {
	return s != nil && s.enabled
}

func (s *ClusterNodeState) Identity() (deploymentID, nodeID, bootID string) {
	if s == nil {
		return "", "", ""
	}
	return s.deploymentID, s.nodeID, s.bootID
}

func (s *ClusterNodeState) IsDraining() bool {
	return s != nil && s.draining.Load()
}

func (s *ClusterNodeState) SetDraining(draining bool) {
	if s != nil {
		s.draining.Store(draining)
	}
}
