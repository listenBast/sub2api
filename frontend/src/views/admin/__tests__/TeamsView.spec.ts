import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  overview: vi.fn(),
  get: vi.fn(),
  dashboard: vi.fn(),
  transactions: vi.fn(),
  deleteTeam: vi.fn(),
  adjustMemberTeamBalance: vi.fn(),
  setMemberAdmin: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'zh-CN' },
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: mocks.showError,
    showSuccess: mocks.showSuccess
  })
}))

vi.mock('@/api/team', () => ({
  adminTeamsAPI: {
    list: mocks.list,
    overview: mocks.overview,
    get: mocks.get,
    dashboard: mocks.dashboard,
    transactions: mocks.transactions,
    usage: vi.fn(),
    setStatus: vi.fn(),
    removeMember: vi.fn(),
    create: vi.fn(),
    addMember: vi.fn(),
    updateMemberRemark: vi.fn(),
    transferOwner: vi.fn(),
    usageSummary: vi.fn(),
    deleteTeam: mocks.deleteTeam,
    adjustMemberTeamBalance: mocks.adjustMemberTeamBalance,
    setMemberAdmin: mocks.setMemberAdmin
  }
}))

import TeamsView from '@/views/admin/TeamsView.vue'

const emptyTeam = {
  id: 7,
  name: '空成员团队',
  status: 'active' as const,
  owner: { user_id: 1, email: 'owner@example.com', username: 'owner', balance: 12 },
  members: null,
  active_member_count: 0,
  pending_invite_count: 0,
  exit_pending_count: 0,
  total_balance: 12,
  created_at: '2026-07-28T00:00:00Z'
}

