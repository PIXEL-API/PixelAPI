import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const apiMocks = vi.hoisted(() => ({
  listIdeas: vi.fn(),
  listMyIdeas: vi.fn(),
  listIdeaTags: vi.fn(),
  getIdea: vi.fn(),
  createIdea: vi.fn(),
  updateIdea: vi.fn(),
  publishIdea: vi.fn(),
  deleteIdea: vi.fn(),
}))

const appMocks = vi.hoisted(() => ({
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
}))

const routerMocks = vi.hoisted(() => ({
  push: vi.fn(),
  routeParams: {} as Record<string, string | undefined>,
}))

vi.mock('@/api/ideas', () => ({
  ...apiMocks,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appMocks,
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerMocks.push }),
  useRoute: () => ({ params: routerMocks.routeParams }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en-US' },
      t: (key: string, params?: Record<string, unknown> | string) => {
        if (key === 'ideas.mine.deleteConfirm' && typeof params === 'object') {
          return `delete:${String(params.title)}`
        }
        return key
      },
    }),
  }
})

import IdeasListView from '../IdeasListView.vue'
import MyIdeasView from '../MyIdeasView.vue'
import IdeaEditorView from '../IdeaEditorView.vue'

const EmptyStateStub = defineComponent({
  name: 'EmptyState',
  props: {
    title: { type: String, default: '' },
    description: { type: String, default: '' },
  },
  template: '<div data-testid="empty-state">{{ title }}|{{ description }}<slot name="action" /></div>',
})

const PaginationStub = defineComponent({
  name: 'Pagination',
  props: {
    total: { type: Number, required: true },
    page: { type: Number, required: true },
    pageSize: { type: Number, required: true },
  },
  emits: ['update:page', 'update:page-size'],
  template: `
    <div data-testid="pagination" :data-total="total" :data-page="page">
      <button data-testid="page-two" @click="$emit('update:page', 2)">page 2</button>
    </div>
  `,
})

const RouterLinkStub = defineComponent({
  name: 'RouterLink',
  props: {
    to: { type: [String, Object], required: true },
  },
  template: '<a><slot /></a>',
})

const globalMountOptions = {
  stubs: {
    IdeasHeader: true,
    RouterLink: RouterLinkStub,
    Skeleton: true,
    EmptyState: EmptyStateStub,
    Pagination: PaginationStub,
  },
}

