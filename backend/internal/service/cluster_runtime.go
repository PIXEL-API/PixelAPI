package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	clusterHealthCheckTimeout = 3 * time.Second
	clusterOperationBatchSize = 10
	clusterInstanceRetention  = 30 * 24 * time.Hour
	clusterRetentionInterval  = 24 * time.Hour
)

var clusterSafeCacheKeys = []string{
	ClusterCacheKeyChannelRouting,
	ClusterCacheKeyRuntimeSettings,
	ClusterCacheKeyPolicyMetadata,
}

// ClusterReadiness is an immutable snapshot used by health handlers.
type ClusterReadiness struct {
	Enabled           bool   `json:"cluster_enabled"`
	Ready             bool   `json:"ready"`
	DesiredState      string `json:"desired_state"`
	ObservedState     string `json:"observed_state"`
	DatabaseHealthy   bool   `json:"database_healthy"`
	RedisHealthy      bool   `json:"redis_healthy"`
	CacheHealthy      bool   `json:"cache_healthy"`
	MigrationHealthy  bool   `json:"migration_healthy"`
	ConfigCompatible  bool   `json:"config_compatible"`
	IdentityOwned     bool   `json:"identity_owned"`
	ShutdownRequested bool   `json:"shutdown_requested"`
	Message           string `json:"message"`
}

// ClusterRuntime owns only process-local cluster coordination state. Shared
// state is persisted through ClusterRepository and all expiry decisions use
// PostgreSQL time inside that repository.
type ClusterRuntime struct {
	enabled      bool
	clusterCfg   config.ClusterConfig
	serverCfg    config.ServerConfig
	databaseCfg  config.DatabaseConfig
	redisCfg     config.RedisConfig
	buildInfo    BuildInfo
	repository   ClusterRepository
	db           *sql.DB
	redis        ClusterRedisPort
	connections  *ClusterConnectionTracker
	nodeState    *ClusterNodeState
	clusterCache *ClusterCacheCoordinator
	taskExecutor *ClusterTaskExecutor
	channel      *ChannelService
	settings     *SettingService
	moderation   *ContentModerationService
	bootID       string
	hostname     string
	startedAt    time.Time
	configHash   string
	secretHash   string
	notifyTopic  string
	processStats clusterProcessMetricsSampler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	desiredState  atomic.Value // string
	observedState atomic.Value // string
	shuttingDown  atomic.Bool
	drainAfter    atomic.Int64

	databaseHealthy  atomic.Bool
	redisHealthy     atomic.Bool
	cacheHealthy     atomic.Bool
	migrationHealthy atomic.Bool
	configCompatible atomic.Bool
	identityOwned    atomic.Bool

	cacheMu       sync.RWMutex
	cacheVersions map[string]int64
	cacheError    string
	healthMu      sync.RWMutex
	healthError   string

	cacheWake chan struct{}
	fatal     chan error
	fatalOnce sync.Once
}

type clusterCacheNotification struct {
	CacheKey string `json:"cache_key"`
	Version  int64  `json:"version"`
	NodeID   string `json:"node_id"`
}

