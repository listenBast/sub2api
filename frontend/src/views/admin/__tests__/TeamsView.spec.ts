import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  overview: vi.fn(),
  get: vi.fn(),
  dashboard: vi.fn(),
  transactions: vi.fn(),
  deleteTeam: vi.fn(),
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
    deleteTeam: mocks.deleteTeam
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
    expect(dialog!.text()).toContain('team.empty')
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
})
