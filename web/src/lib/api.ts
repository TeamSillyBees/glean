import type {
  CatalogHistory,
  Correction,
  CorrectionFields,
  ManualReview,
  TopicInput,
  Facets,
  Filters,
  Identity,
  Paper,
  PaperPage,
  Preferences,
  ReadStatus,
  Topic,
  Venue,
} from './types'
import { encodeFilters } from './filters'
const API_BASE = (import.meta.env.VITE_API_BASE_URL || '').replace(/\/$/, '')
let accessToken = ''
let refreshToken = ''
let sessionEpoch = 0
let refreshPromise: { epoch: number; promise: Promise<boolean> } | null = null
let onExpired: (() => void) | undefined
export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}
export function setSessionExpired(handler: () => void) {
  onExpired = handler
  return () => {
    onExpired = undefined
  }
}
export function clearSession() {
  sessionEpoch += 1
  accessToken = ''
  refreshToken = ''
  refreshPromise = null
}
interface Envelope<T> {
  code: string | number
  message: string
  data: T
}
async function refreshSession(epoch: number) {
  if (!refreshToken || epoch !== sessionEpoch) return false
  try {
    const response = await fetch(`${API_BASE}/api/v1/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refreshToken }),
    })
    if (!response.ok) return false
    const body: Envelope<{ accessToken: string; refreshToken: string }> = await response.json()
    if (epoch !== sessionEpoch) return false
    accessToken = body.data.accessToken
    refreshToken = body.data.refreshToken
    return true
  } catch {
    return false
  }
}
export async function request<T>(
  path: string,
  init: RequestInit = {},
  canRefresh = true,
): Promise<T> {
  const epoch = sessionEpoch
  let response: Response
  try {
    response = await fetch(`${API_BASE}${path}`, {
      ...init,
      headers: {
        Accept: 'application/json',
        ...(init.body ? { 'Content-Type': 'application/json' } : {}),
        ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
        ...init.headers,
      },
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    throw new ApiError('暂时无法连接后端，请检查服务后重试。', 0)
  }
  const authenticatedRequest = !path.includes('/auth/')
  if (authenticatedRequest && epoch !== sessionEpoch)
    throw new ApiError('会话已切换，已忽略旧请求。', 401)
  if (response.status === 401 && canRefresh && authenticatedRequest) {
    if (!refreshPromise || refreshPromise.epoch !== epoch) {
      const promise = refreshSession(epoch).finally(() => {
        if (refreshPromise?.epoch === epoch) refreshPromise = null
      })
      refreshPromise = { epoch, promise }
    }
    const refreshed = await refreshPromise.promise
    if (epoch !== sessionEpoch) throw new ApiError('会话已切换，已忽略旧请求。', 401)
    if (refreshed) return request<T>(path, init, false)
    clearSession()
    onExpired?.()
    throw new ApiError('登录已过期，请重新登录。', 401)
  }
  if (response.status === 401 && authenticatedRequest) {
    clearSession()
    onExpired?.()
    throw new ApiError('登录已过期，请重新登录。', 401)
  }
  let body: Envelope<T> | { detail?: string; message?: string }
  try {
    body = await response.json()
  } catch {
    throw new ApiError('后端响应格式异常，请检查 API 地址。', response.status)
  }
  if (authenticatedRequest && epoch !== sessionEpoch)
    throw new ApiError('会话已切换，已忽略旧请求。', 401)
  if (!response.ok)
    throw new ApiError(
      response.status === 401
        ? path.endsWith('/auth/login')
          ? '用户名或密码不正确，请重试。'
          : '登录已过期，请重新登录。'
        : body.message || ('detail' in body && body.detail) || `请求失败 (${response.status})`,
      response.status,
    )
  if (!('data' in body)) throw new ApiError('后端未返回预期的数据结构。', response.status)
  return body.data
}
export function queryString(filters: Partial<Filters> | Record<string, unknown>) {
  const params = new URLSearchParams()
  Object.entries(encodeFilters(filters)).forEach(([key, value]) => {
    if (value !== '' && value !== undefined && value !== null) params.set(key, String(value))
  })
  return params.toString()
}
export const api = {
  catalogAccess: (signal?: AbortSignal) =>
    request<{ canManage: boolean }>('/api/v1/catalog/access', { signal }),
  saveTopic: (input: TopicInput, slug?: string) =>
    request<Topic>(`/api/v1/catalog/topics${slug ? `/${encodeURIComponent(slug)}` : ''}`, {
      method: slug ? 'PUT' : 'POST',
      body: JSON.stringify(input),
    }),
  review: (
    id: string,
    topic: string,
    input: Pick<ManualReview, 'status' | 'note' | 'labels'> & { expectedVersion: number },
  ) =>
    request<ManualReview>(
      `/api/v1/catalog/papers/${encodeURIComponent(id)}/topics/${encodeURIComponent(topic)}/review`,
      { method: 'PUT', body: JSON.stringify(input) },
    ),
  correct: (id: string, fields: CorrectionFields, reason: string, expectedVersion: number) =>
    request<Correction>(`/api/v1/catalog/papers/${encodeURIComponent(id)}/correction`, {
      method: 'PUT',
      body: JSON.stringify({ fields, reason, expectedVersion }),
    }),
  history: (id: string, signal?: AbortSignal) =>
    request<{ items: CatalogHistory[] }>(
      `/api/v1/catalog/papers/${encodeURIComponent(id)}/history`,
      { signal },
    ),
  async login(username: string, password: string) {
    clearSession()
    const epoch = sessionEpoch
    const data = await request<{ accessToken: string; refreshToken: string; user: Identity }>(
      '/api/v1/auth/login',
      { method: 'POST', body: JSON.stringify({ username, password }) },
      false,
    )
    if (epoch !== sessionEpoch) throw new ApiError('登录已取消，请重试。', 401)
    accessToken = data.accessToken
    refreshToken = data.refreshToken
    return data.user
  },
  async changePassword(currentPassword: string, newPassword: string) {
    const epoch = sessionEpoch
    try {
      // Do not retry a credential write or retain its variables in a mutation cache.
      await request(
        '/api/v1/auth/password',
        {
          method: 'PUT',
          body: JSON.stringify({ currentPassword, newPassword }),
        },
        false,
      )
    } catch (error) {
      if (epoch !== sessionEpoch) throw new ApiError('会话已切换，已忽略旧请求。', 401)
      if (error instanceof ApiError && error.status === 401) {
        clearSession()
        onExpired?.()
      }
      throw error
    }
    if (epoch !== sessionEpoch) throw new ApiError('会话已切换，已忽略旧请求。', 401)
    // The backend revokes every session; discard this browser's credentials too.
    clearSession()
  },
  async logout() {
    const token = accessToken
    const refresh = refreshToken
    clearSession()
    if (refresh)
      await request(
        '/api/v1/auth/logout',
        {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
          body: JSON.stringify({ refreshToken: refresh }),
        },
        false,
      )
  },
  topics: (signal?: AbortSignal) =>
    request<{ items: Topic[] }>('/api/v1/catalog/topics', { signal }),
  papers: (filters: Filters, signal?: AbortSignal) =>
    request<PaperPage>(`/api/v1/catalog/papers?${queryString(filters)}`, { signal }),
  paper: (id: string, signal?: AbortSignal) =>
    request<Paper>(`/api/v1/catalog/papers/${encodeURIComponent(id)}`, { signal }),
  facets: (filters: Filters, signal?: AbortSignal) =>
    request<Facets>(`/api/v1/catalog/facets?${queryString(filters)}`, { signal }),
  venues: (topic: string, q: string, signal?: AbortSignal) =>
    request<{ items: Venue[] }>(`/api/v1/catalog/venues?${queryString({ topic, q })}`, { signal }),
  preferences: (signal?: AbortSignal) =>
    request<Preferences>('/api/v1/catalog/preferences', { signal }),
  savePreferences: (filters: Record<string, unknown>) =>
    request<Preferences>('/api/v1/catalog/preferences', {
      method: 'PUT',
      body: JSON.stringify({ filters }),
    }),
  state: (id: string, hidden: boolean, readStatus: ReadStatus) =>
    request<{ hidden: boolean; readStatus: ReadStatus }>(
      `/api/v1/catalog/papers/${encodeURIComponent(id)}/state`,
      { method: 'PUT', body: JSON.stringify({ hidden, readStatus }) },
    ),
}
