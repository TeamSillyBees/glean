import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { VenuesView } from './venues'

let client: QueryClient
let fetcher: ReturnType<typeof vi.fn>
beforeEach(() => {
  clearSession()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  fetcher = vi
    .fn()
    .mockImplementation(
      async () =>
        new Response(
          JSON.stringify({ code: 'OK', data: { total: 45, items: [], page: 1, pageSize: 20 } }),
        ),
    )
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => {
  client.clear()
  vi.unstubAllGlobals()
})
const view = (topic = 'biomed-ai', q = '') => (
  <QueryClientProvider client={client}>
    <VenuesView topic={topic} q={q} />
  </QueryClientProvider>
)
function lastQuery() {
  return new URL(fetcher.mock.calls.at(-1)![0], 'http://localhost').searchParams
}
it('requests bounded pages with impact descending by default and resets after filters change', async () => {
  const mounted = render(view())
  await screen.findByText('45 条指标记录')
  expect(Object.fromEntries(lastQuery())).toMatchObject({
    topic: 'biomed-ai',
    page: '1',
    pageSize: '20',
    sort: 'jif_desc',
  })
  fireEvent.click(screen.getByRole('button', { name: '下一页' }))
  await waitFor(() => expect(lastQuery().get('page')).toBe('2'))
  await waitFor(() => expect(screen.getByRole('button', { name: '下一页' })).toBeEnabled())
  fireEvent.change(screen.getByRole('combobox', { name: '期刊会议排序' }), {
    target: { value: 'jif_asc' },
  })
  await waitFor(() =>
    expect(Object.fromEntries(lastQuery())).toMatchObject({ page: '1', sort: 'jif_asc' }),
  )
  fireEvent.change(screen.getByRole('combobox', { name: '期刊会议类型' }), {
    target: { value: 'conference' },
  })
  await waitFor(() => expect(lastQuery().get('kind')).toBe('conference'))
  fireEvent.change(screen.getByRole('combobox', { name: '每页记录数' }), {
    target: { value: '50' },
  })
  await waitFor(() => expect(lastQuery().get('pageSize')).toBe('50'))
  mounted.rerender(view('llm-agent', 'Nature'))
  await waitFor(() =>
    expect(Object.fromEntries(lastQuery())).toMatchObject({
      topic: 'llm-agent',
      q: 'Nature',
      page: '1',
      pageSize: '50',
      sort: 'jif_asc',
      kind: 'conference',
    }),
  )
})
it('keeps pagination disabled on request failure and retries the same page', async () => {
  fetcher.mockRejectedValueOnce(new Error('offline'))
  render(view())
  await screen.findByRole('alert')
  expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
  await screen.findByText('45 条指标记录')
  expect(lastQuery().get('page')).toBe('1')
})
