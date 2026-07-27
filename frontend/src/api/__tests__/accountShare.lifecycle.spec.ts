import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, patch, deleteRequest } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  deleteRequest: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    patch,
    delete: deleteRequest
  }
}))

import {
  activateRoom,
  beginListingEdit,
  createJoinIntent,
  createRoomDeleteIntent,
  deleteRoom,
  drainRoom,
  exchangeAnthropicCode,
  exchangeOpenAICode,
  getRoomManagementState,
  getRoomOperation,
  joinListing,
  listMembershipHistory,
  releaseListingEdit,
  submitReview,
  suspendRoom,
  updateListing
} from '@/api/accountShare'

describe('account share room lifecycle API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    patch.mockReset()
    deleteRequest.mockReset()
  })

  it('reads management state and operation progress with cancellation support', async () => {
    const controller = new AbortController()
    get
      .mockResolvedValueOnce({ data: { listing_id: 42 } })
      .mockResolvedValueOnce({ data: { id: 'operation/42' } })

    await getRoomManagementState(42, { signal: controller.signal })
    await getRoomOperation('operation/42', { signal: controller.signal })

    expect(get).toHaveBeenNthCalledWith(
      1,
      '/account-share/listings/42/management-state',
      { signal: controller.signal }
    )
    expect(get).toHaveBeenNthCalledWith(
      2,
      '/account-share/room-operations/operation%2F42',
      { signal: controller.signal }
    )
  })

  it('reads immutable membership history with pagination and cancellation support', async () => {
    const controller = new AbortController()
    get.mockResolvedValueOnce({
      data: {
        items: [],
        total: 0,
        page: 2,
        page_size: 10,
        pages: 1
      }
    })

    await listMembershipHistory(2, 10, { signal: controller.signal })

    expect(get).toHaveBeenCalledWith(
      '/account-share/history/memberships',
      {
        params: {
          page: 2,
          page_size: 10
        },
        signal: controller.signal
      }
    )
  })

  it('posts strict lifecycle commands with the required idempotency header', async () => {
    const payload = {
      expected_version: 7,
      confirmed: true
    }
    post.mockResolvedValue({ data: { listing_id: 42, row_version: 8 } })

    await drainRoom(42, payload, 'drain-key')
    await activateRoom(42, payload, 'activate-key')
    await suspendRoom(42, { ...payload, reason: 'admin reason' }, 'suspend-key')

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/account-share/listings/42/drain',
      payload,
      { headers: { 'Idempotency-Key': 'drain-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/account-share/listings/42/activate',
      payload,
      { headers: { 'Idempotency-Key': 'activate-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      3,
      '/account-share/listings/42/suspend',
      { ...payload, reason: 'admin reason' },
      { headers: { 'Idempotency-Key': 'suspend-key' } }
    )
  })

  it('checks delete intent before sending the token and exact room name in a DELETE body', async () => {
    post.mockResolvedValueOnce({ data: { listing_id: 42, can_delete: true } })
    deleteRequest.mockResolvedValueOnce({ data: { id: 'operation-42', status: 'pending' } })

    await createRoomDeleteIntent(42, {
      expected_version: 7
    })
    await deleteRoom(
      42,
      {
        expected_version: 7,
        room_name: '我的房间',
        token: 'signed-token',
        confirmed: true
      },
      'delete-key'
    )

    expect(post).toHaveBeenCalledWith(
      '/account-share/listings/42/delete-intent',
      { expected_version: 7 }
    )
    expect(deleteRequest).toHaveBeenCalledWith(
      '/account-share/listings/42',
      {
        data: {
          expected_version: 7,
          room_name: '我的房间',
          token: 'signed-token',
          confirmed: true
        },
        headers: { 'Idempotency-Key': 'delete-key' }
      }
    )
  })

  it('patches listing configuration with the caller supplied optimistic version', async () => {
    patch.mockResolvedValueOnce({ data: { id: 42, row_version: 8 } })

    await updateListing(
      42,
      {
        expected_version: 7,
        seat_limit: 15
      },
      'update-key'
    )

    expect(patch).toHaveBeenCalledWith(
      '/account-share/listings/42',
      {
        expected_version: 7,
        seat_limit: 15
      },
      { headers: { 'Idempotency-Key': 'update-key' } }
    )
  })

  it('sends required idempotency headers for OAuth, edit sessions, and reviews', async () => {
    post.mockResolvedValue({ data: {} })
    const openAIPayload = {
      session_id: 'openai-session',
      code: 'openai-code',
      state: 'openai-state',
      proxy_id: 8,
      concurrency: 10,
      seat_limit: 3,
      rate_multiplier: 1,
      allowed_models: ['gpt-5.5'],
      per_user_concurrency: 2,
      hourly_rate: 0,
    }
    const anthropicPayload = {
      session_id: 'anthropic-session',
      code: 'anthropic-code',
      proxy_id: 9,
      concurrency: 10,
      seat_limit: 4,
      rate_multiplier: 1,
      allowed_models: ['claude-sonnet-4-5'],
      per_user_concurrency: 2,
      hourly_rate: 0,
    }

    await exchangeOpenAICode(openAIPayload, 'oauth-openai-key')
    await exchangeAnthropicCode(anthropicPayload, 'oauth-anthropic-key')
    await beginListingEdit(42, { force: true }, 'begin-edit-key')
    await releaseListingEdit(42, 'edit-session', 'release-edit-key')
    await submitReview(81, { score: 9, comment: '稳定' }, 'review-key')

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/account-share/openai/exchange-code',
      openAIPayload,
      { headers: { 'Idempotency-Key': 'oauth-openai-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/account-share/anthropic/exchange-code',
      anthropicPayload,
      { headers: { 'Idempotency-Key': 'oauth-anthropic-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      3,
      '/account-share/listings/42/edit-session',
      { force: true },
      { headers: { 'Idempotency-Key': 'begin-edit-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      4,
      '/account-share/listings/42/edit-session/release',
      { session_id: 'edit-session' },
      { headers: { 'Idempotency-Key': 'release-edit-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      5,
      '/account-share/memberships/81/review',
      { score: 9, comment: '稳定' },
      { headers: { 'Idempotency-Key': 'review-key' } }
    )
  })

  it('creates a signed join intent and completes the join with the exact accepted snapshot', async () => {
    const intentPayload = {
      api_key_id: 9,
      idle_timeout_minutes: 30,
      accept_queue: true
    }
    const joinPayload = {
      ...intentPayload,
      intent_token: 'signed-join-intent',
      expected_version: 11,
      expected_revision_id: 17
    }
    post
      .mockResolvedValueOnce({ data: { listing_id: 42, token: 'signed-join-intent' } })
      .mockResolvedValueOnce({ data: { id: 88, status: 'queued' } })

    await createJoinIntent(42, intentPayload)
    await joinListing(42, joinPayload)

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/account-share/listings/42/join-intent',
      intentPayload
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/account-share/listings/42/join',
      joinPayload
    )
  })
})
