import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  CheckCircle2,
  Clock3,
  Database,
  FileCode2,
  Radio,
  ShieldCheck,
  Timer,
  TriangleAlert,
} from 'lucide-react'
import { api, request } from '../lib/api'
import { date, number, topicLabels } from '../lib/utils'
import type { TopicSlug } from '../lib/types'
import { Badge, Empty, ErrorState, Loading, MockNotice } from './common'
interface Checkpoint {
  source: string
  acquiredThrough: string
  reviewedThrough: string
  publishedThrough: string
  nextRetryAt: string
  lastError?: string
  consecutiveFailures: number
}
interface Schedule {
  anchorUtc: string
  nextRunUtc: string
  everyHours: number
  completedRuns: number
  realFetchEnabled: boolean
}
interface Progress {
  acquired: boolean
  reviewed: boolean
  published: boolean
  failed: boolean
  attempts: number
  pages: number
  lastError?: string
  window: { start: string; end: string }
}
interface Run {
  id: string
  number: number
  startedAt: string
  fixedEnd: string
  finishedAt: string
  status: string
  mode: string
  templateVersion: string
  sources: Record<string, Progress>
}
interface SearchTemplate {
  id: string
  version: string
  topic: string
  query: string
  terms: string[]
  core: boolean
  policy: {
    jifMinimum: number
    jifStrict: boolean
    ccf: string[]
    fullPaperRequired: boolean
    mainTrackRequired: boolean
  }
}
const validDate = (value?: string) =>
  value && !value.startsWith('0001-') ? date(value) : '尚未建立'
