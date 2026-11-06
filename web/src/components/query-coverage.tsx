import { number, topicLabels } from '../lib/utils'
import { Badge } from './common'
import type { QueryProgress } from './run-types'
const labels: Record<QueryProgress['status'], string> = {
  unavailable: '未执行 · 来源未启用',
  not_started: '尚未开始',
  running: '进行中',
  retry_wait: '等待重试',
  partial_budget: '部分完成 · 预算已用尽',
  completed: '窗口已完成',
  completed_empty: '窗口已完成 · 无命中',
  blocked: '受阻',
  failed: '失败',
}
const timestamp = (value?: string) => (value && !value.startsWith('0001-') ? value : '尚未建立')
export function QueryCoverage({ queries }: { queries?: Record<string, QueryProgress> }) {
  const items = Object.entries(queries || {})
  if (!items.length)
    return (
      <p className="w-full text-xs text-muted-foreground">
        此记录无逐查询覆盖数据，不能据此判断各路线完成情况。
      </p>
    )
  const completed = items.filter(([, item]) =>
    ['completed', 'completed_empty'].includes(item.status),
  ).length
  return (
    <details className="w-full min-w-0 rounded-lg border p-3">
      <summary className="cursor-pointer font-medium">
        查询覆盖：{completed} / {items.length} 个窗口完成
      </summary>
      <p className="mt-2 text-xs leading-6 text-muted-foreground">
        完成表示该查询窗口处理结束，不代表全部历史文献已收齐。观察数包含重复命中；新增候选不等于已纳入论文。预算耗尽或受阻不能视为完整覆盖。
      </p>
      <div className="mt-3 divide-y">
        {items.map(([key, item]) => (
          <section key={key} aria-label={`查询 ${item.queryId}`} className="min-w-0 space-y-2 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="break-all font-medium">{item.queryId}</h3>
              <Badge
                tone={
                  ['completed', 'completed_empty'].includes(item.status)
                    ? 'green'
                    : ['failed', 'blocked'].includes(item.status)
                      ? 'red'
                      : 'amber'
                }
              >
                {labels[item.status] || item.status}
              </Badge>
            </div>
            <p className="break-words text-xs text-muted-foreground">
              主题 {topicLabels[item.topic] || item.topic} · {item.source} · 模式 {item.mode} · 版本{' '}
              {item.queryVersion}
            </p>
            <p className="text-xs">
              {number(item.requests)} 次请求 · {number(item.pages)} 页 · {number(item.observed)}{' '}
              条观察 · {number(item.quarantined)} 条隔离 · {number(item.newCandidates)} 条新增候选
            </p>
            <p className="break-words text-xs">
              {item.coverageWindow ? '整体请求范围：' : '查询窗口（旧记录）：'}
              {timestamp((item.coverageWindow || item.window)?.start)} →{' '}
              {timestamp((item.coverageWindow || item.window)?.end)}
            </p>
            {item.coverageWindow && (
              <p className="break-words text-xs">
                当前时间分区：{timestamp(item.window?.start)} → {timestamp(item.window?.end)}
              </p>
            )}
            {item.advanceCheckpoint === false && (
              <p className="text-xs text-amber-800">
                本次扫描未建立连续增量覆盖，不推进增量检查点。
              </p>
            )}
            <p className="break-words text-xs">
              目标结束：{timestamp(item.targetEnd)} · 完成时间：{timestamp(item.completedAt)}
            </p>
            {item.queries?.length || item.query ? (
              <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-secondary p-2 text-xs">
                {item.queries?.length ? item.queries.join('\n') : item.query}
              </pre>
            ) : null}
            {item.completedPartitions != null && (
              <p className="text-xs text-muted-foreground">
                已处理 {number(item.completedPartitions)} 个时间分区
              </p>
            )}
            {item.lastError && (
              <p className="break-words text-xs text-amber-800">最近查询错误：{item.lastError}</p>
            )}
            {item.lastError?.startsWith('protocol_') && (
              <p className="text-xs text-amber-800">
                已达到采集预算，原查询和游标保留。队列排空后可恢复；管理员可在本运行下方提高操作额度，再按原命令续跑。
              </p>
            )}
            {item.warnings?.map((warning, index) => (
              <p key={index} className="break-words text-xs text-amber-800">
                来源限制：{warning}
              </p>
            ))}
          </section>
        ))}
      </div>
    </details>
  )
}
