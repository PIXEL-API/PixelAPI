import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post
  }
}))

import {
  drainInstance,
  getInstance,
  getInstances,
  getOperations,
  getSummary,
  getTasks,
  refreshCache,
  resumeInstance
} from '@/api/admin/cluster'

describe('admin cluster API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('uses the dedicated cluster monitoring endpoints', async () => {
    get
      .mockResolvedValueOnce({ data: { enabled: true } })
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: [] })

    await getSummary()
    await getInstances()
    await getTasks()
    await getOperations(25)

    expect(get).toHaveBeenNthCalledWith(1, '/admin/ops/cluster/summary', {
      signal: undefined
    })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/ops/cluster/instances', {
      signal: undefined
    })
    expect(get).toHaveBeenNthCalledWith(3, '/admin/ops/cluster/tasks', {
      signal: undefined
    })
    expect(get).toHaveBeenNthCalledWith(4, '/admin/ops/cluster/operations', {
      params: { limit: 25 },
      signal: undefined
    })
  })

  it('encodes node IDs used in path parameters', async () => {
    get.mockResolvedValue({ data: { node_id: 'app/east 1' } })

    await getInstance('app/east 1')

    expect(get).toHaveBeenCalledWith(
      '/admin/ops/cluster/instances/app%2Feast%201',
      { signal: undefined }
    )
  })

  it('sends idempotency keys for drain and resume operations', async () => {
    post.mockResolvedValue({ data: { operation_ids: ['operation-1'], status: 'pending' } })

    await drainInstance('app-01', { reason: 'planned maintenance' }, 'uuid-drain')
    await resumeInstance('app-01', { reason: 'maintenance complete' }, 'uuid-resume')

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/admin/ops/cluster/instances/app-01/drain',
      { reason: 'planned maintenance' },
      { headers: { 'Idempotency-Key': 'uuid-drain' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/admin/ops/cluster/instances/app-01/resume',
      { reason: 'maintenance complete' },
      { headers: { 'Idempotency-Key': 'uuid-resume' } }
    )
  })

  it('sends one safe cache scope, audit reason, and idempotency key', async () => {
    post.mockResolvedValue({ data: { operation_ids: ['operation-2'], status: 'pending' } })
    const request = {
      scope: 'channel_routing' as const,
      reason: 'routing configuration changed'
    }

    await refreshCache(request, 'uuid-cache')

    expect(post).toHaveBeenCalledWith(
      '/admin/ops/cluster/cache-refresh',
      request,
      { headers: { 'Idempotency-Key': 'uuid-cache' } }
    )
  })
})
