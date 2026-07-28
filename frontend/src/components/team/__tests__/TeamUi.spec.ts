import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

const messages: Record<string, string> = {
  'team.actions': '操作',
  'team.allocate': '调整额度',
  'team.approveExit': '批准退出',
  'team.rejectExit': '拒绝退出',
  'team.cancelInvite': '撤销邀请',
  'team.remove': '踢出团队',
  'team.editRemark': '修改备注',
  'team.ledger': '资金流水',
  'team.transactionType': '流水类型',
  'team.createdAt': '创建时间',
  'team.amount': '额度',
  'team.relatedMember': '关联成员',
  'team.ownerBalance': '主账号余额',
  'team.memberBalance': '成员余额',
  'team.note': '备注',
  'team.empty': '暂无数据'
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'zh-CN' },
    t: (key: string, params?: Record<string, unknown> | string) => {
      if (key === 'team.recordCount') return `共 ${(params as { count: number }).count} 条记录`
      if (key.startsWith('team.actionsMap.')) return typeof params === 'string' ? params : key
      return messages[key] ?? key
    }
  })
}))

import type { TeamMember, TeamTransaction } from '@/api/team'
import Pagination from '@/components/common/Pagination.vue'
import TeamLedgerPanel from '../TeamLedgerPanel.vue'
import TeamMemberActions from '../TeamMemberActions.vue'

const activeMember: TeamMember = {
  membership_id: 1,
  user_id: 11,
  email: 'member@example.com',
  username: 'member',
  remark: '',
  status: 'active',
  balance: 20,
  frozen_balance: 0,
  concurrency: 5,
  rpm_limit: 60,
  joined_at: '2026-07-27T14:52:00Z',
  created_at: '2026-07-27T14:52:00Z'
}

const transaction: TeamTransaction = {
  id: 1,
  team_id: 2,
  operator_id: 3,
  member_id: 11,
  member_email: 'member@example.com',
  member_username: 'member',
  member_remark: '研发一组',
  action: 'balance_allocated',
  amount: 20,
  owner_balance_before: 100,
  owner_balance_after: 80,
  member_balance_before: 0,
  member_balance_after: 20,
  note: '初始额度',
  created_at: '2026-07-27T14:52:00Z'
}

describe('team member actions', () => {
  it('uses compact API-key-style actions without request-limit controls', async () => {
    const wrapper = mount(TeamMemberActions, { props: { member: activeMember } })

    expect(wrapper.findAll('.member-action')).toHaveLength(3)
    expect(wrapper.text()).toContain('修改备注')
    expect(wrapper.text()).toContain('调整额度')
    expect(wrapper.text()).toContain('踢出团队')
    expect(wrapper.text()).not.toContain('请求限制')
    expect(wrapper.text()).not.toContain('并发')
    expect(wrapper.findAll('.member-action span').every((item) => item.classes().includes('whitespace-nowrap'))).toBe(true)

    await wrapper.findAll('.member-action')[0].trigger('click')
    expect(wrapper.emitted('edit-remark')?.[0]).toEqual([activeMember])

    await wrapper.findAll('.member-action')[1].trigger('click')
    expect(wrapper.emitted('allocate')?.[0]).toEqual([activeMember])
  })

  it('shows complete exit-review actions without truncating their labels', () => {
    const wrapper = mount(TeamMemberActions, {
      props: { member: { ...activeMember, status: 'exit_pending' } }
    })

    expect(wrapper.findAll('.member-action')).toHaveLength(5)
    expect(wrapper.text()).toContain('批准退出')
    expect(wrapper.text()).toContain('拒绝退出')
  })
})

describe('team balance ledger', () => {
  it('renders the styled table and pagination even when there is only one page', () => {
    const wrapper = mount(TeamLedgerPanel, {
      props: {
        items: [transaction],
        page: 1,
        total: 1
      }
    })

    expect(wrapper.text()).toContain('资金流水')
    expect(wrapper.text()).toContain('共 1 条记录')
    expect(wrapper.text()).toContain('初始额度')
    expect(wrapper.text()).toContain('研发一组')
    expect(wrapper.text()).toContain('member@example.com')
    expect(wrapper.text()).toContain('US$0.00')
    expect(wrapper.text()).toContain('US$20.00')
    expect(wrapper.find('table').classes()).toContain('border-collapse')
    expect(wrapper.findComponent(Pagination).exists()).toBe(true)
  })

  it('shows reclaimed balance as a negative member adjustment', () => {
    const wrapper = mount(TeamLedgerPanel, {
      props: {
        items: [{
          ...transaction,
          id: 2,
          action: 'balance_recovered',
          amount: -10,
          owner_balance_before: 60,
          owner_balance_after: 70,
          member_balance_before: 40,
          member_balance_after: 30
        }],
        page: 1,
        total: 1
      }
    })

    expect(wrapper.text()).toContain('-US$10.00')
    expect(wrapper.find('.text-red-600').exists()).toBe(true)
  })
})
