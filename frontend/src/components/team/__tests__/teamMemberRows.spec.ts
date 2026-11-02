import { describe, expect, it } from 'vitest'
import type { TeamMember, TeamSummary } from '@/api/team'
import { buildTeamMemberRows, teamMemberRole } from '../teamMemberRows'

const member = (over: Partial<TeamMember> = {}): TeamMember => ({
  membership_id: 7, user_id: 8, email: 'm@example.com', username: 'm', remark: '', status: 'active', is_admin: false,
  balance: 1, team_balance: 2, total_balance: 3, frozen_balance: 0, concurrency: 1, rpm_limit: 0, created_at: '2026-07-28T00:00:00Z', ...over
})
const team = (members: TeamMember[] | null): Pick<TeamSummary, 'owner' | 'members' | 'created_at'> => ({
  owner: { user_id: 1, email: 'o@example.com', username: 'boss', balance: 9.5 },
  members: members as TeamMember[],
  created_at: '2026-07-01T00:00:00Z'
})

describe('buildTeamMemberRows', () => {
  it('puts the owner first, followed by the real members unchanged', () => {
    const rows = buildTeamMemberRows(team([member(), member({ membership_id: 9, user_id: 10, is_admin: true })]))
    expect(rows.map((row) => row.user_id)).toEqual([1, 8, 10])
    expect(rows[0]).toMatchObject({ is_owner: true, email: 'o@example.com', username: 'boss', balance: 9.5, total_balance: 9.5, membership_id: 0 })
    expect(rows.slice(1).every((row) => !row.is_owner)).toBe(true)
  })

  it('still returns the owner row when members is null or empty, and nothing without a team', () => {
    expect(buildTeamMemberRows(team(null))).toHaveLength(1)
    expect(buildTeamMemberRows(team([]))).toHaveLength(1)
    expect(buildTeamMemberRows(null)).toEqual([])
    expect(buildTeamMemberRows(undefined)).toEqual([])
  })
})

describe('teamMemberRole', () => {
  it('maps the owner row, admins and plain members', () => {
    const [owner, plain, admin] = buildTeamMemberRows(team([member(), member({ is_admin: true })]))
    expect(teamMemberRole(owner)).toBe('owner')
    expect(teamMemberRole(plain)).toBe('member')
    expect(teamMemberRole(admin)).toBe('admin')
  })
})