// NewClusterRuntime claims this process identity before returning. A live
// process using the same deployment_id + node_id causes startup to fail.
func NewClusterRuntime(
	cfg *config.Config,
	repository ClusterRepository,
	db *sql.DB,
	redisPort ClusterRedisPort,
	connectionTracker *ClusterConnectionTracker,
	nodeState *ClusterNodeState,
	clusterCache *ClusterCacheCoordinator,
	taskExecutor *ClusterTaskExecutor,
	buildInfo BuildInfo,
	channelService *ChannelService,
	settingService *SettingService,
	contentModerationService *ContentModerationService,
) (*ClusterRuntime, error) {
	if cfg == nil {
		return nil, errors.New("cluster runtime requires config")
	}
	if cfg.Cluster.Enabled {
		var missing []string
		if repository == nil {
			missing = append(missing, "cluster repository")
		}
		if db == nil {
			missing = append(missing, "PostgreSQL")
		}
		if redisPort == nil {
			missing = append(missing, "Redis")
		}
		if connectionTracker == nil {
			missing = append(missing, "connection tracker")
		}
		if nodeState == nil {
			missing = append(missing, "node state")
		}
		if clusterCache == nil {
			missing = append(missing, "cache coordinator")
		}
		if taskExecutor == nil {
			missing = append(missing, "task executor")
		}
		if channelService == nil {
			missing = append(missing, "channel cache service")
		}
		if settingService == nil {
			missing = append(missing, "runtime settings service")
		}
		if contentModerationService == nil {
			missing = append(missing, "policy metadata service")
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("cluster runtime missing required dependencies: %s", strings.Join(missing, ", "))
		}

		deploymentID, nodeID, bootID := nodeState.Identity()
		if deploymentID != cfg.Cluster.DeploymentID || nodeID != cfg.Cluster.NodeID || bootID == "" {
			return nil, errors.New("cluster runtime node state identity does not match configuration")
		}
		if taskExecutor.initErr != nil {
			return nil, fmt.Errorf("cluster runtime task executor is invalid: %w", taskExecutor.initErr)
		}
		if !taskExecutor.enabled() {
			return nil, errors.New("cluster runtime task executor is not ready")
		}
		if !clusterCache.enabled ||
			clusterCache.deploymentID != cfg.Cluster.DeploymentID ||
			clusterCache.nodeID != cfg.Cluster.NodeID ||
			clusterCache.repository == nil ||
			clusterCache.publisher == nil {
			return nil, errors.New("cluster runtime cache coordinator is not ready")
		}
		if channelService.clusterCache != clusterCache ||
			settingService.clusterCache != clusterCache ||
			contentModerationService.clusterCache != clusterCache {
			return nil, errors.New("cluster runtime cache services are not wired to the shared coordinator")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	runtimeService := &ClusterRuntime{
		enabled:       cfg.Cluster.Enabled,
		clusterCfg:    cfg.Cluster,
		serverCfg:     cfg.Server,
		databaseCfg:   cfg.Database,
		redisCfg:      cfg.Redis,
		buildInfo:     buildInfo,
		repository:    repository,
		db:            db,
		redis:         redisPort,
		connections:   connectionTracker,
		channel:       channelService,
		settings:      settingService,
		moderation:    contentModerationService,
		startedAt:     time.Now().UTC(),
		notifyTopic:   "sub2api:cluster:" + cfg.Cluster.DeploymentID + ":cache-versions",
		ctx:           ctx,
		cancel:        cancel,
		cacheVersions: make(map[string]int64, len(clusterSafeCacheKeys)),
		cacheWake:     make(chan struct{}, 1),
		fatal:         make(chan error, 1),
		nodeState:     nodeState,
		clusterCache:  clusterCache,
		taskExecutor:  taskExecutor,
	}
	if nodeState != nil {
		_, _, runtimeService.bootID = nodeState.Identity()
	}
	runtimeService.desiredState.Store(ClusterDesiredStateActive)
	runtimeService.observedState.Store(ClusterObservedStateStarting)
	runtimeService.migrationHealthy.Store(true)
	runtimeService.configCompatible.Store(true)

	if !runtimeService.enabled {
		runtimeService.identityOwned.Store(true)
		runtimeService.databaseHealthy.Store(true)
		runtimeService.redisHealthy.Store(true)
		runtimeService.cacheHealthy.Store(true)
		runtimeService.observedState.Store(ClusterObservedStateReady)
		return runtimeService, nil
	}
	hostname, err := os.Hostname()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("resolve cluster hostname: %w", err)
	}
	runtimeService.hostname = hostname
	runtimeService.configHash = clusterConfigFingerprint(cfg)
	runtimeService.secretHash = clusterSecretFingerprint(cfg)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer startupCancel()
	if err := repository.EnsureCacheVersions(
		startupCtx,
		cfg.Cluster.DeploymentID,
		cfg.Cluster.NodeID,
	); err != nil {
		cancel()
		return nil, fmt.Errorf("ensure cluster cache versions: %w", err)
	}
	if err := repository.ClaimInstance(
		startupCtx,
		runtimeService.baseHeartbeat(ClusterObservedStateStarting),
		time.Duration(cfg.Cluster.NodeTTLSeconds)*time.Second,
	); err != nil {
		cancel()
		return nil, fmt.Errorf("claim cluster node identity: %w", err)
	}
	runtimeService.identityOwned.Store(true)

	// Initial cache reconciliation is synchronous so a node can never become
	// ready with an unknown cache generation.
	if err := runtimeService.reconcileCaches(startupCtx); err != nil {
		runtimeService.setCacheError(err)
	}
	runtimeService.refreshDependencies(startupCtx)
	runtimeService.refreshCompatibility(startupCtx)
	if err := runtimeService.sendHeartbeat(startupCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("write initial cluster heartbeat: %w", err)
	}

	runtimeService.start()
	return runtimeService, nil
}

func (r *ClusterRuntime) start() {
	if r == nil || !r.enabled {
		return
	}
	r.wg.Add(5)
	go r.heartbeatLoop()
	go r.operationLoop()
	go r.cacheLoop()
	go r.cacheSubscriberLoop()
	go r.instanceRetentionLoop()
}

func (r *ClusterRuntime) instanceRetentionLoop() {
	defer r.wg.Done()
	r.runInstanceRetention()
	ticker := time.NewTicker(clusterRetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.runInstanceRetention()
		}
	}
}

