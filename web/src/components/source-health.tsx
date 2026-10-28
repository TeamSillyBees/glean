import { useQuery } from '@tanstack/react-query'
import {
  ArrowUpRight,
  Clock3,
  Database,
  Radio,
  RefreshCw,
  ShieldCheck,
  Timer,
  TriangleAlert,
} from 'lucide-react'
import { request } from '../lib/api'
import { date, number, topicLabels } from '../lib/utils'
import type { TopicSlug } from '../lib/types'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
export interface Checkpoint {
  source: string
  lastAcquiredAt?: string
  quarantined?: number
  handledThrough?: string
  acquiredThrough: string
  reviewedThrough: string
  publishedThrough: string
  nextRetryAt: string
  lastError?: string
  consecutiveFailures: number
  enabled?: boolean
  acquisitionMode?: string
  topicAcquiredThrough?: Record<string, string> | null
  lagSeconds?: number
  blocked?: boolean
  lastStatusCode?: number
  coreBackfillPending?: boolean
}
interface Schedule {
  anchorUtc: string
  nextRunUtc: string
  everyHours: number
  completedRuns: number
  realFetchEnabled: boolean
  schedulerEnabled?: boolean
  acquisitionMode?: string
  reviewMode?: string
}
export interface SourcesData {
  counts?: { quarantinedCandidates?: number }
  items: Checkpoint[]
  schedule: Schedule
}
const hasDate = (value?: string) =>
  !!value && !value.startsWith('0001-') && Number.isFinite(Date.parse(value))
