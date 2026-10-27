import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import type { Paper, PaperPage } from '../lib/types'
import { paper } from '../test/fixtures'
import { PaperDetail } from './papers'
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
let current: Paper
let unmount: () => void
beforeEach(() => {
  current = structuredClone(paper)
  vi.spyOn(api, 'paper').mockImplementation(async () => structuredClone(current))
  vi.spyOn(api, 'state').mockImplementation(async (_, hidden, readStatus) => {
    current = { ...current, hidden, readStatus }
    return { hidden, readStatus }
  })
})
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData<PaperPage>(['papers', 'test'], {
    items: [structuredClone(current)],
    total: 1,
    page: 1,
    pageSize: 20,
  })
  function Harness() {
    const [id, setId] = useState<string | null>('123')
    return (
      <>
        <button onClick={() => setId('123')}>重开详情</button>
        <PaperDetail id={id} onClose={() => setId(null)} notify={() => {}} />
      </>
    )
  }
  const view = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <Harness />
      </TooltipProvider>
    </QueryClientProvider>,
  )
  unmount = view.unmount
  return client
}

describe('personal paper state consistency', () => {
  it('merges the canonical write response before a subsequent hide, without waiting for a detail GET', async () => {
    const client = mount()
    await screen.findByRole('button', { name: '已读' })
    // Any unnecessary detail refetch would remain blocked, reproducing the old stale closure.
    const delayed = deferred<Paper>()
    vi.mocked(api.paper).mockReturnValue(delayed.promise)
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '隐藏' })).toBeEnabled())
    expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('read')
    expect(client.getQueryData<PaperPage>(['papers', 'test'])?.items[0].readStatus).toBe('read')
    fireEvent.click(screen.getByRole('button', { name: '隐藏' }))
    await waitFor(() => expect(api.state).toHaveBeenLastCalledWith('123', true, 'read'))
    await waitFor(() => expect(screen.getByRole('button', { name: '恢复显示' })).toBeEnabled())
    expect(current).toMatchObject({ hidden: true, readStatus: 'read' })
    fireEvent.click(screen.getByRole('button', { name: '恢复显示' }))
    await waitFor(() => expect(current).toMatchObject({ hidden: false, readStatus: 'read' }))
  })

  it('blocks repeated writes across close/reopen and ignores a late pre-write GET', async () => {
    const client = mount()
    await screen.findByRole('button', { name: '已读' })
    const write = deferred<{ hidden: boolean; readStatus: 'read' }>()
    vi.mocked(api.state).mockReturnValueOnce(write.promise)
    const oldGet = deferred<Paper>()
    vi.mocked(api.paper).mockReturnValueOnce(oldGet.promise)
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    await waitFor(() => expect(api.state).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '重开详情' }))
    expect(await screen.findByRole('button', { name: '隐藏' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '隐藏' }))
    expect(api.state).toHaveBeenCalledTimes(1)
    await act(async () => {
      write.resolve({ hidden: false, readStatus: 'read' })
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '隐藏' })).toBeEnabled())
    await act(async () => {
      oldGet.resolve(structuredClone(paper))
    })
    expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('read')
    fireEvent.click(screen.getByRole('button', { name: '隐藏' }))
    await waitFor(() => expect(api.state).toHaveBeenLastCalledWith('123', true, 'read'))
  })

  it('does not optimistically persist failed writes and allows a deliberate retry', async () => {
    const client = mount()
    await screen.findByRole('button', { name: '已读' })
    vi.mocked(api.state).mockRejectedValueOnce(new Error('保存失败，请重试'))
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('保存失败，请重试')
    expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('unread')
    expect(screen.getByRole('button', { name: '已读' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    await waitFor(() =>
      expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('read'),
    )
    expect(api.state).toHaveBeenCalledTimes(2)
  })
  it('never repopulates or overwrites a new session cache after the old session is cleared', async () => {
    const client = mount()
    await screen.findByRole('button', { name: '已读' })
    const write = deferred<{ hidden: boolean; readStatus: 'read' }>()
    vi.mocked(api.state).mockReturnValueOnce(write.promise)
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    await waitFor(() => expect(api.state).toHaveBeenCalledTimes(1))
    unmount()
    client.clear()
    const nextUserPaper = { ...paper, hidden: true, readStatus: 'reading' as const }
    client.setQueryData(['paper', '123'], nextUserPaper)
    await act(async () => {
      write.resolve({ hidden: false, readStatus: 'read' })
    })
    expect(client.getQueryData(['paper', '123'])).toEqual(nextUserPaper)
    expect(client.getQueryData(['papers', 'test'])).toBeUndefined()
  })

  it('keeps cached detail usable after closing and reopening a failed write', async () => {
    const client = mount()
    await screen.findByRole('button', { name: '已读' })
    const write = deferred<{ hidden: boolean; readStatus: 'read' }>()
    vi.mocked(api.state).mockReturnValueOnce(write.promise)
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    await waitFor(() => expect(api.state).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '重开详情' }))
    await screen.findByRole('button', { name: '隐藏' })
    await act(async () => {
      write.reject(new Error('保存失败'))
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '已读' })).toBeEnabled())
    expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('unread')
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    await waitFor(() =>
      expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('read'),
    )
  })
  it('does not start an old operation if the session switches while pending GETs are canceled', async () => {
    const client = mount()
    await screen.findByRole('button', { name: '已读' })
    const cancel = deferred<void>()
    vi.spyOn(client, 'cancelQueries').mockReturnValue(cancel.promise)
    fireEvent.click(screen.getByRole('button', { name: '已读' }))
    unmount()
    client.clear()
    client.setQueryData(['paper', '123'], { ...paper, readStatus: 'reading' })
    await act(async () => {
      cancel.resolve()
    })
    expect(api.state).not.toHaveBeenCalled()
    expect(client.getQueryData<Paper>(['paper', '123'])?.readStatus).toBe('reading')
  })
})