const statusLabels: Record<string, string> = {
  completed: '已完成',
  running: '进行中',
  failed: '失败',
  pending: '待运行',
  retrying: '等待重试',
  partial: '部分完成',
  paused: '已暂停',
}
export function SourcesView() {
  const query = useQuery({
    queryKey: ['sources'],
    queryFn: ({ signal }) =>
      request<{ items: Checkpoint[]; schedule: Schedule }>('/api/v1/monitor/sources', { signal }),
  })
  if (query.isLoading) return <Loading />
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const { schedule } = query.data!
  const items = query.data?.items || []
  return (
    <div className="space-y-6">
      {!schedule.realFetchEnabled && <MockNotice />}
      <div className="grid gap-4 sm:grid-cols-3">
        <Metric
          icon={Timer}
          label="计划间隔"
          value={`${schedule.everyHours} 小时`}
          detail="固定 UTC 锚点，不随运行漂移"
        />
        <Metric
          icon={CheckCircle2}
          label="已完成轮次"
          value={number(schedule.completedRuns)}
          detail="来自后端持久化运行记录"
        />
        <Metric
          icon={Clock3}
          label="下一计划时间"
          value={validDate(schedule.nextRunUtc)}
          detail={schedule.realFetchEnabled ? '真实采集已启用' : '仅配置计划 · 真实采集未启用'}
        />
      </div>
      <div className="flex items-center gap-2">
        <Radio className="size-4 text-primary" />
        <h2 className="text-base font-semibold">来源检查点</h2>
        <Badge>{items.length} 个来源</Badge>
      </div>
      {!items.length ? (
        <Empty
          title="尚未建立来源检查点"
          description="首次模拟扫描后，这里将显示各来源独立的采集、审核和目录投影进度。"
        />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {items.map((source) => (
            <section key={source.source} className="rounded-xl border bg-white p-6">
              <div className="mb-5 flex items-center gap-3">
                <span className="flex size-10 items-center justify-center rounded-xl bg-secondary">
                  <Database className="size-4 text-slate-500" />
                </span>
                <h3 className="font-semibold">{source.source}</h3>
                <Badge className="ml-auto" tone={source.consecutiveFailures ? 'amber' : 'green'}>
                  {source.consecutiveFailures
                    ? `重试中 · ${source.consecutiveFailures}`
                    : '无失败记录'}
                </Badge>
              </div>
              <dl className="space-y-3 text-sm">
                {[
                  ['采集水位', source.acquiredThrough],
                  ['审核水位', source.reviewedThrough],
                  ['目录投影水位', source.publishedThrough],
                ].map(([label, value]) => (
                  <div key={label} className="flex justify-between gap-3">
                    <dt className="text-muted-foreground">{label}</dt>
                    <dd className="font-mono text-xs">{validDate(value)}</dd>
                  </div>
                ))}
              </dl>
              {source.lastError && (
                <p className="mt-5 flex items-start gap-2 rounded-lg bg-amber-50 p-3 text-xs leading-6 text-amber-800">
                  <TriangleAlert className="mt-1 size-3.5 shrink-0" />
                  {source.lastError}
                </p>
              )}
              {source.consecutiveFailures > 0 && (
                <p className="mt-3 text-xs text-muted-foreground">
                  下次重试：{validDate(source.nextRetryAt)}
                </p>
              )}
            </section>
          ))}
        </div>
      )}
      <p className="text-xs leading-6 text-muted-foreground">
        采集、审核和目录投影水位分别记录。目录投影完成不等于论文正式发表，模拟审核也不代表科学审读。
      </p>
    </div>
  )
}
function Metric({
  icon: Icon,
  label,
  value,
  detail,
}: {
  icon: typeof Timer
  label: string
  value: string
  detail: string
}) {
  return (
    <div className="rounded-xl border bg-white p-5">
      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <Icon className="size-4" />
        {label}
      </p>
      <p className="mt-4 text-2xl font-semibold tracking-tight">{value}</p>
      <p className="mt-2 text-[11px] text-muted-foreground">{detail}</p>
    </div>
  )
}
export function RunsView() {
  const query = useQuery({
    queryKey: ['runs'],
    queryFn: ({ signal }) => request<{ items: Run[] }>('/api/v1/monitor/runs', { signal }),
  })
  if (query.isLoading) return <Loading />
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const items = query.data?.items || []
  return (
    <div className="space-y-5">
      {items.some((run) => run.mode === 'mock') && <MockNotice />}
      {!items.length ? (
        <Empty
          title="还没有扫描记录"
          description="后端运行后会留下可追溯的扫描记录。此查询界面不会启动采集或调用模型。"
        />
      ) : (
        items.map((run) => (
          <section key={run.id} className="rounded-xl border bg-white p-6">
            <div className="flex flex-wrap items-center gap-3">
              <span className="flex size-10 items-center justify-center rounded-xl bg-violet-50">
                <Activity className="size-4 text-primary" />
              </span>
              <div>
                <h2 className="font-semibold">第 {run.number} 次扫描</h2>
                <p className="mt-1 text-xs text-muted-foreground">
                  {validDate(run.startedAt)} · 模板 {run.templateVersion}
                </p>
              </div>
              <Badge
                className="ml-auto"
                tone={
                  run.status === 'completed' ? 'green' : run.status === 'failed' ? 'red' : 'amber'
                }
              >
                {statusLabels[run.status] || run.status}
              </Badge>
              <Badge tone="violet">{run.mode === 'mock' ? 'MOCK' : 'LIVE'}</Badge>
            </div>
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
            <div className="mt-5 space-y-2">
              {Object.entries(run.sources || {}).map(([name, progress]) => (
                <div
                  key={name}
                  className="flex flex-wrap items-center gap-3 rounded-lg bg-secondary/50 px-3 py-2.5 text-xs"
                >
                  <span className="min-w-24 font-medium">{name}</span>
                  <span className="text-muted-foreground">
                    {progress.pages} 页 · {progress.attempts} 次尝试
                  </span>
                  <span className="ml-auto flex gap-2">
                    <Badge tone={progress.acquired ? 'green' : 'neutral'}>
                      采集 {progress.acquired ? '✓' : '—'}
                    </Badge>
                    <Badge tone={progress.reviewed ? 'green' : 'neutral'}>
                      审核 {progress.reviewed ? '✓' : '—'}
                    </Badge>
                    <Badge tone={progress.published ? 'green' : 'neutral'}>
                      投影 {progress.published ? '✓' : '—'}
                    </Badge>
                  </span>
                  {progress.lastError && (
                    <p className="w-full text-amber-800">{progress.lastError}</p>
                  )}
                </div>
              ))}
            </div>
          </section>
        ))
      )}
    </div>
  )
}
export function TemplatesView({ topic }: { topic: TopicSlug }) {
  const query = useQuery({
    queryKey: ['templates'],
    queryFn: ({ signal }) =>
      request<{ items: SearchTemplate[]; version: string }>('/api/v1/monitor/templates', {
        signal,
      }),
  })
  if (query.isLoading) return <Loading />
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const items = (query.data?.items || []).filter((item) => item.topic === topic)
  return (
    <div className="space-y-5">
      <div className="rounded-xl border bg-white p-5">
        <div className="flex items-center gap-2">
          <ShieldCheck className="size-4 text-primary" />
          <h2 className="text-sm font-semibold">检索与纳入，分开判断</h2>
          <Badge className="ml-auto">{query.data?.version}</Badge>
        </div>
        <p className="mt-3 text-sm leading-7 text-muted-foreground">
          统一模板表达检索意图，由后端翻译为各来源语法。检索命中不意味着满足主题、期刊或全文门槛。
        </p>
      </div>
      {!items.length ? (
        <Empty title="暂无该主题的检索模板" description="已配置的版本化模板会在这里展示。" />
      ) : (
        items.map((template) => (
          <section key={template.id} className="overflow-hidden rounded-xl border bg-white">
            <div className="flex flex-wrap items-center gap-3 border-b px-6 py-4">
              <FileCode2 className="size-4 text-primary" />
              <h3 className="text-sm font-semibold">
                {topicLabels[template.topic] || template.topic}
              </h3>
              {template.core && <Badge tone="violet">核心模板</Badge>}
              <span className="ml-auto font-mono text-[11px] text-muted-foreground">
                {template.id}
              </span>
            </div>
            <div className="p-6">
              <div className="flex flex-wrap gap-2">
                {(template.terms || []).map((term) => (
                  <Badge key={term}>{term}</Badge>
                ))}
              </div>
              <pre className="mt-5 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-slate-950 p-5 font-mono text-xs leading-7 text-slate-200">
                {template.query}
              </pre>
              <div className="mt-5 flex flex-wrap gap-2">
                <Badge tone="green">
                  JIF {template.policy.jifStrict ? '>' : '≥'} {template.policy.jifMinimum}
                </Badge>
                <Badge>CCF {(template.policy.ccf || []).join(' / ')}</Badge>
                {template.policy.fullPaperRequired && <Badge>完整论文</Badge>}
                {template.policy.mainTrackRequired && <Badge>主会 / 主轨</Badge>}
              </div>
            </div>
          </section>
        ))
      )}
    </div>
  )
}
export function VenuesView({ topic, q }: { topic: TopicSlug; q: string }) {
  const query = useQuery({
    queryKey: ['venues', topic, q],
    queryFn: ({ signal }) => api.venues(topic, q, signal),
  })
  if (query.isLoading) return <Loading />
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const items = query.data?.items || []
  return (
    <div>
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {number(items.length)} 个期刊 / 会议 · 指标保留年份与来源
        </p>
        <Badge tone="violet">版本化注册表</Badge>
      </div>
      {!items.length ? (
        <Empty
          title="暂无匹配的期刊或会议"
          description="已核验的来源与指标会显示在这里；历史分类不会被自动覆盖。"
        />
      ) : (
        <div className="overflow-x-auto rounded-xl border bg-white">
          <table className="w-full min-w-[660px] text-left">
            <thead className="border-b bg-secondary/50 text-xs text-muted-foreground">
              <tr>
                {['期刊 / 会议', '类型', 'JIF / CCF', '指标年份', '核验状态'].map((label) => (
                  <th key={label} className="px-5 py-4 font-medium">
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((venue) => (
                <tr key={venue.id} className="border-b last:border-0 hover:bg-secondary/30">
                  <td className="max-w-80 px-5 py-5">
                    <div className="text-sm font-semibold">{venue.name}</div>
                    <p className="mt-1 text-[11px] text-muted-foreground">
                      {venue.source || '来源待补充'} · {venue.registryVersion || '版本待补充'}
                    </p>
                  </td>
                  <td className="px-5 text-xs text-muted-foreground">{venue.kind || '待核验'}</td>
                  <td className="px-5 text-sm font-medium">
                    {venue.jif != null && venue.jif > 0 ? venue.jif : venue.ccf || '待核验'}
                  </td>
                  <td className="px-5 text-xs text-muted-foreground">{venue.metricYear || '—'}</td>
                  <td className="px-5">
                    <Badge tone={venue.qualificationStatus === 'verified' ? 'green' : 'neutral'}>
                      {venue.qualificationStatus === 'verified'
                        ? '已核验'
                        : venue.qualificationStatus === 'pending'
                          ? '待核验'
                          : venue.qualificationStatus || '待核验'}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="mt-4 text-xs leading-6 text-muted-foreground">
        指标变化不会回写历史主题归属。缺失的 JIF、CCF 和指标年份均保持待核验。
      </p>
    </div>
  )
}
