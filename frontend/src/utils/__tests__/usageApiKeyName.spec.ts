import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'

import en from '@/i18n/locales/en/dashboard'
import zh from '@/i18n/locales/zh/dashboard'
import { formatUsageApiKeyName } from '../usageApiKeyName'

function translator(locale: 'zh' | 'en') {
  const i18n = createI18n({ legacy: false, locale, messages: { zh: { usage: zh.usage }, en: { usage: en.usage } } })
  return (key: string, params?: Record<string, unknown>) => i18n.global.t(key, params ?? {}) as string
}

describe('formatUsageApiKeyName', () => {
  it('shows the original name with a deleted marker in zh / en', () => {
    const deleted = { name: 'gzt', deleted_at: '2026-11-01T08:00:00Z' }
    expect(formatUsageApiKeyName(deleted, translator('zh'))).toBe('gzt（已删除）')
    expect(formatUsageApiKeyName(deleted, translator('en'))).toBe('gzt (deleted)')
  })

  it('shows a live key as its plain name', () => {
    expect(formatUsageApiKeyName({ name: 'prod' }, translator('zh'))).toBe('prod')
    expect(formatUsageApiKeyName({ name: 'prod', deleted_at: null }, translator('en'))).toBe('prod')
  })

  it('returns an empty string when there is no key info so callers can fall back to "-"', () => {
    expect(formatUsageApiKeyName(undefined, translator('en'))).toBe('')
    expect(formatUsageApiKeyName(null, translator('en'))).toBe('')
    expect(formatUsageApiKeyName({ name: '  ' }, translator('en'))).toBe('')
  })
})
