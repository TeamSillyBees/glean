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
  const mapping = Object.hasOwn(topicDimensions, String(filters.topic))
    ? topicDimensions[filters.topic as TopicSlug]
    : undefined
  const dimension: Record<string, string> = {}
  if (typeof filters.manualLabel === 'string' && filters.manualLabel)
    dimension.manual_labels = filters.manualLabel
  delete result.manualLabel
  for (const [key, sourceKey] of Object.entries(mapping || {})) {
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
  if (key === 'manualLabel') return facets?.dimensions?.manual_labels || []
  const dimension = Object.hasOwn(topicDimensions, topic)
    ? topicDimensions[topic]?.[key]
    : undefined
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
    'manualLabel',
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
  if (typeof saved.topic === 'string' && /^[a-z0-9][a-z0-9-]*$/.test(saved.topic))
    result.topic = saved.topic as TopicSlug
  if (['unread', 'reading', 'read', ''].includes(String(saved.readStatus)))
    result.readStatus = String(saved.readStatus)
  if (['curated', 'pending', 'excluded', 'all'].includes(String(saved.collectionScope)))
    result.collectionScope = saved.collectionScope as Filters['collectionScope']
  if (['exclude', 'include', 'only'].includes(String(saved.hidden)))
    result.hidden = saved.hidden as Filters['hidden']
  if ([20, 40, 100].includes(Number(saved.pageSize))) result.pageSize = Number(saved.pageSize)
  if (
    ['newest', 'oldest', 'title', 'observed', 'source-updated', 'model-priority'].includes(
      String(saved.sort),
    )
  )
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
      help: '按来源目录记录的论文类型筛选。',
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
      help: '按本主题已记录的研究方向或尺度筛选。',
    },
    ...(topic === 'biomed-ai'
      ? [
          {
            key: 'aiMethod' as const,
            label: 'AI方法',
            facet: 'methods' as const,
            help: '按已记录的 AI 方法筛选，可与研究方向、技术任务组合使用。',
          },
        ]
      : []),
    {
      key: 'tier',
      label: '期刊/会议范围',
      facet: 'tiers',
      help: '按期刊影响因子或 CCF 会议评级范围筛选，单篇论文的审核结果另行记录。',
    },
    {
      key: 'task',
      label: '技术任务',
      facet: 'tasks',
      help: '按已记录的技术任务筛选，一篇论文可对应多个任务。',
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
      help: '按目录记录的作用筛选，与论文的审读状态分开。',
    },
    {
      key: 'year',
      label: '发表年份',
      facet: 'years',
      help: '按目录中的发表年份筛选，具体在线发表和卷期日期见论文详情。',
    },
    {
      key: 'status',
      label: '收录状态',
      facet: 'tiers',
      help: '原目录中的纳入、待核或范围外状态，与个人阅读状态无关。',
    },
    {
      key: 'venue',
      label: '具体期刊/会议',
      facet: 'venues',
      help: '输入名称查找具体期刊或会议，可与其他条件组合筛选。',
    },
  ]
  if (!Object.hasOwn(topicDimensions, topic))
    return [
      ...common.filter((spec) => ['year', 'venue'].includes(spec.key)),
      {
        key: 'manualLabel',
        label: '人工标签',
        facet: 'categories',
        help: '按当前主题的人工审核标签筛选，与来源原分类分开。',
      },
    ]
  return common
}