function ideaPost(id: number, status = 'published', title = `Post ${id}`) {
  return {
    id,
    author_user_id: 7,
    author_name: 'Author',
    current_revision_id: id * 10,
    status,
    like_count: 0,
    favorite_count: 0,
    view_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    revision: {
      id: id * 10,
      post_id: id,
      revision_no: 1,
      title,
      summary: 'Summary',
      body: 'Body',
      body_hash: 'hash',
      moderation_status: status,
      created_by: 7,
      created_at: '2026-08-01T00:00:00Z',
    },
    tags: [],
    can_edit: true,
    can_reward: false,
    liked: false,
    favorited: false,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function findButton(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  expect(button, `button containing ${text}`).toBeDefined()
  return button!
}

describe('Ideas views regressions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    routerMocks.routeParams.id = undefined
    apiMocks.listIdeaTags.mockResolvedValue([])
    apiMocks.publishIdea.mockResolvedValue(ideaPost(1, 'pending_review'))
    apiMocks.deleteIdea.mockResolvedValue({ deleted: true })
    apiMocks.createIdea.mockResolvedValue(ideaPost(1, 'draft'))
    apiMocks.updateIdea.mockResolvedValue(ideaPost(1, 'draft'))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  it('requests a server-side status filter and resets the page to one', async () => {
    apiMocks.listMyIdeas.mockImplementation(async (params: { page?: number; status?: string }) => {
      if (params.status === 'draft') return { items: [ideaPost(22, 'draft')], total: 1 }
      if (params.page === 2) return { items: [ideaPost(21)], total: 21 }
      return { items: [ideaPost(1)], total: 21 }
    })

    const wrapper = mount(MyIdeasView, { global: globalMountOptions })
    await flushPromises()

    await wrapper.get('[data-testid="page-two"]').trigger('click')
    await flushPromises()
    expect(apiMocks.listMyIdeas).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))

    await findButton(wrapper, 'ideas.status.draft').trigger('click')
    await flushPromises()

    expect(apiMocks.listMyIdeas).toHaveBeenLastCalledWith({
      page: 1,
      page_size: 20,
      status: 'draft',
    })
    expect(wrapper.text()).toContain('Post 22')
  })

  it('returns to the last valid page after deleting the final item on a page', async () => {
    let deleted = false
    apiMocks.deleteIdea.mockImplementation(async () => {
      deleted = true
      return { deleted: true }
    })
    apiMocks.listMyIdeas.mockImplementation(async (params: { page?: number }) => {
      if (params.page === 2) {
        return deleted
          ? { items: [], total: 20 }
          : { items: [ideaPost(21, 'draft', 'Last post')], total: 21 }
      }
      return { items: [ideaPost(1)], total: deleted ? 20 : 21 }
    })

    const wrapper = mount(MyIdeasView, { global: globalMountOptions })
    await flushPromises()
    await wrapper.get('[data-testid="page-two"]').trigger('click')
    await flushPromises()

    await findButton(wrapper, 'ideas.common.delete').trigger('click')
    await flushPromises()

    const calls = apiMocks.listMyIdeas.mock.calls.map(([params]) => params.page)
    expect(calls).toEqual([1, 2, 2, 1])
    expect(wrapper.text()).toContain('Post 1')
    expect(wrapper.find('[data-testid="empty-state"]').exists()).toBe(false)
  })

  it('ignores an older list response that arrives after a newer filter response', async () => {
    const older = deferred<{ items: ReturnType<typeof ideaPost>[]; total: number }>()
    const newer = deferred<{ items: ReturnType<typeof ideaPost>[]; total: number }>()
    apiMocks.listIdeas
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(newer.promise)

    const wrapper = mount(IdeasListView, { global: globalMountOptions })
    expect(apiMocks.listIdeas).toHaveBeenCalledTimes(1)

    await findButton(wrapper, 'ideas.list.hot').trigger('click')
    expect(apiMocks.listIdeas).toHaveBeenCalledTimes(2)

    newer.resolve({ items: [ideaPost(2, 'published', 'Newer result')], total: 1 })
    await flushPromises()
    expect(wrapper.text()).toContain('Newer result')

    older.resolve({ items: [ideaPost(1, 'published', 'Stale result')], total: 99 })
    await flushPromises()
    expect(wrapper.text()).toContain('Newer result')
    expect(wrapper.text()).not.toContain('Stale result')
    expect(wrapper.find('[data-testid="pagination"]').exists()).toBe(false)
  })

  it('offers one explicit revision-review action for a published post', async () => {
    routerMocks.routeParams.id = '42'
    apiMocks.getIdea.mockResolvedValue(ideaPost(42, 'published'))
    apiMocks.updateIdea.mockResolvedValue(ideaPost(42, 'pending_revision'))

    const wrapper = mount(IdeaEditorView, { global: globalMountOptions })
    await flushPromises()

    expect(wrapper.text()).toContain('ideas.editor.submitRevision')
    expect(wrapper.text()).not.toContain('ideas.editor.saveDraft')
    expect(wrapper.text()).not.toContain('ideas.editor.submitReview')

    await findButton(wrapper, 'ideas.editor.submitRevision').trigger('click')
    await flushPromises()

    expect(apiMocks.updateIdea).toHaveBeenCalledTimes(1)
    expect(apiMocks.publishIdea).not.toHaveBeenCalled()
  })

  it('keeps draft saving separate from review submission for a rejected post', async () => {
    routerMocks.routeParams.id = '43'
    apiMocks.getIdea.mockResolvedValue(ideaPost(43, 'rejected'))
    apiMocks.updateIdea.mockResolvedValue(ideaPost(43, 'rejected'))
    apiMocks.publishIdea.mockResolvedValue(ideaPost(43, 'pending_review'))

    const wrapper = mount(IdeaEditorView, { global: globalMountOptions })
    await flushPromises()

    await findButton(wrapper, 'ideas.editor.saveDraft').trigger('click')
    await flushPromises()
    expect(apiMocks.updateIdea).toHaveBeenCalledTimes(1)
    expect(apiMocks.publishIdea).not.toHaveBeenCalled()

    await findButton(wrapper, 'ideas.editor.submitReview').trigger('click')
    await flushPromises()
    expect(apiMocks.updateIdea).toHaveBeenCalledTimes(2)
    expect(apiMocks.publishIdea).toHaveBeenCalledTimes(1)
    expect(apiMocks.publishIdea).toHaveBeenCalledWith(43)
  })
})
