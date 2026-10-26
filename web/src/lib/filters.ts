import type { Facet, Facets, Filters, TopicSlug } from './types'
// These are imported HTML taxonomy fields, not scientific classifications inferred by this client.
export const topicDimensions: Record<TopicSlug, Record<string, string>> = {
  'biomed-ai': {
    method: 'paper_types',
    direction: 'biomedical_domains',
    tier: 'display_scope',
    task: 'technical_tasks',
    role: 'ai_leadership',
    reviewStatus: 'display_review_status',
  },
  'llm-agent': {
    method: 'paper_types',
    direction: 'relation_direction',
    tier: 'scope',
    task: 'technical_tasks',
    role: 'llm_role',
    reviewStatus: 'display_review_status',
  },
  'biomed-worldmodel': {
    method: 'display_type',
    direction: 'display_scale',
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
  if (['exclude', 'include', 'only'].includes(String(saved.hidden)))
    result.hidden = saved.hidden as Filters['hidden']
  if (['newest', 'oldest', 'title'].includes(String(saved.sort)))
    result.sort = saved.sort as Filters['sort']
  return result
}
