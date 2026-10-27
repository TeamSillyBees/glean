import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { QuarantineView, type QuarantineDetail } from './quarantine'

function item(id = '1'): QuarantineDetail {
  return {
    id,
    source: 'arxiv',
    sourceId: `source-${id}`,
    title: `Record ${id}`,
    doi: '',
    arxivId: '',
    pmid: '',
    topics: [],
    reason: 'invalid_source_record',
    quarantinedAt: '2026-10-27T00:00:00Z',
    acquiredAt: '',
    sourceUpdatedAt: '0001-01-01T00:00:00Z',
    runIds: ['run-1'],
    sourceUrl: 'javascript:alert(1)',
    evidence: {
      format: 'json',
      bytes: 999,
      sha256: 'abc123',
      preview: '<img src=x onerror=alert(1)>',
      redacted: true,
      truncated: true,
    },
  }
}
function response(data: unknown) {
  return new Response(JSON.stringify({ code: 'OK', message: '', data }), { status: 200 })
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}
let fetcher: ReturnType<typeof vi.fn<(input: string, options?: RequestInit) => Promise<Response>>>
const clients: QueryClient[] = []
beforeEach(() => {
  clearSession()
  fetcher = vi.fn(async (input: string) => {
    if (input.endsWith('/sources'))
      return response({ items: [{ source: 'arxiv' }, { source: 'pubmed' }] })
    if (input.includes('/quarantine?')) {
      const page = Number(new URL(input, 'http://local').searchParams.get('page'))
      return response({ items: [item(String(page))], total: 21, page, pageSize: 20 })
    }
    return response(item(input.split('/').pop()))
  })
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
})
function mount(initialSource = '') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  return {
    client,
    ...render(
      <QueryClientProvider client={client}>
        <QuarantineView initialSource={initialSource} />
      </QueryClientProvider>,
    ),
  }
}
describe('read-only quarantine evidence', () => {
  it('escapes evidence, labels filtering/truncation and restores focus after close button and Escape', async () => {
    const user = userEvent.setup()
    mount()
    const trigger = await screen.findByRole('button', { name: '查看隔离证据：Record 1' })
    await user.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: '隔离记录证据' })
    expect(await within(dialog).findByText('<img src=x onerror=alert(1)>')).toBeInTheDocument()
    expect(dialog.querySelector('img')).toBeNull()
    expect(within(dialog).getByText('已过滤敏感字段')).toBeInTheDocument()
    expect(within(dialog).getByText('预览已截断')).toBeInTheDocument()
    expect(within(dialog).queryByRole('link')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '关闭隔离证据' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
    await user.click(trigger)
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    await waitFor(() =>
      expect(fetcher.mock.calls.filter(([url]) => url.endsWith('/quarantine/1'))).toHaveLength(2),
    )
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
    expect(
      fetcher.mock.calls.every(
        ([, options]) =>
          !(options as RequestInit)?.method || (options as RequestInit).method === 'GET',
      ),
    ).toBe(true)
  })
  it('paginates and resets page when the source filter changes', async () => {
    const user = userEvent.setup()
    mount()
    await screen.findByText('Record 1')
    await user.click(screen.getByRole('button', { name: '下一页' }))
    await screen.findByText('Record 2')
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
    await user.click(screen.getByRole('combobox', { name: '隔离记录来源' }))
    await user.click(screen.getByRole('option', { name: 'pubmed' }))
    await screen.findByText('Record 1')
    expect(
      fetcher.mock.calls.some(([url]) => url.includes('page=1&pageSize=20&source=pubmed')),
    ).toBe(true)
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
  })
  it('ignores a delayed page response after changing the source', async () => {
    const user = userEvent.setup()
    const late = deferred<Response>()
    const defaultFetch = fetcher.getMockImplementation()!
    fetcher.mockImplementation((url: string) =>
      url.includes('page=2') ? late.promise : defaultFetch(url),
    )
    mount()
    await screen.findByText('Record 1')
    await user.click(screen.getByRole('button', { name: '下一页' }))
    await screen.findByText('正在读取隔离记录…')
    await user.click(screen.getByRole('combobox', { name: '隔离记录来源' }))
    await user.click(screen.getByRole('option', { name: 'pubmed' }))
    await screen.findByText('Record 1')
    await act(async () =>
      late.resolve(
        response({
          items: [{ ...item('2'), title: 'STALE PAGE' }],
          total: 21,
          page: 2,
          pageSize: 20,
        }),
      ),
    )
    expect(screen.queryByText('STALE PAGE')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
  })
  it('does not let closed detail requests replace another record', async () => {
    const late = deferred<Response>()
    const defaultFetch = fetcher.getMockImplementation()!
    fetcher.mockImplementation((url: string) =>
      url.endsWith('/quarantine/1') ? late.promise : defaultFetch(url),
    )
    mount()
    fireEvent.click(await screen.findByRole('button', { name: '查看隔离证据：Record 1' }))
    await screen.findByText('正在读取隔离证据…')
    fireEvent.click(screen.getByRole('button', { name: '关闭隔离证据' }))
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    fireEvent.click(await screen.findByRole('button', { name: '查看隔离证据：Record 2' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText('Record 2')
    await act(async () => late.resolve(response({ ...item('1'), title: 'STALE EVIDENCE' })))
    expect(screen.queryByText('STALE EVIDENCE')).not.toBeInTheDocument()
  })
  it('discards old-session requests and resets state for a fresh workspace', async () => {
    const late = deferred<Response>()
    const defaultFetch = fetcher.getMockImplementation()!
    fetcher.mockImplementation((url: string) =>
      url.endsWith('/quarantine/1') ? late.promise : defaultFetch(url),
    )
    const old = mount('arxiv')
    fireEvent.click(await screen.findByRole('button', { name: '查看隔离证据：Record 1' }))
    await screen.findByText('正在读取隔离证据…')
    clearSession()
    old.client.clear()
    old.unmount()
    mount()
    await screen.findByText('Record 1')
    await act(async () => late.resolve(response({ ...item(), title: 'OLD SESSION EVIDENCE' })))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByText('OLD SESSION EVIDENCE')).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '隔离记录来源' })).toHaveTextContent('全部来源')
  })
  it('shows list and detail errors with retry, and supports empty results', async () => {
    fetcher
      .mockResolvedValueOnce(response({ items: [] }))
      .mockRejectedValueOnce(new Error('list unavailable'))
    mount()
    expect(await screen.findByRole('alert')).toHaveTextContent('暂时无法连接后端')
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
    await screen.findByText('Record 1')
    fetcher.mockRejectedValueOnce(new Error('detail unavailable'))
    fireEvent.click(screen.getByRole('button', { name: '查看隔离证据：Record 1' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('暂时无法连接后端')
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
    await screen.findByText('SHA-256：abc123')
    fireEvent.click(screen.getByRole('button', { name: '关闭隔离证据' }))
    fetcher.mockResolvedValueOnce(response({ items: [], total: 0, page: 1, pageSize: 20 }))
    fireEvent.click(screen.getByRole('button', { name: '刷新记录' }))
    await screen.findByText('没有匹配的隔离记录')
  })
})
