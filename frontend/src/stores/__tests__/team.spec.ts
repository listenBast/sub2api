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

  it('keeps the legacy financial restriction flag off for active members', async () => {
    getContext.mockResolvedValue({ role: 'member', membership_status: 'active', financial_restricted: false })
    const store = useTeamStore()
    await store.fetchContext()
    expect(store.isMember).toBe(true)
    expect(store.financialRestricted).toBe(false)
  })

  it('deduplicates concurrent context requests', async () => {
    getContext.mockResolvedValue({ role: 'individual', financial_restricted: false })
    const store = useTeamStore()
    const first = store.fetchContext()
    const second = store.fetchContext()
    await Promise.all([first, second])
    expect(getContext).toHaveBeenCalledTimes(1)
  })

  it('derives owner / admin / manager flags from the role', async () => {
    const store = useTeamStore()
    for (const [role, owner, admin, manager] of [['owner', true, false, true], ['admin', false, true, true], ['member', false, false, false]] as const) {
      getContext.mockResolvedValue({ role, membership_status: 'active', financial_restricted: false })
      await store.fetchContext(true)
      expect([store.isOwner, store.isAdmin, store.isManager]).toEqual([owner, admin, manager])
    }
  })

  it('picks up a role change when the context is force-refreshed', async () => {
    const store = useTeamStore()
    getContext.mockResolvedValue({ role: 'member', membership_status: 'active', financial_restricted: false })
    await store.fetchContext()
    expect(store.isManager).toBe(false)
    getContext.mockResolvedValue({ role: 'admin', membership_status: 'active', financial_restricted: false })
    await store.fetchContext(true)
    expect(store.isManager).toBe(true)
  })
})
