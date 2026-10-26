import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { clearSession } from './lib/api'
import { paper, topics, facets } from './test/fixtures'
import type { Paper } from './lib/types'
let currentPaper: Paper
let saved: Record<string, unknown>
let paths: string[]
let failPapers: boolean
let logoutGate: Promise<Response> | null
const ok = (data: unknown) =>
  new Response(JSON.stringify({ code: 'OK', message: 'success', data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
beforeEach(() => {
  clearSession()
  currentPaper = structuredClone(paper)
  saved = {}
  paths = []
  failPapers = false
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
      if (url.pathname.endsWith('/topics')) return ok({ items: topics })
      if (url.pathname.endsWith('/preferences')) {
        if (init?.method === 'PUT') saved = JSON.parse(String(init.body)).filters
        return ok({ filters: saved })
      }
      if (url.pathname.endsWith('/facets')) return ok(facets)
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
            realFetchEnabled: false,
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
  it('logs in against the backend, shows API counts and ordered original taxonomy filters', async () => {
    const user = mount()
    await signIn(user)
    expect(await screen.findByRole('heading', { name: paper.titleZh })).toBeInTheDocument()
    expect(screen.getByText('MOCK')).toBeInTheDocument()
    const names = screen
      .getAllByRole('combobox')
      .map((element) => element.getAttribute('aria-label'))
    expect(names.slice(0, 8)).toEqual([
      '论文类型',
      '研究方向',
      '期刊 / 会议范围',
      '技术任务',
      '模型角色',
      '发表年份',
      '审读状态',
      '期刊 / 会议',
    ])
    expect(paths.some((path) => path.startsWith('/api/v1/catalog/papers?'))).toBe(true)
    const requests = vi.mocked(fetch).mock.calls
    expect(requests.every(([input]) => String(input).startsWith('/api/'))).toBe(true)
    expect(
      requests.find(([input]) => String(input).includes('/topics'))?.[1]?.headers,
    ).toMatchObject({ Authorization: 'Bearer test-access' })
  })
  it('queries literal search, shows an honest empty state, and clears it', async () => {
    const user = mount()
    await signIn(user)
    const input = screen.getByRole('textbox', { name: '搜索论文或来源' })
    await user.type(input, '不存在')
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
    expect(within(dialog).getByText('正式 online')).toBeInTheDocument()
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
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: '已隐藏' }))
    await user.click(
      await screen.findByRole('button', { name: new RegExp(`查看 ${paper.titleZh}`) }),
    )
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '恢复显示' }))
    await waitFor(() => expect(currentPaper.hidden).toBe(false))
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
  it('keeps monitoring read-only and clearly marks mock/inactive collection', async () => {
    const user = mount()
    await signIn(user)
    await user.click(screen.getByRole('button', { name: '来源健康' }))
    expect(await screen.findByText('Mock 开发模式')).toBeInTheDocument()
    expect(screen.getByText('仅配置计划 · 真实采集未启用')).toBeInTheDocument()
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
    await user.click(within(dialog).getByRole('button', { name: /大模型与智能体/ }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
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
