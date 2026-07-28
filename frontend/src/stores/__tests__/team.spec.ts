import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTeamStore } from '@/stores/team'

const { getContext } = vi.hoisted(() => ({ getContext: vi.fn() }))

vi.mock('@/api/team', () => ({
  default: { getContext }
}))

describe('team store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getContext.mockReset()
  })

  it('derives the financial restriction from an active membership', async () => {
    getContext.mockResolvedValue({ role: 'member', membership_status: 'active', financial_restricted: true })
    const store = useTeamStore()
    await store.fetchContext()
    expect(store.isMember).toBe(true)
    expect(store.financialRestricted).toBe(true)
  })

  it('deduplicates concurrent context requests', async () => {
    getContext.mockResolvedValue({ role: 'individual', financial_restricted: false })
    const store = useTeamStore()
    const first = store.fetchContext()
    const second = store.fetchContext()
    await Promise.all([first, second])
    expect(getContext).toHaveBeenCalledTimes(1)
  })
})
