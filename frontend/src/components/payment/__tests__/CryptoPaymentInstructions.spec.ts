import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import QRCode from 'qrcode'
import CryptoPaymentInstructions from '../CryptoPaymentInstructions.vue'

const copy = vi.hoisted(() => vi.fn())
vi.mock('qrcode', () => ({ default: { toCanvas: vi.fn().mockResolvedValue(undefined) } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
const address = '0x1111111111111111111111111111111111111111'
const amount = '1.234567890123456789'

beforeEach(() => { vi.clearAllMocks() })
describe('USDT address-only QR', () => {
  it.each(['usdt_bep20', 'usdt_polygon', 'usdt_trc20', 'usdt_erc20'])('shows exact %s instructions without a wallet URI or CNY quantity', async (method) => {
    const wrapper = mount(CryptoPaymentInstructions, { props: { details: { method, asset: 'USDT', address, amount } } })
    await flushPromises()
    expect(QRCode.toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), address, expect.objectContaining({ margin: 4, width: 240 }))
    expect(wrapper.get('[data-test="usdt-network"]').text()).toBe(`payment.methods.${method}`)
    expect(wrapper.get('[data-test="usdt-amount"]').text()).toBe(`${amount} USDT`)
    expect(wrapper.get('[data-test="usdt-address"]').text()).toBe(address)
    expect(wrapper.text()).toContain('payment.usdtQrHint')
    await wrapper.get('[data-test="copy-usdt-amount"]').trigger('click')
    expect(copy).toHaveBeenLastCalledWith(amount)
    await wrapper.get('[data-test="copy-usdt-address"]').trigger('click')
    expect(copy).toHaveBeenLastCalledWith(address)
    wrapper.unmount()
  })
  it('regenerates on address change and reports QR failure', async () => {
    const wrapper = mount(CryptoPaymentInstructions, { props: { details: { method: 'usdt_bep20', asset: 'USDT', address, amount } } })
    await flushPromises()
    vi.mocked(QRCode.toCanvas).mockRejectedValueOnce(new Error('canvas failure'))
    await wrapper.setProps({ details: { method: 'usdt_bep20', asset: 'USDT', address: address.replaceAll('1', '2'), amount } })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('payment.usdtQrFailed')
    wrapper.unmount()
  })
})