func (r *ClusterRuntime) runInstanceRetention() {
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	_, err := r.taskExecutor.Run(ctx, "cluster.instances.retention", func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		_, err := r.repository.DeleteOfflineInstances(
			taskCtx,
			r.clusterCfg.DeploymentID,
			clusterInstanceRetention,
		)
		return err
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("cluster offline instance retention failed", "error", err)
	}
}

func (r *ClusterRuntime) Enabled() bool {
	return r != nil && r.enabled
}

func (r *ClusterRuntime) DeploymentID() string {
	if r == nil {
		return ""
	}
	return r.clusterCfg.DeploymentID
}

func (r *ClusterRuntime) NodeID() string {
	if r == nil {
		return ""
	}
	return r.clusterCfg.NodeID
}

func (r *ClusterRuntime) BootID() string {
	if r == nil {
		return ""
	}
	return r.bootID
}

func (r *ClusterRuntime) Identity() (deploymentID, nodeID, bootID string) {
	if r == nil {
		return "", "", ""
	}
	return r.clusterCfg.DeploymentID, r.clusterCfg.NodeID, r.bootID
}

func (r *ClusterRuntime) IsDraining() bool {
	return r != nil && r.enabled && r.nodeState != nil && r.nodeState.IsDraining()
}

func (r *ClusterRuntime) AcceptingGateway() bool {
	return r == nil || !r.ShouldRejectNewGatewayRequests()
}

func (r *ClusterRuntime) Fatal() <-chan error {
	if r == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return r.fatal
}

func (r *ClusterRuntime) fail(err error) {
	if r == nil || err == nil {
		return
	}
	r.identityOwned.Store(false)
	r.observedState.Store(ClusterObservedStateUnhealthy)
	r.setHealthError(err)
	r.fatalOnce.Do(func() {
		r.fatal <- err
	})
}

func (r *ClusterRuntime) BeginShutdown() {
	if r == nil || !r.enabled || !r.shuttingDown.CompareAndSwap(false, true) {
		return
	}
	r.observedState.Store(ClusterObservedStateDraining)
	r.nodeState.SetDraining(true)
	r.drainAfter.Store(time.Now().Add(time.Duration(r.serverCfg.DrainDelaySeconds) * time.Second).UnixNano())
}

func (r *ClusterRuntime) ShouldRejectNewGatewayRequests() bool {
	if r == nil || !r.enabled {
		return false
	}
	if r.desired() != ClusterDesiredStateDraining && !r.shuttingDown.Load() {
		return false
	}
	rejectAt := r.drainAfter.Load()
	return rejectAt > 0 && time.Now().UnixNano() >= rejectAt
}

func (r *ClusterRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.BeginShutdown()
	r.cancel()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		<-done
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop cluster runtime: %w", ctx.Err())
	}
}

