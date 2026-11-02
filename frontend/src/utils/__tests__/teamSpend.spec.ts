import { describe, expect, it } from 'vitest'
import { buildTeamSpendRange } from '@/utils/teamSpend'

describe('buildTeamSpendRange', () => {
  it('uses local midnight for start and the next local midnight (exclusive) for end', () => {
    const range = buildTeamSpendRange('2026-10-01', '2026-10-31')
    expect(range.start_date).toBe(new Date(2026, 9, 1, 0, 0, 0, 0).toISOString())
    expect(range.end_date).toBe(new Date(2026, 10, 1, 0, 0, 0, 0).toISOString())
  })

  it('rolls the end date across month and year boundaries', () => {
    expect(buildTeamSpendRange(undefined, '2026-12-31').end_date).toBe(new Date(2027, 0, 1).toISOString())
    expect(buildTeamSpendRange(undefined, '2026-02-28').end_date).toBe(new Date(2026, 2, 1).toISOString())
  })

  it('omits missing or malformed bounds', () => {
    expect(buildTeamSpendRange()).toEqual({})
    expect(buildTeamSpendRange('nope', '2026-1-1')).toEqual({})
    expect(buildTeamSpendRange('2026-10-01')).toEqual({ start_date: new Date(2026, 9, 1).toISOString() })
  })
})
