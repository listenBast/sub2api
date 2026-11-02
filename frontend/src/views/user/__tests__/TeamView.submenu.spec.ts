import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

const mocks = vi.hoisted(() => ({
  getContext: vi.fn(),
  listUsage: vi.fn(),
  getUsageSummary: vi.fn(),
  listTransactions: vi.fn(),
  getMemberSpendStats: vi.fn(),
  showError: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ locale: { value: 'en-US' }, t: (key: string) => key }) }
})
vi.mock('@/api/team', () => ({
  default: {
    getContext: mocks.getContext,
    listUsage: mocks.listUsage,
    getUsageSummary: mocks.getUsageSummary,
    listTransactions: mocks.listTransactions,
    getMemberSpendStats: mocks.getMemberSpendStats
  }
}))
vi.mock('@/stores', async () => {
  const team = await import('@/stores/team')
  return {
    useTeamStore: team.useTeamStore,
    useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() }),
    useAuthStore: () => ({ refreshUser: vi.fn() })
  }
})

import TeamView from '@/views/user/TeamView.vue'
import { useTeamStore } from '@/stores/team'

const owner = { user_id: 1, email: 'owner@example.com', username: 'boss', balance: 12.5 }
const member = {
  membership_id: 2, user_id: 2, email: 'm@example.com', username: 'm', remark: '', status: 'active', is_admin: false,
  balance: 1, team_balance: 2, total_balance: 3, frozen_balance: 0, concurrency: 1, rpm_limit: 0, created_at: '2026-07-28T00:00:00Z'
}
const contextFor = (role: string) => ({
  role, membership_status: 'active', financial_restricted: false,
  current_membership: role === 'member' ? member : undefined,
  team: { id: 5, name: 'Alpha', status: 'active', owner, members: [member], active_member_count: 1, pending_invite_count: 0, exit_pending_count: 0, total_balance: 10, created_at: '2026-07-28T00:00:00Z' }
})
const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  Skeleton: true,
  Pagination: true,
  TeamLedgerPanel: { template: '<div data-testid="ledger-panel" />' },
  TeamMemberSpendPanel: { template: '<div data-testid="spend-panel" />' },
  TeamUsageSummaryCards: true,
  BaseDialog: true,
  ConfirmDialog: true,
  Icon: true
}
const tabs = ['overview', 'members', 'usage', 'spend', 'ledger']

async function mountAt(path: string, role: string) {
  mocks.getContext.mockResolvedValue(contextFor(role))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: tabs.map((tab) => ({ path: `/team/${tab}`, component: { template: '<div />' }, meta: { teamTab: tab } }))
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(TeamView, { global: { stubs, plugins: [router] } })
  await flushPromises()
  return { wrapper, router }
}

describe('user TeamView as a sidebar submenu', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mocks.listUsage.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    mocks.listTransactions.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    mocks.getUsageSummary.mockResolvedValue(null)
  })

  it('renders only the page for the current route and has no in-page tab strip', async () => {
    const { wrapper } = await mountAt('/team/overview', 'owner')
    expect(wrapper.find('[role="tab"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="team-dissolve"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="team-usage-filters"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ledger-panel"]').exists()).toBe(false)
  })

  it('lists the owner as the first member row with the owner badge and no actions', async () => {
    const { wrapper } = await mountAt('/team/members', 'owner')
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('owner@example.com')
    expect(rows[0].text()).toContain('team.roleOwner')
    expect(rows[0].find('button').exists()).toBe(false)
    expect(rows[1].text()).toContain('m@example.com')
    expect(rows[1].find('button').exists()).toBe(true)
  })

  it('includes the owner in the usage member filter and uses the shared form controls', async () => {
    const { wrapper } = await mountAt('/team/usage', 'owner')
    expect(mocks.listUsage).toHaveBeenCalledTimes(1)
    const form = wrapper.get('[data-testid="team-usage-filters"]')
    const select = form.findComponent({ name: 'Select' })
    const options = select.props('options') as Array<{ value: number; label: string }>
    expect(options.map((item) => item.value)).toEqual([0, 1, 2])
    expect(form.findAll('input').every((input) => input.classes().includes('input'))).toBe(true)
    expect(form.find('select').exists()).toBe(false)
  })

  it('lets a plain member see own usage and ledger, without the member filter', async () => {
    const usage = await mountAt('/team/usage', 'member')
    expect(usage.wrapper.find('[data-testid="team-member-usage-notice"]').exists()).toBe(true)
    expect(usage.wrapper.findComponent({ name: 'Select' }).exists()).toBe(false)
    expect(mocks.getUsageSummary).not.toHaveBeenCalled()
    const ledger = await mountAt('/team/ledger', 'member')
    expect(ledger.wrapper.find('[data-testid="ledger-panel"]').exists()).toBe(true)
    expect(mocks.listTransactions).toHaveBeenCalled()
  })

  it.each(['members', 'spend'])('sends a plain member away from /team/%s', async (tab) => {
    const { router, wrapper } = await mountAt(`/team/${tab}`, 'member')
    expect(router.currentRoute.value.path).toBe('/team/overview')
    expect(wrapper.find('[data-testid="spend-panel"]').exists()).toBe(false)
  })

  it('sends a user without a team to the overview from every sub-page', async () => {
    const { router } = await mountAt('/team/ledger', 'individual')
    expect(router.currentRoute.value.path).toBe('/team/overview')
  })

  it('redirects right away when the role is downgraded while on a manager-only page', async () => {
    const { router, wrapper } = await mountAt('/team/spend', 'admin')
    expect(wrapper.find('[data-testid="spend-panel"]').exists()).toBe(true)
    useTeamStore().setContext(contextFor('member') as never)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/team/overview')
  })
})
