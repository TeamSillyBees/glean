import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { TemplatesView } from './search-plans'
import { api } from '../lib/api'
import { topics } from '../test/fixtures'
let fetcher: ReturnType<typeof vi.fn>
const clients: QueryClient[] = []
function mount(data: object, canManage = false) {
  fetcher.mockImplementation(
    async () => new Response(JSON.stringify({ code: 'OK', message: '', data }), { status: 200 }),
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['topics'], { items: topics })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <TemplatesView topic="biomed-ai" topicInfo={topics[0]} canManage={canManage} />
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
    await screen.findByText('检索来源')
    expect(screen.getByText(base.query)).not.toBeVisible()
    fireEvent.click(screen.getByText('高级：查看各来源查询语句'))
    expect(screen.getByText(/仅编译预览，尚未执行/)).toBeVisible()
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
    await screen.findByText('检索来源')
    fireEvent.click(screen.getByText('高级：查看各来源查询语句'))
    expect(screen.getByText(/暂无该主题的逐来源编译预览/)).toBeVisible()
    expect(screen.getByText('暂无该主题的检索方案')).toBeVisible()
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: '编辑检索方案' })).not.toBeInTheDocument()
  })
  it('shows active route groups instead of the inactive fallback keywords', async () => {
    mount({
      items: [
        {
          id: 'configured',
          topic: 'biomed-ai',
          termGroups: [['inactive-fallback']],
          terms: ['active-route'],
          policy: { ccf: [], jifMinimum: 10 },
          routes: [
            {
              id: 'specific',
              name: '目标研究',
              sources: ['pubmed'],
              termGroups: [['active-route']],
            },
          ],
        },
      ],
      version: 'v1',
    })
    expect(await screen.findByRole('heading', { name: '目标研究' })).toBeVisible()
    expect(screen.getByText('active-route')).toBeVisible()
    expect(screen.queryByText('inactive-fallback')).not.toBeInTheDocument()
  })
  it('edits effective keyword groups without changing topic metadata or executing collection', async () => {
    const save = vi.spyOn(api, 'saveTopic').mockResolvedValue(topics[0])
    mount(
      {
        items: [
          {
            id: 'default',
            topic: 'biomed-ai',
            termGroups: [['cancer', 'tumor'], ['AI']],
            terms: [],
            query: '',
            policy: { ccf: [], jifMinimum: 10 },
          },
        ],
        version: 'v1',
      },
      true,
    )
    fireEvent.click(await screen.findByRole('button', { name: '编辑检索方案' }))
    expect(screen.queryByLabelText('主题名称')).not.toBeInTheDocument()
    expect(screen.getByLabelText('第 1 组')).toHaveValue('cancer\ntumor')
    fireEvent.change(screen.getByLabelText('第 1 组'), {
      target: { value: 'glioma\nglioblastoma' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存方案' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        name: topics[0].name,
        description: topics[0].description,
        rule: topics[0].rule,
        termGroups: [['glioma', 'glioblastoma'], ['AI']],
        expectedVersion: 0,
      }),
      'biomed-ai',
    )
    expect(fetcher.mock.calls.every(([path]) => path === '/api/v1/monitor/templates')).toBe(true)
    save.mockRestore()
  })
})
