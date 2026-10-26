import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { AbstractEnrichment, Paper, SourceObservation } from '../lib/types'
import { paper } from '../test/fixtures'
import { PaperEvidence, PaperOverview } from './paper-content'

// 明确的界面测试夹具；不请求来源，也不伪装成真实论文或真实核验结果。
const enrichment = (): AbstractEnrichment => ({
  observationId: 'test-abstract-observation',
  abstract: 'Explicitly synthetic source abstract used only by UI tests.',
  source: 'Crossref',
  sourceId: '10.0000/test-abstract-recovery',
  sourceUrl: 'https://api.crossref.org/works/10.0000/test-abstract-recovery',
  doi: '10.0000/test-abstract-recovery',
  sourceVersion: 'test-source-v1',
  completeness: 'source-field-complete',
  verificationStatus: 'source-response-verified',
  abstractHash: 'test-abstract-hash',
  responseHash: 'test-response-hash',
  parserVersion: 'test-parser-v1',
  sourceField: 'message.abstract',
  enrichedAt: '2026-10-26T00:00:00Z',
})
const record = (): Paper => ({
  ...structuredClone(paper),
  abstract: '',
  abstractEnrichment: enrichment(),
  memberships: paper.memberships.map((membership) => ({
    ...structuredClone(membership),
    readingNotes: { contribution: '保留的原始阅读笔记' },
  })),
  review: {
    provider: 'mock',
    model: 'test-model',
    promptVersion: 'test-v1',
    reviewedAt: '2026-10-26T00:00:00Z',
    isMock: true,
    scientificReviewed: false,
    inclusionStatus: 'pending-review',
    summary: '明确标识的模拟总结，不是来源摘要',
  },
})
function observation(value: AbstractEnrichment): SourceObservation {
  return {
    id: value.observationId,
    kind: 'historical-abstract-enrichment',
    abstractEvidence: value,
    source: value.source,
    sourceId: value.sourceId,
    version: value.sourceVersion,
    contentHash: value.abstractHash,
    observedAt: value.enrichedAt,
    sourceUpdatedAt: '',
    title: 'Synthetic enrichment evidence for UI tests',
    abstract: value.abstract,
    url: value.sourceUrl,
    doi: value.doi,
    arxivId: '',
    venue: '',
    issns: [],
    publicationEvents: [],
    topics: [],
    raw: {},
  }
}

