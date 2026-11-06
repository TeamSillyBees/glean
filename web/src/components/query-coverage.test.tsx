import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { QueryCoverage } from './query-coverage'
import type { QueryProgress } from './run-types'
const query: QueryProgress = {
  queryId: 'medical',
  queryVersion: 'v2',
  topic: 'biomed-ai',
  source: 'pubmed',
  mode: 'backfill',
  query: 'cancer[Title/Abstract]',
  window: { start: '2020-01-01T00:00:00Z', end: '2020-02-01T00:00:00Z' },
  targetEnd: '2026-11-01T00:00:00Z',
  status: 'partial_budget',
  requests: 3,
  pages: 2,
  observed: 50,
  quarantined: 1,
  newCandidates: 20,
  warnings: ['Source-specific match semantics'],
}
describe('query coverage', () => {
  it('separates partial budgets from completed and empty windows without leaking internals', () => {
    const internal = {
      ...query,
      candidateKeys: ['private-candidate'],
      cursor: { token: 'private-token' },
    }
    render(
      <QueryCoverage
        queries={{
          partial: internal,
          completed: { ...query, queryId: 'finished', status: 'completed' },
          empty: { ...query, queryId: 'empty', status: 'completed_empty', observed: 0 },
        }}
      />,
    )
    fireEvent.click(screen.getByText('查询覆盖：2 / 3 个窗口完成'))
    for (const text of ['部分完成 · 预算已用尽', '窗口已完成', '窗口已完成 · 无命中'])
      expect(screen.getByText(text)).toBeVisible()
    expect(screen.getAllByText(/3 次请求 · 2 页/)).toHaveLength(3)
    expect(screen.getAllByText(/2020-01-01T00:00:00Z/)).toHaveLength(3)
    expect(screen.getAllByText(/来源限制：Source-specific/)).toHaveLength(3)
    expect(screen.queryByText(/private-token|private-candidate/)).not.toBeInTheDocument()
  })
  it('shows unavailable planned sources as unexecuted', () => {
    render(
      <QueryCoverage
        queries={{
          missing: { ...query, status: 'unavailable', warnings: ['Source not configured'] },
        }}
      />,
    )
    fireEvent.click(screen.getByText('查询覆盖：0 / 1 个窗口完成'))
    expect(screen.getByText('未执行 · 来源未启用')).toBeVisible()
    expect(screen.getByText('来源限制：Source not configured')).toBeVisible()
  })
  it('separates overall range from current partition and warns for nonadvancing scans', () => {
    render(
      <QueryCoverage
        queries={{
          manual: {
            ...query,
            coverageWindow: { start: '2010-01-01T00:00:00Z', end: '2026-11-01T00:00:00Z' },
            advanceCheckpoint: false,
          },
        }}
      />,
    )
    fireEvent.click(screen.getByText('查询覆盖：0 / 1 个窗口完成'))
    expect(
      screen.getByText('整体请求范围：2010-01-01T00:00:00Z → 2026-11-01T00:00:00Z'),
    ).toBeVisible()
    expect(
      screen.getByText('当前时间分区：2020-01-01T00:00:00Z → 2020-02-01T00:00:00Z'),
    ).toBeVisible()
    expect(screen.getByText(/不推进增量检查点/)).toBeVisible()
  })
  it('uses legacy window without inventing a checkpoint warning', () => {
    render(<QueryCoverage queries={{ old: query }} />)
    fireEvent.click(screen.getByText('查询覆盖：0 / 1 个窗口完成'))
    expect(
      screen.getByText('查询窗口（旧记录）：2020-01-01T00:00:00Z → 2020-02-01T00:00:00Z'),
    ).toBeVisible()
    expect(screen.queryByText(/不推进增量检查点|整体请求范围/)).not.toBeInTheDocument()
  })
  it('does not infer coverage from absent legacy query progress', () => {
    render(<QueryCoverage />)
    expect(screen.getByText(/此记录无逐查询覆盖数据/)).toBeVisible()
    expect(screen.queryByText(/个窗口完成/)).not.toBeInTheDocument()
  })
})
