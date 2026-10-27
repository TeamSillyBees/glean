import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { clearSession } from './lib/api'
import { paper, topics, facets } from './test/fixtures'
import type { Paper } from './lib/types'
let canManage: boolean
let failAccess: boolean
let currentTopics: typeof topics
let currentPaper: Paper
let saved: Record<string, unknown>
let paths: string[]
let failPapers: boolean
let failFacets: boolean
let facetsGate: Promise<Response> | null
let logoutGate: Promise<Response> | null
const ok = (data: unknown) =>
  new Response(JSON.stringify({ code: 'OK', message: 'success', data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
beforeEach(() => {
  clearSession()
  canManage = false
  failAccess = false
  currentTopics = structuredClone(topics)
  currentPaper = structuredClone(paper)
  saved = {}
  paths = []
  failPapers = false
  failFacets = false
  facetsGate = null
  logoutGate = null
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string, init?: RequestInit) => {
      const url = new URL(input, 'http://localhost')
      paths.push(url.pathname + url.search)
      if (url.pathname === '/api/v1/auth/login')
        return ok({
          accessToken: 'test-access',
          refreshToken: 'test-refresh',
          user: { userId: '9', username: 'researcher', nickname: '测试研究员' },
        })
      if (url.pathname === '/api/v1/auth/logout') return logoutGate || ok({})
      if (url.pathname.endsWith('/catalog/access'))
        return failAccess
          ? new Response(JSON.stringify({ message: 'unavailable' }), { status: 503 })
          : ok({ canManage })
      if (url.pathname.endsWith('/topics') && init?.method === 'POST') {
        const input = JSON.parse(String(init.body))
        const topic = { ...input, slug: 'custom-topic', membershipCount: 0, version: 1 }
        currentTopics.push(topic)
        return ok(topic)
      }
      if (url.pathname.endsWith('/topics')) return ok({ items: currentTopics })
      if (url.pathname.endsWith('/preferences')) {
        if (init?.method === 'PUT') saved = JSON.parse(String(init.body)).filters
        return ok({ filters: saved })
      }
      if (url.pathname.endsWith('/facets')) {
        if (failFacets)
          return new Response(JSON.stringify({ message: '测试筛选服务不可用' }), { status: 503 })
        return facetsGate || ok(facets)
      }
      if (url.pathname.endsWith('/papers/123/state')) {
        Object.assign(currentPaper, JSON.parse(String(init?.body)))
        return ok({ hidden: currentPaper.hidden, readStatus: currentPaper.readStatus })
      }
      if (url.pathname.endsWith('/papers/123')) return ok(currentPaper)
      if (url.pathname.endsWith('/papers')) {
        if (failPapers)
          return new Response(JSON.stringify({ message: '测试后端不可用' }), { status: 503 })
        const hiddenMatches =
          url.searchParams.get('hidden') === 'only' ? currentPaper.hidden : !currentPaper.hidden
        const items = url.searchParams.get('q') === '不存在' || !hiddenMatches ? [] : [currentPaper]
        return ok({
          items,
          total: items.length,
          page: Number(url.searchParams.get('page') || 1),
          pageSize: 20,
        })
      }
      if (url.pathname.endsWith('/sources'))
        return ok({
          items: [],
          schedule: {
            anchorUtc: '2026-10-01T00:00:00Z',
            nextRunUtc: '2026-10-04T00:00:00Z',
            everyHours: 72,
            completedRuns: 0,
            realFetchEnabled: true,
            acquisitionMode: 'live',
            reviewMode: 'mock',
            schedulerEnabled: false,
          },
        })
      if (url.pathname.endsWith('/runs')) return ok({ items: [] })
      if (url.pathname.endsWith('/templates')) return ok({ items: [], version: 'test-v1' })
      if (url.pathname.endsWith('/venues')) return ok({ items: [] })
      throw new Error(`Unexpected API route: ${url}`)
    }),
  )
})
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 0 }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>,
  )
  return userEvent.setup()
}
async function signIn(user: ReturnType<typeof userEvent.setup>) {
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'researcher' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'test-password' } })
  await user.click(screen.getByRole('button', { name: '进入工作台' }))
  await screen.findByRole('heading', { name: '论文工作台', level: 1 })
}
describe('Glean query workspace', () => {
  it('keeps management hidden for readers and still renders papers if capability lookup fails', async () => {
    failAccess = true
    const user = mount()
    await signIn(user)
    await screen.findByRole('heading', { name: paper.titleZh })
    expect(screen.queryByRole('button', { name: '用户管理' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '研究主题' })).not.toBeInTheDocument()
  })
  it('uses server capability and exposes a newly created topic in navigation with generic filters', async () => {
    canManage = true
    const user = mount()
    await signIn(user)
    expect(await screen.findByRole('button', { name: '用户管理' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '研究主题' }))
    await user.click(screen.getByRole('button', { name: '新增研究主题' }))
    fireEvent.change(screen.getByLabelText('主题名称'), { target: { value: '测试新方向' } })
    fireEvent.change(screen.getByLabelText('检索词组'), {
      target: { value: 'genomics | genetics' },
    })
    await user.click(screen.getByRole('button', { name: '保存主题' }))
    await screen.findByRole('button', { name: '编辑主题 测试新方向' })
    const nav = within(screen.getByRole('complementary', { name: '工作台导航' }))
    await user.click(nav.getByRole('button', { name: /测试新方向/ }))
    await waitFor(() =>
      expect(paths.some((path) => path.includes('topic=custom-topic'))).toBe(true),
    )
    expect(screen.getByRole('combobox', { name: '发表年份' })).toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: '世界模型的作用' })).not.toBeInTheDocument()
  })
  it('logs in against the backend, shows API counts and ordered original taxonomy filters', async () => {
    const user = mount()
    await signIn(user)
    expect(await screen.findByRole('heading', { name: paper.titleZh })).toBeInTheDocument()
    expect(screen.queryByText('Mock 开发模式')).not.toBeInTheDocument()
    const names = screen
      .getAllByRole('combobox')
      .map((element) => element.getAttribute('aria-label'))
    expect(names.slice(0, 9)).toEqual([
      '论文类型',
      '生物医学方向',
      'AI方法',
      '期刊/会议范围',
      '技术任务',
      'AI的作用',
      '发表年份',
      '收录状态',
      '具体期刊/会议',
    ])
    expect(paths.some((path) => path.startsWith('/api/v1/catalog/papers?'))).toBe(true)
    const requests = vi.mocked(fetch).mock.calls
    expect(requests.every(([input]) => String(input).startsWith('/api/'))).toBe(true)
    expect(
      requests.find(([input]) => String(input).includes('/topics'))?.[1]?.headers,
    ).toMatchObject({ Authorization: 'Bearer test-access' })
  })
  it('distinguishes loading facets from an empty taxonomy and disables controls until ready', async () => {
    let finish!: (response: Response) => void
    facetsGate = new Promise((resolve) => {
      finish = resolve
    })
    const user = mount()
    await signIn(user)
    await screen.findByRole('heading', { name: paper.titleZh })
    expect(screen.getByTestId('filters-loading')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByRole('combobox', { name: '论文类型' })).toBeDisabled()
    expect(screen.queryByText('当前目录暂无此标签')).not.toBeInTheDocument()
    await act(async () => {
      finish(ok(facets))
    })
    await screen.findByTestId('filters-ready')
    expect(screen.getByRole('combobox', { name: '论文类型' })).toBeEnabled()
  })
  it('offers a retry rather than claiming there are no facet labels on failure', async () => {
    failFacets = true
    const user = mount()
    await signIn(user)
    await screen.findByTestId('filters-error')
    expect(screen.getByRole('combobox', { name: '论文类型' })).toBeDisabled()
    failFacets = false
    await user.click(screen.getByRole('button', { name: '重新读取' }))
    await screen.findByTestId('filters-ready')
  })
  it('starts in backend-curated scope and explicitly queries all or pending records', async () => {
    const user = mount()
    await signIn(user)
    await screen.findByRole('heading', { name: paper.titleZh })
    expect(paths.find((path) => path.startsWith('/api/v1/catalog/papers?'))).toContain(
      'collectionScope=curated',
    )
    const scopes = screen.getByRole('group', { name: '目录范围' })
    await user.click(within(scopes).getByRole('button', { name: '全部记录' }))
    await waitFor(() =>
      expect(
        paths.some((path) => path.includes('/papers?') && path.includes('collectionScope=all')),
      ).toBe(true),
    )
    await user.click(within(scopes).getByRole('button', { name: '待审核' }))
    expect(
      screen.getByText('包括新发现及手动转回待审核的记录，尚未完成科学审读。'),
    ).toBeInTheDocument()
    await waitFor(() =>
      expect(
        paths.some((path) => path.includes('/facets?') && path.includes('collectionScope=pending')),
      ).toBe(true),
    )
  })
  it('queries literal search, shows an honest empty state, and clears it', async () => {
    const user = mount()
    await signIn(user)
    const input = screen.getByRole('textbox', { name: '搜索论文或来源' })
    fireEvent.change(input, { target: { value: '不存在' } })
    await user.click(screen.getByRole('button', { name: '搜索' }))
    expect(await screen.findByText('暂时没有匹配的论文')).toBeInTheDocument()
    expect(paths.some((path) => path.includes('q=%E4%B8%8D%E5%AD%98%E5%9C%A8'))).toBe(true)
    await user.click(screen.getByRole('button', { name: '清空搜索' }))
    expect(await screen.findByRole('heading', { name: paper.titleZh })).toBeInTheDocument()
  })
  it('opens and dismisses evidence drawer repeatedly with Escape and restores focus', async () => {
    const user = mount()
    await signIn(user)
    const open = await screen.findByRole('button', { name: new RegExp(`查看 ${paper.titleZh}`) })
    await user.click(open)
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('heading', { name: '论文详情' })).toBeInTheDocument()
    // DOM tests cannot hit-test CSS. Guard the sticky ownership here; Playwright
    // covers real pointer clicks at the top and after scrolling on both viewports.
    const header = within(dialog).getByTestId('paper-detail-header')
    expect(within(header).getByRole('button', { name: '关闭' })).toBeInTheDocument()
    expect(within(dialog).getAllByRole('button', { name: '关闭' })).toHaveLength(1)
    expect(within(dialog).getByText('正式在线发表')).toBeInTheDocument()
    expect(within(dialog).getByText('卷期发表')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('tab', { name: '来源与证据' }))
    expect(await screen.findByText('Mock source')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(open).toHaveFocus()
    await user.click(open)
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(open).toHaveFocus()
  })
  it('persists personal read and hidden state and allows restoring a hidden paper', async () => {
    const user = mount()
    await signIn(user)
    await user.click(
      await screen.findByRole('button', { name: new RegExp(`查看 ${paper.titleZh}`) }),
    )
    let dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '已读' }))
    await waitFor(() => expect(currentPaper.readStatus).toBe('read'))
    await user.click(within(dialog).getByRole('button', { name: '隐藏' }))
    await waitFor(() => expect(currentPaper.hidden).toBe(true))
    await waitFor(() => expect(screen.queryByTestId('paper-card')).not.toBeInTheDocument())
    await user.click(within(dialog).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('textbox', { name: '搜索论文或来源' })).toHaveFocus()
    await user.click(screen.getByRole('button', { name: '已隐藏' }))
    await user.click(
      await screen.findByRole('button', { name: new RegExp(`查看 ${paper.titleZh}`) }),
    )
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '恢复显示' }))
    await waitFor(() => expect(currentPaper.hidden).toBe(false))
    await waitFor(() => expect(screen.queryByTestId('paper-card')).not.toBeInTheDocument())
    await user.click(within(dialog).getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('textbox', { name: '搜索论文或来源' })).toHaveFocus()
  })
  it('restores saved filters before its first paper request, and saves explicit preferences', async () => {
    saved = { topic: 'llm-agent', q: 'agent', hidden: 'exclude', year: '2026' }
    const user = mount()
    await signIn(user)
    await screen.findByRole('heading', { name: paper.titleZh })
    const firstQuery = paths.find((path) => path.startsWith('/api/v1/catalog/papers?'))
    expect(firstQuery).toContain('topic=llm-agent')
    expect(firstQuery).toContain('q=agent')
    expect(firstQuery).toContain('year=2026')
    await user.click(screen.getByRole('button', { name: '保存当前筛选' }))
    expect(await screen.findByText('筛选偏好已保存，下次登录时会自动恢复。')).toBeInTheDocument()
    expect(saved.topic).toBe('llm-agent')
  })
  it('renders backend errors without falling back to mock papers', async () => {
    failPapers = true
    const user = mount()
    await signIn(user)
    expect(await screen.findByText('测试后端不可用')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: paper.titleZh })).not.toBeInTheDocument()
    failPapers = false
    await user.click(screen.getByRole('button', { name: '重新连接' }))
    expect(await screen.findByRole('heading', { name: paper.titleZh })).toBeInTheDocument()
  })
  it('keeps real monitoring read-only and separates model mock from disabled scheduling', async () => {
    const user = mount()
    await signIn(user)
    await user.click(screen.getByRole('button', { name: '来源健康' }))
    expect(await screen.findByText('真实来源采集')).toBeInTheDocument()
    expect(screen.getByText('定时计划未启用')).toBeInTheDocument()
    expect(screen.queryByText('Mock 开发模式')).not.toBeInTheDocument()
    expect(screen.getByText('尚未建立来源检查点')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '扫描记录' }))
    expect(await screen.findByText('还没有扫描记录')).toBeInTheDocument()
    expect(paths.every((path) => !path.includes('/run-now'))).toBe(true)
  })
  it('transports a Radix filter selection as an exact imported dimension and removes its chip', async () => {
    const user = mount()
    await signIn(user)
    await screen.findByRole('heading', { name: paper.titleZh })
    await user.click(screen.getByRole('combobox', { name: '论文类型' }))
    await user.click(await screen.findByRole('option', { name: /原创研究/ }))
    await waitFor(() => {
      const path = paths.filter((path) => path.startsWith('/api/v1/catalog/papers?')).at(-1)!
      expect(JSON.parse(new URL(path, 'http://localhost').searchParams.get('dimension')!)).toEqual({
        paper_types: '原创研究',
      })
    })
    await user.click(screen.getByRole('button', { name: '移除 论文类型：原创研究' }))
    await waitFor(() => {
      const path = paths.filter((path) => path.startsWith('/api/v1/catalog/papers?')).at(-1)!
      expect(new URL(path, 'http://localhost').searchParams.has('dimension')).toBe(false)
    })
  })
  it('opens the mobile filter dialog, dismisses it, and keeps navigation intact', async () => {
    const user = mount()
    await signIn(user)
    await user.click(screen.getByRole('button', { name: '筛选' }))
    expect(await screen.findByRole('dialog')).toHaveAccessibleName('筛选论文')
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '筛选' })).toHaveFocus()
    await user.click(screen.getByRole('button', { name: '打开导航' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: /大模型知识工程/ }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(
      await screen.findByRole('heading', {
        name: 'LLM / Agent 知识工程：知识抽取、构建、融合与验证',
      }),
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '论文工作台', level: 1 })).toBeInTheDocument()
  })
  it('signs out immediately and a delayed logout cannot close a new signed-in workspace', async () => {
    let finish!: (response: Response) => void
    logoutGate = new Promise<Response>((resolve) => {
      finish = resolve
    })
    const user = mount()
    await signIn(user)
    await user.click(screen.getByRole('button', { name: /测试研究员/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '退出登录' }))
    expect(await screen.findByRole('heading', { name: '回到你的研究工作台' })).toBeInTheDocument()
    await signIn(user)
    await act(async () => {
      finish(ok({}))
      await logoutGate
    })
    expect(screen.getByRole('heading', { name: '论文工作台', level: 1 })).toBeInTheDocument()
  })
  it('focuses search with the command shortcut', async () => {
    const user = mount()
    await signIn(user)
    fireEvent.keyDown(document, { key: 'k', ctrlKey: true })
    expect(screen.getByRole('textbox', { name: '搜索论文或来源' })).toHaveFocus()
  })
})
