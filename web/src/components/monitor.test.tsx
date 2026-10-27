import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { RunsView } from './monitor'

function run(status = 'running', number = 1) {
  return {
    id: String(number),
    number,
    status,
    mode: 'live',
    templateVersion: 'v1',
    startedAt: '2026-10-27T00:00:00Z',
    fixedEnd: '2026-10-27T00:00:00Z',
    finishedAt: '',
    sources: {
      arxiv: {
        pages: 2,
        attempts: 1,
        acquired: false,
        reviewed: false,
        published: false,
        failed: status === 'completed_with_errors',
        lastError: status === 'completed_with_errors' ? 'source returned HTTP 503' : undefined,
      },
    },
  }
}
function response(items = [run()]) {
  return new Response(JSON.stringify({ code: 'OK', message: '', data: { items } }), { status: 200 })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}
let fetcher: ReturnType<typeof vi.fn>
const clients: QueryClient[] = []
beforeEach(() => {
  clearSession()
  fetcher = vi.fn().mockImplementation(async () => response())
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.useRealTimers()
})
function mount(client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  clients.push(client)
  return {
    client,
    ...render(
      <QueryClientProvider client={client}>
        <RunsView />
      </QueryClientProvider>,
    ),
  }
}

describe('scan progress visibility', () => {
  it('renders terminal failure, retry wait, cancellation and quarantine distinctly in Chinese', async () => {
    fetcher.mockResolvedValue(
      response([
        run('completed_with_errors'),
        run('retry_wait', 2),
        run('canceled', 3),
        run('completed_with_quarantine', 4),
        run('completed', 5),
      ]),
    )
    mount()
    expect(await screen.findByText('已结束 · 存在失败')).toHaveClass('text-red-700')
    expect(screen.getByText('等待重试')).toBeVisible()
    expect(screen.getByText('已取消')).toHaveClass('text-secondary-foreground')
    expect(screen.getByText('完成 · 有隔离记录')).toHaveClass('text-amber-800')
    expect(screen.getByText('已完成')).toHaveClass('text-emerald-700')
    expect(screen.getByText('来源处理失败')).toBeVisible()
    expect(screen.getByText(/最近错误：source returned HTTP 503/)).toBeVisible()
    expect(screen.getByText(/不代表整个扫描窗口已完成/)).toBeVisible()
  })

  it('refreshes manually without launching scans, disables duplicate clicks and shows progress', async () => {
    mount()
    await screen.findByText('第 1 次扫描')
    const pending = deferred<Response>()
    fetcher.mockReturnValueOnce(pending.promise)
    fireEvent.click(screen.getByRole('button', { name: '刷新扫描状态' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '刷新扫描状态' })).toBeDisabled())
    expect(screen.getByText('正在刷新扫描状态…')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '刷新扫描状态' }))
    expect(fetcher).toHaveBeenCalledTimes(2)
    await act(async () => pending.resolve(response([run('completed')])))
    expect(await screen.findByText('已完成')).toBeVisible()
    expect(screen.getByRole('button', { name: '刷新扫描状态' })).toBeEnabled()
    for (const [path, init] of fetcher.mock.calls) {
      expect(path).toBe('/api/v1/monitor/runs')
      expect(init.method).toBeUndefined()
    }
  })

  it('preserves last successful records after refresh failure and supports recovery', async () => {
    mount()
    await screen.findByText('第 1 次扫描')
    fetcher.mockRejectedValueOnce(new Error('offline'))
    fireEvent.click(screen.getByRole('button', { name: '刷新扫描状态' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('可能不是最新进度')
    expect(screen.getByText('第 1 次扫描')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '刷新扫描状态' }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    expect(screen.getByText('第 1 次扫描')).toBeVisible()
  })

  it('shows initial errors rather than an empty history, then retries successfully', async () => {
    fetcher.mockRejectedValueOnce(new Error('offline'))
    mount()
    expect(await screen.findByRole('alert')).toHaveTextContent('暂时没能读取数据')
    expect(screen.queryByText('还没有扫描记录')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
    expect(await screen.findByText('第 1 次扫描')).toBeVisible()
  })

  it('polls once per minute while mounted and stops polling after leaving the view', async () => {
    const view = mount()
    await screen.findByText('第 1 次扫描')
    vi.useFakeTimers()
    // Re-establish the observer timer under the controlled clock.
    view.unmount()
    const next = mount(view.client)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    const calls = fetcher.mock.calls.length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(59_000)
    })
    expect(fetcher).toHaveBeenCalledTimes(calls)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(fetcher).toHaveBeenCalledTimes(calls + 1)
    next.unmount()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000)
    })
    expect(fetcher).toHaveBeenCalledTimes(calls + 1)
  })

  it('aborts a late refresh on logout and never restores old-session history into new cache', async () => {
    const view = mount()
    await screen.findByText('第 1 次扫描')
    const pending = deferred<Response>()
    fetcher.mockReturnValueOnce(pending.promise)
    fireEvent.click(screen.getByRole('button', { name: '刷新扫描状态' }))
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2))
    const oldSignal = fetcher.mock.calls[1][1].signal as AbortSignal
    view.unmount()
    clearSession()
    view.client.clear()
    fetcher.mockResolvedValue(response([run('completed', 2)]))
    mount(view.client)
    expect(await screen.findByText('第 2 次扫描')).toBeVisible()
    expect(oldSignal.aborted).toBe(true)
    await act(async () => pending.resolve(response([run('failed', 1)])))
    expect(screen.queryByText('第 1 次扫描')).not.toBeInTheDocument()
    expect(
      view.client.getQueryData<{ items: { number: number }[] }>(['runs'])?.items[0].number,
    ).toBe(2)
  })
})
