import { describe, expect, it } from 'vitest'
import { normalizeTeamContext, normalizeTeamSummary, type TeamContext, type TeamSummary } from '@/api/team'

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
