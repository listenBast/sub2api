import type { TeamMember, TeamSummary } from '@/api/team'

/** 成员列表里的一行：真实成员，或由主账号合成的第一行（is_owner）。 */
export type TeamMemberRow = TeamMember & { is_owner?: boolean }

/**
 * 后端 team.members 只包含成员关系，不含主账号本人。
 * 这里把主账号合成为第一行，让用户端和管理端的成员列表都能看到「主账号」。
 * 主账号的余额就是团队资金池，没有“团队额度”概念，也没有成员关系可操作。
 */
export function buildTeamMemberRows(team?: Pick<TeamSummary, 'owner' | 'members' | 'created_at'> | null): TeamMemberRow[] {
  if (!team) return []
  const owner = team.owner
  const ownerRow: TeamMemberRow = {
    membership_id: 0,
    user_id: owner.user_id,
    email: owner.email,
    username: owner.username,
    remark: '',
    status: 'active',
    is_admin: false,
    is_owner: true,
    balance: owner.balance,
    team_balance: 0,
    total_balance: owner.balance,
    frozen_balance: 0,
    concurrency: 0,
    rpm_limit: 0,
    joined_at: team.created_at,
    created_at: team.created_at
  }
  return [ownerRow, ...(team.members ?? [])]
}

export function teamMemberRole(member: TeamMemberRow): 'owner' | 'admin' | 'member' {
  if (member.is_owner) return 'owner'
  return member.is_admin ? 'admin' : 'member'
}
