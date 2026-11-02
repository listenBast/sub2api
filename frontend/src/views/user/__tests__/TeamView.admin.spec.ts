import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

const mocks = vi.hoisted(() => ({
  getContext: vi.fn(),
  setMemberAdmin: vi.fn(),
  transferOwnership: vi.fn(),
  getMemberSpendStats: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  refreshUser: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ locale: { value: 'en-US' }, t: (key: string) => key }) }
})
vi.mock('@/api/team', () => ({
  default: {
    getContext: mocks.getContext,
    setMemberAdmin: mocks.setMemberAdmin,
    transferOwnership: mocks.transferOwnership,
    getMemberSpendStats: mocks.getMemberSpendStats
  }
}))
vi.mock('@/stores', async () => {
  const team = await import('@/stores/team')
  return {
    useTeamStore: team.useTeamStore,
    useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }),
    useAuthStore: () => ({ refreshUser: mocks.refreshUser })
  }
})

import TeamView from '@/views/user/TeamView.vue'

const owner = { user_id: 1, email: 'owner@example.com', username: 'owner', balance: 10 }
const memberRow = (overrides = {}) => ({
  membership_id: 2, user_id: 2, email: 'm@example.com', username: 'm', remark: '', status: 'active', is_admin: false,
  balance: 0, team_balance: 0, total_balance: 0, frozen_balance: 0, concurrency: 1, rpm_limit: 0, created_at: '2026-07-28T00:00:00Z', ...overrides
})
const contextFor = (role: string, members = [memberRow()]) => ({
  role, membership_status: 'active', financial_restricted: false,
  team: { id: 5, name: 'Alpha', status: 'active', owner, members, active_member_count: members.length, pending_invite_count: 0, exit_pending_count: 0, total_balance: 10, created_at: '2026-07-28T00:00:00Z' }
})

const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  Skeleton: true,
  Pagination: true,
  TeamLedgerPanel: true,
  TeamMemberSpendPanel: { template: '<div data-testid="spend-panel" />' },
  TeamUsageSummaryCards: true,
  BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' },
  ConfirmDialog: { props: ['show', 'message'], emits: ['confirm'], template: '<button v-if="show" data-testid="confirm-ok" @click="$emit(\'confirm\')">ok</button>' },
  Icon: true
}

async function mountAs(role: string, members?: ReturnType<typeof memberRow>[]) {
  mocks.getContext.mockResolvedValue(contextFor(role, members))
  const wrapper = mount(TeamView, { global: { stubs } })
  await flushPromises()
  await wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'team.members')!.trigger('click')
  return wrapper
}

describe('user TeamView with the admin role', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mocks.refreshUser.mockResolvedValue(undefined)
  })

  it('gives the owner the transfer and set-admin controls', async () => {
    const wrapper = await mountAs('owner')
    expect(wrapper.find('[data-testid="team-admin-notice"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="member-toggle-admin"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="member-transfer-owner"]').exists()).toBe(true)
  })

  it('gives an admin the manager UI but no transfer or admin-promotion controls', async () => {
    const wrapper = await mountAs('admin')
    expect(wrapper.find('[data-testid="team-admin-notice"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('team.inviteMember')
    expect(wrapper.find('[data-testid="member-transfer-owner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="member-toggle-admin"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('team.remove')
  })

  it('does not show the spend statistics tab content to plain members', async () => {
    mocks.getContext.mockResolvedValue(contextFor('member'))
    const wrapper = mount(TeamView, { global: { stubs } })
    await flushPromises()
    expect(wrapper.findAll('[role="tab"]').map((t) => t.text())).not.toContain('team.spendStats')
  })

  it('shows the per-member spend tab to admins', async () => {
    mocks.getContext.mockResolvedValue(contextFor('admin'))
    const wrapper = mount(TeamView, { global: { stubs } })
    await flushPromises()
    await wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'team.spendStats')!.trigger('click')
    expect(wrapper.find('[data-testid="spend-panel"]').exists()).toBe(true)
  })

  it('sets an admin after confirmation, then refreshes the context', async () => {
    const wrapper = await mountAs('owner')
    mocks.setMemberAdmin.mockResolvedValue({})
    mocks.getContext.mockResolvedValue(contextFor('owner', [memberRow({ is_admin: true })]))
    await wrapper.get('[data-testid="member-toggle-admin"]').trigger('click')
    await wrapper.get('[data-testid="confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.setMemberAdmin).toHaveBeenCalledWith(2, true)
    expect(mocks.showSuccess).toHaveBeenCalledWith('team.adminGranted')
    expect(wrapper.get('[data-testid="member-toggle-admin"]').text()).toContain('team.unsetAdmin')
  })

  it('re-syncs the role right away when the server rejects a manager action', async () => {
    const wrapper = await mountAs('owner')
    mocks.setMemberAdmin.mockRejectedValue(Object.assign(new Error('forbidden'), { reason: 'TEAM_MANAGER_REQUIRED' }))
    mocks.getContext.mockClear()
    // 服务端此时已把这个用户降级为普通成员。
    mocks.getContext.mockResolvedValue(contextFor('member'))
    await wrapper.get('[data-testid="member-toggle-admin"]').trigger('click')
    await wrapper.get('[data-testid="confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('forbidden')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.getContext).toHaveBeenCalled()
    expect(wrapper.find('[data-testid="member-toggle-admin"]').exists()).toBe(false)
  })
})
