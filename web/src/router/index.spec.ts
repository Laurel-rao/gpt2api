import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const authMocks = vi.hoisted(() => ({ getMe: vi.fn() }))

vi.mock('@/api/auth', () => ({ getMe: authMocks.getMe }))
vi.mock('@/views/personal/VideoWorkflows.vue', () => ({ default: { template: '<div>video workflow</div>' } }))
vi.mock('@/views/Error403.vue', () => ({ default: { template: '<div>forbidden</div>' } }))

import router from './index'
import { useUserStore } from '@/stores/user'

const user = {
  id: 1001,
  email: 'user@example.com',
  nickname: '普通用户',
  role: 'user',
  status: 'active',
  group_id: 1,
  credit_balance: 0,
  credit_frozen: 0,
}

describe('video workflow admin route guard', () => {
  beforeEach(async () => {
    const values = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => values.get(key) || null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
      clear: () => values.clear(),
    })
    setActivePinia(createPinia())
    authMocks.getMe.mockReset()
    await router.replace('/')
  })

  it('rejects a non-admin even when stale storage still contains the old permission', async () => {
    const store = useUserStore()
    store.accessToken = 'stale-access-token'
    store.user = user
    store.role = 'user'
    store.permissions = ['self:video_workflow']
    authMocks.getMe.mockResolvedValue({ user, role: 'user', permissions: ['self:video_workflow'] })

    await router.push('/personal/video-workflows')

    expect(authMocks.getMe).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/403')
  })

  it('allows a freshly verified administrator', async () => {
    const store = useUserStore()
    store.accessToken = 'admin-access-token'
    store.user = { ...user, role: 'admin' }
    store.role = 'admin'
    store.permissions = ['self:video_workflow']
    authMocks.getMe.mockResolvedValue({
      user: { ...user, role: 'admin' },
      role: 'admin',
      permissions: ['self:video_workflow'],
    })

    await router.push('/personal/video-workflows')

    expect(authMocks.getMe).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/personal/video-workflows')
  })
})
