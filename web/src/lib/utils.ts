import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
export const number = (value: number | undefined) =>
  value == null ? '—' : new Intl.NumberFormat('zh-CN').format(value)
// Evidence can contain year-only, month-only or ambiguous original dates.
// Never manufacture day precision or normalize an impossible calendar date.
export function date(value: string | null | undefined): string {
  if (!value || value.startsWith('0001-01-01')) return '待核验'
  const full =
    /^(\d{4})-(\d{2})-(\d{2})(?:T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))?$/.exec(value)
  if (!full) return value
  const [, year, month, day] = full
  const calendar = new Date(`${year}-${month}-${day}T00:00:00Z`)
  const parsed = new Date(value)
  if (
    !Number.isFinite(parsed.getTime()) ||
    calendar.getUTCFullYear() !== Number(year) ||
    calendar.getUTCMonth() + 1 !== Number(month) ||
    calendar.getUTCDate() !== Number(day)
  )
    return value
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    timeZone: 'UTC',
  }).format(parsed)
}
export function safeLink(value: string | undefined): string | undefined {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}
export const readLabels: Record<string, string> = { unread: '未读', reading: '在读', read: '已读' }
export const topicLabels: Record<string, string> = Object.assign(Object.create(null), {
  'biomed-ai': '生物医学 × AI',
  'llm-agent': '大模型知识工程',
  'biomed-worldmodel': '生物医学世界模型',
})

export const reviewLabels: Record<string, string> = {
  reviewed_current: '本轮已审读',
  not_reviewed_current: '本轮尚未审读',
}

export const manualReviewLabels: Record<string, string> = {
  pending: '人工待审核',
  included: '人工纳入',
  excluded: '已移出此主题',
}
