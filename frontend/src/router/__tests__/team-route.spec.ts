import { describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    siteName: 'Sub2API',
    backendModeEnabled: false,
    cachedPublicSettings: null,
  }),
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

describe('team routes', () => {
  it.each([
    ['Team', 'Team Center', 'team.title'],
    ['AdminTeams', 'Team Management', 'team.adminTitle'],
    ['AdminTeamMembers', 'Team Members & Admins', 'team.adminMembersTitle'],
    ['AdminTeamUsage', 'Team Spend Statistics', 'team.adminUsageTitle'],
  ])('registers translated title metadata for %s', async (name, title, titleKey) => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === name)

    expect(route?.meta.title).toBe(title)
    expect(route?.meta.titleKey).toBe(titleKey)
  })

  it('mirrors the channel management layout: /admin/teams redirects to the list and children live under it', async () => {
    const { default: router } = await import('@/router')
    const records = router.getRoutes()
    const parent = records.find((record) => record.path === '/admin/teams')
    expect(parent?.redirect).toBe('/admin/teams/list')
    for (const [name, path] of [['AdminTeams', '/admin/teams/list'], ['AdminTeamMembers', '/admin/teams/members'], ['AdminTeamUsage', '/admin/teams/usage']]) {
      const record = records.find((item) => item.name === name)
      expect(record?.path).toBe(path)
      expect(record?.meta.requiresAdmin).toBe(true)
      expect(record?.meta.requiresAuth).toBe(true)
    }
  })
})
