import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const fetchContext = vi.hoisted(() => vi.fn())

vi.mock('@/stores', () => ({
  useTeamStore: () => ({ fetchContext })
}))

import { useTeamRoleSync } from '@/composables/useTeamRoleSync'

function mountWith(options: Parameters<typeof useTeamRoleSync>[0] = {}) {
  return mount(defineComponent({
    setup() { useTeamRoleSync(options); return () => null }
  }))
}

describe('useTeamRoleSync', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-02T00:00:00Z'))
    fetchContext.mockReset()
    fetchContext.mockResolvedValue({})
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('refreshes the team context on focus, forcing past the request cache', () => {
    const wrapper = mountWith()
    window.dispatchEvent(new Event('focus'))
    expect(fetchContext).toHaveBeenCalledTimes(1)
    expect(fetchContext).toHaveBeenCalledWith(true)
    wrapper.unmount()
  })

  it('debounces bursts of focus events but refreshes again after the gap', () => {
    const wrapper = mountWith({ minGapMs: 3000 })
    window.dispatchEvent(new Event('focus'))
    window.dispatchEvent(new Event('focus'))
    expect(fetchContext).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(3500)
    window.dispatchEvent(new Event('focus'))
    expect(fetchContext).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('polls while the page is visible', () => {
    const wrapper = mountWith({ intervalMs: 10_000 })
    vi.advanceTimersByTime(10_000)
    expect(fetchContext).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(10_000)
    expect(fetchContext).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('swallows refresh failures so the UI is not interrupted', async () => {
    fetchContext.mockRejectedValue(new Error('offline'))
    const wrapper = mountWith()
    expect(() => window.dispatchEvent(new Event('focus'))).not.toThrow()
    await Promise.resolve()
    wrapper.unmount()
  })

  it('does nothing when disabled and stops listening after unmount', () => {
    const disabled = mountWith({ enabled: () => false, intervalMs: 1000 })
    window.dispatchEvent(new Event('focus'))
    vi.advanceTimersByTime(5000)
    expect(fetchContext).not.toHaveBeenCalled()
    disabled.unmount()

    const wrapper = mountWith({ intervalMs: 1000 })
    wrapper.unmount()
    window.dispatchEvent(new Event('focus'))
    vi.advanceTimersByTime(5000)
    expect(fetchContext).not.toHaveBeenCalled()
  })
})
