import { describe, expect, it } from 'vitest'
import { defaultFilters } from './types'
import { encodeFilters, filterFacets, restoreFilters, getFilterSpecs } from './filters'
import { facets } from '../test/fixtures'
import { safeLink } from './utils'
describe('backend taxonomy transport', () => {
  it('uses exact imported dimensions, not model classification', () => {
    const result = encodeFilters({
      ...defaultFilters,
      method: '原创研究',
      direction: '医学影像',
      task: '分类',
      role: '核心方法',
      tier: '核心库',
    })
    expect(result.dimension).toBe(
      JSON.stringify({
        paper_types: '原创研究',
        biomedical_domains: '医学影像',
        display_scope: '核心库',
        technical_tasks: '分类',
        ai_leadership: '核心方法',
      }),
    )
    expect(result).not.toHaveProperty('method')
    expect(result).not.toHaveProperty('role')
  })
  it('uses world model display taxonomy prepared by backend importer', () => {
    const result = encodeFilters({
      ...defaultFilters,
      topic: 'biomed-worldmodel',
      method: '原创研究',
      reviewStatus: '已读摘要',
    })
    expect(JSON.parse(String(result.dimension))).toEqual({
      display_type: '原创研究',
      display_review_status: '已读摘要',
    })
  })
  it('reads exact facet labels and does not synthesize missing classifications', () => {
    expect(filterFacets(facets, 'biomed-ai', 'method', 'methods')).toEqual([
      { value: '原创研究', count: 1 },
    ])
    expect(filterFacets(undefined, 'biomed-ai', 'method', 'methods')).toEqual([])
  })
  it('restores dynamic topic slugs and uses only supported generic dimensions', () => {
    expect(restoreFilters({ topic: 'custom-research', collectionScope: 'excluded' })).toEqual({
      topic: 'custom-research',
      collectionScope: 'excluded',
    })
    expect(getFilterSpecs('custom-research').map((item) => item.key)).toEqual([
      'year',
      'venue',
      'manualLabel',
    ])
    expect(encodeFilters({ topic: 'custom-research', year: '2026' })).toEqual({
      topic: 'custom-research',
      year: '2026',
    })
  })
  it('encodes manual labels separately from immutable imported categories', () => {
    const result = encodeFilters({ ...defaultFilters, manualLabel: '需复核', method: '原创研究' })
    expect(JSON.parse(String(result.dimension))).toEqual({
      manual_labels: '需复核',
      paper_types: '原创研究',
    })
    expect(result).not.toHaveProperty('manualLabel')
    expect(
      filterFacets(
        { ...facets, dimensions: { manual_labels: [{ value: '需复核', count: 1 }] } },
        'custom-topic',
        'manualLabel',
        'categories',
      ),
    ).toEqual([{ value: '需复核', count: 1 }])
  })
  it('rejects invalid saved enums and pagination values', () => {
    expect(
      restoreFilters({
        topic: 'invalid/topic',
        page: -10,
        hidden: 'everything',
        sort: 'unsafe',
        readStatus: 'reviewed',
        q: '<script>',
      }),
    ).toEqual({ q: '<script>' })
  })
  it('keeps all nine biomedical controls in the original order and eight LLM controls', () => {
    expect(getFilterSpecs('biomed-ai').map((item) => item.label)).toEqual([
      '论文类型',
      '生物医学方向',
      'AI方法',
      '期刊/会议范围',
      '技术任务',
      'AI的作用',
      '发表年份',
      '收录状态',
      '具体期刊/会议',
    ])
    expect(getFilterSpecs('llm-agent').map((item) => item.label)).toEqual([
      '论文类型',
      '构建/应用方向',
      '期刊/会议范围',
      '技术任务',
      'LLM/Agent的作用',
      '发表年份',
      '收录状态',
      '具体期刊/会议',
    ])
  })
  it('transports AI method and exact original collection independently', () => {
    expect(
      JSON.parse(
        String(
          encodeFilters({ ...defaultFilters, aiMethod: '深度学习', status: '正式纳入' }).dimension,
        ),
      ),
    ).toEqual({ ai_methods: '深度学习', collection: '正式纳入' })
    expect(
      JSON.parse(
        String(
          encodeFilters({ ...defaultFilters, topic: 'llm-agent', status: '预印本候选' }).dimension,
        ),
      ),
    ).toEqual({ display_collection: '预印本候选' })
  })
  it('does not turn unsafe evidence URLs into links', () => {
    expect(safeLink('javascript:alert(1)')).toBeUndefined()
    expect(safeLink('data:text/html,x')).toBeUndefined()
    expect(safeLink('file:///etc/passwd')).toBeUndefined()
    expect(safeLink('https://user:pass@example.com')).toBeUndefined()
    expect(safeLink('https://example.com')).toBe('https://example.com/')
  })
})
