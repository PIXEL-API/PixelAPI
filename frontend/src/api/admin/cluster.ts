import { apiClient } from '../client'

export type ClusterDesiredState = 'active' | 'draining'
export type ClusterObservedState = 'starting' | 'ready' | 'draining' | 'unhealthy'
export type ClusterDerivedStatus =
  | ClusterObservedState
  | 'stale'
  | 'offline'
  | string
export type ClusterOperationKind = 'drain' | 'resume' | 'cache_refresh' | string
export type ClusterOperationStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | string
export type ClusterCacheScope =
  | 'channel_routing'
  | 'runtime_settings'
  | 'policy_metadata'
  | 'all_safe'

export interface ClusterSummary {
  enabled: boolean
  deployment_id: string
  expected_nodes: number
  counts: {
    ready: number
    draining: number
    unhealthy: number
    stale: number
    offline: number
  }
  n_minus_one_ready: boolean
  version_consistent: boolean
  versions: string[]
  active_connections: {
    http: number
    sse: number
    websocket: number
  }
  pools: {
    database_open: number
    database_max: number
    redis_total: number
    redis_max: number
  }
  cache_lagging_nodes: number
  refreshed_at: string
}

export interface ClusterInstance {
  node_id: string
  boot_id: string
  hostname: string
  version: string
  commit: string
  build_date: string
  desired_state: ClusterDesiredState
  observed_state: ClusterObservedState
  status: ClusterDerivedStatus
  started_at: string
  last_seen_at: string
  ready: boolean
  db_ok: boolean
  redis_ok: boolean
  cpu_usage_percent: number
  memory_used_bytes: number
  memory_limit_bytes: number
  goroutine_count: number
  fd_open: number
  fd_limit: number
  active_http: number
  active_sse: number
  active_ws: number
  db_conn_active: number
  db_conn_idle: number
  db_conn_waiting: number
  db_conn_max_open: number
  redis_conn_total: number
  redis_conn_idle: number
  redis_pool_size: number
  cache_versions: Record<string, number>
  readiness_message: string
}

export interface ClusterTaskLease {
  task_name: string
  owner_node_id: string
  owner_boot_id: string
  fencing_token: number
  lease_expires_at: string | null
  last_run_at: string | null
  last_success_at: string | null
  last_error: string
  last_duration_ms: number | null
}

export interface ClusterOperation {
  id: string
  batch_id: string
  kind: ClusterOperationKind
  target_node_id: string | null
  status: ClusterOperationStatus
  reason: string
  requested_by: string
  requested_at: string
  started_at: string | null
  completed_at: string | null
  error: string
}

export interface ClusterOperationResponse {
  operation_ids: string[]
  status: 'pending'
}

export interface ClusterRequestOptions {
  signal?: AbortSignal
}

export interface ClusterNodeOperationRequest {
  reason: string
}

export interface ClusterCacheRefreshRequest {
  scope: ClusterCacheScope
  reason: string
}

export async function getSummary(
  options: ClusterRequestOptions = {}
): Promise<ClusterSummary> {
  const { data } = await apiClient.get<ClusterSummary>('/admin/ops/cluster/summary', {
    signal: options.signal
  })
  return data
}

export async function getInstances(
  options: ClusterRequestOptions = {}
): Promise<ClusterInstance[]> {
  const { data } = await apiClient.get<ClusterInstance[]>('/admin/ops/cluster/instances', {
    signal: options.signal
  })
  return data
}

export async function getInstance(
  nodeId: string,
  options: ClusterRequestOptions = {}
): Promise<ClusterInstance> {
  const { data } = await apiClient.get<ClusterInstance>(
    `/admin/ops/cluster/instances/${encodeURIComponent(nodeId)}`,
    { signal: options.signal }
  )
  return data
}

export async function getTasks(
  options: ClusterRequestOptions = {}
): Promise<ClusterTaskLease[]> {
  const { data } = await apiClient.get<ClusterTaskLease[]>('/admin/ops/cluster/tasks', {
    signal: options.signal
  })
  return data
}

export async function getOperations(
  limit = 50,
  options: ClusterRequestOptions = {}
): Promise<ClusterOperation[]> {
  const { data } = await apiClient.get<ClusterOperation[]>(
    '/admin/ops/cluster/operations',
    {
      params: { limit },
      signal: options.signal
    }
  )
  return data
}

export async function drainInstance(
  nodeId: string,
  request: ClusterNodeOperationRequest,
  idempotencyKey: string
): Promise<ClusterOperationResponse> {
  const { data } = await apiClient.post<ClusterOperationResponse>(
    `/admin/ops/cluster/instances/${encodeURIComponent(nodeId)}/drain`,
    request,
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )
  return data
}

export async function resumeInstance(
  nodeId: string,
  request: ClusterNodeOperationRequest,
  idempotencyKey: string
): Promise<ClusterOperationResponse> {
  const { data } = await apiClient.post<ClusterOperationResponse>(
    `/admin/ops/cluster/instances/${encodeURIComponent(nodeId)}/resume`,
    request,
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )
  return data
}

export async function refreshCache(
  request: ClusterCacheRefreshRequest,
  idempotencyKey: string
): Promise<ClusterOperationResponse> {
  const { data } = await apiClient.post<ClusterOperationResponse>(
    '/admin/ops/cluster/cache-refresh',
    request,
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )
  return data
}

export const clusterAPI = {
  getSummary,
  getInstances,
  getInstance,
  getTasks,
  getOperations,
  drainInstance,
  resumeInstance,
  refreshCache
}

export default clusterAPI
