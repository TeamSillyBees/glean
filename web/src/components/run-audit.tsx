import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, request } from '../lib/api'
import { Button } from './ui/button'
import { ErrorState, Loading } from './common'
import type { AcquisitionBudget, Run } from './run-types'
import { managementField } from './management-field'

interface RequestAttempt {
  request_id: string
  source: string
  query_id: string
  requested_at: string
  status: string
  attempted: boolean
  http_status: number | null
  returned_count: number | null
  cursor_in: string
  cursor_out: string
  terminal_evidence: string
  next_action: string
}
export function RunAudit({ run, canManage }: { run: Run; canManage: boolean }) {
  const id = run.id
  const [open, setOpen] = useState(false)
  const [after, setAfter] = useState('')
  const [downloading, setDownloading] = useState(false)
  const [error, setError] = useState('')
  const query = useQuery({
    queryKey: ['run-requests', id, after],
    enabled: open,
    queryFn: ({ signal }) =>
      request<{ items: RequestAttempt[]; nextAfter: string | null }>(
        `/api/v1/monitor/runs/${encodeURIComponent(id)}/requests?limit=50${after ? `&after=${encodeURIComponent(after)}` : ''}`,
        { signal },
      ),
  })
  async function download() {
    setDownloading(true)
    setError('')
    try {
      const blob = await api.downloadAudit(id)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `glean-run-${id}.zip`
      link.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setDownloading(false)
    }
  }
  return (
    <div className="mt-4 border-t pt-3">
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => setOpen(!open)}>
          {open ? '收起请求账本' : '查看请求账本'}
        </Button>
        <Button variant="outline" size="sm" onClick={download} disabled={downloading}>
          {downloading ? '导出中…' : '下载完整审计 ZIP'}
        </Button>
      </div>
      {error && (
        <p role="alert" className="mt-2 text-sm text-red-700">
          {error}
        </p>
      )}
      {run.budgetChanges?.map((change) => (
        <p className="mt-2 text-xs" key={change.revision}>
          预算调整 #{change.revision} · {change.topic} ·{' '}
          {new Date(change.changedAt).toLocaleString('zh-CN')} · {change.reason}
        </p>
      ))}
      {canManage &&
        (!run.finishedAt || run.finishedAt.startsWith('0001-')) &&
        run.searchTemplates
          ?.filter((t) => t.protocol)
          .map((template) => {
            const limits =
              [...(run.budgetChanges ?? [])]
                .reverse()
                .find((change) => change.topic === template.topic)?.after ??
              template.protocol!.budget
            return (
              <BudgetIncrease
                key={`${template.topic}-${run.budgetChanges?.length ?? 0}`}
                run={run}
                topic={template.topic}
                limits={limits}
              />
            )
          })}
      {open && (
        <div className="mt-3 space-y-3">
          <p className="text-xs text-muted-foreground">
            每页 50
            次尝试；未请求与零结果分别记录。完整导出包含全部发现路径、证据状态及当前用户隐藏标记；取得不等于审读或正式纳入。
          </p>
          {query.isPending ? (
            <Loading />
          ) : query.isError ? (
            <ErrorState error={query.error} retry={() => query.refetch()} />
          ) : (
            <>
              {!query.data.items.length && (
                <p className="text-sm">暂无请求记录；旧运行可能没有请求级审计。</p>
              )}
              <div className="divide-y">
                {query.data.items.map((attempt) => (
                  <div className="py-3 text-xs" key={attempt.request_id}>
                    <p className="break-words font-medium">
                      {attempt.source} · {attempt.query_id} · {attempt.status}
                    </p>
                    <p className="mt-1">
                      {new Date(attempt.requested_at).toLocaleString('zh-CN')} ·{' '}
                      {attempt.attempted ? `HTTP ${attempt.http_status ?? '无响应'}` : '未发出请求'}{' '}
                      · 已取得 {attempt.returned_count ?? '未知'}
                    </p>
                    <p className="mt-1 break-all">
                      输入游标：{attempt.cursor_in || '起点'}；输出游标：
                      {attempt.cursor_out || '无'}
                    </p>
                    {attempt.terminal_evidence && <p>终止依据：{attempt.terminal_evidence}</p>}
                    {attempt.next_action && <p>下一动作：{attempt.next_action}</p>}
                  </div>
                ))}
              </div>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" disabled={!after} onClick={() => setAfter('')}>
                  回到首页
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!query.data.nextAfter}
                  onClick={() => setAfter(query.data.nextAfter!)}
                >
                  下一页请求
                </Button>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function BudgetIncrease({
  run,
  topic,
  limits,
}: {
  run: Run
  topic: string
  limits: AcquisitionBudget
}) {
  const [next, setNext] = useState(limits)
  const [reason, setReason] = useState('')
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: () =>
      request(`/api/v1/monitor/runs/${encodeURIComponent(run.id)}/budget`, {
        method: 'POST',
        body: JSON.stringify({
          topic,
          expectedRevision: run.budgetChanges?.length ?? 0,
          limits: next,
          reason: reason.trim(),
        }),
      }),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['runs'] }),
        client.invalidateQueries({ queryKey: ['runs-archive'] }),
      ])
    },
  })
  const fields = [
    ['max_requests', '累计请求'],
    ['max_pages', '累计页数'],
    ['max_records', '累计记录'],
    ['max_queue', '待处理队列'],
  ] as const
  return (
    <details className="mt-3 rounded border p-3">
      <summary className="cursor-pointer text-sm">提高采集预算 · {topic}</summary>
      <p className="my-2 text-xs text-muted-foreground">
        仅提高本运行的操作额度，原协议与历史范围保持冻结。保存后按原命令续跑；此操作不启动采集或模型。
      </p>
      <fieldset disabled={mutation.isPending} className="grid grid-cols-2 gap-3">
        {fields.map(([key, label]) => (
          <label key={key} className="text-xs">
            {label}（当前 {limits[key]}）
            <input
              className={managementField}
              type="number"
              min={limits[key]}
              max={key === 'max_records' ? 10000000 : 1000000}
              value={next[key]}
              onChange={(e) => setNext({ ...next, [key]: Number(e.target.value) })}
            />
          </label>
        ))}
        <label className="col-span-2 text-xs">
          提高预算的理由
          <textarea
            className={managementField}
            maxLength={1000}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>
        <Button
          variant="outline"
          size="sm"
          disabled={
            !reason.trim() ||
            fields.some(([key]) => !Number.isInteger(next[key]) || next[key] < limits[key]) ||
            fields.every(([key]) => next[key] === limits[key])
          }
          onClick={() => mutation.mutate()}
        >
          保存预算调整
        </Button>
      </fieldset>
      {mutation.isError && (
        <p role="alert" className="mt-2 text-xs text-red-700">
          {mutation.error.message}
        </p>
      )}
    </details>
  )
}
