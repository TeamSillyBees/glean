import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useRef, useState, type ComponentType } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { paper } from '../test/fixtures'
import { createPaperDetail } from './create-paper-detail'
import type { PaperBodyProps } from './deferred-paper-body'
import { TooltipProvider } from './ui/tooltip'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
type BodyModule = { default: ComponentType<PaperBodyProps> }
const loaded: BodyModule = { default: ({ id }) => <p>正文 {id}</p> }
function mount(load: () => Promise<BodyModule>) {
  const Detail = createPaperDetail(load)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Harness({ account }: { account: string }) {
    const [id, setId] = useState<string | null>(null)
    const trigger = useRef<HTMLButtonElement>(null)
    return (
      <>
        <button ref={trigger} onClick={() => setId('123')}>
          打开 {account}
        </button>
        <button onClick={() => setId('456')}>下一篇</button>
        <Detail
          id={id}
          onClose={() => setId(null)}
          notify={() => {}}
          restoreFocus={() => trigger.current?.focus()}
        />
      </>
    )
  }
  function tree(account: string) {
    return (
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <Harness key={account} account={account} />
        </TooltipProvider>
      </QueryClientProvider>
    )
  }
  const view = render(tree('甲'))
  return {
    ...view,
    client,
    switchAccount: () => {
      client.clear()
      view.rerender(tree('乙'))
    },
  }
}
function open(account = '甲') {
  const trigger = screen.getByRole('button', { name: `打开 ${account}` })
  trigger.focus()
  fireEvent.click(trigger)
  return trigger
}

describe('deferred paper dialog lifecycle', () => {
  it('does not import while closed; pending shell is named, described and dismissible with focus restored', async () => {
    const gate = deferred<BodyModule>()
    const load = vi.fn(() => gate.promise)
    mount(load)
    expect(load).not.toHaveBeenCalled()
    const trigger = open()
    const dialog = screen.getByRole('dialog', { name: '论文详情' })
    expect(dialog).toHaveAccessibleDescription('原始证据与个人阅读状态分开保存。')
    expect(screen.getByRole('status')).toHaveTextContent('正在打开论文详情')
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(trigger).toHaveFocus())
    await act(async () => gate.resolve(loaded))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    open()
    expect(await screen.findByText('正文 123')).toBeInTheDocument()
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('keeps the same shell and uses the newest paper when selection changes during import', async () => {
    const gate = deferred<BodyModule>()
    mount(() => gate.promise)
    open()
    const shell = screen.getByRole('dialog')
    fireEvent.click(screen.getByRole('button', { name: '下一篇', hidden: true }))
    await act(async () => gate.resolve(loaded))
    expect(screen.getByRole('dialog')).toBe(shell)
    expect(screen.getByText('正文 456')).toBeInTheDocument()
    expect(screen.queryByText('正文 123')).not.toBeInTheDocument()
  })

  it('keeps failed import close/Escape available, retries within the same shell and caches recovery', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const load = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(loaded)
    mount(load)
    const trigger = open()
    const shell = screen.getByRole('dialog')
    await screen.findByRole('alert')
    fireEvent.keyDown(shell, { key: 'Escape' })
    await waitFor(() => expect(trigger).toHaveFocus())
    open()
    await screen.findByRole('alert')
    const retryShell = screen.getByRole('dialog')
    fireEvent.click(screen.getByRole('button', { name: '重试打开详情' }))
    await screen.findByText('正文 123')
    expect(screen.getByRole('dialog')).toBe(retryShell)
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(trigger).toHaveFocus())
    open()
    expect(await screen.findByText('正文 123')).toBeInTheDocument()
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('does not reopen on a late rejection after close', async () => {
    const gate = deferred<BodyModule>()
    mount(() => gate.promise)
    open()
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    await act(async () => gate.reject(new Error('offline')))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('ignores a late detail response after switching papers', async () => {
    const first = deferred<typeof paper>()
    vi.spyOn(api, 'paper').mockImplementation((id) =>
      id === '123'
        ? first.promise
        : Promise.resolve({ ...paper, id, title: '最新论文', titleZh: '' }),
    )
    const body = await import('./paper-detail-body')
    mount(async () => body)
    open()
    await waitFor(() => expect(api.paper).toHaveBeenCalledWith('123', expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole('button', { name: '下一篇', hidden: true }))
    await screen.findByRole('heading', { name: '最新论文' })
    await act(async () => first.resolve({ ...paper, title: '旧论文', titleZh: '' }))
    expect(screen.queryByRole('heading', { name: '旧论文' })).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '最新论文' })).toBeInTheDocument()
  })

  it('does not fetch old-account data when the body finishes importing after logout; new account can open', async () => {
    const gate = deferred<BodyModule>()
    const get = vi.spyOn(api, 'paper').mockResolvedValue(paper)
    const view = mount(() => gate.promise)
    open()
    view.switchAccount()
    await act(async () => gate.resolve(await import('./paper-detail-body')))
    expect(get).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(view.client.getQueryCache().getAll()).toHaveLength(0)
    open('乙')
    await screen.findByRole('group', { name: '个人阅读与隐藏' })
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('123', expect.any(AbortSignal))
  })
})
