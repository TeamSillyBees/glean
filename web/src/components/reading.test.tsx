import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PaperCard } from './papers'
import { TooltipProvider } from './ui/tooltip'
import { PaperOverview, PaperEvidence } from './paper-content'
import { SourcesView, type SourcesData } from './source-health'
import { VenuesView } from './monitor'
import { FilterCombobox } from './ui/filter-combobox'
import { paper } from '../test/fixtures'
import type { Paper } from '../lib/types'
const actualRecord = (): Paper => ({
  ...structuredClone(paper),
  isMock: false,
  abstract: '',
  memberships: paper.memberships.map((m) => ({
    ...structuredClone(m),
    isMock: false,
    readingNotes: { contribution: '目录保存的原始贡献笔记', method_summary: '目录保存的方法说明' },
  })),
  review: {
    provider: 'mock',
    model: 'deterministic',
    promptVersion: 'v1',
    reviewedAt: '2026-10-01T00:00:00Z',
    isMock: true,
    scientificReviewed: false,
    inclusionStatus: 'pending-review',
    summary: '仅用于测试的模型输出',
  },
})
describe('historical venue discovery is not current qualification', () => {
  it('labels discovery-only venues without inventing a metric year or verified status', () => {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
    client.setQueryData(['venues', 'llm-agent', ''], {
      items: [
        {
          id: 'historical-discovery',
          name: 'Historical discovery journal',
          kind: 'journal',
          jif: 14,
          ccf: '',
          metricYear: 0,
          source: 'Historical evidence',
          registryVersion: 'v3',
          qualificationStatus: 'historical-curated-discovery',
        },
      ],
    })
    render(
      <QueryClientProvider client={client}>
        <VenuesView topic="llm-agent" q="" />
      </QueryClientProvider>,
    )
    expect(screen.getByText('1 条期刊 / 会议证据记录（含不同版本）')).toBeInTheDocument()
    const statuses = screen.getAllByText('历史精选发现目标 · 当前资格待核')
    expect(statuses).toHaveLength(2)
    expect(statuses.every((status) => !status.classList.contains('bg-emerald-50'))).toBe(true)
    expect(screen.getByText('指标年份待核验')).toBeInTheDocument()
    expect(screen.getByText('期刊影响因子 14')).toBeInTheDocument()
    expect(screen.queryByText('已核验')).not.toBeInTheDocument()
    expect(screen.queryByText('historical-curated-discovery')).not.toBeInTheDocument()
  })
})
describe('rich reading without fabricated completeness', () => {
  it('shows historical notes on real paper cards without marking the source paper mock', () => {
    const value = actualRecord()
    render(
      <TooltipProvider>
        <PaperCard paper={value} topic="biomed-ai" onOpen={() => {}} />
      </TooltipProvider>,
    )
    expect(screen.getByText('目录保存的原始贡献笔记')).toBeInTheDocument()
    expect(screen.queryByText('MOCK')).not.toBeInTheDocument()
    expect(screen.getByTestId('paper-card')).toHaveAttribute('data-paper-id', value.id)
  })
  it('omits generic pending tags without changing the stored categories', () => {
    const value = actualRecord()
    value.publicationType = '其他/待核'
    value.memberships[0].dimensions!.technical_tasks = ['其他/待核', '精确任务标签']
    render(
      <TooltipProvider>
        <PaperCard paper={value} topic="biomed-ai" onOpen={() => {}} />
      </TooltipProvider>,
    )
    expect(screen.queryByText('其他/待核')).not.toBeInTheDocument()
    expect(screen.getByText('精确任务标签')).toBeInTheDocument()
    expect(value.memberships[0].dimensions!.technical_tasks).toContain('其他/待核')
  })
  it('keeps missing abstract, original notes and model mock output distinct', () => {
    render(<PaperOverview paper={actualRecord()} />)
    expect(screen.getByText(/当前未获取摘要/)).toBeInTheDocument()
    expect(screen.getByText('目录保存的方法说明')).toBeInTheDocument()
    expect(screen.getByText('模型模拟输出')).toBeInTheDocument()
    expect(screen.getByText('查看明确标记的模拟总结').closest('details')).not.toHaveAttribute(
      'open',
    )
    expect(screen.getByText('正式在线发表')).toBeInTheDocument()
    expect(screen.queryByText(paper.dateProvenance)).not.toBeInTheDocument()
    expect(screen.getByText('卷期发表')).toBeInTheDocument()
  })
  it('only displays a supplied Chinese abstract and preserves evidence date precision', () => {
    const value = actualRecord()
    value.abstract = 'Actual source abstract'
    value.abstractZh = '原目录真实保存的中文摘要'
    value.evidence[0].date = '2025-03'
    const { unmount } = render(<PaperOverview paper={value} />)
    expect(screen.getByText(value.abstractZh)).toBeInTheDocument()
    expect(screen.getByText('历史来源中文摘要')).toBeInTheDocument()
    unmount()
    render(<PaperEvidence paper={value} />)
    expect(screen.getByText('2025-03')).toBeInTheDocument()
    expect(screen.getByText('查看原始证据字段').closest('details')).not.toHaveAttribute('open')
  })
  it('shows source content versions without calling acceptance a formal publication', async () => {
    const value = actualRecord()
    value.observationCount = 1
    value.observations = [
      {
        id: 'obs-1',
        source: 'Crossref',
        sourceId: 'source-1',
        version: 'v2',
        contentHash: 'abc',
        observedAt: '2026-10-02T00:00:00Z',
        sourceUpdatedAt: '2026-10-01T00:00:00Z',
        title: 'Source title version 2',
        abstract: 'Source abstract version 2',
        url: 'https://example.com/v2',
        doi: '',
        arxivId: '',
        venue: 'Journal',
        issns: [],
        topics: ['biomed-ai'],
        publicationEvents: [
          {
            kind: 'accepted',
            date: '2026-09-01T00:00:00Z',
            evidence: 'Source explicitly says accepted',
          },
        ],
        raw: { version: 2 },
      },
    ]
    const user = userEvent.setup()
    render(<PaperEvidence paper={value} />)
    await user.click(screen.getByText('展开版本内容与日期依据'))
    expect(screen.getByText('接收日期')).toBeVisible()
    expect(screen.getByText('Source abstract version 2')).toBeVisible()
    expect(screen.queryByText('模拟文献')).not.toBeInTheDocument()
  })
})
describe('backend source state and exact searchable facets', () => {
  it('does not turn no failure/no watermark into a healthy or current source', () => {
    const client = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    })
    const state: SourcesData = {
      items: [
        {
          source: 'PubMed',
          enabled: true,
          acquiredThrough: '0001-01-01T00:00:00Z',
          reviewedThrough: '',
          publishedThrough: '',
          nextRetryAt: '',
          consecutiveFailures: 0,
          topicAcquiredThrough: null,
        },
      ],
      schedule: {
        anchorUtc: '',
        nextRunUtc: '2026-10-01T00:00:00Z',
        everyHours: 72,
        completedRuns: 0,
        realFetchEnabled: true,
        schedulerEnabled: false,
        acquisitionMode: 'live',
        reviewMode: 'mock',
      },
    }
    client.setQueryData(['sources'], state)
    render(
      <QueryClientProvider client={client}>
        <SourcesView topic="biomed-ai" />
      </QueryClientProvider>,
    )
    expect(screen.getByText('真实来源采集')).toBeInTheDocument()
    expect(screen.getByText('尚无完成记录')).toBeInTheDocument()
    expect(screen.getAllByText('未知').length).toBeGreaterThan(0)
    expect(screen.getByText('定时计划未启用')).toBeInTheDocument()
    expect(screen.queryByText('Mock 开发模式')).not.toBeInTheDocument()
  })
  it('keeps completed acquisition with quarantined records amber rather than green success', () => {
    const client = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity, retry: false } },
    })
    const state: SourcesData = {
      counts: { quarantinedCandidates: 2 },
      items: [
        {
          source: 'EuropePMC',
          enabled: true,
          quarantined: 2,
          handledThrough: '2026-10-01T00:00:00Z',
          acquiredThrough: '2026-10-01T00:00:00Z',
          reviewedThrough: '',
          publishedThrough: '',
          nextRetryAt: '',
          consecutiveFailures: 0,
          topicAcquiredThrough: { 'biomed-ai': '2026-10-01T00:00:00Z' },
        },
      ],
      schedule: {
        anchorUtc: '',
        nextRunUtc: '',
        everyHours: 72,
        completedRuns: 1,
        realFetchEnabled: true,
        acquisitionMode: 'live',
        reviewMode: 'mock',
        schedulerEnabled: false,
      },
    }
    client.setQueryData(['sources'], state)
    render(
      <QueryClientProvider client={client}>
        <SourcesView topic="biomed-ai" />
      </QueryClientProvider>,
    )
    expect(screen.getByText('2 条隔离待处理')).toHaveClass('bg-amber-50')
    expect(screen.queryByText('已有完整窗口')).not.toBeInTheDocument()
    expect(screen.getByText('处理窗口（含隔离）')).toBeInTheDocument()
  })
  it('finds a rare exact backend label beyond the first 100 options', async () => {
    const user = userEvent.setup()
    let selected = ''
    render(
      <FilterCombobox
        label="技术任务"
        value=""
        onValueChange={(value) => {
          selected = value
        }}
        options={Array.from({ length: 150 }, (_, i) => ({
          value: `原始任务-${i}`,
          label: `原始任务-${i}`,
          count: i + 1,
        }))}
      />,
    )
    await user.click(screen.getByRole('combobox', { name: '技术任务' }))
    await user.type(screen.getByRole('combobox', { name: '查找技术任务选项' }), '原始任务-149')
    await user.click(
      within(screen.getByRole('listbox')).getByRole('option', { name: /原始任务-149/ }),
    )
    expect(selected).toBe('原始任务-149')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })
})
