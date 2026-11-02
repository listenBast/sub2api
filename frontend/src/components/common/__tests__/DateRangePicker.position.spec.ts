import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import DateRangePicker from '../DateRangePicker.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))

const POPOVER_WIDTH = 400
let wrapper: ReturnType<typeof mount<typeof DateRangePicker>>

function stubLayout(triggerLeft: number, triggerWidth: number, viewport: number) {
  Object.defineProperty(document.documentElement, 'clientWidth', { configurable: true, value: viewport })
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    const isContainer = this.classList.contains('relative')
    return { left: isContainer ? triggerLeft : 0, width: isContainer ? triggerWidth : POPOVER_WIDTH, right: 0, top: 0, bottom: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) }
  })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return this.classList.contains('date-picker-dropdown') ? POPOVER_WIDTH : 0
  })
}

async function open(props: Record<string, unknown> = {}) {
  wrapper = mount(DateRangePicker, { props: { startDate: '2026-09-13', endDate: '2026-09-13', ...props }, attachTo: document.body })
  await wrapper.get('.date-picker-trigger').trigger('click')
  await nextTick()
  await nextTick()
  return wrapper.get('[data-testid="date-picker-dropdown"]')
}
const leftOf = (el: { element: Element }) => parseInt((el.element as HTMLElement).style.left, 10)

afterEach(() => { wrapper?.unmount(); vi.restoreAllMocks() })

describe('DateRangePicker popover placement', () => {
  it('stays left-aligned when it fits', async () => {
    stubLayout(100, 200, 1280)
    expect(leftOf(await open())).toBe(0)
  })

  it('shifts left when a trigger at the right end of a toolbar would overflow the viewport (1024px)', async () => {
    stubLayout(900, 120, 1024) // 900 + 400 > 1024 - 8
    const left = leftOf(await open())
    expect(left).toBe(1024 - 8 - POPOVER_WIDTH - 900)
    expect(900 + left + POPOVER_WIDTH).toBeLessThanOrEqual(1024 - 8)
    expect(900 + left).toBeGreaterThanOrEqual(8)
  })

  it('pins to the trigger right edge with align="right"', async () => {
    stubLayout(500, 150, 1280)
    expect(leftOf(await open({ align: 'right' }))).toBe(150 - POPOVER_WIDTH)
  })

  it('keeps the left edge on-screen on a narrow mobile viewport', async () => {
    stubLayout(40, 300, 390) // popover (400) wider than the viewport
    const left = leftOf(await open())
    expect(40 + left).toBe(8)
  })

  it('re-clamps when the window is resized while open', async () => {
    stubLayout(300, 120, 1280)
    const dropdown = await open()
    expect(leftOf(dropdown)).toBe(0)
    stubLayout(300, 120, 600)
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(300 + leftOf(dropdown) + POPOVER_WIDTH).toBeLessThanOrEqual(600 - 8)
  })
})
