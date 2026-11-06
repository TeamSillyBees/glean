import { describe, expect, it } from 'vitest'
import { exampleSearchRoutes, parseSearchRoutes } from './search-routes'
describe('search route validation', () => {
  const valid = { id: 'test', sources: ['pubmed'], termGroups: [['cancer']] }
  it('accepts empty routes and the documented example', () => {
    expect(parseSearchRoutes('')).toEqual([])
    expect(parseSearchRoutes(JSON.stringify(exampleSearchRoutes))).toEqual(exampleSearchRoutes)
  })
  it.each([
    null,
    {},
    [null],
    [{ ...valid, id: '' }],
    [valid, valid],
    [{ ...valid, id: 'UpperCase' }],
    [{ ...valid, sources: ['unknown'] }],
    [{ ...valid, sources: ['pubmed', 'pubmed'] }],
    [{ ...valid, sources: [] }],
    [{ ...valid, termGroups: [[]] }],
    [{ ...valid, termGroups: [[' ']] }],
    [{ ...valid, termGroups: [['bad\nterm']] }],
    [{ ...valid, name: '中'.repeat(101) }],
    [{ ...valid, fieldMode: 'fulltext' }],
    [{ ...valid, fieldMode: 'title_abstract', sources: ['arxiv'] }],
    [{ ...valid, backfillFrom: 'tomorrow' }],
    [{ ...valid, backfillFrom: '2999-01-01T00:00:00Z' }],
    [{ ...valid, backfillFrom: '2020-01-01' }],
    [{ ...valid, backfillFrom: '2020-02-31T00:00:00Z' }],
    [{ ...valid, unknown: true }],
    Array.from({ length: 21 }, (_, index) => ({ ...valid, id: String(index) })),
    [{ ...valid, termGroups: Array.from({ length: 11 }, () => ['cancer']) }],
    [{ ...valid, termGroups: [Array.from({ length: 51 }, (_, index) => String(index))] }],
  ])('rejects invalid config %#', (input) => {
    expect(() => parseSearchRoutes(JSON.stringify(input))).toThrow()
  })
})
