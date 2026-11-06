import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ApiError, api } from '../lib/api'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'

export function PasswordChangeDialog({
  onClose,
  onChanged,
  username,
}: {
  onClose: () => void
  onChanged: () => void
  username?: string
}) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const inFlight = useRef(false)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  function close() {
    if (inFlight.current) return
    setCurrent('')
    setNext('')
    setConfirm('')
    setError('')
    onClose()
  }
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (inFlight.current) return
    setError('')
    if (!current.trim() || [...next].length < 8) {
      setError('请输入当前密码，新密码至少需要 8 个字符。')
      return
    }
    if (next !== confirm) {
      setError('两次输入的新密码不一致。')
      return
    }
    if (current === next) {
      setError('新密码不能与当前密码相同。')
      return
    }
    inFlight.current = true
    setPending(true)
    try {
      await api.changePassword(current, next)
      if (mounted.current) {
        setCurrent('')
        setNext('')
        setConfirm('')
        onChanged()
      }
    } catch (cause) {
      if (mounted.current) {
        // Do not display a server message that could echo submitted credentials.
        setError(
          cause instanceof ApiError && cause.status === 400
            ? '修改失败，请检查当前密码，并使用不同的新密码。'
            : cause instanceof ApiError && cause.status === 401
              ? '会话已结束或已切换，请重新登录后再试。'
              : '暂时无法确认修改结果。请重试；若会话已结束，请使用新密码登录。',
        )
        setCurrent('')
        setNext('')
        setConfirm('')
      }
    } finally {
      inFlight.current = false
      if (mounted.current) setPending(false)
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent showCloseButton={!pending}>
        <DialogTitle className="text-lg font-semibold">修改密码</DialogTitle>
        <DialogDescription className="mt-2 text-sm leading-6 text-muted-foreground">
          修改后所有旧会话都会失效，需要使用新密码重新登录。新密码至少 8 个字符。
        </DialogDescription>
        <form
          id="glean-change-password"
          name="change-password"
          method="post"
          action="/api/v1/auth/password"
          className="mt-6 space-y-4"
          onSubmit={submit}
          aria-busy={pending}
        >
          <input
            type="text"
            id="change-password-username"
            name="username"
            autoComplete="username"
            value={username || ''}
            readOnly
            hidden
          />
          <label className="block text-sm font-medium">
            当前密码
            <input
              className="form-input mt-2"
              type="password"
              autoComplete="current-password"
              id="change-password-current"
              name="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              required
              disabled={pending}
            />
          </label>
          <label className="block text-sm font-medium">
            新密码
            <input
              className="form-input mt-2"
              type="password"
              autoComplete="new-password"
              id="change-password-new"
              name="new-password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              required
              disabled={pending}
            />
          </label>
          <label className="block text-sm font-medium">
            确认密码
            <input
              className="form-input mt-2"
              type="password"
              autoComplete="new-password"
              id="change-password-confirm"
              name="confirm-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
              disabled={pending}
            />
          </label>
          {error && (
            <p role="alert" className="text-sm text-red-700">
              {error}
            </p>
          )}
          {pending && (
            <p role="status" className="text-xs text-muted-foreground">
              正在修改密码，请勿关闭页面。
            </p>
          )}
          <div className="flex justify-end gap-3">
            <Button type="button" variant="outline" disabled={pending} onClick={close}>
              取消
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? '正在修改…' : '确认修改'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
