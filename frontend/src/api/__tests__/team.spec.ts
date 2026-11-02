import { beforeEach, describe, expect, it, vi } from 'vitest'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: http }))

import teamAPI, { adminTeamsAPI, normalizeTeamContext, normalizeTeamSummary, type TeamContext, type TeamSummary } from '@/api/team'

const emptyTeam = {
  id: 9,
  name: 'Empty Team',
  status: 'active',
  owner: { user_id: 1, email: 'owner@example.com', username: 'owner', balance: 0 },
  active_member_count: 0,
  pending_invite_count: 0,
  exit_pending_count: 0,
  total_balance: 0,
  created_at: '2026-07-28T00:00:00Z'
} as TeamSummary

describe('team response normalization', () => {
  it('adds an empty members array when the backend omits it', () => {
    expect(normalizeTeamSummary(emptyTeam).members).toEqual([])
  })

  it('normalizes the team inside the current user context', () => {
    const context = { role: 'owner', financial_restricted: false, team: emptyTeam } satisfies TeamContext
    expect(normalizeTeamContext(context).team?.members).toEqual([])
  })
})

describe('team admin and spend endpoints', () => {
  beforeEach(() => {
    http.get.mockReset().mockResolvedValue({ data: { ok: true } })
    http.post.mockReset().mockResolvedValue({ data: { ok: true } })
    http.put.mockReset().mockResolvedValue({ data: { ok: true } })
  })

  it('lets the owner set / unset a team admin', async () => {
    await teamAPI.setMemberAdmin(9, true)
    expect(http.put).toHaveBeenCalledWith('/team/members/9/admin', { is_admin: true })
  })

  it('queries per-member spend for the current team with the date range', async () => {
    await teamAPI.getMemberSpendStats({ start_date: '2026-10-01T00:00:00Z', end_date: '2026-11-01T00:00:00Z' })
    expect(http.get).toHaveBeenCalledWith('/team/usage/members', { params: { start_date: '2026-10-01T00:00:00Z', end_date: '2026-11-01T00:00:00Z' } })
  })

  it('exposes site-admin variants addressed by team id', async () => {
    await adminTeamsAPI.setMemberAdmin(3, 9, false)
    expect(http.put).toHaveBeenCalledWith('/admin/teams/3/members/9/admin', { is_admin: false })
    await adminTeamsAPI.memberSpendStats(3, { start_date: 'a' })
    expect(http.get).toHaveBeenCalledWith('/admin/teams/3/usage/members', { params: { start_date: 'a' } })
    await adminTeamsAPI.adjustOwnerBalance(3, { operation: 'subtract', balance: 2, notes: 'n' })
    expect(http.post).toHaveBeenCalledWith('/admin/teams/3/owner-balance', { operation: 'subtract', balance: 2, notes: 'n' })
  })
})
