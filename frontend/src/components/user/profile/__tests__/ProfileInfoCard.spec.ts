import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileInfoCard from '@/components/user/profile/ProfileInfoCard.vue'
import type { User } from '@/types'

const {
  updateProfileMock,
  showSuccessMock,
  showErrorMock,
  authStoreState
} = vi.hoisted(() => ({
  updateProfileMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  authStoreState: {
    user: null as User | null
  }
}))

vi.mock('@/api', () => ({
  userAPI: {
    updateProfile: updateProfileMock
  }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStoreState
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess: showSuccessMock,
    showError: showErrorMock
  })
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (error: unknown) => (error as Error).message || 'request failed'
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => {
        if (key === 'profile.administrator') return 'Administrator'
        if (key === 'profile.user') return 'User'
        if (key === 'profile.avatar.title') return 'Profile avatar'
        if (key === 'profile.avatar.description') return 'Set avatar by image URL or upload'
        if (key === 'profile.avatar.inputLabel') return 'Avatar URL or data URL'
        if (key === 'profile.avatar.inputPlaceholder') return 'https://cdn.example.com/avatar.png'
        if (key === 'profile.avatar.uploadAction') return 'Upload image'
        if (key === 'profile.avatar.uploadHint') return 'Images must be 100KB or smaller and will be compressed to 20KB'
        if (key === 'profile.avatar.saveSuccess') return 'Avatar updated'
        if (key === 'profile.avatar.deleteSuccess') return 'Avatar removed'
        if (key === 'profile.avatar.invalidType') return 'Please choose an image file'
        if (key === 'profile.avatar.fileTooLarge') return 'Avatar image must be 100KB or smaller'
        if (key === 'profile.avatar.gifTooLarge') return 'GIF avatars must already be 20KB or smaller'
        if (key === 'profile.avatar.compressTooLarge') return 'Unable to compress this image below 20KB'
        if (key === 'profile.avatar.compressFailed') return 'Failed to compress the selected image'
        if (key === 'profile.avatar.readFailed') return 'Failed to read the selected image'
        if (key === 'profile.avatar.invalidValue') return 'Enter a valid avatar URL or image data URL'
        if (key === 'profile.avatar.emptyDeleteHint') return 'Avatar already removed'
        if (key === 'profile.authBindings.providers.email') return 'Email'
        if (key === 'profile.authBindings.providers.linuxdo') return 'LinuxDo'
        if (key === 'profile.authBindings.providers.wechat') return 'WeChat'
        if (key === 'profile.authBindings.providers.oidc') return params?.providerName || 'OIDC'
        if (key === 'profile.authBindings.source.avatar') return `Avatar synced from ${params?.providerName || 'provider'}`
        if (key === 'profile.authBindings.source.username') return `Username synced from ${params?.providerName || 'provider'}`
        if (key === 'common.save') return 'Save'
        if (key === 'common.delete') return 'Delete'
        return key
      }
    })
  }
})

function createUser(overrides: Partial<User> = {}): User {
  return {
    id: 5,
    username: 'alice',
    email: 'alice@example.com',
    avatar_url: null,
    role: 'user',
    balance: 10,
    concurrency: 2,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: true,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-20T00:00:00Z',
    ...overrides
  }
}

async function flushAsyncWork(): Promise<void> {
  await Promise.resolve()
  await Promise.resolve()
}

const originalFileReader = globalThis.FileReader
const originalImage = globalThis.Image
const originalCreateElement = document.createElement.bind(document)

function installAvatarCompressionMocks() {
  class MockFileReader {
    result: string | ArrayBuffer | null = null
    onload: ((this: FileReader, ev: ProgressEvent<FileReader>) => any) | null = null
    onerror: ((this: FileReader, ev: ProgressEvent<FileReader>) => any) | null = null
    error: DOMException | null = null

    readAsDataURL(blob: Blob) {
      if (blob.type === 'image/webp') {
        this.result = 'data:image/webp;base64,' + Buffer.from('compressed-avatar').toString('base64')
      } else {
        this.result = 'data:image/png;base64,' + Buffer.from('original-avatar').toString('base64')
      }
      this.onload?.call(this as unknown as FileReader, new ProgressEvent('load'))
    }
  }

  class MockImage {
    naturalWidth = 1200
    naturalHeight = 1200
    onload: (() => void) | null = null
    onerror: (() => void) | null = null

    set src(_value: string) {
      this.onload?.()
    }
  }

  globalThis.FileReader = MockFileReader as unknown as typeof FileReader
  globalThis.Image = MockImage as unknown as typeof Image
  vi.spyOn(document, 'createElement').mockImplementation(((tagName: string, options?: ElementCreationOptions) => {
    if (tagName === 'canvas') {
      return {
        width: 0,
        height: 0,
        getContext: () => ({
          clearRect: vi.fn(),
          drawImage: vi.fn(),
        }),
        toBlob: (callback: BlobCallback) => {
          callback(new Blob([new Uint8Array(8 * 1024)], { type: 'image/webp' }))
        },
      } as unknown as HTMLCanvasElement
    }
    return originalCreateElement(tagName, options)
  }) as typeof document.createElement)
}

