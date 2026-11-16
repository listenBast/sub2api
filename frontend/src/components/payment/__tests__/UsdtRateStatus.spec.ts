import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import UsdtRateStatus from '../UsdtRateStatus.vue'
const { getUSDTRate } = vi.hoisted(() => ({ getUSDTRate: vi.fn() }))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getUSDTRate } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('USDT quote status', () => {
  it('displays exact decimal, source times and safe attribution', async () => {
    const time = new Date().toISOString()
    getUSDTRate.mockResolvedValueOnce({ data: { available: true, quote: { rate: '6.6623951423103795', quoted_at: time, fetched_at: time } } })
    const w = mount(UsdtRateStatus)
    await flushPromises()
    expect(w.text()).toContain('1 USDT = 6.6623951423103795 CNY')
    expect(w.get('a').attributes('href')).toBe('https://www.coingecko.com/')
    expect(w.text()).toContain('usdtQuoteTime')
    w.unmount()
  })
  it('does not display a fallback on failure', async () => {
    getUSDTRate.mockRejectedValueOnce(new Error('offline'))
    const w = mount(UsdtRateStatus)
    await flushPromises()
    expect(w.text()).toContain('usdtRateUnavailable')
    expect(w.text()).not.toContain('1 USDT =')
    w.unmount()
  })
  it('rejects stale display metadata', async () => {
    const time = new Date(Date.now() - 121_000).toISOString()
    getUSDTRate.mockResolvedValueOnce({ data: { available: true, quote: { rate: '6.67', quoted_at: time, fetched_at: time } } })
    const w = mount(UsdtRateStatus)
    await flushPromises()
    expect(w.text()).toContain('usdtRateUnavailable')
    expect(w.text()).not.toContain('1 USDT =')
    w.unmount()
  })
})
