import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { SourcesView, type SourcesData } from './source-health'

const now = Date.parse('2026-10-28T12:00:00Z')
const state: SourcesData = {
  counts: { quarantinedCandidates: 3 },
  schedule: {
    anchorUtc: '',
    nextRunUtc: '',
    everyHours: 72,
    completedRuns: 1,
    realFetchEnabled: true,
    schedulerEnabled: true,
    acquisitionMode: 'live',
  },
  items: [
    {
      source: 'crossref',
      enabled: true,
      acquiredThrough: '2026-10-20T12:00:00Z',
      lastAcquiredAt: '2026-10-28T10:00:00Z',
      reviewedThrough: '',
      publishedThrough: '',
      nextRetryAt: '',
      consecutiveFailures: 0,
      quarantined: 0,
    },
    {
      source: 'europepmc',
      enabled: true,
      acquiredThrough: '',
      reviewedThrough: '',
      publishedThrough: '',
      nextRetryAt: '2026-10-28T13:00:00Z',
      consecutiveFailures: 2,
      lastStatusCode: 503,
      lastError: 'source unavailable',
      quarantined: 3,
    },
  ],
}
const response = (value = state) =>
  new Response(JSON.stringify({ code: 'OK', message: '', data: value }), { status: 200 })
let fetcher: ReturnType<typeof vi.fn>
let client: QueryClient
beforeEach(() => {
  clearSession()
  vi.spyOn(Date, 'now').mockReturnValue(now)
  fetcher = vi.fn().mockResolvedValue(response())
  vi.stubGlobal('fetch', fetcher)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})
function mount() {
  return render(
    <QueryClientProvider client={client}>
      <SourcesView topic="biomed-ai" />
    </QueryClientProvider>,
  )
}
it('distinguishes successful collection time from window age and names failures and quarantine', async () => {
  mount()
  const summary = await screen.findByRole('region', { name: '采集健康概览' })
  expect(summary).toHaveTextContent('最近成功采集2 小时前')
  expect(summary).toHaveTextContent('失败来源1 个Europe PMC')
  expect(summary).toHaveTextContent('待处理记录3 条')
  expect(summary).toHaveTextContent('所有启用来源中的最近一次')
  expect(screen.getByText('HTTP 503 · source unavailable')).toBeVisible()
  expect(screen.getByText(/下一次允许重试/)).toBeVisible()
})
it('does not infer legacy success from watermarks, disabled sources, or an invalid date', async () => {
  fetcher.mockResolvedValue(
    response({
      ...state,
      counts: { quarantinedCandidates: 0 },
      items: [
        { ...state.items[0], lastAcquiredAt: 'invalid' },
        { ...state.items[0], source: 'disabled', enabled: false },
      ],
    }),
  )
  mount()
  const summary = await screen.findByRole('region', { name: '采集健康概览' })
  expect(summary).toHaveTextContent('尚无成功记录')
  expect(summary).toHaveTextContent('失败来源0 个')
  expect(summary).toHaveTextContent('待处理记录0 条')
})
it('keeps cached diagnostics after refresh failure then recovers without launching collection', async () => {
  mount()
  await screen.findByRole('region', { name: '采集健康概览' })
  fetcher.mockRejectedValueOnce(new Error('offline'))
  fireEvent.click(screen.getByRole('button', { name: '刷新状态' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('可能不是最新进度')
  expect(
    within(screen.getByRole('region', { name: '采集健康概览' })).getByText('Europe PMC'),
  ).toBeVisible()
  fetcher.mockResolvedValue(
    response({ ...state, counts: { quarantinedCandidates: 0 }, items: [state.items[0]] }),
  )
  fireEvent.click(screen.getByRole('button', { name: '刷新状态' }))
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  expect(screen.getByRole('status')).toHaveTextContent('已刷新 · 采集状态已更新')
  expect(screen.getByRole('region', { name: '采集健康概览' })).toHaveTextContent('失败来源0 个')
  for (const [path, init] of fetcher.mock.calls) {
    expect(path).toBe('/api/v1/monitor/sources')
    expect(init.method).toBeUndefined()
  }
})

it('shows refresh progress and confirms an unchanged response without starting a scan', async () => {
  mount()
  await screen.findByRole('region', { name: '采集健康概览' })
  let finish!: (value: Response) => void
  fetcher.mockReturnValueOnce(
    new Promise<Response>((resolve) => {
      finish = resolve
    }),
  )
  fireEvent.click(screen.getByRole('button', { name: '刷新状态' }))
  await waitFor(() => expect(screen.getByRole('button', { name: '刷新状态' })).toBeDisabled())
  expect(screen.getByRole('status')).toHaveTextContent('正在刷新采集状态')
  finish(response())
  await waitFor(() =>
    expect(screen.getByRole('status')).toHaveTextContent('已刷新 · 采集状态没有变化'),
  )
  expect(screen.getByRole('button', { name: '刷新状态' })).toBeEnabled()
  expect(fetcher).toHaveBeenCalledTimes(2)
  expect(fetcher.mock.calls[1][1].method).toBeUndefined()
})
