import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import DashboardView from '../DashboardView.vue'

const {
  authStore,
  teamStore,
  getDashboardStats,
  getDashboardTrend,
  getDashboardModels,
  getByDateRange,
  getMyPlatformQuotas,
} = vi.hoisted(() => ({
  authStore: {
    user: { balance: 10 },
    isSimpleMode: false,
    refreshUser: vi.fn(),
  },
  teamStore: {
    isOwner: false,
    context: null as any,
    fetchContext: vi.fn(),
  },
  getDashboardStats: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getByDateRange: vi.fn(),
  getMyPlatformQuotas: vi.fn(),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/team', () => ({
  useTeamStore: () => teamStore,
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardStats,
    getDashboardTrend,
    getDashboardModels,
    getByDateRange,
  },
}))

vi.mock('@/api/user', () => ({ getMyPlatformQuotas }))

const simpleStub = { template: '<div><slot /></div>' }

function mountDashboard() {
  return mount(DashboardView, {
    global: {
      stubs: {
        AppLayout: simpleStub,
        LoadingSpinner: true,
        UserDashboardStats: {
          props: ['balance'],
          template: '<div data-testid="dashboard-stats" :data-balance="balance" />',
        },
        UserDashboardCharts: true,
        UserDashboardRecentUsage: true,
        UserDashboardQuickActions: true,
      },
    },
  })
}

describe('user DashboardView team mode', () => {
  beforeEach(() => {
    teamStore.isOwner = false
    teamStore.context = null
    teamStore.fetchContext.mockReset()
    teamStore.fetchContext.mockResolvedValue({ role: 'individual' })
    authStore.refreshUser.mockReset()
    authStore.refreshUser.mockResolvedValue(undefined)
    getDashboardStats.mockReset()
    getDashboardStats.mockResolvedValue({ total_requests: 0 })
    getDashboardTrend.mockReset()
    getDashboardTrend.mockResolvedValue({ trend: [] })
    getDashboardModels.mockReset()
    getDashboardModels.mockResolvedValue({ models: [] })
    getByDateRange.mockReset()
    getByDateRange.mockResolvedValue({ items: [] })
    getMyPlatformQuotas.mockReset()
    getMyPlatformQuotas.mockResolvedValue({ platform_quotas: [] })
  })

  it('loads the original personal dashboard for a non-owner', async () => {
    mountDashboard()
    await flushPromises()

    expect(teamStore.fetchContext).toHaveBeenCalledWith(true)
    expect(getDashboardStats).toHaveBeenCalled()
    expect(getDashboardTrend).toHaveBeenCalled()
    expect(getDashboardModels).toHaveBeenCalled()
    expect(getByDateRange).toHaveBeenCalled()
    expect(getMyPlatformQuotas).toHaveBeenCalled()
  })

  it('keeps the original dashboard for an owner and uses the team balance', async () => {
    teamStore.isOwner = true
    teamStore.context = {
      role: 'owner',
      team: { total_balance: 42 },
    }

    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.find('[data-testid="dashboard-stats"]').attributes('data-balance')).toBe('42')
    expect(getDashboardStats).toHaveBeenCalled()
    expect(getDashboardTrend).toHaveBeenCalled()
    expect(getDashboardModels).toHaveBeenCalled()
    expect(getByDateRange).toHaveBeenCalled()
    expect(getMyPlatformQuotas).toHaveBeenCalled()
  })
})
