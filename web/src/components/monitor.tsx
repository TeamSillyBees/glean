import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Activity, FileCode2, RefreshCw, ShieldCheck } from 'lucide-react'
import { api, request } from '../lib/api'
import { date, number, topicLabels } from '../lib/utils'
import type { TopicSlug } from '../lib/types'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
import type { Run } from './run-types'
import { QueryCoverage } from './query-coverage'
import { RunRecovery } from './run-recovery'
import { RunAudit } from './run-audit'
export { SourcesView } from './source-health'
export { FreshnessStrip } from './freshness-strip'
interface QueryPreview {
  queryId: string
  queryVersion: string
  topic: string
  source: string
  mode: string
  query: string
  queries?: string[]
  warnings?: string[]
  enabled: boolean
  backfillFrom?: string
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
export function TemplatesView({ topic }: { topic: TopicSlug }) {
  const query = useQuery({
    queryKey: ['templates'],
    queryFn: ({ signal }) =>
      request<{ items: SearchTemplate[]; version: string; previews?: QueryPreview[] }>(
        '/api/v1/monitor/templates',
        {
          signal,
        },
      ),
  })
  if (query.isLoading) return <Loading />
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const items = (query.data?.items || []).filter((item) => item.topic === topic)
  const previews = (query.data?.previews || []).filter((item) => item.topic === topic)
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
      <section aria-label="来源查询编译预览" className="space-y-3">
        <h2 className="text-sm font-semibold">来源查询编译预览</h2>
        <p className="text-sm text-muted-foreground">
          仅编译预览，尚未执行。以下来自已保存的主题与当前来源配置；打开此页不会请求外部文献库或启动采集。
        </p>
        {!previews.length ? (
          <p className="text-xs text-muted-foreground">
            暂无该主题的逐来源编译预览；旧接口可能尚未提供，不能据此认定路线已执行。
          </p>
        ) : (
          previews.map((preview, index) => (
            <article
              key={`${preview.source}-${preview.queryId}-${index}`}
              className="min-w-0 space-y-2 rounded-xl border p-4"
            >
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="break-all text-sm font-medium">
                  {preview.queryId} · {preview.source}
                </h3>
                <Badge tone={preview.enabled ? 'neutral' : 'amber'}>
                  {preview.enabled ? '来源已配置启用' : '来源未启用'}
                </Badge>
              </div>
              <p className="break-all text-xs text-muted-foreground">
                编译模式 {preview.mode} · 查询版本 {preview.queryVersion}
              </p>
              {!preview.enabled && (
                <p className="text-xs text-amber-800">
                  当前配置未启用此来源，运行时不会执行这条来源查询。
                </p>
              )}
              {preview.backfillFrom && <p className="text-xs">历史起点：{preview.backfillFrom}</p>}
              {preview.mode === 'seed_union' && (
                <p className="text-xs text-muted-foreground">
                  独立种子查询分别请求后取并集，不等价于服务端布尔词组匹配。
                </p>
              )}
              {preview.mode === 'full_window' && (
                <p className="text-xs text-amber-800">
                  采集整个时间窗口，不在来源端按关键词过滤；保存后仍需下游评估主题相关性。
                </p>
              )}
              {preview.queries?.length ? (
                <ol className="space-y-2">
                  {preview.queries.map((seed, seedIndex) => (
                    <li key={seedIndex}>
                      <p className="mb-1 text-xs text-muted-foreground">独立查询 {seedIndex + 1}</p>
                      <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-secondary p-3 text-xs">
                        {seed}
                      </pre>
                    </li>
                  ))}
                </ol>
              ) : preview.query ? (
                <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-secondary p-3 text-xs">
                  {preview.query}
                </pre>
              ) : null}
              {preview.warnings?.map((warning, warningIndex) => (
                <p key={warningIndex} className="break-words text-xs text-amber-800">
                  来源限制：{warning}
                </p>
              ))}
            </article>
          ))
        )}
      </section>
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
