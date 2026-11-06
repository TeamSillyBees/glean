import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { TemplatesView } from './monitor'
let fetcher: ReturnType<typeof vi.fn>
const clients: QueryClient[] = []
function mount(data: object) {
  fetcher.mockResolvedValue(
    new Response(JSON.stringify({ code: 'OK', message: '', data }), { status: 200 }),
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <TemplatesView topic="biomed-ai" />
    </QueryClientProvider>,
  )
}
beforeEach(() => {
  clearSession()
  fetcher = vi.fn()
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
})
describe('saved source compilation previews', () => {
  it('shows compiled strings, seeds and disabled routes with no extra endpoint or scan', async () => {
    const base = {
      queryId: 'biomed-ai/medical',
      queryVersion: 'v1',
      topic: 'biomed-ai',
      source: 'pubmed',
      mode: 'boolean',
      query: '("cancer"[tiab]) AND ("AI"[tiab])',
      enabled: true,
    }
    mount({
      items: [],
      version: 'v1',
      previews: [
        base,
        {
          ...base,
          source: 'crossref',
          mode: 'seed_union',
          query: '',
          queries: ['cancer', 'tumor'],
          warnings: ['Seeds require downstream relevance evaluation'],
        },
        {
          ...base,
          source: 'arxiv',
          mode: 'full_window',
          query: '',
          enabled: false,
          backfillFrom: '2020-01-01T00:00:00Z',
        },
        { ...base, topic: 'other', query: 'other-topic-secret-query' },
      ],
    })
    expect(await screen.findByText(/仅编译预览，尚未执行/)).toBeVisible()
    for (const text of [
      base.query,
      '独立查询 1',
      '独立查询 2',
      'cancer',
      'tumor',
      '来源限制：Seeds require downstream relevance evaluation',
    ])
      expect(screen.getByText(text)).toBeVisible()
    expect(screen.getByText(/当前配置未启用此来源/)).toBeVisible()
    expect(screen.getByText(/不在来源端按关键词过滤/)).toBeVisible()
    expect(screen.queryByText('other-topic-secret-query')).not.toBeInTheDocument()
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/monitor/templates')
    expect(fetcher.mock.calls[0][1].method).toBeUndefined()
  })
  it('handles absent legacy previews', async () => {
    mount({ items: [], version: 'legacy' })
    expect(await screen.findByText(/暂无该主题的逐来源编译预览/)).toBeVisible()
    expect(screen.getByText('暂无该主题的检索模板')).toBeVisible()
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})
