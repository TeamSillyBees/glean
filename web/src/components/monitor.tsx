import { useQuery } from '@tanstack/react-query'
import { Activity, FileCode2, RefreshCw, ShieldCheck } from 'lucide-react'
import { api, request } from '../lib/api'
import { date, number, topicLabels } from '../lib/utils'
import type { TopicSlug } from '../lib/types'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
import type { Run } from './run-types'
import { RunRecovery } from './run-recovery'
export { SourcesView } from './source-health'
export { FreshnessStrip } from './freshness-strip'
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
export function RunsView() {
  const query = useQuery({
    queryKey: ['runs'],
    queryFn: ({ signal }) => request<{ items: Run[] }>('/api/v1/monitor/runs', { signal }),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  if (query.isLoading) return <Loading label="正在读取扫描记录…" />
  if (query.isError && !query.data)
    return <ErrorState error={query.error} retry={() => query.refetch()} />
  const items = query.data?.items || []
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
        采集、模型占位与目录写入分开记录；模型阶段尚待真实模型接入，不代表科学审读。
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
                      模型占位 {progress.reviewed ? '✓' : '—'}
                    </Badge>
                    <Badge tone={progress.published ? 'green' : 'neutral'}>
                      目录写入 {progress.published ? '✓' : '—'}
                    </Badge>
                  </span>
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
      <div className="border-b pb-5">
        <div className="flex flex-wrap items-center gap-2">
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
          <section key={template.id} className="overflow-hidden rounded-xl border bg-card">
            <div className="flex flex-wrap items-center gap-3 border-b px-4 py-4 sm:px-5">
              <FileCode2 className="size-4 text-primary" />
              <h3 className="text-sm font-semibold">
                {topicLabels[template.topic] || template.topic}
              </h3>
              {template.core && <Badge tone="violet">核心模板</Badge>}
              <span className="ml-auto font-mono text-xs text-muted-foreground">{template.id}</span>
            </div>
            <div className="p-4 sm:p-5">
              <div className="flex flex-wrap gap-2">
                {(template.terms || []).map((term) => (
                  <Badge key={term}>{term}</Badge>
                ))}
              </div>
              <pre className="mt-5 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-secondary p-4 font-mono text-sm leading-7 text-foreground">
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
              <section key={venue.id} className="min-w-0 rounded-xl border bg-card p-4 sm:p-5">
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
                <p className="mt-4 break-words text-sm leading-6 text-muted-foreground">
                  {venue.source || '来源待补充'} · {venue.registryVersion || '版本待补充'}
                </p>
                <p className="mt-2 text-xs text-muted-foreground">
                  {venueStatusLabels[venue.qualificationStatus] ||
                    venue.qualificationStatus ||
                    '待核验'}
                </p>
              </section>
            ))}
          </div>
          <div className="hidden overflow-x-auto rounded-xl border bg-card md:block">
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
                    <td className="max-w-80 break-words px-5 py-4">
                      <div className="text-sm font-semibold">{venue.name}</div>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {venue.source || '来源待补充'} · {venue.registryVersion || '版本待补充'}
                      </p>
                    </td>
                    <td className="px-5 text-xs text-muted-foreground">{venue.kind || '待核验'}</td>
                    <td className="px-5 text-sm font-medium tabular-nums">
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
      <p className="mt-4 text-sm leading-6 text-muted-foreground">
        指标变化不会回写历史主题归属。缺失的期刊影响因子、CCF 会议评级和指标年份均保持待核验。
      </p>
    </div>
  )
}
