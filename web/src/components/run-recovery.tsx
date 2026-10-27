import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { request } from '../lib/api'
import type { Run } from './run-types'
import type { SourcesData } from './source-health'
import { Button } from './ui/button'

const hasID = (id?: string) => !!id && /^[1-9]\d*$/.test(id)
const instant = (value?: string) => {
  if (!value || value.startsWith('0001-')) return 0
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : 0
}
const provider = (name: string) => (name === 'medrxiv' ? 'biorxiv' : name)
const sourceNames = new Set(['arxiv', 'crossref', 'pubmed', 'europepmc', 'biorxiv', 'medrxiv'])

function RunLink({ id, label, runs }: { id: string; label: string; runs: Run[] }) {
  const present = runs.some((item) => item.id === id)
  return (
    <p className="break-all text-sm leading-6">
      {label}：{id}{' '}
      {present ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            const target = document.getElementById(`scan-run-${id}`)
            target?.focus({ preventScroll: true })
            target?.scrollIntoView?.({ block: 'center', behavior: 'smooth' })
          }}
        >
          查看{label}
        </Button>
      ) : (
        <span className="text-muted-foreground">已不在当前保留历史中，不推测其结果</span>
      )}
    </p>
  )
}

export function RunRecovery({
  run,
  runs,
  historyStale,
}: {
  run: Run
  runs: Run[]
  historyStale: boolean
}) {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="mt-4 space-y-2">
      {hasID(run.retryOf) && <RunLink id={run.retryOf!} label="原失败运行" runs={runs} />}
      {hasID(run.retriedBy) && <RunLink id={run.retriedBy!} label="关联恢复运行" runs={runs} />}
      {(hasID(run.retryOf) || hasID(run.retriedBy)) && (
        <p className="text-xs text-muted-foreground">
          恢复沿用原扫描轮次；旧运行保留失败证据，不会因恢复成功改写成成功。
        </p>
      )}
      {run.status === 'completed_with_errors' && (
        <>
          <Button
            variant="outline"
            size="sm"
            aria-expanded={expanded}
            aria-controls={`recovery-${run.id}`}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? '收起恢复诊断' : '查看恢复诊断'}
          </Button>
          {expanded && <RecoveryDetails run={run} runs={runs} historyStale={historyStale} />}
        </>
      )}
    </div>
  )
}

function RecoveryDetails({
  run,
  runs,
  historyStale,
}: {
  run: Run
  runs: Run[]
  historyStale: boolean
}) {
  const query = useQuery({
    queryKey: ['sources'],
    queryFn: ({ signal }) => request<SourcesData>('/api/v1/monitor/sources', { signal }),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  const checkpoints = query.data?.items || []
  const names = Object.keys(run.sources || {}).sort()
  const active = runs.find((item) => item.id !== run.id && !instant(item.finishedAt))
  const alreadyRetried = hasID(run.retriedBy)
  const uncertain = historyStale || query.isError || query.isFetching || !query.data
  // Use the last completed source read, not a moving browser clock, for advisory cooldown checks.
  const now = query.dataUpdatedAt
  const failed = names.filter((name) => run.sources[name].failed)
  const evidence = failed.map((name) => {
    const progress = run.sources[name]
    const current = checkpoints.find((item) => item.source === name)
    const peers = checkpoints.filter((item) => provider(item.source) === provider(name))
    const restricted = peers.some((item) => item.blocked)
    const retryAt = Math.max(
      instant(progress.retryNotBefore),
      instant(current?.nextRetryAt),
      ...peers
        .filter((item) => [429, 503, 403].includes(item.lastStatusCode || 0))
        .map((item) => instant(item.nextRetryAt)),
    )
    const temporary = progress.retryableFailure === true
    return { name, current, restricted, retryAt, temporary }
  })
  const eligible = evidence.some((item) => item.temporary)
  const blocked = evidence.some(
    ({ temporary, current, restricted, retryAt }) =>
      temporary && (!current || current.enabled === false || restricted || retryAt > now),
  )
  const rows = evidence.map(({ name, current, restricted, retryAt, temporary }) => {
    const message = !temporary
      ? '未标记为可重试：永久错误或旧记录分类未知，需检查，不能凭错误文字重试。'
      : uncertain
        ? '已记录为暂时失败；当前来源状态尚未确认，不能判断恢复条件。'
        : !current
          ? '缺少当前来源检查点，需检查后端状态。'
          : current.enabled === false
            ? '来源当前未启用，需先检查后端配置。'
            : restricted
              ? '来源或共享服务访问受限，禁止通过恢复绕过限制。'
              : retryAt > now
                ? '仍在退避期，不能提前恢复。'
                : '已记录为暂时失败，已知退避时间已过；仍须后端校验后才能恢复。'
    return (
      <li key={name} className="min-w-0 space-y-1 py-3">
        <strong>{name}</strong>
        <p>{message}</p>
        {temporary && retryAt > 0 && (
          <p className="text-muted-foreground">
            记录中的最早重试时间：{new Date(retryAt).toISOString()}
          </p>
        )}
      </li>
    )
  })
  const canExplain =
    !uncertain &&
    !alreadyRetried &&
    !active &&
    eligible &&
    !blocked &&
    !!instant(run.finishedAt) &&
    hasID(run.id) &&
    run.mode === 'live' &&
    names.every((name) => sourceNames.has(name))
  return (
    <div id={`recovery-${run.id}`} className="space-y-3 border-y py-4 text-sm leading-6">
      <p>只读诊断，不会启动采集。来源状态与历史可能在查看后变化，实际执行由后端再次校验。</p>
      {query.isFetching && <p role="status">正在核对当前来源状态…</p>}
      {(query.isError || historyStale) && (
        <p role="alert" className="text-amber-800">
          状态刷新失败或历史已过期，暂停给出恢复参数。请先刷新状态。
        </p>
      )}
      <Button variant="ghost" size="sm" disabled={query.isFetching} onClick={() => query.refetch()}>
        刷新恢复诊断
      </Button>
      {alreadyRetried ? (
        <p className="text-amber-800">
          此失败运行已创建恢复运行，请查看关联结果，不要为原运行重复申请新预算。
        </p>
      ) : active ? (
        <p className="text-amber-800">
          当前保留历史中还有未结束运行（{active.id}），应先处理该运行，不能抢占。
        </p>
      ) : null}
      <ul className="divide-y">{rows}</ul>
      {!failed.length && <p>未找到明确的来源失败证据，需检查后端记录。</p>}
      {canExplain && (
        <div className="space-y-1 border-t pt-3">
          <p className="font-medium">供管理员核对的 CLI 参数（不是已获准执行）</p>
          <p className="break-all font-mono text-sm">--retry-failed {run.id}</p>
          <p className="break-all font-mono text-sm">--sources {names.join(',')}</p>
          <p>
            完整源集和原模板版本必须匹配；另需明确独立节点 --node、启用账号 --operator 与有限
            --max-pages 预算。不要添加新时间窗，不要在页面输入数据库凭据。
          </p>
          <p>
            实际执行还会检查模板、访问限制、候选证据、并发租约与当前状态；本页不代表这些检查已通过。
          </p>
        </div>
      )}
    </div>
  )
}
