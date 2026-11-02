import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  adjustOwnerBalance: vi.fn(),
  list: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en-US' },
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess })
}))

vi.mock('@/api/team', () => ({
  adminTeamsAPI: { adjustOwnerBalance: mocks.adjustOwnerBalance, list: mocks.list }
}))

import type { TeamMember, TeamMemberSpendStats } from '@/api/team'
import AdminOwnerBalanceDialog from '../AdminOwnerBalanceDialog.vue'
import AdminTeamPicker from '../AdminTeamPicker.vue'
import TeamMemberActions from '../TeamMemberActions.vue'
import TeamMemberSpendPanel from '../TeamMemberSpendPanel.vue'
import TeamRoleBadge from '../TeamRoleBadge.vue'

const member: TeamMember = {
  membership_id: 1, user_id: 11, email: 'm@example.com', username: 'm', remark: '', status: 'active',
  balance: 0, team_balance: 0, total_balance: 0, frozen_balance: 0, concurrency: 5, rpm_limit: 60,
  joined_at: '2026-07-27T00:00:00Z', created_at: '2026-07-27T00:00:00Z'
}

const team = { id: 3, name: 'Alpha', owner: { user_id: 1, email: 'owner@example.com', username: 'owner', balance: 10 } }
const BaseDialogStub = { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' }

describe('TeamRoleBadge', () => {
  it.each([['owner', 'team.roleOwner'], ['admin', 'team.roleAdmin'], ['member', 'team.roleMember']] as const)('labels %s', (role, label) => {
    const wrapper = mount(TeamRoleBadge, { props: { role } })
    expect(wrapper.text()).toBe(label)
    expect(wrapper.classes()).toContain(`role-${role}`)
  })
})

describe('TeamMemberActions admin controls', () => {
  it('hides the toggle and shows transfer for the default (owner-like) view', () => {
    const wrapper = mount(TeamMemberActions, { props: { member } })
    expect(wrapper.find('[data-testid="member-toggle-admin"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="member-transfer-owner"]').exists()).toBe(true)
  })

  it('lets only the owner toggle admin and emits the member', async () => {
    const wrapper = mount(TeamMemberActions, { props: { member, canSetAdmin: true } })
    expect(wrapper.find('[data-testid="member-toggle-admin"]').text()).toBe('team.setAdmin')
    await wrapper.find('[data-testid="member-toggle-admin"]').trigger('click')
    expect(wrapper.emitted('toggle-admin')?.[0]).toEqual([member])

    await wrapper.setProps({ member: { ...member, is_admin: true } })
    expect(wrapper.find('[data-testid="member-toggle-admin"]').text()).toBe('team.unsetAdmin')
  })

  it('never offers ownership transfer to an admin viewer', () => {
    const wrapper = mount(TeamMemberActions, { props: { member, canTransfer: false } })
    expect(wrapper.find('[data-testid="member-transfer-owner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="member-toggle-admin"]').exists()).toBe(false)
  })

  it('does not offer admin promotion for pending invitations', () => {
    const wrapper = mount(TeamMemberActions, { props: { member: { ...member, status: 'invited' }, canSetAdmin: true } })
    expect(wrapper.find('[data-testid="member-toggle-admin"]').exists()).toBe(false)
  })
})

describe('TeamMemberSpendPanel', () => {
  const stats: TeamMemberSpendStats = {
    team_id: 3, start: '2026-10-01T00:00:00Z', end: '2026-11-01T00:00:00Z',
    members: [
      { user_id: 1, email: 'owner@example.com', username: 'owner', role: 'owner', status: 'active', requests: 10, tokens: 1000, total_cost: 3, actual_cost: 3 },
      { user_id: 2, email: 'a@example.com', username: 'a', remark: '研发', role: 'admin', status: 'active', requests: 5, tokens: 500, total_cost: 1, actual_cost: 1 }
    ],
    totals: { requests: 15, tokens: 1500, total_cost: 4, actual_cost: 4 }
  }
  const stubs = { DateRangePicker: true }

  it('loads with an RFC3339 range, renders per-member rows and a totals row', async () => {
    const load = vi.fn().mockResolvedValue(stats)
    const wrapper = mount(TeamMemberSpendPanel, { props: { load }, global: { stubs } })
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(1)
    const range = load.mock.calls[0][0]
    expect(range.start_date).toMatch(/T/)
    expect(range.end_date).toMatch(/T/)
    expect(new Date(range.end_date).getTime()).toBeGreaterThan(new Date(range.start_date).getTime())
    const rows = wrapper.findAll('[data-testid="team-spend-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[1].text()).toContain('研发')
    expect(rows[1].text()).toContain('$1.00')
    expect(wrapper.find('[data-testid="team-spend-totals-row"]').text()).toContain('$4.00')
    expect(wrapper.find('[data-testid="team-spend-total-cost"]').text()).toBe('$4.00')
    expect(wrapper.findAllComponents(TeamRoleBadge).map((b) => b.props('role'))).toContain('admin')
  })

  it('reloads when reloadKey changes and reports failures', async () => {
    const load = vi.fn().mockResolvedValueOnce(stats).mockRejectedValueOnce(new Error('boom'))
    const wrapper = mount(TeamMemberSpendPanel, { props: { load, reloadKey: 3 }, global: { stubs } })
    await flushPromises()
    await wrapper.setProps({ reloadKey: 4 })
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('error')?.[0]?.[0]).toBeInstanceOf(Error)
    expect(wrapper.findAll('[data-testid="team-spend-row"]')).toHaveLength(0)
  })

  it('refreshes manually through the refresh button', async () => {
    const load = vi.fn().mockResolvedValue(stats)
    const wrapper = mount(TeamMemberSpendPanel, { props: { load }, global: { stubs } })
    await flushPromises()
    await wrapper.find('[data-testid="team-spend-refresh"]').trigger('click')
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(2)
  })
})

describe('AdminOwnerBalanceDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.adjustOwnerBalance.mockResolvedValue({})
  })

  function mountDialog() {
    return mount(AdminOwnerBalanceDialog, { props: { show: true, team }, global: { stubs: { BaseDialog: BaseDialogStub } } })
  }

  it('previews the balance after adding and submits the add operation', async () => {
    const wrapper = mountDialog()
    expect(wrapper.find('[data-testid="owner-balance-email"]').text()).toBe('owner@example.com')
    expect(wrapper.find('[data-testid="owner-balance-current"]').text()).toBe('$10.00')
    await wrapper.find('[data-testid="owner-balance-amount"]').setValue('5.5')
    expect(wrapper.find('[data-testid="owner-balance-after"]').text()).toBe('$15.50')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.adjustOwnerBalance).toHaveBeenCalledWith(3, { operation: 'add', balance: 5.5, notes: undefined })
    expect(wrapper.emitted('saved')?.[0]).toEqual([3])
    expect(mocks.showSuccess).toHaveBeenCalled()
  })

  it('submits subtract with notes and blocks subtracting more than the balance', async () => {
    const wrapper = mountDialog()
    await wrapper.find('[data-testid="owner-balance-op-subtract"]').trigger('click')
    await wrapper.find('[data-testid="owner-balance-amount"]').setValue('50')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.adjustOwnerBalance).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('team.ownerBalanceNegative')

    await wrapper.find('[data-testid="owner-balance-amount"]').setValue('4')
    await wrapper.find('textarea').setValue('refund')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.adjustOwnerBalance).toHaveBeenCalledWith(3, { operation: 'subtract', balance: 4, notes: 'refund' })
  })

  it('rejects an empty amount and shows API errors without emitting saved', async () => {
    const wrapper = mountDialog()
    await wrapper.find('form').trigger('submit')
    expect(mocks.showError).toHaveBeenCalledWith('team.ownerBalanceAmountRequired')
    expect(mocks.adjustOwnerBalance).not.toHaveBeenCalled()

    mocks.adjustOwnerBalance.mockRejectedValueOnce(new Error('server said no'))
    await wrapper.find('[data-testid="owner-balance-amount"]').setValue('1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.showError).toHaveBeenLastCalledWith('server said no')
    expect(wrapper.emitted('saved')).toBeUndefined()
  })
})

describe('AdminTeamPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue({ items: [team, { ...team, id: 4, name: 'Beta' }], total: 2, page: 1, pages: 1 })
  })

  it('auto-selects the first team and lets the admin switch teams', async () => {
    const wrapper = mount(AdminTeamPicker, { props: { modelValue: null } })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([3])
    await wrapper.find('select').setValue('4')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([4])
    expect(wrapper.emitted('change')?.at(-1)?.[0]).toMatchObject({ id: 4 })
  })

  it('prefers the initial team id from the route', async () => {
    const wrapper = mount(AdminTeamPicker, { props: { modelValue: null, initialTeamId: 4 } })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([4])
  })

  it('searches teams with debounce', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mount(AdminTeamPicker, { props: { modelValue: 3 } })
      await flushPromises()
      mocks.list.mockClear()
      await wrapper.find('[data-testid="admin-team-picker-search"]').setValue('bet')
      vi.advanceTimersByTime(350)
      await flushPromises()
      expect(mocks.list).toHaveBeenCalledWith({ page: 1, page_size: 50, search: 'bet' })
    } finally {
      vi.useRealTimers()
    }
  })
})