describe('ProfileInfoCard', () => {
  beforeEach(() => {
    updateProfileMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    authStoreState.user = null
  })

  afterEach(() => {
    globalThis.FileReader = originalFileReader
    globalThis.Image = originalImage
    vi.restoreAllMocks()
  })

  it('saves a remote avatar URL and updates the auth store', async () => {
    const updatedUser = createUser({ avatar_url: 'https://cdn.example.com/new.png' })
    updateProfileMock.mockResolvedValue(updatedUser)
    authStoreState.user = createUser()

    const wrapper = mount(ProfileInfoCard, {
      props: {
        user: authStoreState.user
      },
      global: {
        stubs: {
          Icon: true,
          ProfileIdentityBindingsSection: true
        }
      }
    })

    await wrapper.get('[data-testid="profile-avatar-input"]').setValue('https://cdn.example.com/new.png')
    await wrapper.get('[data-testid="profile-avatar-save"]').trigger('click')

    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: 'https://cdn.example.com/new.png' })
    expect(authStoreState.user?.avatar_url).toBe('https://cdn.example.com/new.png')
    expect(showSuccessMock).toHaveBeenCalledWith('Avatar updated')
  })

  it('rejects an oversized data URL before sending the request', async () => {
    authStoreState.user = createUser()
    const oversized = `data:image/png;base64,${Buffer.from(new Uint8Array(102401)).toString('base64')}`

    const wrapper = mount(ProfileInfoCard, {
      props: {
        user: authStoreState.user
      },
      global: {
        stubs: {
          Icon: true,
          ProfileIdentityBindingsSection: true
        }
      }
    })

    await wrapper.get('[data-testid="profile-avatar-input"]').setValue(oversized)
    await wrapper.get('[data-testid="profile-avatar-save"]').trigger('click')

    expect(updateProfileMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('Avatar image must be 100KB or smaller')
  })

  it('compresses uploaded images under 100KB before saving', async () => {
    installAvatarCompressionMocks()
    const updatedUser = createUser({ avatar_url: 'data:image/webp;base64,Y29tcHJlc3NlZC1hdmF0YXI=' })
    updateProfileMock.mockResolvedValue(updatedUser)
    authStoreState.user = createUser()

    const wrapper = mount(ProfileInfoCard, {
      props: {
        user: authStoreState.user
      },
      global: {
        stubs: {
          Icon: true,
          ProfileIdentityBindingsSection: true
        }
      }
    })

    const fileInput = wrapper.get('[data-testid="profile-avatar-file-input"]')
    Object.defineProperty(fileInput.element, 'files', {
      value: [new File([new Uint8Array(80 * 1024)], 'avatar.png', { type: 'image/png' })],
      configurable: true
    })

    await fileInput.trigger('change')
    await flushAsyncWork()
    await wrapper.get('[data-testid="profile-avatar-save"]').trigger('click')

    expect(updateProfileMock).toHaveBeenCalledWith({
      avatar_url: 'data:image/webp;base64,Y29tcHJlc3NlZC1hdmF0YXI='
    })
  })

  it('deletes the current avatar', async () => {
    const updatedUser = createUser({ avatar_url: null })
    updateProfileMock.mockResolvedValue(updatedUser)
    authStoreState.user = createUser({ avatar_url: 'https://cdn.example.com/old.png' })

    const wrapper = mount(ProfileInfoCard, {
      props: {
        user: authStoreState.user
      },
      global: {
        stubs: {
          Icon: true,
          ProfileIdentityBindingsSection: true
        }
      }
    })

    await wrapper.get('[data-testid="profile-avatar-delete"]').trigger('click')

    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: '' })
    expect(authStoreState.user?.avatar_url).toBeNull()
    expect(showSuccessMock).toHaveBeenCalledWith('Avatar removed')
  })

  it('renders third-party source hints from profile_sources', () => {
    authStoreState.user = createUser({
      avatar_url: 'https://cdn.example.com/linuxdo.png',
      profile_sources: {
        avatar: { provider: 'linuxdo', source: 'linuxdo' },
        username: { provider: 'linuxdo', source: 'linuxdo' }
      }
    })

    const wrapper = mount(ProfileInfoCard, {
      props: {
        user: authStoreState.user
      },
      global: {
        stubs: {
          Icon: true,
          ProfileIdentityBindingsSection: true
        }
      }
    })

    expect(wrapper.text()).toContain('Avatar synced from LinuxDo')
    expect(wrapper.text()).toContain('Username synced from LinuxDo')
  })
})
