import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from '../lib/api'
import { topics } from '../test/fixtures'
import type { Topic } from '../lib/types'
import { TopicsView } from './topics-management'
let rows: Topic[]
beforeEach(() => {
  rows = structuredClone(topics)
  vi.spyOn(api, 'topics').mockImplementation(async () => ({ items: structuredClone(rows) }))
  vi.spyOn(api, 'saveTopic').mockImplementation(async (input, slug) => {
    const next: Topic = {
      ...input,
      slug: slug || 'new-topic',
      membershipCount: 0,
      version: input.expectedVersion + 1,
    }
    rows = slug ? rows.map((row) => (row.slug === slug ? next : row)) : [...rows, next]
    return next
  })
})
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const notify = vi.fn()
  const view = render(
    <QueryClientProvider client={client}>
      <TopicsView currentUserId="9" notify={notify} />
    </QueryClientProvider>,
  )
  return { ...view, client, notify }
}
async function edit() {
  fireEvent.click(await screen.findByRole('button', { name: '编辑主题 生物医学 × AI' }))
}
describe('simple research topic management', () => {
  it('creates a topic with parsed term groups and refreshes the shared topic query', async () => {
    mount()
    await screen.findByRole('button', { name: '编辑主题 生物医学 × AI' })
    fireEvent.click(screen.getByRole('button', { name: '新增研究主题' }))
    fireEvent.change(screen.getByLabelText('主题名称'), { target: { value: '新研究主题' } })
    fireEvent.change(screen.getByLabelText('检索词组'), {
      target: { value: 'cancer | tumor\nAI | learning' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    expect(await screen.findByRole('button', { name: '编辑主题 新研究主题' })).toBeInTheDocument()
    expect(api.saveTopic).toHaveBeenCalledWith(
      expect.objectContaining({
        name: '新研究主题',
        termGroups: [
          ['cancer', 'tumor'],
          ['AI', 'learning'],
        ],
        expectedVersion: 0,
        searchEnabled: true,
      }),
      undefined,
    )
  })
  it('edits with the current version and cancel restores server values', async () => {
    rows[0].version = 4
    mount()
    await edit()
    for (const name of ['主题说明', '纳入规则', '检索词组']) {
      const field = screen.getByRole('textbox', { name })
      const label = document.getElementById(field.getAttribute('aria-labelledby') || '')
      expect(label?.textContent).toBe(name)
      expect(label).not.toContainElement(field)
    }
    fireEvent.change(screen.getByLabelText('主题名称'), { target: { value: '未保存' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await edit()
    expect(screen.getByLabelText('主题名称')).toHaveValue(topics[0].name)
    fireEvent.change(screen.getByLabelText('主题名称'), { target: { value: '更新主题' } })
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    await screen.findByRole('button', { name: '编辑主题 更新主题' })
    expect(api.saveTopic).toHaveBeenCalledWith(
      expect.objectContaining({ expectedVersion: 4 }),
      'biomed-ai',
    )
  })
  it('locks dismissal and repeated save while pending', async () => {
    let resolve!: (topic: Topic) => void
    vi.mocked(api.saveTopic).mockReturnValue(
      new Promise((yes) => {
        resolve = yes
      }),
    )
    mount()
    await edit()
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '取消' })).toBeDisabled())
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '关闭' })).not.toBeInTheDocument()
    await act(async () => resolve(rows[0]))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.saveTopic).toHaveBeenCalledTimes(1)
  })
  it('offers a refresh instead of overwriting a conflict', async () => {
    vi.mocked(api.saveTopic).mockRejectedValue(new ApiError('conflict', 409))
    mount()
    await edit()
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    fireEvent.click(await screen.findByRole('button', { name: '重新读取主题' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.saveTopic).toHaveBeenCalledTimes(1)
  })
  it('ignores a save resolved after session cache was replaced', async () => {
    let resolve!: (topic: Topic) => void
    vi.mocked(api.saveTopic).mockReturnValue(
      new Promise((yes) => {
        resolve = yes
      }),
    )
    const { client, notify, unmount } = mount()
    await edit()
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    await waitFor(() => expect(api.saveTopic).toHaveBeenCalled())
    unmount()
    client.clear()
    client.setQueryData(['topics'], { items: [] })
    await act(async () => resolve(rows[0]))
    expect(client.getQueryData(['topics'])).toEqual({ items: [] })
    expect(notify).not.toHaveBeenCalled()
  })
})

