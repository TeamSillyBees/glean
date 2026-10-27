import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clearSession } from '../lib/api'
import { UsersView } from './users'

const admin = {
  id: '1',
  username: 'admin',
  nickname: '管理员甲',
  isEnable: true,
  isSuperAdmin: true,
}
const member = {
  id: '2',
  username: 'reader',
  nickname: '读者乙',
  isEnable: true,
  isSuperAdmin: false,
  roleCodes: ['reader'],
  email: 'reader@example.test',
}
let rows = [admin, member]
let writes: { url: string; body: Record<string, unknown>; method: string }[]
let save: (() => Promise<Response>) | undefined
let reads: string[]
function response(data: unknown, status = 200, message = '') {
  return new Response(JSON.stringify({ data, message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}
function deferred() {
  let resolve!: (value: Response) => void
  const promise = new Promise<Response>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}
beforeEach(() => {
  clearSession()
  rows = [structuredClone(admin), structuredClone(member)]
  writes = []
  reads = []
  save = undefined
  // All transport is explicit local fake data; tests never contact a running API.
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === 'POST' || init?.method === 'PUT') {
        const body = JSON.parse(String(init.body))
        writes.push({ url, body, method: init.method })
        if (save) return save()
        if (init.method === 'POST') rows.push({ ...body, id: '3' })
        else rows = rows.map((row) => (url.endsWith(`/${row.id}`) ? { ...row, ...body } : row))
        return response({})
      }
      if (!url.startsWith('/api/v1/users?')) throw new Error(`Unexpected test request: ${url}`)
      reads.push(url)
      const params = new URL(url, 'http://test.local').searchParams
      const keyword = params.get('keyword') || ''
      const items = rows.filter((row) => `${row.username} ${row.nickname}`.includes(keyword))
      return response({ items, total: items.length, totalPages: 2 })
    }),
  )
})
function mount() {
  const notify = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const view = render(
    <QueryClientProvider client={client}>
      <UsersView currentUserId="1" notify={notify} />
    </QueryClientProvider>,
  )
  return { ...view, notify, client }
}
async function edit(name = 'reader') {
  fireEvent.click(await screen.findByRole('button', { name: `编辑 ${name}` }))
  return screen.getByRole('dialog')
}
async function create() {
  fireEvent.click(screen.getByRole('button', { name: '新增用户' }))
  fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'newreader' } })
  fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'test-only-password' } })
  fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '新读者' } })
}
describe('simple user management', () => {
  it('searches and pages server results', async () => {
    mount()
    await screen.findByRole('button', { name: '编辑 reader' })
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(reads.some((url) => url.includes('page=2'))).toBe(true))
    fireEvent.change(screen.getByLabelText('搜索用户'), { target: { value: 'reader' } })
    fireEvent.click(screen.getByRole('button', { name: '搜索' }))
    await waitFor(() => expect(reads.at(-1)).toContain('keyword=reader&page=1'))
    expect(screen.queryByRole('button', { name: '编辑 admin' })).not.toBeInTheDocument()
  })
  it('shows list errors and retries without exposing cached rows', async () => {
    const transport = vi.mocked(fetch)
    transport.mockResolvedValueOnce(response(null, 503, '用户服务暂时不可用'))
    mount()
    expect(await screen.findByRole('alert')).toHaveTextContent('用户服务暂时不可用')
    expect(screen.queryByRole('button', { name: '编辑 reader' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
    await screen.findByRole('button', { name: '编辑 reader' })
  })
  it('discards canceled account credentials', async () => {
    mount()
    await screen.findByRole('button', { name: '编辑 reader' })
    await create()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    fireEvent.click(screen.getByRole('button', { name: '新增用户' }))
    expect(screen.getByLabelText('用户名')).toHaveValue('')
    expect(screen.getByLabelText('初始密码')).toHaveValue('')
    expect(writes).toHaveLength(0)
  })
  it('creates a user, refetches the list and clears password on reopening', async () => {
    const { notify } = mount()
    await screen.findByRole('button', { name: '编辑 reader' })
    await create()
    fireEvent.click(screen.getByRole('button', { name: '创建用户' }))
    await screen.findByRole('button', { name: '编辑 newreader' })
    expect(writes[0].body).toEqual({
      username: 'newreader',
      password: 'test-only-password',
      nickname: '新读者',
      isEnable: true,
      isSuperAdmin: false,
    })
    expect(notify).toHaveBeenCalledWith('用户已创建')
    fireEvent.click(screen.getByRole('button', { name: '新增用户' }))
    expect(screen.getByLabelText('初始密码')).toHaveValue('')
  })
  it('updates nickname and disables/re-enables without deleting or overwriting role/contact fields', async () => {
    mount()
    await edit()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '新昵称' } })
    fireEvent.click(screen.getByLabelText('启用账号'))
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(writes[0]).toEqual({
      url: '/api/v1/users/2',
      method: 'PUT',
      body: { nickname: '新昵称', isEnable: false, isSuperAdmin: false },
    })
    expect(rows[1]).toMatchObject({ roleCodes: ['reader'], email: 'reader@example.test' })
    await edit()
    expect(screen.getByLabelText('启用账号')).not.toBeChecked()
    fireEvent.click(screen.getByLabelText('启用账号'))
    fireEvent.change(screen.getByLabelText('用户类型'), { target: { value: 'admin' } })
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1].body).toMatchObject({ isEnable: true, isSuperAdmin: true })
  })
  it('protects current account from self-disable/demotion', async () => {
    mount()
    await edit('admin')
    expect(screen.getByLabelText('启用账号')).toBeDisabled()
    const userType = screen.getByRole('combobox', {
      name: '用户类型',
    }) as HTMLSelectElement
    expect(userType).toBeDisabled()
    // Keep the associated label text exact in real browsers: option descendants
    // inside a wrapping label can become part of Playwright's label match.
    expect(userType.labels?.[0].textContent).toBe('用户类型')
    expect(userType.labels?.[0]).not.toContainElement(userType)
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ nickname: '管理员甲' })
  })
  it('cancels without requests and resets the form on reopen', async () => {
    mount()
    await edit()
    fireEvent.change(screen.getByLabelText('昵称'), { target: { value: '未保存' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(writes).toHaveLength(0)
    await edit()
    expect(screen.getByLabelText('昵称')).toHaveValue('读者乙')
  })
  it('locks duplicate save and dismissal, then preserves draft on server rejection for retry', async () => {
    const delayed = deferred()
    save = () => delayed.promise
    mount()
    const dialog = await edit()
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    expect(screen.getByRole('button', { name: '正在保存…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '取消' })).toBeDisabled()
    fireEvent.keyDown(dialog, { key: 'Escape' })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await act(async () => delayed.resolve(response(null, 409, '不能停用最后一位管理员')))
    expect(await screen.findByRole('alert')).toHaveTextContent('不能停用最后一位管理员')
    expect(screen.getByLabelText('昵称')).toHaveValue('读者乙')
    save = undefined
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(writes).toHaveLength(2)
  })
  it.each([200, 503])('ignores late %s after navigating away', async (status) => {
    const delayed = deferred()
    save = () => delayed.promise
    const { unmount, notify } = mount()
    await edit()
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    unmount()
    await act(async () => delayed.resolve(response({}, status, '暂时不可用')))
    expect(notify).not.toHaveBeenCalled()
    expect(reads).toHaveLength(1)
  })
  it('ignores old-session mutations after the query cache and credentials are cleared', async () => {
    const delayed = deferred()
    save = () => delayed.promise
    const { client, notify } = mount()
    await edit()
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    act(() => {
      clearSession()
      client.clear()
    })
    await act(async () => delayed.resolve(response({})))
    expect(notify).not.toHaveBeenCalled()
    expect(within(screen.getByRole('dialog')).queryByRole('alert')).not.toBeInTheDocument()
  })
})
