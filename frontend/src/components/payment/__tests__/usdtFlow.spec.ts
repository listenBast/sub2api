import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PaymentMethodSelector from '../PaymentMethodSelector.vue'
import { buildCreateOrderPayload, decidePaymentLaunch, readPaymentRecoverySnapshot } from '../paymentFlow'
import { PROVIDER_CONFIG_FIELDS, PROVIDER_SUPPORTED_TYPES } from '../providerConfig'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('USDT hosted checkout', () => {
  it.each(PROVIDER_SUPPORTED_TYPES.bepusdt)('preserves %s and opens a recoverable cashier/QR flow', (method) => {
    expect(buildCreateOrderPayload({ amount: 14, paymentType: method, orderType: 'balance', origin: 'https://merchant.example', isMobile: false, isWechatBrowser: false }).payment_type).toBe(method)
    const result = decidePaymentLaunch({ order_id: 1, amount: 14, pay_amount: 14, pay_url: 'https://crypto.example/pay/trade', qr_code: 'https://crypto.example/pay/trade', expires_at: '2026-11-17T00:00:00Z' }, { visibleMethod: method, orderType: 'balance', isMobile: false })
    expect(result.kind).toBe('qr_waiting')
    expect(result.recovery.paymentType).toBe(method)
    expect(result.recovery.payUrl).toBe('https://crypto.example/pay/trade')
    expect(result.recovery.orderId).toBe(1)
  })
  it('renders independent chain choices, warning and disabled state', async () => {
    const wrapper = mount(PaymentMethodSelector, { props: { selected: '', methods: PROVIDER_SUPPORTED_TYPES.bepusdt.map((type, i) => ({ type, fee_rate: 0, available: i !== 3 })) } })
    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(4)
    await buttons[0].trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['usdt_bep20'])
    expect(buttons[3].attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('payment.usdtNetworkWarning')
  })
  it('has no default financial policy or receiving addresses', () => {
    const fields = PROVIDER_CONFIG_FIELDS.bepusdt
    expect(fields.filter(field => field.key.endsWith('Address'))).toHaveLength(4)
    for (const field of fields) expect(field.defaultValue).toBeUndefined()
    expect(fields.find(field => field.key === 'apiToken')?.sensitive).toBe(true)
  })
})

describe('USDT exact instructions on mobile and recovery', () => {
  it.each(PROVIDER_SUPPORTED_TYPES.bepusdt)('keeps %s instructions visible without auto-redirect', (method) => {
    const crypto = { method, asset: 'USDT' as const, address: 'mock-receiving-address', amount: '1.234567890123456789' }
    const result = decidePaymentLaunch({ order_id: 9, amount: 14, pay_amount: 14, pay_url: 'https://crypto.example/pay/trade', qr_code: crypto.address, crypto_payment: crypto, expires_at: new Date(Date.now() + 600000).toISOString() }, { visibleMethod: method, orderType: 'balance', isMobile: true })
    expect(result.kind).toBe('qr_waiting')
    expect(result.paymentState.cryptoPayment).toEqual(crypto)
    expect(readPaymentRecoverySnapshot(JSON.stringify(result.recovery))?.cryptoPayment?.amount).toBe(crypto.amount)
    expect(readPaymentRecoverySnapshot(JSON.stringify({ ...result.recovery, cryptoPayment: { ...crypto, amount: 1.23 } }))).toBeNull()
    expect(readPaymentRecoverySnapshot(JSON.stringify({ ...result.recovery, cryptoPayment: { ...crypto, method: 'alipay' } }))).toBeNull()
  })
})
