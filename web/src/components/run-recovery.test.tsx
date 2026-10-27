import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { RunsView } from './monitor'
import type { Run } from './run-types'

function failedRun(id = '11'): Run {
  return {
    id,
    number: 1,
    status: 'completed_with_errors',
    mode: 'live',
    templateVersion: 'v1',
    startedAt: '2026-10-27T00:00:00Z',
    fixedEnd: '2026-10-27T00:00:00Z',
    finishedAt: '2026-10-27T01:00:00Z',
    sources: {
      europepmc: {
        pages: 2,
        attempts: 1,
        acquired: false,
        reviewed: false,
        published: false,
        failed: true,
        retryableFailure: true,
        retryNotBefore: '2026-10-27T02:00:00Z',
        window: { start: '2026-10-26T00:00:00Z', end: '2026-10-27T00:00:00Z' },
      },
    },
  }
}
const ok = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 })
let runs: Run[]
let checkpoints: Record<string, unknown>[]
let fetcher: ReturnType<typeof vi.fn>
let client: QueryClient
function mount() {
  return render(
    <QueryClientProvider client={client}>
      <RunsView />
    </QueryClientProvider>,
  )
}
async function open() {
  fireEvent.click(await screen.findByRole('button', { name: '查看恢复诊断' }))
  await waitFor(() => expect(screen.queryByText('正在核对当前来源状态…')).not.toBeInTheDocument())
}
beforeEach(() => {
  clearSession()
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-10-27T14:00:00Z'))
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  runs = [failedRun()]
  checkpoints = [{ source: 'europepmc', nextRetryAt: '2026-10-27T02:00:00Z', blocked: false }]
  fetcher = vi.fn(async (path: string) =>
    ok({ items: path.endsWith('/runs') ? runs : checkpoints }),
  )
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => {
  client.clear()
  vi.restoreAllMocks()
})

describe('read-only failed-run recovery evidence', () => {
  it('loads current checkpoints only on disclosure and gives bounded exact-source parameters without executing', async () => {
    runs[0].sources.arxiv = { ...runs[0].sources.europepmc, failed: false, published: true }
    mount()
    await screen.findByText('第 1 次扫描')
    expect(fetcher).toHaveBeenCalledTimes(1)
    await open()
    expect(screen.getByText('--retry-failed 11')).toBeVisible()
    expect(screen.getByText('--sources arxiv,europepmc')).toBeVisible()
    expect(screen.getByText(/实际执行还会检查模板/)).toBeVisible()
    for (const [path, init] of fetcher.mock.calls) {
      expect(['/api/v1/monitor/runs', '/api/v1/monitor/sources']).toContain(path)
      expect(init.method).toBeUndefined()
    }
    fireEvent.click(screen.getByRole('button', { name: '收起恢复诊断' }))
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
    await open()
    expect(screen.getByText('--retry-failed 11')).toBeVisible()
  })

  it('does not infer retryability from a 503 error string on legacy or permanent failures', async () => {
    delete runs[0].sources.europepmc.retryableFailure
    runs[0].sources.europepmc.lastError = 'HTTP 503'
    mount()
    await open()
    expect(screen.getByText(/永久错误或旧记录分类未知/)).toBeVisible()
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
  })

  it('honors provider blocks and a future Retry-After, including the shared bioRxiv provider', async () => {
    const progress = runs[0].sources.europepmc
    runs[0].sources = { medrxiv: progress }
    checkpoints = [
      { source: 'medrxiv', nextRetryAt: '', blocked: false },
      { source: 'biorxiv', blocked: true, lastStatusCode: 403 },
    ]
    mount()
    await open()
    expect(screen.getByText(/来源或共享服务访问受限/)).toBeVisible()
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
    checkpoints[1] = { source: 'biorxiv', lastStatusCode: 503, nextRetryAt: '2026-10-28T00:00:00Z' }
    fireEvent.click(screen.getByRole('button', { name: '刷新恢复诊断' }))
    expect(await screen.findByText('仍在退避期，不能提前恢复。')).toBeVisible()
    expect(screen.getByText(/2026-10-28T00:00:00.000Z/)).toBeVisible()
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
  })

  it('links retained parent and child by unique run ID without changing the failed parent result', async () => {
    const child = { ...failedRun('12'), status: 'completed', retryOf: '11' }
    runs[0].retriedBy = '12'
    runs.push(child)
    mount()
    const parent = await screen.findByRole('region', { name: '运行 11' })
    fireEvent.click(within(parent).getByRole('button', { name: '查看关联恢复运行' }))
    expect(screen.getByRole('region', { name: '运行 12' })).toHaveFocus()
    fireEvent.click(screen.getByRole('button', { name: '查看原失败运行' }))
    expect(parent).toHaveFocus()
    expect(within(parent).getByText('已结束 · 存在失败')).toBeVisible()
    await open()
    expect(screen.getByText(/不要为原运行重复申请新预算/)).toBeVisible()
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
  })

  it('shows missing retained ancestry honestly and blocks missing child receipts from creating a new budget', async () => {
    runs[0].retryOf = '9'
    runs[0].retriedBy = '12'
    mount()
    expect(await screen.findAllByText('已不在当前保留历史中，不推测其结果')).toHaveLength(2)
    expect(screen.queryByRole('button', { name: '查看原失败运行' })).not.toBeInTheDocument()
    await open()
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
  })

  it('fails closed on missing checkpoints and another active run', async () => {
    checkpoints = []
    runs.push({ ...failedRun('12'), status: 'running', finishedAt: '' })
    mount()
    await open()
    expect(screen.getByText(/缺少当前来源检查点/)).toBeVisible()
    expect(screen.getByText(/还有未结束运行（12）/)).toBeVisible()
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
  })

  it('removes recovery parameters on refresh error and restores them only after a successful check', async () => {
    mount()
    await open()
    fetcher.mockRejectedValueOnce(new Error('offline'))
    fireEvent.click(screen.getByRole('button', { name: '刷新恢复诊断' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('暂停给出恢复参数')
    expect(screen.queryByText('--retry-failed 11')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '刷新恢复诊断' }))
    expect(await screen.findByText('--retry-failed 11')).toBeVisible()
  })

  it('aborts late source diagnosis on leaving the view and cannot revive a former session', async () => {
    let resolve!: (value: Response) => void
    const pending = new Promise<Response>((done) => {
      resolve = done
    })
    const view = mount()
    await screen.findByText('第 1 次扫描')
    fetcher.mockReturnValueOnce(pending)
    fireEvent.click(screen.getByRole('button', { name: '查看恢复诊断' }))
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2))
    const signal = fetcher.mock.calls[1][1].signal as AbortSignal
    view.unmount()
    clearSession()
    client.clear()
    expect(signal.aborted).toBe(true)
    await act(async () => resolve(ok({ items: checkpoints })))
    expect(client.getQueryData(['sources'])).toBeUndefined()
  })
})
