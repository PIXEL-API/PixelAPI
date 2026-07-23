package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const clusterCacheVersionColumns = `
	deployment_id,
	cache_key,
	version,
	COALESCE(updated_by_node_id, ''),
	updated_at`

func (r *clusterRepository) GetCacheVersion(ctx context.Context, deploymentID, cacheKey string) (*service.ClusterCacheVersion, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}
	if err := validateClusterCacheKey(cacheKey); err != nil {
		return nil, err
	}

	version, err := clusterQueryOne(
		ctx,
		r.db,
		`
		SELECT `+clusterCacheVersionColumns+`
		FROM cluster_cache_versions
		WHERE deployment_id = $1 AND cache_key = $2
		`,
		[]any{deploymentID, cacheKey},
		scanClusterCacheVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrClusterCacheVersionNotFound
	}
	return version, err
}

func (r *clusterRepository) ListCacheVersions(ctx context.Context, deploymentID string) ([]service.ClusterCacheVersion, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+clusterCacheVersionColumns+`
		FROM cluster_cache_versions
		WHERE deployment_id = $1
		ORDER BY cache_key ASC
	`, deploymentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	versions := make([]service.ClusterCacheVersion, 0, 3)
	for rows.Next() {
		version, scanErr := scanClusterCacheVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		versions = append(versions, *version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

func (r *clusterRepository) EnsureCacheVersions(ctx context.Context, deploymentID, nodeID string) error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return err
	}
	if err := validateClusterRequired("node_id", nodeID); err != nil {
		return err
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cluster_cache_versions (
			deployment_id,
			cache_key,
			version,
			updated_by_node_id,
			updated_at
		) VALUES
			($1, 'channel_routing', 0, $2, clock_timestamp()),
			($1, 'runtime_settings', 0, $2, clock_timestamp()),
			($1, 'policy_metadata', 0, $2, clock_timestamp())
		ON CONFLICT (deployment_id, cache_key) DO NOTHING
	`, deploymentID, nodeID)
	return err
}

func (r *clusterRepository) BumpCacheVersion(ctx context.Context, deploymentID, cacheKey, nodeID string) (*service.ClusterCacheVersion, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("deployment_id", deploymentID); err != nil {
		return nil, err
	}
	if err := validateClusterCacheKey(cacheKey); err != nil {
		return nil, err
	}
	if err := validateClusterRequired("node_id", nodeID); err != nil {
		return nil, err
	}

	return clusterQueryOne(
		ctx,
		r.db,
		`
		INSERT INTO cluster_cache_versions (
			deployment_id,
			cache_key,
			version,
			updated_by_node_id
		) VALUES ($1, $2, 1, $3)
		ON CONFLICT (deployment_id, cache_key) DO UPDATE
		SET
			version = cluster_cache_versions.version + 1,
			updated_by_node_id = EXCLUDED.updated_by_node_id,
			updated_at = clock_timestamp()
		RETURNING `+clusterCacheVersionColumns,
		[]any{deploymentID, cacheKey, nodeID},
		scanClusterCacheVersion,
	)
}

func validateClusterCacheKey(cacheKey string) error {
	switch cacheKey {
	case service.ClusterCacheKeyChannelRouting,
		service.ClusterCacheKeyRuntimeSettings,
		service.ClusterCacheKeyPolicyMetadata:
		return nil
	default:
		return fmt.Errorf("invalid cache_key %q", cacheKey)
	}
}

func scanClusterCacheVersion(scanner clusterRowScanner) (*service.ClusterCacheVersion, error) {
	var version service.ClusterCacheVersion
	if err := scanner.Scan(
		&version.DeploymentID,
		&version.CacheKey,
		&version.Version,
		&version.UpdatedByNodeID,
		&version.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &version, nil
}
