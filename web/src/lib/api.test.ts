import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, clearSession, request, setSessionExpired, queryString } from './api'
import { defaultFilters } from './types'
const ok = (data: unknown) => new Response(JSON.stringify({ code: 'OK', data }), { status: 200 })
const unauthorized = () => new Response(JSON.stringify({ message: 'expired' }), { status: 401 })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
const loginData = (name = 'one') => ({
  accessToken: `access-${name}`,
  refreshToken: `refresh-${name}`,
  user: { userId: '1', username: name, nickname: name },
})
beforeEach(() => clearSession())
describe('API transport and session isolation', () => {
  it('uses the standard envelope and encodes exact dimensions', async () => {
    const fetcher = vi.fn().mockResolvedValue(ok({ items: [] }))
    vi.stubGlobal('fetch', fetcher)
    expect(await api.topics()).toEqual({ items: [] })
    expect(queryString({ ...defaultFilters, method: '原创研究' })).toContain('dimension=')
    expect(queryString({ ...defaultFilters, method: '原创研究' })).not.toContain('method=')
  })
  it('gives an actionable login error without claiming an expired signed-in session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(unauthorized()))
    await expect(api.login('researcher', 'wrong-password')).rejects.toThrow('用户名或密码不正确')
  })
  it('never silently serves fixtures when the backend is unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network')))
    await expect(api.topics()).rejects.toThrow('暂时无法连接后端')
  })
  it('refreshes once for concurrent expired requests', async () => {
    const refresh = deferred<Response>()
    let calls = 0
    let dataCalls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.endsWith('/login')) return ok(loginData())
        if (url.endsWith('/refresh')) {
          calls++
          return refresh.promise
        }
        return ++dataCalls <= 2 ? unauthorized() : ok({ done: true })
      }),
    )
    await api.login('one', 'password')
    const a = request('/api/test/a')
    const b = request('/api/test/b')
    await vi.waitFor(() => expect(calls).toBe(1))
    refresh.resolve(ok(loginData('refreshed')))
    await expect(Promise.all([a, b])).resolves.toEqual([{ done: true }, { done: true }])
    expect(calls).toBe(1)
  })
  it('does not let an in-flight refresh resurrect a cleared session', async () => {
    const refresh = deferred<Response>()
    let refreshing = false
    const expired = vi.fn()
    const unsubscribe = setSessionExpired(expired)
    const fetcher = vi.fn(async (url: string) => {
      if (url.endsWith('/login')) return ok(loginData())
      if (url.endsWith('/refresh')) {
        refreshing = true
        return refresh.promise
      }
      return unauthorized()
    })
    vi.stubGlobal('fetch', fetcher)
    await api.login('one', 'password')
    const pending = request('/api/test/old').catch((error) => error)
    await vi.waitFor(() => expect(refreshing).toBe(true))
    clearSession()
    refresh.resolve(ok(loginData('stale')))
    expect(await pending).toMatchObject({ message: expect.stringContaining('会话已切换') })
    expect(expired).not.toHaveBeenCalled()
    expect(fetcher.mock.calls.filter(([url]) => url === '/api/test/old')).toHaveLength(1)
    unsubscribe()
  })
  it('does not let a stale 401 clear a newer login', async () => {
    const old = deferred<Response>()
    const expired = vi.fn()
    const unsubscribe = setSessionExpired(expired)
    const fetcher = vi.fn(async (url: string) =>
      url.endsWith('/login')
        ? ok(loginData('new'))
        : url.endsWith('/old')
          ? old.promise
          : ok({ done: true }),
    )
    vi.stubGlobal('fetch', fetcher)
    await api.login('one', 'password')
    const pending = request('/api/test/old').catch((error) => error)
    await api.login('new', 'password')
    old.resolve(unauthorized())
    expect(await pending).toMatchObject({ message: expect.stringContaining('会话已切换') })
    await request('/api/test/new')
    expect(fetcher.mock.lastCall?.[0]).toBe('/api/test/new')
    expect(expired).not.toHaveBeenCalled()
    unsubscribe()
  })
  it('rejects a stale success body that finishes parsing after a session change', async () => {
    const body = deferred<unknown>()
    let started = false
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) =>
        url.endsWith('/login')
          ? ok(loginData())
          : ({
              ok: true,
              status: 200,
              json: () => {
                started = true
                return body.promise
              },
            } as Response),
      ),
    )
    await api.login('one', 'password')
    const pending = request('/api/test/old').catch((error) => error)
    await vi.waitFor(() => expect(started).toBe(true))
    clearSession()
    body.resolve({ code: 'OK', data: { sensitive: true } })
    expect(await pending).toMatchObject({ message: expect.stringContaining('会话已切换') })
  })
  it('does not let a late logout erase a newly authenticated session', async () => {
    const logout = deferred<Response>()
    const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith('/login')) return ok(loginData(JSON.parse(String(init?.body)).username))
      if (url.endsWith('/logout')) return logout.promise
      return ok({
        authorization: (init?.headers as Record<string, string> | undefined)?.Authorization,
      })
    })
    vi.stubGlobal('fetch', fetcher)
    await api.login('old', 'password')
    const pending = api.logout()
    await api.login('new', 'password')
    logout.resolve(ok({}))
    await pending
    expect(await request('/api/test/current')).toEqual({ authorization: 'Bearer access-new' })
  })
})
