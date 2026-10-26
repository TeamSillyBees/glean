import { useQuery } from '@tanstack/react-query'
import { Activity, FileCode2, ShieldCheck } from 'lucide-react'
import { api, request } from '../lib/api'
import { date, number, topicLabels } from '../lib/utils'
import type { TopicSlug } from '../lib/types'
import { Badge, Empty, ErrorState, Loading } from './common'
export { SourcesView, FreshnessStrip } from './source-health'
interface Progress {
  acquired: boolean
  handled?: boolean
  quarantined?: number
  reviewed: boolean
  published: boolean
  failed: boolean
  attempts: number
  pages: number
  observed?: number
  projected?: number
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
const venueStatusLabels: Record<string, string> = {
  verified: '已核验',
  pending: '待核验',
  'historical-curated-discovery': '历史精选发现目标 · 当前资格待核',
}
const statusLabels: Record<string, string> = {
  completed: '已完成',
  completed_with_quarantine: '完成 · 有隔离记录',
  running: '进行中',
  failed: '失败',
  pending: '待运行',
  retrying: '等待重试',
  partial: '部分完成',
  paused: '已暂停',
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
      <p className="text-xs leading-6 text-muted-foreground">
        采集、模型占位与目录写入分开记录；模型阶段尚待真实模型接入，不代表科学审读。
      </p>
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
              <Badge tone="violet">{run.mode === 'mock' ? '历史模拟运行' : '真实来源采集'}</Badge>
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
                      模型占位 {progress.reviewed ? '✓' : '—'}
                    </Badge>
                    <Badge tone={progress.published ? 'green' : 'neutral'}>
                      目录写入 {progress.published ? '✓' : '—'}
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
                  期刊影响因子 {template.policy.jifStrict ? '>' : '≥'} {template.policy.jifMinimum}
                </Badge>
                <Badge>CCF 会议评级 {(template.policy.ccf || []).join(' / ')}</Badge>
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
          {number(items.length)} 条期刊 / 会议证据记录（含不同版本）
        </p>
        <Badge tone="violet">版本化注册表</Badge>
      </div>
      {!items.length ? (
        <Empty
          title="暂无匹配的期刊或会议"
          description="已核验的来源与指标会显示在这里；历史分类不会被自动覆盖。"
        />
      ) : (
        <>
          <div className="space-y-3 md:hidden">
            {items.map((venue) => (
              <section key={venue.id} className="rounded-xl border bg-white p-5">
                <div className="flex items-start justify-between gap-3">
                  <h2 className="text-sm font-semibold leading-6">{venue.name}</h2>
                  <Badge>{venue.kind || '待核验'}</Badge>
                </div>
                <div className="mt-4 flex flex-wrap gap-2">
                  {venue.jif != null && venue.jif > 0 && (
                    <Badge tone="violet">期刊影响因子 {venue.jif}</Badge>
                  )}
                  {venue.ccf && <Badge tone="violet">CCF 会议评级 {venue.ccf}</Badge>}
                  {!venue.jif && !venue.ccf && <Badge>指标待核验</Badge>}
                  <Badge>
                    {venue.metricYear ? `${venue.metricYear} 年指标` : '指标年份待核验'}
                  </Badge>
                </div>
                <p className="mt-4 break-words text-[11px] leading-6 text-muted-foreground">
                  {venue.source || '来源待补充'} · {venue.registryVersion || '版本待补充'}
                </p>
                <p className="mt-2 text-xs text-slate-600">
                  {venueStatusLabels[venue.qualificationStatus] ||
                    venue.qualificationStatus ||
                    '待核验'}
                </p>
              </section>
            ))}
          </div>
          <div className="hidden overflow-x-auto rounded-xl border bg-white md:block">
            <table className="w-full min-w-[660px] text-left">
              <thead className="border-b bg-secondary/50 text-xs text-muted-foreground">
                <tr>
                  {[
                    '期刊 / 会议',
                    '类型',
                    '期刊影响因子 / CCF 会议评级',
                    '指标年份',
                    '核验状态',
                  ].map((label) => (
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
                    <td className="px-5 text-xs text-muted-foreground">
                      {venue.metricYear || '—'}
                    </td>
                    <td className="px-5">
                      <Badge tone={venue.qualificationStatus === 'verified' ? 'green' : 'neutral'}>
                        {venueStatusLabels[venue.qualificationStatus] ||
                          venue.qualificationStatus ||
                          '待核验'}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <p className="mt-4 text-xs leading-6 text-muted-foreground">
        指标变化不会回写历史主题归属。缺失的期刊影响因子、CCF 会议评级和指标年份均保持待核验。
      </p>
    </div>
  )
}
