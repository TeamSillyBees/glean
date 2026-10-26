import { describe, expect, it } from 'vitest'
import { defaultFilters } from './types'
import { encodeFilters, filterFacets, restoreFilters } from './filters'
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
  it('rejects invalid saved enums and pagination values', () => {
    expect(
      restoreFilters({
        topic: 'unknown',
        page: -10,
        hidden: 'everything',
        sort: 'unsafe',
        readStatus: 'reviewed',
        q: '<script>',
      }),
    ).toEqual({ q: '<script>' })
  })
  it('does not turn unsafe evidence URLs into links', () => {
    expect(safeLink('javascript:alert(1)')).toBeUndefined()
    expect(safeLink('data:text/html,x')).toBeUndefined()
    expect(safeLink('https://example.com')).toBe('https://example.com/')
  })
})