describe('admin team details', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue({ items: [emptyTeam], total: 1, page: 1, pages: 1 })
    mocks.overview.mockResolvedValue({ total_teams: 1, active_teams: 1, suspended_teams: 0, member_count: 0, pending_invites: 0, exit_pending: 0, total_balance: 12 })
    mocks.get.mockResolvedValue(emptyTeam)
    mocks.dashboard.mockRejectedValue(new Error('统计接口暂时不可用'))
    mocks.transactions.mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    mocks.deleteTeam.mockResolvedValue(undefined)
  })

  it('keeps an empty-member team dialog open when members is null and statistics fail', async () => {
    const wrapper = mount(TeamsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" data-testid="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' },
          ConfirmDialog: true,
          Pagination: true,
          TeamLedgerPanel: true,
          Icon: true
        }
      }
    })
    await flushPromises()

    const detailsButton = wrapper.findAll('button').find((button) => button.text() === 'team.details')
    expect(detailsButton).toBeTruthy()
    await detailsButton!.trigger('click')
    await flushPromises()

    const dialog = wrapper.findAll('[data-testid="dialog"]').find((item) => item.text().includes('空成员团队'))
    expect(dialog).toBeTruthy()
    // 成员为空时依然显示主账号这一行，且没有任何成员操作按钮。
    expect(dialog!.find('[data-testid="admin-team-owner-row"]').exists()).toBe(true)
    expect(dialog!.findAll('[data-testid="admin-team-member-row"]')).toHaveLength(0)
    expect(dialog!.find('[data-testid="admin-team-owner-row"] button').exists()).toBe(false)
    expect(mocks.showError).toHaveBeenCalledWith('统计接口暂时不可用')
  })

  it('lets an administrator delete the selected team', async () => {
    const wrapper = mount(TeamsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          AdminUserSearchSelect: true,
          BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" data-testid="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' },
          ConfirmDialog: { props: ['show'], emits: ['confirm'], template: '<button v-if="show" data-testid="confirm-delete" @click="$emit(\'confirm\')">confirm</button>' },
          Pagination: true,
          TeamLedgerPanel: true,
          Icon: true
        }
      }
    })
    await flushPromises()

    const detailsButton = wrapper.findAll('button').find((button) => button.text() === 'team.details')
    await detailsButton!.trigger('click')
    await flushPromises()

    const deleteButton = wrapper.findAll('button').find((button) => button.text() === 'team.deleteTeam')
    expect(deleteButton).toBeTruthy()
    await deleteButton!.trigger('click')
    await wrapper.get('[data-testid="confirm-delete"]').trigger('click')
    await flushPromises()

    expect(mocks.deleteTeam).toHaveBeenCalledWith(emptyTeam.id)
    expect(mocks.showSuccess).toHaveBeenCalledWith('team.saved')
  })
  it('lets an administrator adjust a member team quota from the owner pool', async () => {
    const member = {
      membership_id: 3, user_id: 9, email: 'member@example.com', username: 'member', remark: '', status: 'active',
      balance: 0, team_balance: 5, total_balance: 5, frozen_balance: 0, concurrency: 1, rpm_limit: 0, created_at: '2026-07-28T00:00:00Z'
    }
    const team = { ...emptyTeam, owner: { ...emptyTeam.owner, balance: 100 }, members: [member], active_member_count: 1 }
    mocks.list.mockResolvedValue({ items: [team], total: 1, page: 1, pages: 1 })
    mocks.get.mockResolvedValue(team)
    mocks.adjustMemberTeamBalance.mockResolvedValue({ ...member, team_balance: 15, total_balance: 15 })
    const wrapper = mount(TeamsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          AdminUserSearchSelect: true,
          BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" data-testid="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' },
          ConfirmDialog: true,
          Pagination: true,
          TeamLedgerPanel: true,
          Icon: true
        }
      }
    })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'team.details')!.trigger('click')
    await flushPromises()

    await wrapper.find('button[aria-label="team.adminAdjustTeamQuota"]').trigger('click')
    const form = wrapper.get('#admin-team-quota-form')
    await form.get('input[type="number"]').setValue('10')
    await form.trigger('submit')
    await flushPromises()

    expect(mocks.adjustMemberTeamBalance).toHaveBeenCalledWith(team.id, member.user_id, { operation: 'add', amount: 10, source: 'owner_pool', notes: undefined })
    expect(mocks.showSuccess).toHaveBeenCalledWith('team.saved')
  })

  const adminMember = {
    membership_id: 3, user_id: 9, email: 'member@example.com', username: 'member', remark: '', status: 'active',
    is_admin: false, balance: 0, team_balance: 5, total_balance: 5, frozen_balance: 0, concurrency: 1, rpm_limit: 0, created_at: '2026-07-28T00:00:00Z'
  }
  const mountView = () => mount(TeamsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        AdminUserSearchSelect: true,
        AdminOwnerBalanceDialog: { props: ['show', 'team'], emits: ['saved', 'close'], template: '<div v-if="show" data-testid="owner-balance-dialog">{{ team?.owner.email }}<button data-testid="owner-balance-saved" @click="$emit(\'saved\', team.id)">saved</button></div>' },
        BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" data-testid="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' },
        ConfirmDialog: { props: ['show', 'message'], emits: ['confirm'], template: '<button v-if="show" data-testid="confirm-action" @click="$emit(\'confirm\')">{{ message }}</button>' },
        Pagination: true,
        TeamLedgerPanel: true,
        Icon: true
      }
    }
  })

  it('shows the owner balance in the list and opens the owner-balance dialog from the row, refreshing afterwards', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="team-owner-balance"]').text()).toContain('$12.00')

    await wrapper.get('[data-testid="team-adjust-owner-balance"]').trigger('click')
    expect(wrapper.get('[data-testid="owner-balance-dialog"]').text()).toContain('owner@example.com')

    mocks.list.mockClear()
    mocks.overview.mockClear()
    await wrapper.get('[data-testid="owner-balance-saved"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="owner-balance-dialog"]').exists()).toBe(false)
    expect(mocks.list).toHaveBeenCalled()
    expect(mocks.overview).toHaveBeenCalled()
  })

  it('also offers the owner-balance action inside the team detail dialog', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'team.details')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="team-detail-adjust-owner-balance"]').trigger('click')
    expect(wrapper.find('[data-testid="owner-balance-dialog"]').exists()).toBe(true)
  })

  it('shows each member role and lets the site admin set and unset a team admin', async () => {
    const team = { ...emptyTeam, members: [adminMember], active_member_count: 1 }
    mocks.list.mockResolvedValue({ items: [team], total: 1, page: 1, pages: 1 })
    mocks.get.mockResolvedValue(team)
    mocks.setMemberAdmin.mockResolvedValue({ ...adminMember, is_admin: true })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'team.details')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('team.roleMember')

    mocks.get.mockResolvedValue({ ...team, members: [{ ...adminMember, is_admin: true }] })
    await wrapper.get('[data-testid="admin-member-toggle-admin"]').trigger('click')
    expect(wrapper.get('[data-testid="confirm-action"]').text()).toContain('team.confirmSetAdmin')
    await wrapper.get('[data-testid="confirm-action"]').trigger('click')
    await flushPromises()
    expect(mocks.setMemberAdmin).toHaveBeenCalledWith(team.id, adminMember.user_id, true)
    expect(mocks.showSuccess).toHaveBeenCalledWith('team.adminGranted')
    expect(wrapper.text()).toContain('team.roleAdmin')

    mocks.get.mockResolvedValue(team)
    await wrapper.get('[data-testid="admin-member-toggle-admin"]').trigger('click')
    expect(wrapper.get('[data-testid="confirm-action"]').text()).toContain('team.confirmUnsetAdmin')
    await wrapper.get('[data-testid="confirm-action"]').trigger('click')
    await flushPromises()
    expect(mocks.setMemberAdmin).toHaveBeenLastCalledWith(team.id, adminMember.user_id, false)
    expect(mocks.showSuccess).toHaveBeenLastCalledWith('team.adminRevoked')
  })

  it('surfaces errors when setting an admin fails', async () => {
    const team = { ...emptyTeam, members: [adminMember], active_member_count: 1 }
    mocks.list.mockResolvedValue({ items: [team], total: 1, page: 1, pages: 1 })
    mocks.get.mockResolvedValue(team)
    mocks.setMemberAdmin.mockRejectedValue(new Error('不可操作'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === 'team.details')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="admin-member-toggle-admin"]').trigger('click')
    await wrapper.get('[data-testid="confirm-action"]').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledWith('不可操作')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
  })
})
