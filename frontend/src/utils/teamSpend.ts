import type { TeamSpendRangeParams } from '@/api/team'

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/

/**
 * 把日期选择器给出的本地日期（YYYY-MM-DD，起止都含当天）转换为后端使用的时间范围：
 * start = 起始日本地 0 点，end = 结束日次日本地 0 点（后端 end 为开区间）。
 * 用 RFC3339 带上浏览器时区，保证“某一天”按用户所在时区而不是 UTC 划分。
 */
export function buildTeamSpendRange(startDate?: string, endDate?: string): TeamSpendRangeParams {
  const params: TeamSpendRangeParams = {}
  const start = parseLocalDate(startDate)
  if (start) params.start_date = start.toISOString()
  const end = parseLocalDate(endDate)
  if (end) {
    end.setDate(end.getDate() + 1)
    params.end_date = end.toISOString()
  }
  return params
}

function parseLocalDate(value?: string): Date | null {
  if (!value) return null
  const match = DATE_ONLY.exec(value)
  if (!match) return null
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]), 0, 0, 0, 0)
}
