import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// Mock 导航加载状态
vi.mock('@/composables/useNavigationLoading', () => {
  const mockStart = vi.fn()
  const mockEnd = vi.fn()
  return {
    useNavigationLoadingState: () => ({
      startNavigation: mockStart,
      endNavigation: mockEnd,
      isLoading: { value: false },
    }),
    useNavigationLoading: () => ({
      startNavigation: mockStart,
      endNavigation: mockEnd,
      isLoading: { value: false },
    }),
  }
})

// Mock 路由预加载
vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

// Mock API 相关模块
vi.mock('@/api', () => ({
  authAPI: {
    getCurrentUser: vi.fn().mockResolvedValue({ data: {} }),
    logout: vi.fn(),
  },
  isTotp2FARequired: () => false,
}))

vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: vi.fn(),
}))


// 用于测试的 auth 状态
interface MockAuthState {
  isAuthenticated: boolean
  isAdmin: boolean
  isSimpleMode: boolean
}

/**
 * 将 router/index.ts 中 beforeEach 守卫的核心逻辑提取为可测试的函数
 */
function simulateGuard(
  toPath: string,
  toMeta: Record<string, any>,
  authState: MockAuthState
): string | null {
  const requiresAuth = toMeta.requiresAuth !== false
  const requiresAdmin = toMeta.requiresAdmin === true

  // 不需要认证的路由
  if (!requiresAuth) {
    if (
      authState.isAuthenticated &&
      (toPath === '/login' || toPath === '/register')
    ) {
      return authState.isAdmin ? '/admin/dashboard' : '/dashboard'
    }
    return null // 允许通过
  }

  // 需要认证但未登录
  if (!authState.isAuthenticated) {
    return '/login'
  }

  // 需要管理员但不是管理员
  if (requiresAdmin && !authState.isAdmin) {
    return '/dashboard'
  }

  // 简易模式限制
  if (authState.isSimpleMode) {
    const restrictedPaths = [
      '/admin/groups',
      '/admin/subscriptions',
      '/admin/redeem',
      '/subscriptions',
      '/redeem',
    ]
    if (restrictedPaths.some((path) => toPath.startsWith(path))) {
      return authState.isAdmin ? '/admin/dashboard' : '/dashboard'
    }
  }

  return null // 允许通过
}

describe('路由守卫逻辑', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  // --- 未认证用户 ---

  describe('未认证用户', () => {
    const authState: MockAuthState = {
      isAuthenticated: false,
      isAdmin: false,
      isSimpleMode: false,
    }

    it('访问需要认证的页面重定向到 /login', () => {
      const redirect = simulateGuard('/dashboard', {}, authState)
      expect(redirect).toBe('/login')
    })

    it('访问管理页面重定向到 /login', () => {
      const redirect = simulateGuard('/admin/dashboard', { requiresAdmin: true }, authState)
      expect(redirect).toBe('/login')
    })

    it('访问公开页面允许通过', () => {
      const redirect = simulateGuard('/login', { requiresAuth: false }, authState)
      expect(redirect).toBeNull()
    })

    it('访问 /home 公开页面允许通过', () => {
      const redirect = simulateGuard('/home', { requiresAuth: false }, authState)
      expect(redirect).toBeNull()
    })
  })

  // --- 已认证普通用户 ---

  describe('已认证普通用户', () => {
    const authState: MockAuthState = {
      isAuthenticated: true,
      isAdmin: false,
      isSimpleMode: false,
    }

    it('访问 /login 重定向到 /dashboard', () => {
      const redirect = simulateGuard('/login', { requiresAuth: false }, authState)
      expect(redirect).toBe('/dashboard')
    })

    it('访问 /register 重定向到 /dashboard', () => {
      const redirect = simulateGuard('/register', { requiresAuth: false }, authState)
      expect(redirect).toBe('/dashboard')
    })

    it('访问 /dashboard 允许通过', () => {
      const redirect = simulateGuard('/dashboard', {}, authState)
      expect(redirect).toBeNull()
    })

    it('访问管理页面被拒绝，重定向到 /dashboard', () => {
      const redirect = simulateGuard('/admin/dashboard', { requiresAdmin: true }, authState)
      expect(redirect).toBe('/dashboard')
    })

    it('访问 /admin/users 被拒绝', () => {
      const redirect = simulateGuard('/admin/users', { requiresAdmin: true }, authState)
      expect(redirect).toBe('/dashboard')
    })
  })

  // --- 已认证管理员 ---

  describe('已认证管理员', () => {
    const authState: MockAuthState = {
      isAuthenticated: true,
      isAdmin: true,
      isSimpleMode: false,
    }

    it('访问 /login 重定向到 /admin/dashboard', () => {
      const redirect = simulateGuard('/login', { requiresAuth: false }, authState)
      expect(redirect).toBe('/admin/dashboard')
    })

    it('访问管理页面允许通过', () => {
      const redirect = simulateGuard('/admin/dashboard', { requiresAdmin: true }, authState)
      expect(redirect).toBeNull()
    })

    it('访问用户页面允许通过', () => {
      const redirect = simulateGuard('/dashboard', {}, authState)
      expect(redirect).toBeNull()
    })
  })

  // --- 简易模式 ---

  describe('简易模式受限路由', () => {
    it('普通用户简易模式访问 /subscriptions 重定向到 /dashboard', () => {
      const authState: MockAuthState = {
        isAuthenticated: true,
        isAdmin: false,
        isSimpleMode: true,
      }
      const redirect = simulateGuard('/subscriptions', {}, authState)
      expect(redirect).toBe('/dashboard')
    })

    it('普通用户简易模式访问 /redeem 重定向到 /dashboard', () => {
      const authState: MockAuthState = {
        isAuthenticated: true,
        isAdmin: false,
        isSimpleMode: true,
      }
      const redirect = simulateGuard('/redeem', {}, authState)
      expect(redirect).toBe('/dashboard')
    })

    it('管理员简易模式访问 /admin/groups 重定向到 /admin/dashboard', () => {
      const authState: MockAuthState = {
        isAuthenticated: true,
        isAdmin: true,
        isSimpleMode: true,
      }
      const redirect = simulateGuard('/admin/groups', { requiresAdmin: true }, authState)
      expect(redirect).toBe('/admin/dashboard')
    })

    it('管理员简易模式访问 /admin/subscriptions 重定向', () => {
      const authState: MockAuthState = {
        isAuthenticated: true,
        isAdmin: true,
        isSimpleMode: true,
      }
      const redirect = simulateGuard(
        '/admin/subscriptions',
        { requiresAdmin: true },
        authState
      )
      expect(redirect).toBe('/admin/dashboard')
    })

    it('简易模式下非受限页面正常访问', () => {
      const authState: MockAuthState = {
        isAuthenticated: true,
        isAdmin: false,
        isSimpleMode: true,
      }
      const redirect = simulateGuard('/dashboard', {}, authState)
      expect(redirect).toBeNull()
    })

    it('简易模式下 /keys 正常访问', () => {
      const authState: MockAuthState = {
        isAuthenticated: true,
        isAdmin: false,
        isSimpleMode: true,
      }
      const redirect = simulateGuard('/keys', {}, authState)
      expect(redirect).toBeNull()
    })
  })
})
