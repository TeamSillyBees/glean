import type { SearchRoute } from './types'
export const searchSources = [
  'crossref',
  'pubmed',
  'europepmc',
  'arxiv',
  'biorxiv',
  'medrxiv',
] as const
const invalidText = (value: string) =>
  new TextEncoder().encode(value).length > 300 || /\p{Cc}/u.test(value)
function validTimestamp(value: string, now: number) {
  const match =
    /^(\d{4})-(\d{2})-(\d{2})T([0-2]\d):([0-5]\d):([0-5]\d)(\.\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(value)
  if (!match || Number(match[1]) < 1000 || Number(match[4]) > 23) return false
  const [year, month, day] = match.slice(1, 4).map(Number)
  if (month < 1 || month > 12 || day < 1 || day > new Date(Date.UTC(year, month, 0)).getUTCDate())
    return false
  return Number.isFinite(Date.parse(value)) && Date.parse(value) <= now
}
// 校验整个输入，不静默丢弃未知配置。
export function parseSearchRoutes(text: string, now = Date.now()): SearchRoute[] {
  let value: unknown
  try {
    value = JSON.parse(text || '[]')
  } catch {
    throw new Error('检索路线必须是有效 JSON 数组。')
  }
  if (!Array.isArray(value) || value.length > 20)
    throw new Error('检索路线必须是数组，最多 20 条。')
  const ids = new Set<string>()
  for (const [index, route] of value.entries()) {
    const fail = (message: string): never => {
      throw new Error(`路线 ${index + 1}：${message}`)
    }
    if (!route || typeof route !== 'object' || Array.isArray(route)) fail('必须是对象。')
    if (
      Object.keys(route).some(
        (key) =>
          !['id', 'name', 'sources', 'termGroups', 'fieldMode', 'backfillFrom'].includes(key),
      )
    )
      fail('含未知字段，请检查拼写。')
    if (typeof route.id !== 'string' || !/^[a-z0-9][a-z0-9-]{0,79}$/.test(route.id))
      fail('id 须为 1–80 位小写字母、数字或连字符，首位不能为连字符。')
    if (ids.has(route.id)) fail('id 不能重复。')
    ids.add(route.id)
    if (route.name !== undefined && (typeof route.name !== 'string' || invalidText(route.name)))
      fail('name 须为不超过 300 字节且不含控制字符的字符串。')
    if (
      !Array.isArray(route.sources) ||
      !route.sources.length ||
      route.sources.some(
        (source: unknown) => !searchSources.includes(source as (typeof searchSources)[number]),
      ) ||
      new Set(route.sources).size !== route.sources.length
    )
      fail('sources 必须包含不重复的受支持来源。')
    if (
      !Array.isArray(route.termGroups) ||
      !route.termGroups.length ||
      route.termGroups.length > 10 ||
      route.termGroups.some(
        (group: unknown) =>
          !Array.isArray(group) ||
          !group.length ||
          group.some(
            (term: unknown) => typeof term !== 'string' || !term.trim() || invalidText(term),
          ),
      )
    )
      fail('termGroups 须为 1–10 个非空词组；每词不超过 300 字节且不含控制字符。')
    if (route.termGroups.reduce((count: number, group: string[]) => count + group.length, 0) > 50)
      fail('每条路线最多 50 个检索词。')
    if (
      route.fieldMode !== undefined &&
      !['', 'default', 'title_abstract'].includes(route.fieldMode)
    )
      fail('fieldMode 仅支持 default 或 title_abstract。')
    if (
      route.fieldMode === 'title_abstract' &&
      route.sources.some((source: string) => !['pubmed', 'europepmc'].includes(source))
    )
      fail('title_abstract 仅支持 pubmed、europepmc。')
    if (
      route.backfillFrom !== undefined &&
      (typeof route.backfillFrom !== 'string' || !validTimestamp(route.backfillFrom, now))
    )
      fail('backfillFrom 必须是非未来的 ISO 时间戳（含时区）。')
  }
  return value as SearchRoute[]
}
export const exampleSearchRoutes: SearchRoute[] = [
  {
    id: 'biomedical',
    name: '生物医学检索',
    sources: ['pubmed', 'europepmc'],
    termGroups: [['cancer', 'tumor'], ['machine learning']],
    fieldMode: 'title_abstract',
  },
  {
    id: 'preprints',
    name: '预印本补充',
    sources: ['arxiv'],
    termGroups: [['cancer'], ['machine learning']],
  },
]