describe('verified source abstract supplementation', () => {
  it('shows verified source text without claiming paper review, translating, or changing history', async () => {
    const value = record()
    const before = structuredClone(value)
    const user = userEvent.setup()
    render(<PaperOverview paper={value} />)
    expect(screen.getByRole('heading', { name: '补充的来源摘要' })).toBeVisible()
    expect(screen.getByText(value.abstractEnrichment!.abstract)).toBeVisible()
    expect(screen.queryByText(/当前未获取摘要/)).not.toBeInTheDocument()
    expect(
      screen.getByText('已核对保存的来源内容；不代表已完成论文审读或完整性核验。'),
    ).toBeVisible()
    const link = screen.getByRole('link', { name: '查看官方摘要来源' })
    expect(link).toHaveAttribute('href', value.abstractEnrichment!.sourceUrl)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noreferrer noopener')
    expect(screen.getByText('保留的原始阅读笔记')).toBeVisible()
    expect(screen.getByText('模型模拟输出')).toBeVisible()
    expect(screen.queryByText('历史来源中文摘要')).not.toBeInTheDocument()
    expect(screen.getByText('当前未获取中文摘要；模型翻译未接入。')).toBeVisible()
    const provenance = screen.getByText('查看来源与核对依据').closest('details')!
    expect(provenance).not.toHaveAttribute('open')
    await user.click(within(provenance).getByText('查看来源与核对依据'))
    expect(within(provenance).getByText('test-response-hash')).toBeVisible()
    expect(value).toEqual(before)
  })

  it('preserves an existing canonical abstract rather than replacing it with supplementation', () => {
    const value = record()
    value.abstract = 'Original historical abstract retained verbatim.'
    render(<PaperOverview paper={value} />)
    expect(screen.getByText(value.abstract)).toBeVisible()
    expect(screen.queryByText(value.abstractEnrichment!.abstract)).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '补充的来源摘要' })).not.toBeInTheDocument()
  })

  it.each([
    { completeness: 'fragment' },
    { completeness: 'unknown' },
    { completeness: 'complete' },
    { verificationStatus: 'unverified' },
    { verificationStatus: 'secondary-source-only' },
    { abstract: '   ' },
  ])('does not promote fragmentary, unknown, unverified or empty content: %j', (changes) => {
    const value = record()
    Object.assign(value.abstractEnrichment!, changes)
    render(<PaperOverview paper={value} />)
    expect(screen.queryByRole('heading', { name: '补充的来源摘要' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '查看官方摘要来源' })).not.toBeInTheDocument()
    if (value.abstractEnrichment!.abstract.trim()) {
      expect(screen.queryByText(/当前未获取摘要/)).not.toBeInTheDocument()
      expect(screen.getByText(/暂不.*展示/)).toBeVisible()
    } else {
      expect(screen.getByText(/当前未获取摘要/)).toBeVisible()
    }
    expect(screen.getByText('保留的原始阅读笔记')).toBeVisible()
  })

  it.each([
    'javascript:alert(1)',
    'file:///private/source-cache.xml',
    'https://private-user:private-password@example.com/abstract',
    '/private/source-cache.xml',
  ])('never turns unsafe provenance URLs into a link: %s', (sourceUrl) => {
    const value = record()
    value.abstractEnrichment!.sourceUrl = sourceUrl
    const { container } = render(<PaperOverview paper={value} />)
    expect(screen.getByText(value.abstractEnrichment!.abstract)).toBeVisible()
    expect(screen.queryByRole('link', { name: '查看官方摘要来源' })).not.toBeInTheDocument()
    expect(screen.getByText('来源链接待补充')).toBeVisible()
    expect(container.innerHTML).not.toContain(sourceUrl)
  })

  it('renders source text as text rather than markup', () => {
    const value = record()
    value.abstractEnrichment!.abstract = '<img src=x onerror="alert(1)"> a test source string'
    const { container } = render(<PaperOverview paper={value} />)
    expect(screen.getByText(value.abstractEnrichment!.abstract)).toBeVisible()
    expect(container.querySelector('img')).toBeNull()
  })
})

describe('abstract enrichment evidence privacy and boundaries', () => {
  it('shows public provenance instead of raw cache paths, even after expanding evidence', async () => {
    const value = record()
    const source = observation(value.abstractEnrichment!)
    const privatePath = '/private/research/cache/response.json'
    source.raw = { cachePath: privatePath, nested: { responseFile: privatePath } }
    Object.assign(source.abstractEvidence!, { cachePath: privatePath })
    value.observations = [source]
    value.observationCount = 1
    const user = userEvent.setup()
    const { container } = render(<PaperEvidence paper={value} />)
    await user.click(screen.getByText('展开版本内容与日期依据'))
    await user.click(screen.getByText('查看来源与核对依据'))
    expect(screen.getByText('历史摘要恢复')).toBeVisible()
    expect(screen.getByText('test-parser-v1')).toBeVisible()
    expect(screen.getByText('test-abstract-hash')).toBeVisible()
    expect(screen.getByText('test-response-hash')).toBeVisible()
    expect(screen.queryByText('查看该版本原始字段')).not.toBeInTheDocument()
    expect(container.innerHTML).not.toContain(privatePath)
    expect(container.innerHTML).not.toContain('cachePath')
  })

  it.each([
    ['fragment', 'source-response-verified', '来源摘要片段或待核内容'],
    ['source-field-complete', 'unverified', '待核来源摘要内容'],
  ])(
    'keeps %s / %s evidence distinct from a verified main abstract',
    async (completeness, verificationStatus, heading) => {
      const value = record()
      Object.assign(value.abstractEnrichment!, { completeness, verificationStatus })
      value.observations = [observation(value.abstractEnrichment!)]
      const user = userEvent.setup()
      const { unmount } = render(<PaperOverview paper={value} />)
      expect(screen.queryByText(value.abstractEnrichment!.abstract)).not.toBeInTheDocument()
      unmount()
      render(<PaperEvidence paper={value} />)
      await user.click(screen.getByText('展开版本内容与日期依据'))
      expect(screen.getByRole('heading', { name: heading })).toBeVisible()
      expect(screen.getByText(value.abstractEnrichment!.abstract)).toBeVisible()
      expect(screen.queryByRole('heading', { name: '补充的来源摘要' })).not.toBeInTheDocument()
    },
  )
})
