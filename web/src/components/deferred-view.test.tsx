import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { deferredView } from './deferred-view'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

describe('deferred views', () => {
  it('loads only when mounted and uses current props after an interrupted render', async () => {
    const gate = deferred<{ default: (props: { topic: string }) => React.ReactNode }>()
    const load = vi.fn(() => gate.promise)
    const View = deferredView(load)
    expect(load).not.toHaveBeenCalled()
    const page = render(<View topic="old" />)
    expect(screen.getByRole('status')).toHaveTextContent('正在打开页面')
    page.rerender(<View topic="new" />)
    await act(async () => gate.resolve({ default: ({ topic }) => <h2>{topic}</h2> }))
    expect(screen.getByRole('heading')).toHaveTextContent('new')
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('does not resurrect a view after navigation away and can open it again', async () => {
    const gate = deferred<{ default: () => React.ReactNode }>()
    const View = deferredView(() => gate.promise)
    const page = render(<View />)
    page.rerender(<h2>论文工作台</h2>)
    await act(async () => gate.resolve({ default: () => <h2>采集状态</h2> }))
    expect(screen.getByRole('heading')).toHaveTextContent('论文工作台')
    page.rerender(<View />)
    expect(await screen.findByRole('heading', { name: '采集状态' })).toBeInTheDocument()
  })

  it('retries a rejected import without remounting the surrounding session', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const first = deferred<{ default: () => React.ReactNode }>()
    const load = vi
      .fn()
      .mockImplementationOnce(() => first.promise)
      .mockResolvedValue({ default: () => <h2>用户管理</h2> })
    const View = deferredView(load)
    render(
      <>
        <input aria-label="session" defaultValue="signed in" />
        <View />
      </>,
    )
    const session = screen.getByRole('textbox')
    await act(async () => first.reject(new Error('offline')))
    expect(screen.getByRole('alert')).toHaveTextContent('此页面暂时无法打开')
    fireEvent.click(screen.getByRole('button', { name: '重试打开页面' }))
    expect(await screen.findByRole('heading', { name: '用户管理' })).toBeInTheDocument()
    expect(load).toHaveBeenCalledTimes(2)
    expect(screen.getByRole('textbox')).toBe(session)
    expect(session).toHaveValue('signed in')
  })

  it('keeps the recovered module when leaving and reopening after retry', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const load = vi
      .fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValue({ default: () => <h2>已恢复</h2> })
    const View = deferredView(load)
    const page = render(<View />)
    await screen.findByRole('alert')
    fireEvent.click(screen.getByRole('button', { name: '重试打开页面' }))
    await screen.findByRole('heading', { name: '已恢复' })
    page.rerender(<h2>论文工作台</h2>)
    page.rerender(<View />)
    expect(await screen.findByRole('heading', { name: '已恢复' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('ignores a late failed import after navigation away', async () => {
    const gate = deferred<{ default: () => React.ReactNode }>()
    const View = deferredView(() => gate.promise)
    const page = render(<View />)
    page.rerender(<h2>论文工作台</h2>)
    await act(async () => gate.reject(new Error('offline')))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('heading')).toHaveTextContent('论文工作台')
  })
})
