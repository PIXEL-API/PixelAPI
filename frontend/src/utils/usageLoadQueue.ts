import type { Account } from '@/types'

export type UsageRequestSource = 'local' | 'passive' | 'active'

// One scheduler is shared by every account table to cap aggregate request fan-out.
const MAX_CONCURRENT_USAGE_REQUESTS = 5

interface UsageRequestTask<T> {
  key: string
  started: boolean
  settled: boolean
  subscribers: number
  controller: AbortController
  run: (signal: AbortSignal) => Promise<T>
  promise: Promise<T>
  resolve: (value: T | PromiseLike<T>) => void
  reject: (reason?: unknown) => void
}

const pendingQueue: UsageRequestTask<unknown>[] = []
const inFlightRequests = new Map<string, UsageRequestTask<unknown>>()
let activeRequestCount = 0

function createAbortError(): Error {
  const error = new Error('Usage request aborted')
  error.name = 'AbortError'
  return error
}

function settleTask<T>(task: UsageRequestTask<T>, result: { value: T } | { error: unknown }): void {
  if (task.settled) return
  task.settled = true
  if (inFlightRequests.get(task.key) === task) {
    inFlightRequests.delete(task.key)
  }
  if ('error' in result) {
    task.reject(result.error)
    return
  }
  task.resolve(result.value)
}

function releaseSubscriber(task: UsageRequestTask<unknown>): void {
  task.subscribers = Math.max(0, task.subscribers - 1)
  if (task.subscribers > 0 || task.settled) return

  if (inFlightRequests.get(task.key) === task) {
    inFlightRequests.delete(task.key)
  }
  task.controller.abort()
  if (!task.started) {
    settleTask(task, { error: createAbortError() })
    drainQueue()
  }
}

function subscribeToTask<T>(task: UsageRequestTask<T>, signal?: AbortSignal): Promise<T> {
  if (signal?.aborted) {
    return Promise.reject(createAbortError())
  }

  task.subscribers += 1
  return new Promise<T>((resolve, reject) => {
    let finished = false

    const finish = (): boolean => {
      if (finished) return false
      finished = true
      signal?.removeEventListener('abort', handleAbort)
      releaseSubscriber(task as UsageRequestTask<unknown>)
      return true
    }
    const handleAbort = () => {
      if (!finish()) return
      reject(createAbortError())
    }

    signal?.addEventListener('abort', handleAbort, { once: true })
    task.promise.then(
      (value) => {
        if (!finish()) return
        resolve(value)
      },
      (error) => {
        if (!finish()) return
        reject(error)
      }
    )
  })
}

function drainQueue(): void {
  while (
    activeRequestCount < MAX_CONCURRENT_USAGE_REQUESTS &&
    pendingQueue.length > 0
  ) {
    const task = pendingQueue.shift()
    if (!task) return
    if (task.settled || task.subscribers === 0) {
      continue
    }

    task.started = true
    activeRequestCount += 1
    void Promise.resolve()
      .then(() => {
        if (task.settled || task.subscribers === 0 || task.controller.signal.aborted) {
          throw createAbortError()
        }
        return task.run(task.controller.signal)
      })
      .then(
        (value) => settleTask(task, { value }),
        (error) => settleTask(task, { error })
      )
      .finally(() => {
        activeRequestCount -= 1
        drainQueue()
      })
  }
}

export function enqueueUsageRequest<T>(
  account: Account,
  fn: (signal: AbortSignal) => Promise<T>,
  options: {
    scope: string
    source: UsageRequestSource
    signal?: AbortSignal
  }
): Promise<T> {
  if (options.signal?.aborted) {
    return Promise.reject(createAbortError())
  }

  const key = `${options.scope}:${account.id}:${options.source}`
  const existing = inFlightRequests.get(key) as UsageRequestTask<T> | undefined
  if (existing) return subscribeToTask(existing, options.signal)

  let resolveTask!: (value: T | PromiseLike<T>) => void
  let rejectTask!: (reason?: unknown) => void
  const promise = new Promise<T>((resolve, reject) => {
    resolveTask = resolve
    rejectTask = reject
  })
  const task: UsageRequestTask<T> = {
    key,
    started: false,
    settled: false,
    subscribers: 0,
    controller: new AbortController(),
    run: fn,
    promise,
    resolve: resolveTask,
    reject: rejectTask
  }

  inFlightRequests.set(key, task as UsageRequestTask<unknown>)
  pendingQueue.push(task as UsageRequestTask<unknown>)
  const request = subscribeToTask(task, options.signal)
  drainQueue()
  return request
}
