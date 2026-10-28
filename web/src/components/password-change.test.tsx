import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { ApiError, api } from '../lib/api'
import { PasswordChangeDialog } from './password-change'

afterEach(() => vi.restoreAllMocks())
function mount() {
  const onClose = vi.fn(),
    onChanged = vi.fn()
  const view = render(<PasswordChangeDialog onClose={onClose} onChanged={onChanged} />)
  return { ...view, onClose, onChanged, user: userEvent.setup() }
}
async function fill(user: ReturnType<typeof userEvent.setup>, confirmation = 'new-password') {
  await user.type(screen.getByLabelText('当前密码'), 'old-password')
  await user.type(screen.getByLabelText('新密码', { exact: true }), 'new-password')
  await user.type(screen.getByLabelText('确认密码'), confirmation)
}
it('submits exact credentials once and returns to login after success', async () => {
  const change = vi.spyOn(api, 'changePassword').mockResolvedValue()
  const { user, onChanged } = mount()
  await fill(user)
  await user.click(screen.getByRole('button', { name: '确认修改' }))
  expect(change).toHaveBeenCalledExactlyOnceWith('old-password', 'new-password')
  expect(onChanged).toHaveBeenCalledOnce()
  expect(screen.getByLabelText('当前密码')).toHaveValue('')
})
it('rejects mismatched confirmation without a request', async () => {
  const change = vi.spyOn(api, 'changePassword')
  const { user } = mount()
  await fill(user, 'different-password')
  await user.click(screen.getByRole('button', { name: '确认修改' }))
  expect(screen.getByRole('alert')).toHaveTextContent('不一致')
  expect(change).not.toHaveBeenCalled()
})
it('displays a safe error and clears secrets after backend rejection', async () => {
  vi.spyOn(api, 'changePassword').mockRejectedValue(new ApiError('old-password', 400))
  const { user, onChanged } = mount()
  await fill(user)
  await user.click(screen.getByRole('button', { name: '确认修改' }))
  expect(screen.getByRole('alert')).toHaveTextContent('请检查当前密码')
  expect(screen.getByRole('alert')).not.toHaveTextContent('old-password')
  expect(screen.getByLabelText('当前密码')).toHaveValue('')
  expect(screen.getByLabelText('新密码', { exact: true })).toHaveValue('')
  expect(onChanged).not.toHaveBeenCalled()
})
it('prevents duplicate submits and unsafe dismissal while pending', async () => {
  let resolve!: () => void
  const change = vi.spyOn(api, 'changePassword').mockReturnValue(
    new Promise<void>((r) => {
      resolve = r
    }),
  )
  const { user, onClose, onChanged } = mount()
  await fill(user)
  const submit = screen.getByRole('button', { name: '确认修改' })
  await user.dblClick(submit)
  fireEvent.submit(submit.closest('form')!)
  await user.keyboard('{Escape}')
  expect(screen.getByRole('button', { name: '取消' })).toBeDisabled()
  expect(screen.getByLabelText('当前密码')).toBeDisabled()
  expect(screen.getByRole('dialog')).toBeVisible()
  expect(onClose).not.toHaveBeenCalled()
  expect(change).toHaveBeenCalledOnce()
  await act(async () => resolve())
  expect(onChanged).toHaveBeenCalledOnce()
})
it('cancel and Escape clear local fields without submitting', async () => {
  const change = vi.spyOn(api, 'changePassword')
  const { user, onClose } = mount()
  await fill(user)
  await user.click(screen.getByRole('button', { name: '取消' }))
  expect(onClose).toHaveBeenCalledOnce()
  expect(screen.getByLabelText('当前密码')).toHaveValue('')
  await user.keyboard('{Escape}')
  expect(onClose).toHaveBeenCalledTimes(2)
  expect(change).not.toHaveBeenCalled()
})
it('does not invoke a stale component callback after unmount', async () => {
  let resolve!: () => void
  vi.spyOn(api, 'changePassword').mockReturnValue(
    new Promise<void>((r) => {
      resolve = r
    }),
  )
  const { user, unmount, onChanged } = mount()
  await fill(user)
  await user.click(screen.getByRole('button', { name: '确认修改' }))
  unmount()
  await act(async () => resolve())
  expect(onChanged).not.toHaveBeenCalled()
})

it.each([
  ['short', '至少需要 8 个字符'],
  ['old-password', '不能与当前密码相同'],
])('rejects invalid new password locally: %s', async (next, message) => {
  const change = vi.spyOn(api, 'changePassword')
  const { user } = mount()
  await user.type(screen.getByLabelText('当前密码'), 'old-password')
  await user.type(screen.getByLabelText('新密码', { exact: true }), next)
  await user.type(screen.getByLabelText('确认密码'), next)
  await user.click(screen.getByRole('button', { name: '确认修改' }))
  expect(screen.getByRole('alert')).toHaveTextContent(message)
  expect(change).not.toHaveBeenCalled()
})
