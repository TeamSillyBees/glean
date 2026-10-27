import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { request } from '../lib/api'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'

type ManagedUser = {
  id: string
  username: string
  nickname: string
  isEnable: boolean
  isSuperAdmin: boolean
}
type UserPage = { items: ManagedUser[]; total: number; totalPages: number }
type UsersProps = { currentUserId: string; notify: (message: string) => void }
const fieldClass =
  'mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'

// UsersView 使用现有账号接口；权限由父级入口与后端共同控制。
export function UsersView(props: UsersProps) {
  return <UsersList key={props.currentUserId} {...props} />
}
function UsersList({ currentUserId, notify }: UsersProps) {
  const [search, setSearch] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [editor, setEditor] = useState<ManagedUser | 'new' | null>(null)
  const queryKey = ['users', currentUserId, keyword, page] as const
  const users = useQuery({
    queryKey,
    queryFn: ({ signal }) =>
      request<UserPage>(
        `/api/v1/users?${new URLSearchParams({ keyword, page: String(page), pageSize: '20' })}`,
        { signal },
      ),
  })
  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[27px] font-semibold">用户管理</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            管理登录账号。每位用户的阅读、隐藏与偏好记录独立保存。
          </p>
        </div>
        <Button onClick={() => setEditor('new')}>
          <Plus />
          新增用户
        </Button>
      </header>
      <form
        className="flex max-w-lg items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          setKeyword(search.trim())
          setPage(1)
        }}
      >
        <label className="flex-1 text-sm">
          搜索用户
          <input
            className={fieldClass}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="用户名或昵称"
          />
        </label>
        <Button variant="outline" type="submit">
          搜索
        </Button>
      </form>
      {users.isPending ? (
        <Loading label="正在读取用户…" />
      ) : users.isError ? (
        <ErrorState error={users.error} retry={() => void users.refetch()} />
      ) : (
        <>
          {users.data.items.length ? (
            <ul className="divide-y rounded-lg border bg-card" aria-label="用户列表">
              {users.data.items.map((user) => (
                <li
                  key={user.id}
                  className="flex flex-wrap items-center justify-between gap-3 px-5 py-4"
                >
                  <div className="min-w-0 space-y-1">
                    <div className="break-all font-medium">
                      {user.nickname || user.username}
                      {user.id === currentUserId && (
                        <span className="ml-2 text-xs text-muted-foreground">当前账号</span>
                      )}
                    </div>
                    <div className="break-all text-sm text-muted-foreground">{user.username}</div>
                    <div className="flex gap-2">
                      <Badge>{user.isSuperAdmin ? '管理员' : '普通用户'}</Badge>
                      <Badge tone={user.isEnable ? 'green' : 'neutral'}>
                        {user.isEnable ? '已启用' : '已停用'}
                      </Badge>
                    </div>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={`编辑 ${user.username}`}
                    onClick={() => setEditor(user)}
                  >
                    编辑
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <Empty title="没有匹配的用户" description="试试其他关键词，或新增一个登录账号。" />
          )}
          <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
            <span className="tabular-nums text-muted-foreground">
              共 {users.data.total} 位用户 · 第 {page} 页
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                disabled={page <= 1 || users.isFetching}
                onClick={() => setPage(page - 1)}
              >
                上一页
              </Button>
              <Button
                variant="outline"
                disabled={page >= users.data.totalPages || users.isFetching}
                onClick={() => setPage(page + 1)}
              >
                下一页
              </Button>
            </div>
          </div>
        </>
      )}
      {editor !== null && (
        <UserEditor
          key={editor === 'new' ? 'new' : editor.id}
          user={editor === 'new' ? null : editor}
          currentUserId={currentUserId}
          queryKey={queryKey}
          notify={notify}
          onClose={() => setEditor(null)}
        />
      )}
    </section>
  )
}
function UserEditor({
  user,
  currentUserId,
  queryKey,
  notify,
  onClose,
}: UsersProps & { user: ManagedUser | null; queryKey: readonly unknown[]; onClose: () => void }) {
  const client = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState(user?.nickname || '')
  const [enabled, setEnabled] = useState(user?.isEnable ?? true)
  const [admin, setAdmin] = useState(user?.isSuperAdmin ?? false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const lock = useRef(false)
  const mounted = useRef(false)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const self = user?.id === currentUserId
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (lock.current) return
    lock.current = true
    setPending(true)
    setError('')
    const originalQuery = client.getQueryCache().find({ queryKey, exact: true })
    const live = () =>
      mounted.current &&
      !!originalQuery &&
      client.getQueryCache().find({ queryKey, exact: true }) === originalQuery
    try {
      const body = user
        ? { nickname: nickname.trim(), ...(self ? {} : { isEnable: enabled, isSuperAdmin: admin }) }
        : {
            username: username.trim(),
            password,
            nickname: nickname.trim(),
            isEnable: enabled,
            isSuperAdmin: admin,
          }
      await request(user ? `/api/v1/users/${encodeURIComponent(user.id)}` : '/api/v1/users', {
        method: user ? 'PUT' : 'POST',
        body: JSON.stringify(body),
      })
      if (!live()) return
      setPassword('')
      await client.invalidateQueries({ queryKey: ['users', currentUserId] })
      if (!live()) return
      notify(user ? '用户已更新' : '用户已创建')
      onClose()
    } catch (cause) {
      if (live()) setError(cause instanceof Error ? cause.message : '保存失败，请重试。')
    } finally {
      lock.current = false
      if (mounted.current) setPending(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !lock.current) onClose()
      }}
    >
      <DialogContent showCloseButton={!pending} className="max-h-[90dvh] overflow-y-auto">
        <DialogTitle className="text-xl font-semibold">
          {user ? '编辑用户' : '新增用户'}
        </DialogTitle>
        <DialogDescription className="mt-2 text-sm text-muted-foreground">
          {user
            ? `账号：${user.username}。停用保留个人记录，重新启用后可继续使用。`
            : '创建后可使用用户名和初始密码登录。管理员可以管理用户及共享研究数据。'}
        </DialogDescription>
        <form className="mt-5 space-y-4" onSubmit={(event) => void submit(event)}>
          <fieldset disabled={pending} className="space-y-4 disabled:opacity-60">
            {!user && (
              <>
                <label className="block text-sm">
                  用户名
                  <input
                    required
                    autoComplete="off"
                    className={fieldClass}
                    value={username}
                    onChange={(event) => setUsername(event.target.value)}
                  />
                </label>
                <label className="block text-sm">
                  初始密码
                  <input
                    type="password"
                    required
                    autoComplete="new-password"
                    className={fieldClass}
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                  />
                </label>
              </>
            )}
            <label className="block text-sm">
              昵称
              <input
                className={fieldClass}
                value={nickname}
                onChange={(event) => setNickname(event.target.value)}
              />
            </label>
            <label className="block text-sm">
              用户类型
              <select
                className={fieldClass}
                disabled={self}
                value={admin ? 'admin' : 'user'}
                onChange={(event) => setAdmin(event.target.value === 'admin')}
              >
                <option value="user">普通用户</option>
                <option value="admin">管理员</option>
              </select>
            </label>
            <label className="flex min-h-11 items-center gap-2 text-sm">
              <input
                type="checkbox"
                disabled={self}
                checked={enabled}
                onChange={(event) => setEnabled(event.target.checked)}
              />
              启用账号
            </label>
            {self && (
              <p className="text-xs text-muted-foreground">
                不能停用当前账号或撤销自己的管理员权限。
              </p>
            )}
          </fieldset>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={pending} onClick={onClose}>
              取消
            </Button>
            <Button
              type="submit"
              disabled={pending || (!user && (!username.trim() || !password.trim()))}
            >
              {pending ? '正在保存…' : user ? '保存修改' : '创建用户'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