describe('advanced search routes', () => {
  const routes = [
    {
      id: 'medical',
      name: '医学',
      sources: ['pubmed', 'europepmc'],
      termGroups: [['cancer', 'tumor'], ['AI']],
      fieldMode: 'title_abstract',
      backfillFrom: '2020-01-01T00:00:00Z',
    },
    { id: 'preprints', sources: ['arxiv'], termGroups: [['cancer'], ['AI']] },
  ]
  it('saves multiple routes and reloads them without losing legacy term edits', async () => {
    mount()
    await edit()
    fireEvent.click(screen.getByText('高级检索路线（JSON）'))
    fireEvent.change(screen.getByLabelText('检索路线 JSON'), {
      target: { value: JSON.stringify(routes) },
    })
    fireEvent.change(screen.getByLabelText('检索词组'), { target: { value: 'legacy | fallback' } })
    expect(screen.getByText('2 条路线 · 3 个来源查询')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.saveTopic).toHaveBeenCalledWith(
      expect.objectContaining({ searchRoutes: routes, termGroups: [['legacy', 'fallback']] }),
      'biomed-ai',
    )
    await edit()
    expect(
      JSON.parse((screen.getByLabelText('检索路线 JSON') as HTMLTextAreaElement).value),
    ).toEqual(routes)
    fireEvent.change(screen.getByLabelText('检索词组'), { target: { value: 'new legacy' } })
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    await waitFor(() => expect(api.saveTopic).toHaveBeenCalledTimes(2))
    expect(vi.mocked(api.saveTopic).mock.calls[1][0].searchRoutes).toEqual(routes)
  })
  it('keeps malformed JSON visible and prevents writes', async () => {
    mount()
    await edit()
    fireEvent.click(screen.getByText('高级检索路线（JSON）'))
    fireEvent.change(screen.getByLabelText('检索路线 JSON'), { target: { value: '[broken' } })
    expect(screen.getByRole('alert')).toHaveTextContent('有效 JSON 数组')
    expect(screen.getByRole('button', { name: '保存主题' })).toBeDisabled()
    expect(screen.getByLabelText('检索路线 JSON')).toHaveValue('[broken')
    expect(api.saveTopic).not.toHaveBeenCalled()
  })
  it('preserves route drafts on version conflict until explicit reload', async () => {
    vi.mocked(api.saveTopic).mockRejectedValue(new ApiError('conflict', 409))
    mount()
    await edit()
    fireEvent.click(screen.getByText('高级检索路线（JSON）'))
    const draft = JSON.stringify(routes)
    fireEvent.change(screen.getByLabelText('检索路线 JSON'), { target: { value: draft } })
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    await screen.findByRole('button', { name: '重新读取主题' })
    expect(screen.getByLabelText('检索路线 JSON')).toHaveValue(draft)
    expect(api.saveTopic).toHaveBeenCalledTimes(1)
  })
  it('allows new topics with routes and no legacy terms', async () => {
    mount()
    fireEvent.click(screen.getByRole('button', { name: '新增研究主题' }))
    fireEvent.change(screen.getByLabelText('主题名称'), { target: { value: '路线主题' } })
    fireEvent.change(screen.getByLabelText('检索路线 JSON'), {
      target: { value: JSON.stringify(routes) },
    })
    expect(screen.getByLabelText('检索词组')).not.toBeRequired()
    fireEvent.click(screen.getByRole('button', { name: '保存主题' }))
    expect(await screen.findByRole('button', { name: '编辑主题 路线主题' })).toBeInTheDocument()
  })
})
