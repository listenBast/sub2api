import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  setMemberAdmin: vi.fn(),
  memberSpendStats: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  query: {} as Record<string, string>
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: { value: 'en-US' }, t: (key: string, params?: Record<string, string>) => (params?.member ? `${key}:${params.member}` : key) })
  }
})
vi.mock('vue-router', () => ({ useRoute: () => ({ query: mocks.query }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }) }))
vi.mock('@/api/team', () => ({
  adminTeamsAPI: { get: mocks.get, setMemberAdmin: mocks.setMemberAdmin, memberSpendStats: mocks.memberSpendStats, list: vi.fn() }
}))

import TeamMembersView from '@/views/admin/TeamMembersView.vue'
import TeamUsageStatsView from '@/views/admin/TeamUsageStatsView.vue'

const owner = { user_id: 1, email: 'owner@example.com', username: 'owner', balance: 20 }
const member = (overrides = {}) => ({
  membership_id: 3, user_id: 9, email: 'member@example.com', username: 'member', remark: '', status: 'active',
  is_admin: false, balance: 1, team_balance: 5, total_balance: 6, frozen_balance: 0, concurrency: 1, rpm_limit: 0, created_at: '2026-07-28T00:00:00Z', ...overrides
})
const team = (members: unknown[]) => ({
  id: 5, name: 'Alpha', status: 'active', owner, members, active_member_count: members.length, pending_invite_count: 0,
  exit_pending_count: 0, total_balance: 20, created_at: '2026-07-28T00:00:00Z'
})

// 选择器直接触发 change，模拟平台管理员选中团队。
const PickerStub = {
  props: ['modelValue', 'initialTeamId'],
  emits: ['update:modelValue', 'change'],
  template: '<button data-testid="pick" @click="$emit(\'update:modelValue\', 5); $emit(\'change\', { id: 5 })">pick</button>'
}
const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  AdminTeamPicker: PickerStub,
  ConfirmDialog: { props: ['show', 'message', 'title'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-testid="confirm"><span data-testid="confirm-message">{{ message }}</span><button data-testid="confirm-ok" @click="$emit(\'confirm\')">ok</button></div>' }
}

describe('admin TeamMembersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.query = {}
  })

  it('asks the admin to select a team first, then lists owner and members with roles', async () => {
    mocks.get.mockResolvedValue(team([member(), member({ membership_id: 4, user_id: 10, email: 'adm@example.com', is_admin: true })]))
    const wrapper = mount(TeamMembersView, { global: { stubs } })
    expect(wrapper.text()).toContain('team.noTeamSelected')
    await wrapper.get('[data-testid="pick"]').trigger('click')
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith(5)
    expect(wrapper.get('[data-testid="team-members-title"]').text()).toBe('Alpha')
    expect(wrapper.find('[data-testid="team-member-owner-row"]').text()).toContain('team.roleOwner')
    const rows = wrapper.findAll('[data-testid="team-member-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('team.roleMember')
    expect(rows[0].text()).toContain('team.setAdmin')
    expect(rows[1].text()).toContain('team.roleAdmin')
    expect(rows[1].text()).toContain('team.unsetAdmin')
  })

  it('confirms before setting an admin, then refreshes the roles', async () => {
    mocks.get.mockResolvedValueOnce(team([member()])).mockResolvedValueOnce(team([member({ is_admin: true })]))
    mocks.setMemberAdmin.mockResolvedValue({})
    const wrapper = mount(TeamMembersView, { global: { stubs } })
    await wrapper.get('[data-testid="pick"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-member-toggle-admin"]').trigger('click')
    expect(wrapper.get('[data-testid="confirm-message"]').text()).toBe('team.confirmSetAdmin:member')
    expect(mocks.setMemberAdmin).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.setMemberAdmin).toHaveBeenCalledWith(5, 9, true)
    expect(mocks.showSuccess).toHaveBeenCalledWith('team.adminGranted')
    expect(wrapper.findAll('[data-testid="team-member-row"]')[0].text()).toContain('team.roleAdmin')
  })

  it('unsets an admin and does not offer toggling for pending invitations', async () => {
    mocks.get.mockResolvedValue(team([member({ is_admin: true }), member({ membership_id: 8, user_id: 12, status: 'invited' })]))
    mocks.setMemberAdmin.mockResolvedValue({})
    const wrapper = mount(TeamMembersView, { global: { stubs } })
    await wrapper.get('[data-testid="pick"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="team-member-toggle-admin"]')).toHaveLength(1)
    await wrapper.get('[data-testid="team-member-toggle-admin"]').trigger('click')
    expect(wrapper.get('[data-testid="confirm-message"]').text()).toBe('team.confirmUnsetAdmin:member')
    await wrapper.get('[data-testid="confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.setMemberAdmin).toHaveBeenCalledWith(5, 9, false)
    expect(mocks.showSuccess).toHaveBeenCalledWith('team.adminRevoked')
  })

  it('shows the API error and no success toast when the update fails', async () => {
    mocks.get.mockResolvedValue(team([member()]))
    mocks.setMemberAdmin.mockRejectedValue(new Error('denied'))
    const wrapper = mount(TeamMembersView, { global: { stubs } })
    await wrapper.get('[data-testid="pick"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-member-toggle-admin"]').trigger('click')
    await wrapper.get('[data-testid="confirm-ok"]').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('denied')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
  })

  it('passes the team_id route query to the picker as the preselected team', () => {
    mocks.query = { team_id: '5' }
    const wrapper = mount(TeamMembersView, { global: { stubs } })
    expect(wrapper.findComponent(PickerStub).props('initialTeamId')).toBe(5)
  })
})

describe('admin TeamUsageStatsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.query = {}
  })

  it('loads the spend stats of the selected team and reloads when switching team', async () => {
    mocks.memberSpendStats.mockResolvedValue({
      team_id: 5, start: '', end: '',
      members: [{ user_id: 1, email: 'owner@example.com', username: 'owner', role: 'owner', status: 'active', requests: 2, tokens: 20, total_cost: 1, actual_cost: 1 }],
      totals: { requests: 2, tokens: 20, total_cost: 1, actual_cost: 1 }
    })
    const wrapper = mount(TeamUsageStatsView, { global: { stubs: { ...stubs, DateRangePicker: true } } })
    expect(wrapper.text()).toContain('team.noTeamSelected')
    expect(mocks.memberSpendStats).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="pick"]').trigger('click')
    await flushPromises()
    expect(mocks.memberSpendStats).toHaveBeenCalledTimes(1)
    const [teamId, range] = mocks.memberSpendStats.mock.calls[0]
    expect(teamId).toBe(5)
    expect(range.start_date).toBeTruthy()
    expect(range.end_date).toBeTruthy()
    expect(wrapper.findAll('[data-testid="team-spend-row"]')).toHaveLength(1)
  })

  it('reports loading errors through the app store', async () => {
    mocks.memberSpendStats.mockRejectedValue(new Error('no access'))
    const wrapper = mount(TeamUsageStatsView, { global: { stubs: { ...stubs, DateRangePicker: true } } })
    await wrapper.get('[data-testid="pick"]').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('no access')
  })
})
