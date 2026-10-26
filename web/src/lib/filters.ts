import type { Facet, Facets, Filters, TopicSlug } from './types'
// These are imported HTML taxonomy fields, not scientific classifications inferred by this client.
export const topicDimensions: Record<TopicSlug, Record<string, string>> = {
  'biomed-ai': {
    method: 'paper_types',
    direction: 'biomedical_domains',
    aiMethod: 'ai_methods',
    status: 'collection',
    tier: 'display_scope',
    task: 'technical_tasks',
    role: 'ai_leadership',
    reviewStatus: 'display_review_status',
  },
  'llm-agent': {
    method: 'paper_types',
    direction: 'relation_direction',
    status: 'display_collection',
    tier: 'scope',
    task: 'technical_tasks',
    role: 'llm_role',
    reviewStatus: 'display_review_status',
  },
  'biomed-worldmodel': {
    method: 'display_type',
    direction: 'display_scale',
    status: 'eligibility_status',
    tier: 'display_scope',
    task: 'display_tasks',
    role: 'display_roles',
    reviewStatus: 'display_review_status',
  },
}
export function encodeFilters(
  filters: Partial<Filters> | Record<string, unknown>,
): Record<string, unknown> {
  const result = { ...filters }
  const mapping = topicDimensions[filters.topic as TopicSlug]
  if (!mapping) return result
  const dimension: Record<string, string> = {}
  for (const [key, sourceKey] of Object.entries(mapping)) {
    if (
      typeof filters[key as keyof typeof filters] === 'string' &&
      filters[key as keyof typeof filters]
    )
      dimension[sourceKey] = String(filters[key as keyof typeof filters])
    delete result[key as keyof typeof result]
  }
  if (Object.keys(dimension).length) Object.assign(result, { dimension: JSON.stringify(dimension) })
  return result
}
export function filterFacets(
  facets: Facets | undefined,
  topic: TopicSlug,
  key: keyof Filters,
  fallback: keyof Facets,
): Facet[] {
  const dimension = topicDimensions[topic]?.[key]
  if (dimension) return facets?.dimensions?.[dimension] || []
  return (facets?.[fallback] as Facet[] | undefined) || []
}
export function restoreFilters(saved: Record<string, unknown>): Partial<Filters> {
  const result: Partial<Filters> = {}
  const textKeys = [
    'q',
    'year',
    'venue',
    'publicationType',
    'category',
    'direction',
    'aiMethod',
    'status',
    'inclusionStatus',
    'source',
    'method',
    'tier',
    'task',
    'role',
    'reviewStatus',
  ] as const
  textKeys.forEach((key) => {
    if (typeof saved[key] === 'string') result[key] = saved[key]
  })
  if (typeof saved.topic === 'string' && saved.topic in topicDimensions)
    result.topic = saved.topic as TopicSlug
  if (['unread', 'reading', 'read', ''].includes(String(saved.readStatus)))
    result.readStatus = String(saved.readStatus)
  if (['curated', 'pending', 'all'].includes(String(saved.collectionScope)))
    result.collectionScope = saved.collectionScope as Filters['collectionScope']
  if (['exclude', 'include', 'only'].includes(String(saved.hidden)))
    result.hidden = saved.hidden as Filters['hidden']
  if ([20, 40, 100].includes(Number(saved.pageSize))) result.pageSize = Number(saved.pageSize)
  if (['newest', 'oldest', 'title', 'observed', 'source-updated'].includes(String(saved.sort)))
    result.sort = saved.sort as Filters['sort']
  return result
}

export interface FilterSpec {
  key: keyof Filters
  label: string
  facet: keyof Facets
  help: string
}
export function getFilterSpecs(topic: TopicSlug): FilterSpec[] {
  const common: FilterSpec[] = [
    {
      key: 'method',
      label: '论文类型',
      facet: 'methods',
      help: '按原目录中的论文类型筛选，不在前端重新分类。',
    },
    {
      key: 'direction',
      label:
        topic === 'llm-agent'
          ? '构建/应用方向'
          : topic === 'biomed-ai'
            ? '生物医学方向'
            : '研究尺度',
      facet: 'directions',
      help: '保留该主题已有的方向或尺度标签。',
    },
    ...(topic === 'biomed-ai'
      ? [
          {
            key: 'aiMethod' as const,
            label: 'AI方法',
            facet: 'methods' as const,
            help: '原目录中的 AI 方法，与生物医学方向和任务分别筛选。',
          },
        ]
      : []),
    {
      key: 'tier',
      label: '期刊/会议范围',
      facet: 'tiers',
      help: '期刊影响因子与 CCF 会议评级是来源门槛，不替代单篇资格核验。',
    },
    {
      key: 'task',
      label: '技术任务',
      facet: 'tasks',
      help: '同一篇论文可以带有多个已记录任务；选择一个精确标签。',
    },
    {
      key: 'role',
      label:
        topic === 'llm-agent'
          ? 'LLM/Agent的作用'
          : topic === 'biomed-ai'
            ? 'AI的作用'
            : '世界模型的作用',
      facet: 'roles',
      help: '使用原目录的作用标签，不将资料获取视为方法审读。',
    },
    {
      key: 'year',
      label: '发表年份',
      facet: 'years',
      help: '原始年份；正式在线发表和卷期日期在详情中分别展示。',
    },
    {
      key: 'status',
      label: '收录状态',
      facet: 'tiers',
      help: '历史纳入、待核或范围外等状态，与个人已读/未读分开。',
    },
    {
      key: 'venue',
      label: '具体期刊/会议',
      facet: 'venues',
      help: '可输入名称快速找到来源，结果与其他筛选条件取交集。',
    },
  ]
  return common
}