func (r *ClusterRuntime) Readiness() ClusterReadiness {
	if r == nil {
		return ClusterReadiness{Message: "cluster runtime unavailable"}
	}
	if !r.enabled {
		return ClusterReadiness{
			Enabled:          false,
			Ready:            true,
			DesiredState:     ClusterDesiredStateActive,
			ObservedState:    ClusterObservedStateReady,
			DatabaseHealthy:  true,
			RedisHealthy:     true,
			CacheHealthy:     true,
			MigrationHealthy: true,
			ConfigCompatible: true,
			IdentityOwned:    true,
			Message:          "ready",
		}
	}

	snapshot := ClusterReadiness{
		Enabled:           true,
		DesiredState:      r.desired(),
		ObservedState:     r.observed(),
		DatabaseHealthy:   r.databaseHealthy.Load(),
		RedisHealthy:      r.redisHealthy.Load(),
		CacheHealthy:      r.cacheHealthy.Load() && r.clusterCache.Healthy(),
		MigrationHealthy:  r.migrationHealthy.Load(),
		ConfigCompatible:  r.configCompatible.Load(),
		IdentityOwned:     r.identityOwned.Load(),
		ShutdownRequested: r.shuttingDown.Load(),
	}
	snapshot.Ready = snapshot.DesiredState == ClusterDesiredStateActive &&
		snapshot.ObservedState == ClusterObservedStateReady &&
		snapshot.DatabaseHealthy &&
		snapshot.RedisHealthy &&
		snapshot.CacheHealthy &&
		snapshot.MigrationHealthy &&
		snapshot.ConfigCompatible &&
		snapshot.IdentityOwned &&
		!snapshot.ShutdownRequested
	if snapshot.Ready {
		snapshot.Message = "ready"
	} else {
		snapshot.Message = r.readinessMessage(snapshot)
	}
	return snapshot
}

func (r *ClusterRuntime) readinessMessage(snapshot ClusterReadiness) string {
	switch {
	case !snapshot.IdentityOwned:
		return "cluster node identity ownership lost"
	case snapshot.ShutdownRequested:
		return "node is shutting down"
	case snapshot.DesiredState == ClusterDesiredStateDraining:
		return "node is draining"
	case !snapshot.MigrationHealthy:
		return "database migration validation failed"
	case !snapshot.DatabaseHealthy:
		return "PostgreSQL unavailable"
	case !snapshot.RedisHealthy:
		return "Redis unavailable"
	case !snapshot.CacheHealthy:
		if r.clusterCache != nil && !r.clusterCache.Healthy() {
			return r.clusterCache.LastError()
		}
		r.cacheMu.RLock()
		defer r.cacheMu.RUnlock()
		if r.cacheError != "" {
			return r.cacheError
		}
		return "cluster cache is not synchronized"
	case !snapshot.ConfigCompatible:
		return "shared cluster configuration fingerprint mismatch"
	default:
		r.healthMu.RLock()
		defer r.healthMu.RUnlock()
		if r.healthError != "" {
			return r.healthError
		}
		return "node is not ready"
	}
}

func (r *ClusterRuntime) ConnectionCounts() (httpCount, sseCount, websocketCount int64) {
	if r == nil || r.connections == nil {
		return 0, 0, 0
	}
	snapshot := r.connections.Snapshot()
	return snapshot.HTTP, snapshot.SSE, snapshot.WebSocket
}

func (r *ClusterRuntime) AppliedCacheVersions() map[string]int64 {
	if r == nil {
		return map[string]int64{}
	}
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	result := make(map[string]int64, len(r.cacheVersions))
	for key, version := range r.cacheVersions {
		result[key] = version
	}
	return result
}

func (r *ClusterRuntime) heartbeatLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Duration(r.clusterCfg.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(r.ctx, clusterHealthCheckTimeout)
			r.refreshDependencies(checkCtx)
			r.refreshCompatibility(checkCtx)
			err := r.sendHeartbeat(checkCtx)
			cancel()
			if errors.Is(err, ErrClusterInstanceOwnerLost) {
				r.fail(fmt.Errorf("cluster heartbeat ownership lost: %w", err))
				return
			}
			if err != nil {
				r.databaseHealthy.Store(false)
				r.setHealthError(fmt.Errorf("cluster heartbeat failed: %w", err))
			}
		}
	}
}

func (r *ClusterRuntime) operationLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Duration(r.clusterCfg.OperationPollIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.pollOperations()
		}
	}
}