const checkpointDate = (value?: string) => (hasDate(value) ? date(value) : '尚未完成')
function timestamp(value?: string) {
  if (!hasDate(value)) return '尚未完成'
  const parsed = new Date(value!)
  return Number.isFinite(parsed.getTime())
    ? new Intl.DateTimeFormat('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
        timeZone: 'UTC',
      }).format(parsed) + ' UTC'
    : value
}
function lag(seconds?: number) {
  if (seconds == null || !Number.isFinite(seconds)) return '未知'
  if (seconds < 3600) return `${Math.max(0, Math.floor(seconds / 60))} 分钟`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时`
  return `${Math.floor(seconds / 86400)} 天 ${Math.floor((seconds % 86400) / 3600)} 小时`
}
function useSources() {
  return useQuery({
    queryKey: ['sources'],
    queryFn: ({ signal }) => request<SourcesData>('/api/v1/monitor/sources', { signal }),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
}
export function FreshnessStrip({ topic, onOpen }: { topic: TopicSlug; onOpen: () => void }) {
  const query = useSources()
  const items = query.data?.items || []
  const enabled = items.filter((item) => item.enabled !== false)
  const completed = enabled.filter((item) => hasDate(item.topicAcquiredThrough?.[topic]))
  const failures = enabled.filter(
    (item) => item.blocked || item.consecutiveFailures > 0 || (item.quarantined || 0) > 0,
  ).length
  return (
    <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2 border-b py-3 text-xs text-muted-foreground">
      <span className="flex items-center gap-1.5">
        <Radio className="size-3.5 text-primary" />
        {query.isLoading
          ? '正在读取来源进度…'
          : query.isError
            ? '来源进度暂不可用'
            : completed.length
              ? `${completed.length} / ${enabled.length} 个启用来源已完成本主题采集窗口`
              : '本主题尚无完整采集窗口'}
      </span>
      {failures > 0 && <span className="text-amber-700">{failures} 个来源待处理</span>}
      <button
        onClick={onOpen}
        className="ml-auto inline-flex min-h-8 items-center gap-1 rounded px-1 text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        查看来源进度
        <ArrowUpRight className="size-3" />
      </button>
    </div>
  )
}
export function SourcesView({
  topic,
  onOpenQuarantine,
}: {
  topic: TopicSlug
  onOpenQuarantine?: (source?: string) => void
}) {
  const query = useSources()
  if (query.isLoading) return <Loading />
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
        <div className="flex flex-wrap gap-2">
          <Badge tone={schedule.acquisitionMode === 'live' ? 'green' : 'neutral'}>
            {schedule.acquisitionMode === 'live'
              ? '真实来源采集'
              : schedule.acquisitionMode === 'demo'
                ? '独立演示环境'
                : '采集模式待确认'}
          </Badge>
          <Badge>{schedule.schedulerEnabled ? '定时计划已启用' : '定时计划未启用'}</Badge>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => query.refetch()}
        >
          <RefreshCw />
          刷新状态
        </Button>
      </div>
      {query.isError && (
        <p
          role="alert"
          className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs leading-6 text-amber-900"
        >
          刷新失败，以下保留上次成功读取的来源状态，可能不是最新进度。
        </p>
      )}
      <section
        aria-label="采集健康概览"
        className="rounded-xl border bg-white p-5 text-sm leading-7"
      >
        <p>
          最近一次来源完整采集距今：
          <strong>
            {lastSuccess
              ? lag((query.dataUpdatedAt - lastSuccess) / 1000)
              : '未知（尚无成功时间记录）'}
          </strong>
        </p>
        <p className="text-xs text-muted-foreground">
          按最近读取时间计算；仅代表最近成功的启用来源，不代表全部来源或目录写入已更新。
        </p>
        <p>
          失败来源：
          <strong>
            {failedSources.length ? failedSources.map((source) => source.source).join('、') : '无'}
          </strong>
        </p>
        <p>
          隔离记录：<strong>{quarantined == null ? '未知' : `${number(quarantined)} 条`}</strong>
        </p>
      </section>
      {!!quarantined && (
        <p
          role="status"
          className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900"
        >
          {number(quarantined)}{' '}
          条来源记录已隔离待处理，其他有效记录会继续处理。隔离未解决前，目录写入窗口保持保守。
          {onOpenQuarantine && (
            <Button variant="ghost" size="sm" onClick={() => onOpenQuarantine()}>
              查看全部隔离记录
            </Button>
          )}
        </p>
      )}
      <dl className="grid divide-y rounded-xl border bg-card sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        {[
          {
            icon: Timer,
            label: '计划间隔',
            value: `${schedule.everyHours} 小时`,
            detail: '固定 UTC 锚点',
          },
          {
            icon: ShieldCheck,
            label: '已完成轮次',
            value: number(schedule.completedRuns),
            detail: '来自后端运行记录',
          },
          {
            icon: Clock3,
            label: schedule.schedulerEnabled ? '下一计划时间' : '计划中的下一窗口',
            value: checkpointDate(schedule.nextRunUtc),
            detail: schedule.schedulerEnabled ? '以实际运行状态为准' : '未启用，不代表将自动运行',
          },
        ].map(({ icon: Icon, label, value, detail }) => (
          <div key={label} className="min-w-0 px-4 py-4 sm:px-5">
            <dt className="flex items-center gap-2 text-xs text-muted-foreground">
              <Icon className="size-4" aria-hidden="true" />
              {label}
            </dt>
            <dd className="mt-2 break-words text-lg font-semibold tabular-nums">
              {value}
              <p className="mt-1 text-xs font-normal text-muted-foreground">{detail}</p>
            </dd>
          </div>
        ))}
      </dl>
      <div>
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Radio className="size-4 text-primary" />
          {topicLabels[topic] || topic} · 来源进度
        </h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          只有本主题所有分页完成，才推进该主题窗口。没有完成记录时显示未知，不以“无错误”代表最新。
        </p>
      </div>
      {!items.length ? (
        <Empty
          title="尚未建立来源检查点"
          description="真实来源完成采集后，会在这里显示独立进度。"
        />
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {items.map((source) => {
            const completed = hasDate(source.topicAcquiredThrough?.[topic])
            const status =
              (source.quarantined || 0) > 0
                ? `${number(source.quarantined)} 条隔离待处理`
                : source.enabled === false
                  ? '未启用'
                  : source.blocked
                    ? '访问受限 · 已停止重试'
                    : source.consecutiveFailures
                      ? '等待恢复'
                      : completed
                        ? '已有完整窗口'
                        : '尚无完成记录'
            return (
              <section key={source.source} className="min-w-0 rounded-xl border bg-card p-4 sm:p-5">
                <div className="flex flex-wrap items-center gap-3">
                  <Database className="size-4 shrink-0 text-primary" aria-hidden="true" />
                  <h3 className="text-sm font-semibold">{source.source}</h3>
                  {!!source.quarantined && onOpenQuarantine && (
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={`查看 ${source.source} 隔离记录`}
                      onClick={() => onOpenQuarantine(source.source)}
                    >
                      查看隔离记录
                    </Button>
                  )}
                  <Badge
                    className="ml-auto"
                    tone={
                      source.blocked || source.consecutiveFailures || (source.quarantined || 0) > 0
                        ? 'amber'
                        : completed && source.enabled !== false
                          ? 'green'
                          : 'neutral'
                    }
                  >
                    {status}
                  </Badge>
                </div>
                <div className="mt-4 flex flex-wrap items-baseline justify-between gap-2 border-y py-3">
                  <p className="text-xs text-accent-foreground">本主题已完整采集至</p>
                  <p className="text-sm font-medium tabular-nums text-foreground">
                    {timestamp(source.topicAcquiredThrough?.[topic])}
                  </p>
                </div>
                <dl className="mt-4 space-y-3 text-sm">
                  {[
                    ['上次完整采集成功', timestamp(source.lastAcquiredAt)],
                    [
                      '距上次完整采集成功',
                      hasDate(source.lastAcquiredAt)
                        ? lag((query.dataUpdatedAt - Date.parse(source.lastAcquiredAt!)) / 1000)
                        : '未知',
                    ],
                    [
                      '隔离记录',
                      source.quarantined == null ? '未知' : `${number(source.quarantined)} 条`,
                    ],
                    ['失败次数', number(source.consecutiveFailures)],
                    [
                      '本主题窗口距今',
                      hasDate(source.topicAcquiredThrough?.[topic]) &&
                      Number.isFinite(Date.parse(source.topicAcquiredThrough![topic]))
                        ? lag(
                            Math.max(
                              0,
                              (query.dataUpdatedAt -
                                Date.parse(source.topicAcquiredThrough![topic])) /
                                1000,
                            ),
                          )
                        : '未知',
                    ],
                    ['全来源采集窗口', timestamp(source.acquiredThrough)],
                    ['全来源窗口距今', lag(source.lagSeconds)],
                    [
                      schedule.reviewMode === 'mock' ? '模型占位窗口' : '模型阶段窗口',
                      timestamp(source.reviewedThrough),
                    ],
                    ['处理窗口（含隔离）', timestamp(source.handledThrough)],
                    ['目录写入窗口', timestamp(source.publishedThrough)],
                  ].map(([label, value]) => (
                    <div key={label} className="flex flex-wrap justify-between gap-2">
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="min-w-0 break-words tabular-nums">{value}</dd>
                    </div>
                  ))}
                </dl>
                {source.coreBackfillPending && (
                  <p className="mt-4 text-xs text-muted-foreground">核心来源回溯仍待完成。</p>
                )}
                {source.lastError && (
                  <div className="mt-4 rounded-lg bg-amber-50 p-3 text-sm leading-6 text-amber-800">
                    <p className="flex gap-2">
                      <TriangleAlert className="mt-1 size-3.5 shrink-0" />
                      <span className="min-w-0 break-words">
                        {source.lastStatusCode ? `HTTP ${source.lastStatusCode} · ` : ''}
                        {source.lastError}
                      </span>
                    </p>
                    {source.blocked ? (
                      <p className="mt-2">需先处理来源访问限制，后端不会自动重复请求。</p>
                    ) : (
                      hasDate(source.nextRetryAt) && (
                        <p className="mt-2">下一次允许重试：{timestamp(source.nextRetryAt)}</p>
                      )
                    )}
                  </div>
                )}
              </section>
            )
          })}
        </div>
      )}
      <p className="text-sm leading-6 text-muted-foreground">
        {schedule.reviewMode === 'mock'
          ? '模型总结待接入，不影响真实论文检索。模型处理窗口仅代表模拟流程完成，不代表科学审读。'
          : '模型处理、目录写入与科学审读分别记录。'}{' '}
        目录写入不代表正式发表。
      </p>
    </div>
  )
}
