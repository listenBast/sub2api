import type { ApiKey } from '@/types'

type Translate = (key: string, params?: Record<string, unknown>) => string

/**
 * 用量记录里 API Key 列的显示名。已删除的 Key 仍显示原名称并带“已删除”标记，
 * 例如 “gzt（已删除）”；永远只用名称，不会显示 Key 值。没有 Key 信息时返回空字符串。
 */
export function formatUsageApiKeyName(key: Pick<ApiKey, 'name' | 'deleted_at'> | null | undefined, t: Translate): string {
  const name = key?.name?.trim()
  if (!key || !name) return ''
  return key.deleted_at ? t('usage.apiKeyDeleted', { name }) : name
}
