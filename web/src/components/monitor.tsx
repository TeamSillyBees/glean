import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Activity, RefreshCw } from 'lucide-react'
import { request } from '../lib/api'
import { date, number } from '../lib/utils'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
import type { Run } from './run-types'
import { QueryCoverage } from './query-coverage'
import { RunRecovery } from './run-recovery'
import { RunAudit } from './run-audit'
export { SourcesView } from './source-health'
export { FreshnessStrip } from './freshness-strip'
const validDate = (value?: string) =>
  value && !value.startsWith('0001-') ? date(value) : '尚未建立'
const statusLabels: Record<string, string> = {
  completed: '已完成',
  completed_with_gaps: '已结束 · 存在未执行路线',
  completed_with_quarantine: '完成 · 有隔离记录',
  completed_with_errors: '已结束 · 存在失败',
  retry_wait: '等待重试',
  canceled: '已取消',
  running: '进行中',
  failed: '失败',
  pending: '待运行',
  retrying: '等待重试',
  partial: '部分完成',
  paused: '已暂停',
}
export function RunsView({ canManage = false }: { canManage?: boolean }) {
  const [showArchive, setShowArchive] = useState(false)
  const archive = useInfiniteQuery({
    queryKey: ['runs-archive'],
    enabled: showArchive,
    initialPageParam: '',
    queryFn: ({ signal, pageParam }) =>
      request<{ items: Run[]; nextBefore: string | null }>(
        `/api/v1/monitor/archive/runs?limit=20${pageParam ? `&before=${encodeURIComponent(pageParam)}` : ''}`,
        { signal },
      ),
    getNextPageParam: (page) => page.nextBefore ?? undefined,
  })
  const query = useQuery({
    queryKey: ['runs'],
    queryFn: ({ signal }) => request<{ items: Run[] }>('/api/v1/monitor/runs', { signal }),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  if (query.isLoading) return <Loading label="正在读取扫描记录…" />
  if (query.isError && !query.data)
    return <ErrorState error={query.error} retry={() => query.refetch()} />
  const items = [...(query.data?.items || [])]
  for (const page of archive.data?.pages ?? [])
    for (const run of page.items) if (!items.some((item) => item.id === run.id)) items.push(run)
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground" role="status" aria-live="polite">
          {query.isFetching
            ? '正在刷新扫描状态…'
            : `页面可见时每 60 秒更新 · 最近更新 ${new Date(query.dataUpdatedAt).toLocaleTimeString('zh-CN')}`}
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => query.refetch()}
        >
          <RefreshCw className={query.isFetching ? 'animate-spin' : undefined} />
          刷新扫描状态
        </Button>
      </div>
      {query.isError && (
        <div
          role="alert"
          className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900"
        >
          刷新失败，以下保留上次成功读取的记录，可能不是最新进度。
          {query.error instanceof Error && <p>{query.error.message}</p>}
        </div>
      )}
      <p className="text-sm leading-6 text-muted-foreground">
        采集、模型处理与目录写入分开记录；模型阶段状态不代表科学审读或正式纳入。
      </p>
      {!items.length ? (
        <Empty
          title="还没有扫描记录"
          description="后端运行后会留下可追溯的扫描记录。此查询界面不会启动采集或调用模型。"
        />
      ) : (
        items.map((run) => (
          <section
            key={run.id}
            id={`scan-run-${run.id}`}
            tabIndex={-1}
            aria-label={`运行 ${run.id}`}
            className="min-w-0 rounded-xl border bg-card p-4 sm:p-5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <div className="flex flex-wrap items-center gap-3">
              <Activity className="size-5 shrink-0 text-primary" aria-hidden="true" />
              <div>
                <h2 className="font-semibold">第 {run.number} 次扫描</h2>
                <p className="mt-1 text-xs text-muted-foreground">
                  {validDate(run.startedAt)} · 模板 {run.templateVersion}
                </p>
              </div>
              <Badge
                className="ml-auto"
                tone={
                  run.status === 'completed'
                    ? 'green'
                    : ['failed', 'completed_with_errors'].includes(run.status)
                      ? 'red'
                      : run.status === 'canceled'
                        ? 'neutral'
                        : 'amber'
                }
              >
                {statusLabels[run.status] || run.status}
              </Badge>
              <Badge tone="violet">{run.mode === 'mock' ? '历史模拟运行' : '真实来源采集'}</Badge>
            </div>
            <RunAudit run={run} canManage={canManage} />
            {run.status === 'completed_with_gaps' && (
              <p className="mt-4 text-sm leading-6 text-amber-800">
                本次运行已结束，但部分计划路线未执行，不代表整个检索计划已覆盖。
              </p>
            )}
            {!!run.unavailableQueries?.length && (
              <div className="mt-4 space-y-2">
                <h3 className="text-sm font-medium">未启用来源的计划路线</h3>
                <QueryCoverage
                  queries={Object.fromEntries(
                    run.unavailableQueries.map((item, index) => [String(index), item]),
                  )}
                />
              </div>
            )}
            {run.status === 'completed_with_errors' && (
              <p className="mt-4 text-sm leading-6 text-red-700">
                本次运行已结束，但有来源未完成。已获取的数据会保留，不代表整个扫描窗口已完成。
              </p>
            )}
            {run.status === 'retry_wait' && (
              <p className="mt-4 text-sm leading-6 text-amber-800">
                正在等待来源退避或重试条件，刷新此页面不会提前发起采集请求。
              </p>
            )}
            <RunRecovery run={run} runs={items} historyStale={query.isError} />
            <div className="mt-5 grid gap-4 border-t pt-5 sm:grid-cols-2">
              <p className="text-xs text-muted-foreground">
                固定结束水位{' '}
                <strong className="ml-2 font-medium text-foreground">
                  {validDate(run.fixedEnd)}
                </strong>
              </p>
              <p className="text-xs text-muted-foreground">
                完成时间{' '}
                <strong className="ml-2 font-medium text-foreground">
                  {validDate(run.finishedAt)}
                </strong>
              </p>
            </div>
            <div className="mt-4 divide-y border-t">
              {Object.entries(run.sources || {}).map(([name, progress]) => (
                <div
                  key={name}
                  className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 py-3 text-sm"
                >
                  <span className="min-w-24 font-medium">{name}</span>
                  <span className="text-muted-foreground">
                    {progress.pages} 页 · {progress.attempts} 次尝试
                    {progress.observed != null && ` · ${number(progress.observed)} 条观察`}
                    {progress.projected != null && ` · ${number(progress.projected)} 条写入`}
                  </span>
                  <span className="ml-auto flex flex-wrap gap-2">
                    {(progress.quarantined || 0) > 0 && (
                      <Badge tone="amber">
                        {number(progress.quarantined)} 条隔离
                        {progress.handled ? ' · 已处理其余记录' : ''}
                      </Badge>
                    )}
                    <Badge tone={progress.acquired ? 'green' : 'neutral'}>
                      采集 {progress.acquired ? '✓' : '—'}
                    </Badge>
                    <Badge tone={progress.reviewed ? 'green' : 'neutral'}>
                      初步审核 {progress.reviewed ? '✓' : '—'}
                    </Badge>
                    <Badge tone={progress.published ? 'green' : 'neutral'}>
                      目录写入 {progress.published ? '✓' : '—'}
                    </Badge>
                  </span>
                  <QueryCoverage queries={progress.queries} />
                  {progress.failed && <Badge tone="red">来源处理失败</Badge>}
                  {progress.lastError && (
                    <p
                      className={`w-full break-words ${progress.failed ? 'text-red-700' : 'text-amber-800'}`}
                    >
                      最近错误：{progress.lastError}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </section>
        ))
      )}
      {archive.isError && <ErrorState error={archive.error} retry={() => archive.refetch()} />}
      <Button
        variant="outline"
        disabled={archive.isFetching || (showArchive && !archive.hasNextPage)}
        onClick={() => (showArchive ? archive.fetchNextPage() : setShowArchive(true))}
      >
        {archive.isFetching
          ? '正在读取归档…'
          : showArchive && !archive.hasNextPage
            ? '已显示全部归档'
            : '读取更早的检索归档'}
      </Button>
    </div>
  )
}
