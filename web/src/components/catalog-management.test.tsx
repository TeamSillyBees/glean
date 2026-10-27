import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from '../lib/api'
import type { Paper, ManualReview, Correction } from '../lib/types'
import { paper, topics } from '../test/fixtures'
import { PaperDetail } from './papers'
import { TooltipProvider } from './ui/tooltip'
let current: Paper
beforeEach(() => {
  current = structuredClone(paper)
  current.memberships.push({ ...structuredClone(current.memberships[0]), topic: 'llm-agent' })
  vi.spyOn(api, 'paper').mockImplementation(async () => structuredClone(current))
  vi.spyOn(api, 'review').mockImplementation(async (_, topic, input) => {
    const result: ManualReview = {
      ...input,
      version: input.expectedVersion + 1,
      updatedAt: '2026-10-27',
      updatedBy: '9',
    }
    current.memberships = current.memberships.map((m) =>
      m.topic === topic ? { ...m, manualReview: result } : m,
    )
    return result
  })
  vi.spyOn(api, 'correct').mockImplementation(async (_, fields, reason, version) => {
    const correction: Correction = {
      fields,
      reason,
      version: version + 1,
      updatedAt: '2026-10-27',
      updatedBy: '9',
    }
    current = { ...current, ...paper, ...fields, memberships: current.memberships, correction }
    return correction
  })
  vi.spyOn(api, 'history').mockResolvedValue({ items: [] })
})
function mount(canManage = true) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const notify = vi.fn()
  function Harness() {
    const [id, setId] = useState<string | null>('123')
    return (
      <>
        <button onClick={() => setId('123')}>重开详情</button>
        <PaperDetail
          id={id}
          onClose={() => setId(null)}
          canManage={canManage}
          topics={topics}
          activeTopic="llm-agent"
          notify={notify}
        />
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
  return { ...view, client, notify }
}
async function openManagement() {
  await screen.findByRole('heading', { name: paper.titleZh })
  fireEvent.click(screen.getByText('论文管理'))
}
describe('shared paper management separate from personal state', () => {
  it('does not expose management controls to ordinary readers', async () => {
    mount(false)
    await screen.findByRole('heading', { name: paper.titleZh })
    expect(screen.queryByText('论文管理')).not.toBeInTheDocument()
    expect(screen.getByRole('group', { name: '个人阅读与隐藏' })).toBeInTheDocument()
  })
  it('requires an explicit first review decision and never demotes historical papers by default', async () => {
    mount()
    await openManagement()
    const form = screen.getByRole('form', { name: '审核 大模型知识工程' })
    expect(within(form).getByLabelText('审核结论')).toHaveValue('')
    fireEvent.change(within(form).getByLabelText('审核备注'), { target: { value: '仅备注' } })
    fireEvent.submit(form)
    expect(api.review).not.toHaveBeenCalled()
  })
  it('rejects blank or out-of-range correction years before transport', async () => {
    mount()
    await openManagement()
    const form = screen.getByRole('form', { name: '元数据纠错' })
    fireEvent.click(within(form).getByLabelText('纠正发表年份'))
    fireEvent.change(within(form).getByLabelText('纠错原因'), { target: { value: '核对年份' } })
    for (const value of ['', '-1', '10000', '2026.5']) {
      fireEvent.change(within(form).getByLabelText('发表年份'), { target: { value } })
      fireEvent.submit(form)
    }
    expect(api.correct).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('发表年份必须')
    fireEvent.change(within(form).getByLabelText('发表年份'), { target: { value: '2025' } })
    fireEvent.submit(form)
    await waitFor(() =>
      expect(api.correct).toHaveBeenCalledWith('123', { year: 2025 }, '核对年份', 0),
    )
  })
  it('orders the active topic first and sends scoped review/version without touching reading state', async () => {
    current.memberships[1].manualReview = {
      status: 'pending',
      note: '原备注',
      labels: ['原标签'],
      version: 3,
      updatedAt: '',
      updatedBy: '9',
    }
    mount()
    await openManagement()
    const forms = screen.getAllByRole('form')
    expect(forms[0]).toHaveAttribute('aria-label', '审核 大模型知识工程')
    const form = within(forms[0])
    fireEvent.change(form.getByLabelText('审核结论'), { target: { value: 'excluded' } })
    fireEvent.change(form.getByLabelText('审核备注'), { target: { value: '与该主题无关' } })
    fireEvent.change(form.getByLabelText('手动标签'), { target: { value: '复核，方法' } })
    fireEvent.click(form.getByRole('button', { name: '保存审核' }))
    await waitFor(() =>
      expect(api.review).toHaveBeenCalledWith('123', 'llm-agent', {
        status: 'excluded',
        note: '与该主题无关',
        labels: ['复核', '方法'],
        expectedVersion: 3,
      }),
    )
    await waitFor(() => expect(current.memberships[1].manualReview?.version).toBe(4))
    expect(current.memberships[0].manualReview).toBeUndefined()
    expect(current.readStatus).toBe(paper.readStatus)
    expect(current.hidden).toBe(paper.hidden)
  })
  it('corrects metadata and drops an override to restore its source value', async () => {
    mount()
    await openManagement()
    const form = within(screen.getByRole('form', { name: '元数据纠错' }))
    fireEvent.click(form.getByLabelText('纠正原文标题'))
    fireEvent.change(form.getByLabelText('原文标题'), { target: { value: 'Corrected title' } })
    fireEvent.change(form.getByLabelText('纠错原因'), { target: { value: '依据来源核对' } })
    fireEvent.click(form.getByRole('button', { name: '保存纠错' }))
    await waitFor(() =>
      expect(api.correct).toHaveBeenCalledWith(
        '123',
        { title: 'Corrected title' },
        '依据来源核对',
        0,
      ),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '保存纠错' })).toBeEnabled())
    fireEvent.click(screen.getByLabelText('纠正原文标题'))
    fireEvent.click(screen.getByRole('button', { name: '保存纠错' }))
    await waitFor(() => expect(api.correct).toHaveBeenLastCalledWith('123', {}, '依据来源核对', 1))
    expect(current.title).toBe(paper.title)
  })
  it('fetches history only on expansion and never submits DOI or arXiv changes', async () => {
    mount()
    await openManagement()
    expect(api.history).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('修改历史'))
    await screen.findByText('暂无人工修改记录。')
    expect(api.history).toHaveBeenCalledWith('123', expect.any(AbortSignal))
    expect(screen.queryByLabelText('纠正DOI')).not.toBeInTheDocument()
  })
  it('conflict explicitly reloads without resubmitting', async () => {
    vi.mocked(api.review).mockRejectedValue(new ApiError('stale', 409))
    mount()
    await openManagement()
    const form = within(screen.getByRole('form', { name: '审核 大模型知识工程' }))
    fireEvent.change(form.getByLabelText('审核结论'), { target: { value: 'included' } })
    fireEvent.click(form.getByRole('button', { name: '保存审核' }))
    fireEvent.click(await screen.findByRole('button', { name: '重新读取管理记录' }))
    await waitFor(() => expect(api.paper).toHaveBeenCalledTimes(2))
    expect(api.review).toHaveBeenCalledTimes(1)
  })
  it('keeps writes locked after closing and reopening a pending paper', async () => {
    let resolve!: (review: ManualReview) => void
    vi.mocked(api.review).mockReturnValue(
      new Promise((yes) => {
        resolve = yes
      }),
    )
    mount()
    await openManagement()
    fireEvent.change(
      within(screen.getByRole('form', { name: '审核 大模型知识工程' })).getByLabelText('审核结论'),
      { target: { value: 'included' } },
    )
    fireEvent.click(
      within(screen.getByRole('form', { name: '审核 大模型知识工程' })).getByRole('button', {
        name: '保存审核',
      }),
    )
    await waitFor(() => expect(api.review).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    fireEvent.click(screen.getByRole('button', { name: '重开详情' }))
    await openManagement()
    expect(screen.getByRole('button', { name: '保存纠错' })).toBeDisabled()
    await act(async () =>
      resolve({
        status: 'included',
        note: '',
        labels: [],
        version: 1,
        updatedAt: '',
        updatedBy: '9',
      }),
    )
    await waitFor(() => expect(screen.getByRole('button', { name: '保存纠错' })).toBeEnabled())
    expect(api.review).toHaveBeenCalledTimes(1)
  })
  it('does not invalidate or notify a later session on late completion', async () => {
    let resolve!: (review: ManualReview) => void
    vi.mocked(api.review).mockReturnValue(
      new Promise((yes) => {
        resolve = yes
      }),
    )
    const { client, notify, unmount } = mount()
    await openManagement()
    fireEvent.change(
      within(screen.getByRole('form', { name: '审核 大模型知识工程' })).getByLabelText('审核结论'),
      { target: { value: 'included' } },
    )
    fireEvent.click(
      within(screen.getByRole('form', { name: '审核 大模型知识工程' })).getByRole('button', {
        name: '保存审核',
      }),
    )
    await waitFor(() => expect(api.review).toHaveBeenCalled())
    unmount()
    client.clear()
    client.setQueryData(['paper', '123'], { title: 'New session' })
    await act(async () =>
      resolve({
        status: 'included',
        note: '',
        labels: [],
        version: 1,
        updatedAt: '',
        updatedBy: '9',
      }),
    )
    expect(client.getQueryData(['paper', '123'])).toEqual({ title: 'New session' })
    expect(notify).not.toHaveBeenCalled()
  })
})
