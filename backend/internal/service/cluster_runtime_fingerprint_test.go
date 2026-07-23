package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClusterConfigFingerprintIgnoresNodeLocalIdentity(t *testing.T) {
	first := &config.Config{
		Server: config.ServerConfig{
			Host: "10.77.0.21",
			Port: 8080,
		},
		Cluster: config.ClusterConfig{
			Enabled:      true,
			DeploymentID: "pixel-prod",
			NodeID:       "pixel-app-01",
		},
		Database: config.DatabaseConfig{
			Host:          "10.77.0.10",
			Port:          5432,
			MigrationMode: config.DatabaseMigrationModeValidate,
		},
	}
	second := *first
	second.Server.Host = "10.77.0.22"
	second.Cluster.NodeID = "pixel-app-02"

	require.Equal(t, clusterConfigFingerprint(first), clusterConfigFingerprint(&second))
}

func TestClusterConfigFingerprintDetectsAnySharedConfigDifference(t *testing.T) {
	first := &config.Config{
		Server: config.ServerConfig{
			Host: "10.77.0.21",
			Port: 8080,
		},
		Cluster: config.ClusterConfig{
			Enabled:      true,
			DeploymentID: "pixel-prod",
			NodeID:       "pixel-app-01",
		},
		Database: config.DatabaseConfig{
			Host:          "10.77.0.10",
			Port:          5432,
			MigrationMode: config.DatabaseMigrationModeValidate,
		},
		RateLimit: config.RateLimitConfig{
			OverloadCooldownMinutes: 100,
		},
	}
	second := *first
	second.RateLimit.OverloadCooldownMinutes = 101

	require.NotEqual(t, clusterConfigFingerprint(first), clusterConfigFingerprint(&second))
}

func TestClusterConfigFingerprintIsStableAcrossMapInsertionOrder(t *testing.T) {
	first := &config.Config{
		Cluster: config.ClusterConfig{DeploymentID: "pixel-prod"},
		Gemini: config.GeminiConfig{
			Quota: config.GeminiQuotaConfig{
				Tiers: map[string]config.GeminiTierQuotaConfig{
					"pro":   {ProRPD: clusterFingerprintInt64Pointer(100)},
					"flash": {ProRPD: clusterFingerprintInt64Pointer(200)},
				},
			},
		},
	}
	second := &config.Config{
		Cluster: config.ClusterConfig{DeploymentID: "pixel-prod"},
		Gemini: config.GeminiConfig{
			Quota: config.GeminiQuotaConfig{
				Tiers: map[string]config.GeminiTierQuotaConfig{
					"flash": {ProRPD: clusterFingerprintInt64Pointer(200)},
					"pro":   {ProRPD: clusterFingerprintInt64Pointer(100)},
				},
			},
		},
	}

	require.Equal(t, clusterConfigFingerprint(first), clusterConfigFingerprint(second))
}

func clusterFingerprintInt64Pointer(value int64) *int64 {
	return &value
}