func (r *ClusterRuntime) cacheLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(time.Duration(r.clusterCfg.CacheReconcileIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		case <-r.cacheWake:
		}
		checkCtx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
		if err := r.reconcileCaches(checkCtx); err != nil {
			r.setCacheError(err)
		}
		cancel()
	}
}

func (r *ClusterRuntime) cacheSubscriberLoop() {
	defer r.wg.Done()
	retryDelay := time.Duration(r.clusterCfg.OperationPollIntervalSeconds) * time.Second
	if retryDelay < time.Second {
		retryDelay = time.Second
	}
	for {
		if r.ctx.Err() != nil {
			return
		}
		pubsub := r.redis.Subscribe(r.ctx, r.notifyTopic)
		for {
			err := pubsub.Receive(r.ctx)
			if err != nil {
				_ = pubsub.Close()
				break
			}
			r.wakeCacheReconcile()
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func (r *ClusterRuntime) wakeCacheReconcile() {
	select {
	case r.cacheWake <- struct{}{}:
	default:
	}
}

func (r *ClusterRuntime) refreshDependencies(ctx context.Context) {
	dbHealthy := false
	redisHealthy := false
	var healthErrors []string
	if err := r.db.PingContext(ctx); err != nil {
		healthErrors = append(healthErrors, "PostgreSQL: "+err.Error())
	} else {
		dbHealthy = true
	}
	if err := r.redis.Ping(ctx); err != nil {
		healthErrors = append(healthErrors, "Redis: "+err.Error())
	} else {
		redisHealthy = true
	}
	if r.clusterCache != nil {
		if err := r.clusterCache.RetryPending(ctx); err != nil {
			healthErrors = append(healthErrors, "cluster cache version retry: "+err.Error())
		}
	}
	r.databaseHealthy.Store(dbHealthy)
	r.redisHealthy.Store(redisHealthy)
	if len(healthErrors) > 0 {
		r.setHealthError(errors.New(strings.Join(healthErrors, "; ")))
	} else {
		r.setHealthError(nil)
	}
	r.updateObservedState()
}

func (r *ClusterRuntime) refreshCompatibility(ctx context.Context) {
	instances, err := r.repository.ListInstances(
		ctx,
		r.clusterCfg.DeploymentID,
		time.Duration(r.clusterCfg.NodeTTLSeconds)*time.Second,
		time.Duration(r.clusterCfg.OfflineAfterSeconds)*time.Second,
	)
	if err != nil {
		r.configCompatible.Store(false)
		r.setHealthError(fmt.Errorf("check cluster fingerprints: %w", err))
		r.updateObservedState()
		return
	}
	for _, instance := range instances {
		if instance.DerivedState == ClusterDerivedStateStale ||
			instance.DerivedState == ClusterDerivedStateOffline {
			continue
		}
		if instance.ConfigFingerprint != "" && instance.ConfigFingerprint != r.configHash {
			r.configCompatible.Store(false)
			r.setHealthError(fmt.Errorf("config fingerprint mismatch with node %s", instance.NodeID))
			r.updateObservedState()
			return
		}
		if instance.SecretFingerprint != "" && instance.SecretFingerprint != r.secretHash {
			r.configCompatible.Store(false)
			r.setHealthError(fmt.Errorf("secret fingerprint mismatch with node %s", instance.NodeID))
			r.updateObservedState()
			return
		}
	}
	r.configCompatible.Store(true)
	r.updateObservedState()
}

func (r *ClusterRuntime) updateObservedState() {
	switch {
	case r.shuttingDown.Load() || r.desired() == ClusterDesiredStateDraining:
		r.observedState.Store(ClusterObservedStateDraining)
	case r.databaseHealthy.Load() &&
		r.redisHealthy.Load() &&
		r.cacheHealthy.Load() &&
		r.clusterCache.Healthy() &&
		r.migrationHealthy.Load() &&
		r.configCompatible.Load() &&
		r.identityOwned.Load():
		r.observedState.Store(ClusterObservedStateReady)
	default:
		r.observedState.Store(ClusterObservedStateUnhealthy)
	}
}

func (r *ClusterRuntime) sendHeartbeat(ctx context.Context) error {
	heartbeat := r.baseHeartbeat(r.observed())
	metrics := r.processStats.Sample()
	dbStats := r.db.Stats()
	redisStats := r.redis.PoolStats()
	heartbeat.CPUPercent = metrics.CPUPercent
	heartbeat.RSSBytes = metrics.RSSBytes
	heartbeat.MemoryLimitBytes = metrics.MemoryLimitBytes
	heartbeat.GoroutineCount = int64(runtime.NumGoroutine())
	heartbeat.FDOpen = metrics.FDOpen
	heartbeat.FDLimit = metrics.FDLimit
	heartbeat.ActiveHTTP, heartbeat.ActiveSSE, heartbeat.ActiveWebSocket = r.ConnectionCounts()
	heartbeat.DBOpenConnections = dbStats.OpenConnections
	heartbeat.DBInUseConnections = dbStats.InUse
	heartbeat.DBIdleConnections = dbStats.Idle
	heartbeat.DBWaitCount = dbStats.WaitCount
	heartbeat.DBMaxOpenConnections = dbStats.MaxOpenConnections
	heartbeat.RedisPoolConnections = int(redisStats.TotalConnections)
	heartbeat.RedisIdleConnections = int(redisStats.IdleConnections)
	heartbeat.RedisPoolSize = r.redisCfg.PoolSize
	heartbeat.CacheVersions = r.AppliedCacheVersions()
	heartbeat.DatabaseHealthy = r.databaseHealthy.Load()
	heartbeat.RedisHealthy = r.redisHealthy.Load()
	heartbeat.CacheHealthy = r.cacheHealthy.Load() && r.clusterCache.Healthy()
	heartbeat.MigrationHealthy = r.migrationHealthy.Load()
	heartbeat.LastError = r.Readiness().Message
	if heartbeat.LastError == "ready" {
		heartbeat.LastError = ""
	}

	instance, err := r.repository.Heartbeat(ctx, heartbeat)
	if err != nil {
		return err
	}
	if instance != nil && instance.DesiredState != "" && instance.DesiredState != r.desired() {
		r.applyDesiredState(instance.DesiredState)
	}
	return nil
}

func (r *ClusterRuntime) baseHeartbeat(observedState string) ClusterInstanceHeartbeat {
	return ClusterInstanceHeartbeat{
		DeploymentID:      r.clusterCfg.DeploymentID,
		NodeID:            r.clusterCfg.NodeID,
		BootID:            r.bootID,
		Hostname:          r.hostname,
		Version:           r.buildInfo.Version,
		CommitSHA:         r.buildInfo.Commit,
		BuildDate:         r.buildInfo.Date,
		ConfigFingerprint: r.configHash,
		SecretFingerprint: r.secretHash,
		CacheVersions:     r.AppliedCacheVersions(),
		ObservedState:     observedState,
	}
}

func (r *ClusterRuntime) reconcileCaches(ctx context.Context) error {
	authoritative, err := r.repository.ListCacheVersions(ctx, r.clusterCfg.DeploymentID)
	if err != nil {
		return fmt.Errorf("list authoritative cache versions: %w", err)
	}
	versions := make(map[string]int64, len(authoritative))
	for _, item := range authoritative {
		versions[item.CacheKey] = item.Version
	}
	for _, key := range clusterSafeCacheKeys {
		version, ok := versions[key]
		if !ok {
			return fmt.Errorf("authoritative cache version %s is missing", key)
		}
		r.cacheMu.RLock()
		applied, alreadyApplied := r.cacheVersions[key]
		r.cacheMu.RUnlock()
		if alreadyApplied && applied > version {
			return fmt.Errorf("cache version regression for %s: applied=%d authoritative=%d", key, applied, version)
		}
		if alreadyApplied && applied == version {
			continue
		}
		if err := r.refreshCache(ctx, key); err != nil {
			return err
		}
		r.cacheMu.Lock()
		r.cacheVersions[key] = version
		r.cacheMu.Unlock()
	}
	r.cacheMu.Lock()
	r.cacheError = ""
	r.cacheMu.Unlock()
	r.cacheHealthy.Store(true)
	r.updateObservedState()
	return nil
}

func (r *ClusterRuntime) refreshCache(ctx context.Context, key string) error {
	switch key {
	case ClusterCacheKeyChannelRouting:
		if r.channel == nil {
			return errors.New("channel routing cache service unavailable")
		}
		if err := r.channel.ReloadCache(ctx); err != nil {
			return fmt.Errorf("refresh channel routing cache: %w", err)
		}
	case ClusterCacheKeyRuntimeSettings:
		if r.settings == nil {
			return errors.New("runtime settings cache service unavailable")
		}
		if err := r.settings.RefreshRuntimeSettingsCache(ctx); err != nil {
			return fmt.Errorf("refresh runtime settings cache: %w", err)
		}
	case ClusterCacheKeyPolicyMetadata:
		if r.moderation == nil {
			return errors.New("policy metadata cache service unavailable")
		}
		if err := r.moderation.RefreshPolicyMetadataCache(ctx); err != nil {
			return fmt.Errorf("refresh policy metadata cache: %w", err)
		}
	default:
		return fmt.Errorf("unsafe cluster cache scope %q", key)
	}
	return nil
}

func (r *ClusterRuntime) pollOperations() {
	if !r.identityOwned.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	operations, err := r.repository.ClaimPendingOperations(
		ctx,
		r.clusterCfg.DeploymentID,
		r.clusterCfg.NodeID,
		r.bootID,
		clusterOperationBatchSize,
		time.Duration(r.clusterCfg.TaskLeaseSeconds)*time.Second,
	)
	if err != nil {
		slog.Error("cluster operation poll failed", "node_id", r.clusterCfg.NodeID, "error", err)
		return
	}
	for i := range operations {
		r.executeOperation(ctx, &operations[i])
	}
}

func (r *ClusterRuntime) executeOperation(ctx context.Context, operation *ClusterOperation) {
	if operation == nil {
		return
	}
	result, operationErr := r.applyOperation(ctx, operation)
	completed, completeErr := r.repository.CompleteOperation(
		ctx,
		r.clusterCfg.DeploymentID,
		operation.ID,
		r.clusterCfg.NodeID,
		r.bootID,
		operation.AttemptToken,
		operationErr == nil,
		result,
		errorString(operationErr),
	)
	if completeErr != nil {
		slog.Error("complete cluster operation failed", "operation_id", operation.ID, "error", completeErr)
		return
	}
	if !completed {
		slog.Error("cluster operation ownership expired before completion", "operation_id", operation.ID)
	}
}

func (r *ClusterRuntime) applyOperation(ctx context.Context, operation *ClusterOperation) (string, error) {
	switch operation.Type {
	case ClusterOperationTypeDrain:
		if operation.TargetNodeID != r.clusterCfg.NodeID {
			return "", fmt.Errorf("drain operation targets node %s", operation.TargetNodeID)
		}
		instance, err := r.repository.SetInstanceDesiredState(
			ctx,
			r.clusterCfg.DeploymentID,
			r.clusterCfg.NodeID,
			ClusterDesiredStateDraining,
		)
		if err != nil {
			return "", err
		}
		r.applyDesiredState(instance.DesiredState)
		return "node readiness disabled and gateway drain delay started", nil
	case ClusterOperationTypeResume:
		if operation.TargetNodeID != r.clusterCfg.NodeID {
			return "", fmt.Errorf("resume operation targets node %s", operation.TargetNodeID)
		}
		r.refreshDependencies(ctx)
		if err := r.reconcileCaches(ctx); err != nil {
			r.setCacheError(err)
			return "", err
		}
		if !r.databaseHealthy.Load() || !r.redisHealthy.Load() ||
			!r.cacheHealthy.Load() || !r.migrationHealthy.Load() ||
			!r.configCompatible.Load() ||
			!r.identityOwned.Load() {
			return "", errors.New("node dependencies are not healthy enough to resume")
		}
		instance, err := r.repository.SetInstanceDesiredState(
			ctx,
			r.clusterCfg.DeploymentID,
			r.clusterCfg.NodeID,
			ClusterDesiredStateActive,
		)
		if err != nil {
			return "", err
		}
		r.applyDesiredState(instance.DesiredState)
		return "node resumed and readiness enabled", nil
	case ClusterOperationTypeCacheRefresh:
		keys, err := cacheKeysForScope(operation.CacheScope)
		if err != nil {
			return "", err
		}
		for _, key := range keys {
			if _, err := r.bumpAndPublishCacheVersion(ctx, key); err != nil {
				return "", err
			}
		}
		r.wakeCacheReconcile()
		return "authoritative cache version advanced", nil
	default:
		return "", fmt.Errorf("unsupported cluster operation %q", operation.Type)
	}
}

func (r *ClusterRuntime) applyDesiredState(desiredState string) {
	switch desiredState {
	case ClusterDesiredStateActive:
		r.desiredState.Store(desiredState)
		r.nodeState.SetDraining(false)
		r.drainAfter.Store(0)
	case ClusterDesiredStateDraining:
		r.desiredState.Store(desiredState)
		r.nodeState.SetDraining(true)
		if r.drainAfter.Load() == 0 {
			r.drainAfter.Store(time.Now().Add(time.Duration(r.serverCfg.DrainDelaySeconds) * time.Second).UnixNano())
		}
	default:
		r.setHealthError(fmt.Errorf("invalid desired cluster state %q", desiredState))
	}
	r.updateObservedState()
}

func (r *ClusterRuntime) bumpAndPublishCacheVersion(ctx context.Context, key string) (*ClusterCacheVersion, error) {
	version, err := r.repository.BumpCacheVersion(
		ctx,
		r.clusterCfg.DeploymentID,
		key,
		r.clusterCfg.NodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("bump cache version %s: %w", key, err)
	}
	payload, err := json.Marshal(clusterCacheNotification{
		CacheKey: version.CacheKey,
		Version:  version.Version,
		NodeID:   r.clusterCfg.NodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode cache notification: %w", err)
	}
	if err := r.redis.Publish(ctx, r.notifyTopic, payload); err != nil {
		// PostgreSQL remains authoritative. Report the acceleration failure while
		// still waking this node; every node also performs periodic reconciliation.
		slog.Warn("cluster cache notification publish failed",
			"cache_key", key,
			"version", version.Version,
			"error", err,
		)
	}
	return version, nil
}

func cacheKeysForScope(scope string) ([]string, error) {
	switch scope {
	case ClusterCacheScopeAllSafe:
		result := append([]string(nil), clusterSafeCacheKeys...)
		sort.Strings(result)
		return result, nil
	case ClusterCacheKeyChannelRouting,
		ClusterCacheKeyRuntimeSettings,
		ClusterCacheKeyPolicyMetadata:
		return []string{scope}, nil
	default:
		return nil, fmt.Errorf("unsafe cluster cache scope %q", scope)
	}
}

func (r *ClusterRuntime) desired() string {
	if r == nil {
		return ClusterDesiredStateActive
	}
	value, _ := r.desiredState.Load().(string)
	if value == "" {
		return ClusterDesiredStateActive
	}
	return value
}

func (r *ClusterRuntime) observed() string {
	if r == nil {
		return ClusterObservedStateUnhealthy
	}
	value, _ := r.observedState.Load().(string)
	if value == "" {
		return ClusterObservedStateStarting
	}
	return value
}

func (r *ClusterRuntime) setCacheError(err error) {
	if r == nil {
		return
	}
	r.cacheMu.Lock()
	r.cacheError = errorString(err)
	r.cacheMu.Unlock()
	r.cacheHealthy.Store(false)
	r.updateObservedState()
}

func (r *ClusterRuntime) setHealthError(err error) {
	if r == nil {
		return
	}
	r.healthMu.Lock()
	r.healthError = errorString(err)
	r.healthMu.Unlock()
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func clusterConfigFingerprint(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}

	// Hash the complete effective configuration so a node cannot become ready
	// with a divergent billing, routing, rate-limit, or other shared setting.
	// Only fields intentionally owned by an individual node are normalized.
	normalized := *cfg
	normalized.Cluster.NodeID = ""
	normalized.Server.Host = ""

	value, err := json.Marshal(normalized)
	if err != nil {
		// Config only contains JSON-compatible values. Treat a future
		// incompatible field as a programming error instead of silently
		// producing an empty fingerprint and weakening readiness validation.
		panic(fmt.Sprintf("marshal cluster configuration fingerprint: %v", err))
	}
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func clusterSecretFingerprint(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	value := strings.Join([]string{
		cfg.JWT.Secret,
		cfg.Totp.EncryptionKey,
		cfg.Database.Password,
		cfg.Redis.Password,
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
