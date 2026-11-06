import { useEffect, useRef, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Database, History, PlugZap, RefreshCw, Timer } from 'lucide-react'
import { hasDate, useSources, type SourceConnection } from '../lib/source-query'
import { request } from '../lib/api'
export type { SourcesData, Checkpoint } from '../lib/source-query'
import { number, topicLabels } from '../lib/utils'
import type { TopicSlug } from '../lib/types'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
const sourceNames: Record<string, string> = {
  crossref: 'Crossref',
  pubmed: 'PubMed',
  europepmc: 'Europe PMC',
  arxiv: 'arXiv',
  biorxiv: 'bioRxiv',
  medrxiv: 'medRxiv',
}
const connectionLabels: Record<SourceConnection['status'], string> = {
  connected: '已连通',
  timeout: '连接超时',
  network_error: '连接失败',
  blocked: '访问受限',
  rate_limited: '请求限流',
  unavailable: '服务异常',
  invalid_response: '响应异常',
  deferred: '等待重试',
  skipped: '未测试',
}
function timestamp(value?: string) {
  if (!hasDate(value)) return '尚无记录'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value!))
}
function lag(seconds: number) {
  if (seconds < 3600) return Math.max(0, Math.floor(seconds / 60)) + ' 分钟'
  if (seconds < 86400) return Math.floor(seconds / 3600) + ' 小时'
  return Math.floor(seconds / 86400) + ' 天'
}
export function SourcesView({
  topic,
  canManage = false,
  onOpenQuarantine,
  onOpenRuns,
}: {
  topic: TopicSlug
  canManage?: boolean
  onOpenQuarantine?: (source?: string) => void
  onOpenRuns?: () => void
}) {
  const query = useSources()
  const [refreshMessage, setRefreshMessage] = useState('')
  const testController = useRef<AbortController | null>(null)
  useEffect(() => () => testController.current?.abort(), [])
  const connectionTest = useMutation({
    mutationFn: () => {
      testController.current = new AbortController()
      return request<{ items: SourceConnection[] }>('/api/v1/monitor/sources/test', {
        method: 'POST',
        signal: testController.current.signal,
      })
    },
    retry: false,
  })
  async function refresh() {
    const before = JSON.stringify(query.data)
    setRefreshMessage('')
    const result = await query.refetch()
    if (!result.isError)
      setRefreshMessage(
        before === JSON.stringify(result.data)
          ? '已刷新 · 采集状态没有变化'
          : '已刷新 · 采集状态已更新',
      )
  }
  if (query.isPending) return <Loading label="正在读取采集状态…" />
  if (query.isError && !query.data)
    return <ErrorState error={query.error} retry={() => query.refetch()} />
  const { items, schedule } = query.data!
  const failedSources = items.filter((source) => source.blocked || source.consecutiveFailures > 0)
  const quarantined = query.data?.counts?.quarantinedCandidates
  const lastSuccess = Math.max(
    0,
    ...items
      .filter((source) => source.enabled !== false && hasDate(source.lastAcquiredAt))
      .map((source) => Date.parse(source.lastAcquiredAt!)),
  )
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p role="status" aria-live="polite" className="text-xs text-muted-foreground">
          {query.isFetching
            ? '正在刷新采集状态…'
            : query.isError
              ? '刷新失败'
              : refreshMessage ||
                '上次读取 ' + new Date(query.dataUpdatedAt).toLocaleTimeString('zh-CN')}
        </p>
        <div className="flex flex-wrap gap-2">
          {canManage && (
            <Button
              size="sm"
              disabled={connectionTest.isPending}
              onClick={() => connectionTest.mutate()}
              title="从服务器检测来源访问情况，不采集论文"
            >
              {connectionTest.isPending ? <RefreshCw className="animate-spin" /> : <PlugZap />}
              {connectionTest.isPending ? '测试中…' : '测试连通性'}
            </Button>
          )}
          {onOpenRuns && (
            <Button variant="outline" size="sm" onClick={onOpenRuns}>
              <History />
              查看采集记录
            </Button>
          )}
          <Button variant="outline" size="sm" disabled={query.isFetching} onClick={refresh}>
            <RefreshCw className={query.isFetching ? 'animate-spin' : undefined} />
            刷新状态
          </Button>
        </div>
      </div>
      {connectionTest.isPending ? (
        <p role="status" className="rounded-lg border bg-card px-4 py-3 text-sm">
          正在从服务器测试各来源的连通性，最多等待 15 秒…
        </p>
      ) : connectionTest.isError ? (
        <p
          role="alert"
          className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
        >
          连通性测试未完成：{connectionTest.error.message}
        </p>
      ) : connectionTest.data ? (
        <p role="status" className="rounded-lg border bg-card px-4 py-3 text-sm">
          测试完成 ·{' '}
          {connectionTest.data.items.filter((item) => item.status === 'connected').length}/
          {connectionTest.data.items.length} 个来源已连通，结果见下方。未启动论文采集。
        </p>
      ) : null}
      {query.isError && (
        <p
          role="alert"
          className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900"
        >
          刷新失败，以下保留上次成功读取的来源状态，可能不是最新进度。
        </p>
      )}
      <section className="rounded-xl border bg-card p-5">
        <div className="flex items-center gap-3">
          <Timer className="size-5 text-primary" />
          <h2 className="font-semibold">
            {schedule.schedulerEnabled ? '自动采集已开启' : '自动采集已关闭'}
          </h2>
          <Badge className="ml-auto">
            {schedule.acquisitionMode === 'live'
              ? '真实来源采集'
              : schedule.acquisitionMode === 'demo'
                ? '演示环境'
                : '模式待确认'}
          </Badge>
        </div>
        <p className="mt-3 text-sm text-muted-foreground">
          {schedule.schedulerEnabled
            ? '每 ' +
              schedule.everyHours +
              ' 小时采集一次 · 下次计划 ' +
              timestamp(schedule.nextRunUtc)
            : '目前不会自动获取新论文。开启自动采集需由管理员在服务器配置。'}
        </p>
        <p className="mt-2 text-xs text-muted-foreground">
          刷新状态只更新本页显示。{number(schedule.completedRuns)} 次采集已完成。
        </p>
      </section>
      <section aria-label="采集健康概览" className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-xl border bg-card p-4">
          <p className="text-xs text-muted-foreground">最近成功采集</p>
          <p className="mt-2 font-semibold">
            {lastSuccess ? lag((query.dataUpdatedAt - lastSuccess) / 1000) + '前' : '尚无成功记录'}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">所有启用来源中的最近一次</p>
        </div>
        <div className="rounded-xl border bg-card p-4">
          <p className="text-xs text-muted-foreground">失败来源</p>
          <p className="mt-2 font-semibold">{failedSources.length} 个</p>
          {failedSources.length > 0 && (
            <p className="mt-1 text-xs text-muted-foreground">
              {failedSources
                .map((source) => sourceNames[source.source] || source.source)
                .join('、')}
            </p>
          )}
        </div>
        <div className="rounded-xl border bg-card p-4">
          <p className="text-xs text-muted-foreground">待处理记录</p>
          <p className="mt-2 font-semibold">
            {quarantined == null ? '未知' : number(quarantined) + ' 条'}
          </p>
          {!!quarantined && onOpenQuarantine && (
            <Button
              variant="ghost"
              size="sm"
              className="h-auto p-0 mt-1"
              onClick={() => onOpenQuarantine()}
            >
              查看全部隔离记录
            </Button>
          )}
        </div>
      </section>
      <h2 className="text-sm font-semibold">{topicLabels[topic] || topic} · 各来源进度</h2>
      {!items.length ? (
        <Empty title="尚无采集记录" description="采集运行后，这里会显示各来源的结果。" />
      ) : (
        <div className="space-y-3">
          {items.map((source) => {
            const connection = connectionTest.data?.items.find(
              (item) => item.source === source.source,
            )
            const testing = connectionTest.isPending && source.enabled !== false
            const completed = hasDate(source.topicAcquiredThrough?.[topic])
            const status =
              (source.quarantined || 0) > 0
                ? number(source.quarantined) + ' 条隔离待处理'
                : source.enabled === false
                  ? '未启用'
                  : source.blocked
                    ? '访问受限'
                    : source.consecutiveFailures
                      ? '采集失败'
                      : completed
                        ? '已完成采集'
                        : '尚无完成记录'
            return (
              <section
                key={source.source}
                aria-label={(sourceNames[source.source] || source.source) + ' 采集状态'}
                className="min-w-0 rounded-xl border bg-card p-4 sm:p-5"
              >
                <div className="flex flex-wrap items-center gap-3">
                  <Database className="size-4 text-primary" />
                  <h3 className="text-sm font-semibold">
                    {sourceNames[source.source] || source.source}
                  </h3>
                  <Badge
                    className="ml-auto"
                    tone={
                      source.blocked || source.consecutiveFailures || source.quarantined
                        ? 'amber'
                        : completed && source.enabled !== false
                          ? 'green'
                          : 'neutral'
                    }
                  >
                    {status}
                  </Badge>
                </div>
                {canManage && (
                  <div
                    className="mt-3 rounded-lg bg-muted/50 px-3 py-2.5 text-xs"
                    aria-label={(sourceNames[source.source] || source.source) + ' 连通性'}
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-muted-foreground">连通性</span>
                      <Badge
                        tone={
                          testing
                            ? 'neutral'
                            : connection?.status === 'connected'
                              ? 'green'
                              : connection
                                ? 'amber'
                                : 'neutral'
                        }
                      >
                        {testing
                          ? '测试中…'
                          : connection
                            ? connectionLabels[connection.status]
                            : '未测试'}
                      </Badge>
                      {connection && !testing && (
                        <span className="text-muted-foreground tabular-nums">
                          耗时 {(connection.elapsedMs / 1000).toFixed(1)} 秒 ·{' '}
                          {new Date(connection.checkedAt).toLocaleTimeString('zh-CN')}
                        </span>
                      )}
                    </div>
                    {connection && !testing && connection.status !== 'connected' && (
                      <p className="mt-2 break-words text-amber-800">
                        {connection.message}
                        {hasDate(connection.retryAt) &&
                          ' 可重试时间：' + timestamp(connection.retryAt)}
                      </p>
                    )}
                  </div>
                )}
                <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
                  <div>
                    <dt className="text-xs text-muted-foreground">本主题已采集至</dt>
                    <dd className="mt-1 tabular-nums">
                      {timestamp(source.topicAcquiredThrough?.[topic])}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-xs text-muted-foreground">上次成功采集</dt>
                    <dd className="mt-1 tabular-nums">{timestamp(source.lastAcquiredAt)}</dd>
                  </div>
                </dl>
                {source.lastError && (
                  <div className="mt-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-900">
                    <p className="break-words">
                      {source.lastStatusCode ? 'HTTP ' + source.lastStatusCode + ' · ' : ''}
                      {source.lastError}
                    </p>
                    {source.blocked ? (
                      <p className="mt-2 text-xs">访问受限，需管理员处理后再采集。</p>
                    ) : (
                      hasDate(source.nextRetryAt) && (
                        <p className="mt-2 text-xs">
                          下一次允许重试：{timestamp(source.nextRetryAt)}
                        </p>
                      )
                    )}
                  </div>
                )}
                {!!source.quarantined && onOpenQuarantine && (
                  <Button
                    className="mt-3"
                    variant="outline"
                    size="sm"
                    aria-label={'查看 ' + source.source + ' 隔离记录'}
                    onClick={() => onOpenQuarantine(source.source)}
                  >
                    查看隔离记录
                  </Button>
                )}
                <details className="mt-4 border-t pt-3 text-xs">
                  <summary className="cursor-pointer text-muted-foreground">查看处理详情</summary>
                  <dl className="mt-3 space-y-2">
                    {[
                      ['全来源采集窗口', timestamp(source.acquiredThrough)],
                      [
                        schedule.reviewMode === 'mock' ? '模型占位窗口' : '模型处理至',
                        timestamp(source.reviewedThrough),
                      ],
                      ['处理窗口（含隔离）', timestamp(source.handledThrough)],
                      ['目录写入至', timestamp(source.publishedThrough)],
                      ['连续失败次数', number(source.consecutiveFailures)],
                    ].map(([label, value]) => (
                      <div key={label} className="flex flex-wrap justify-between gap-2">
                        <dt className="text-muted-foreground">{label}</dt>
                        <dd>{value}</dd>
                      </div>
                    ))}
                  </dl>
                  {source.coreBackfillPending && (
                    <p className="mt-2 text-muted-foreground">历史文献回溯待完成</p>
                  )}
                </details>
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}
