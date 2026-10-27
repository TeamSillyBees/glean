import { useState, type FormEvent } from 'react'
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../lib/api'
import type { CorrectionFields, ManualReview, Membership, Paper, Topic } from '../lib/types'
import { date, topicLabels } from '../lib/utils'
import { Button } from './ui/button'
import { ErrorState, Loading } from './common'
import { managementField } from './topics-management'

function useCatalogWrite(paperId: string, notify: (message: string) => void) {
  const client = useQueryClient()
  const key = ['paper', paperId]
  const pending = useIsMutating({ mutationKey: ['catalog-write', paperId] }) > 0
  const mutation = useMutation({
    mutationKey: ['catalog-write', paperId],
    mutationFn: async ({
      write,
      original,
    }: {
      write: () => Promise<unknown>
      original: unknown
    }) => {
      if (client.getQueryCache().find({ queryKey: key, exact: true }) !== original)
        throw new Error('会话已切换，已取消旧操作。')
      return write()
    },
    onSuccess: async (_, { original }) => {
      if (client.getQueryCache().find({ queryKey: key, exact: true }) !== original) return
      await Promise.all([
        client.invalidateQueries({ queryKey: key }),
        client.invalidateQueries({ queryKey: ['papers'] }),
        client.invalidateQueries({ queryKey: ['facets'] }),
        client.invalidateQueries({ queryKey: ['topics'] }),
        client.invalidateQueries({ queryKey: ['paper-history', paperId] }),
      ])
      if (client.getQueryCache().find({ queryKey: key, exact: true }) === original)
        notify('管理记录已保存。个人阅读与隐藏状态不变。')
    },
  })
  return {
    pending,
    error: mutation.error,
    save: (write: () => Promise<unknown>) => {
      if (client.isMutating({ mutationKey: ['catalog-write', paperId] })) return
      const original = client.getQueryCache().find({ queryKey: key, exact: true })
      if (original) mutation.mutate({ write, original })
    },
    refresh: () => client.invalidateQueries({ queryKey: key }),
  }
}
function WriteError({ error, refresh }: { error: Error | null; refresh: () => unknown }) {
  if (!error) return null
  return (
    <div role="alert" className="text-sm text-red-700">
      {error instanceof ApiError && error.status === 409 ? (
        <>
          <p>记录已被其他人修改，请重新读取后再编辑。</p>
          <Button type="button" variant="outline" onClick={() => refresh()}>
            重新读取管理记录
          </Button>
        </>
      ) : (
        error.message
      )}
    </div>
  )
}
export function CatalogManagement({
  paper,
  topics,
  activeTopic,
  notify,
}: {
  paper: Paper
  topics: Topic[]
  activeTopic?: string
  notify: (message: string) => void
}) {
  const memberships = [...paper.memberships].sort(
    (a, b) => Number(b.topic === activeTopic) - Number(a.topic === activeTopic),
  )
  return (
    <details className="mt-8 border-t pt-5">
      <summary className="cursor-pointer text-sm font-semibold">论文管理</summary>
      <p className="my-4 text-xs leading-6 text-muted-foreground">
        共享审核按研究主题分别保存，影响该主题所有用户；移出主题不会删除论文，也不影响个人隐藏记录。元数据纠错适用于所有主题，保留来源与修改历史。
      </p>
      <div className="space-y-6">
        {memberships.map((membership) => (
          <ReviewEditor
            key={`${membership.topic}-${membership.manualReview?.version ?? 0}`}
            paperId={paper.id}
            membership={membership}
            topicName={
              topics.find((topic) => topic.slug === membership.topic)?.name ||
              topicLabels[membership.topic] ||
              membership.topic
            }
            notify={notify}
          />
        ))}
        <CorrectionEditor
          key={`correction-${paper.correction?.version ?? 0}`}
          paper={paper}
          notify={notify}
        />
        <History paperId={paper.id} />
      </div>
    </details>
  )
}
function ReviewEditor({
  paperId,
  membership,
  topicName,
  notify,
}: {
  paperId: string
  membership: Membership
  topicName: string
  notify: (message: string) => void
}) {
  const review = membership.manualReview
  const [status, setStatus] = useState<ManualReview['status'] | ''>(review?.status ?? '')
  const [note, setNote] = useState(review?.note || '')
  const [labels, setLabels] = useState(review?.labels.join('，') || '')
  const write = useCatalogWrite(paperId, notify)
  function submit(event: FormEvent) {
    event.preventDefault()
    if (!status) return
    write.save(() =>
      api.review(paperId, membership.topic, {
        status,
        note: note.trim(),
        labels: labels
          .split(/[,，\n]/)
          .map((label) => label.trim())
          .filter(Boolean),
        expectedVersion: review?.version ?? 0,
      }),
    )
  }
  return (
    <form onSubmit={submit} aria-label={`审核 ${topicName}`} className="border-t pt-4">
      <h3 className="mb-3 text-sm font-medium">{topicName} · 手动审核</h3>
      {!review && (
        <p className="mb-3 text-xs text-muted-foreground">
          暂无人工审核记录，保存前请明确选择结论。原收录记录：
          {membership.inclusionStatus || '历史目录'} · {membership.tier || '未标注'}
          ；原分类保持可追溯。
        </p>
      )}
      <fieldset disabled={write.pending} className="space-y-3">
        <label className="block text-sm">
          审核结论
          <select
            required
            className={managementField}
            value={status}
            onChange={(e) => setStatus(e.target.value as ManualReview['status'])}
          >
            <option value="">请选择审核结论</option>
            <option value="pending">待审核</option>
            <option value="included">纳入此主题</option>
            <option value="excluded">移出此主题</option>
          </select>
        </label>
        <label className="block text-sm">
          审核备注
          <textarea
            className={managementField}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </label>
        <label className="block text-sm">
          手动标签
          <input
            className={managementField}
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
            placeholder="用逗号分隔"
          />
        </label>
        <WriteError error={write.error} refresh={write.refresh} />
        <Button type="submit" size="sm">
          保存审核
        </Button>
      </fieldset>
    </form>
  )
}
const correctionNames = {
  title: '原文标题',
  titleZh: '中文标题',
  authors: '作者',
  year: '发表年份',
  venue: '期刊或会议',
  abstract: '摘要',
} as const
function CorrectionEditor({ paper, notify }: { paper: Paper; notify: (message: string) => void }) {
  const [fields, setFields] = useState<CorrectionFields>(paper.correction?.fields || {})
  const [reason, setReason] = useState(paper.correction?.reason || '')
  const [validation, setValidation] = useState('')
  const write = useCatalogWrite(paper.id, notify)
  function submit(event: FormEvent) {
    event.preventDefault()
    if (
      Object.hasOwn(fields, 'year') &&
      (!Number.isInteger(fields.year) || fields.year! < 0 || fields.year! > 9999)
    ) {
      setValidation('发表年份必须是 0–9999 的整数，不能留空。')
      return
    }
    if (!reason.trim()) {
      setValidation('请填写纠错原因。')
      return
    }
    setValidation('')
    write.save(() => api.correct(paper.id, fields, reason.trim(), paper.correction?.version ?? 0))
  }
  return (
    <form onSubmit={submit} aria-label="元数据纠错" className="border-t pt-4">
      <h3 className="text-sm font-medium">元数据纠错</h3>
      <p className="my-3 text-xs text-muted-foreground">
        勾选要纠正的字段；取消勾选并保存即恢复来源值。DOI 与 arXiv 标识只读，不在此合并论文。
      </p>
      <fieldset disabled={write.pending} className="space-y-3">
        {(Object.keys(correctionNames) as (keyof CorrectionFields)[]).map((key) => {
          const selected = Object.hasOwn(fields, key)
          const value = fields[key] ?? paper[key]
          return (
            <div key={key}>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={selected}
                  onChange={(e) =>
                    setFields((old) => {
                      const next = { ...old }
                      if (e.target.checked) Object.assign(next, { [key]: paper[key] })
                      else delete next[key]
                      return next
                    })
                  }
                />
                纠正{correctionNames[key]}
              </label>
              {selected && (
                <label className="block text-sm">
                  <span className="sr-only">{correctionNames[key]}</span>
                  {key === 'year' ? (
                    <input
                      type="number"
                      required
                      min={0}
                      max={9999}
                      step={1}
                      className={managementField}
                      value={Number.isFinite(fields.year) ? fields.year : ''}
                      onChange={(e) =>
                        setFields((old) => ({
                          ...old,
                          year: e.target.value === '' ? NaN : Number(e.target.value),
                        }))
                      }
                    />
                  ) : (
                    <textarea
                      rows={key === 'abstract' ? 5 : 2}
                      className={managementField}
                      value={Array.isArray(value) ? value.join('\n') : String(value ?? '')}
                      onChange={(e) =>
                        setFields((old) => ({
                          ...old,
                          [key]: key === 'authors' ? e.target.value.split('\n') : e.target.value,
                        }))
                      }
                    />
                  )}
                </label>
              )}
            </div>
          )
        })}
        <label className="block text-sm">
          纠错原因
          <textarea
            required
            className={managementField}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>
        <WriteError error={write.error} refresh={write.refresh} />
        {validation && (
          <p role="alert" className="text-sm text-red-700">
            {validation}
          </p>
        )}
        <Button size="sm" type="submit">
          保存纠错
        </Button>
      </fieldset>
    </form>
  )
}
function History({ paperId }: { paperId: string }) {
  const [open, setOpen] = useState(false)
  const history = useQuery({
    queryKey: ['paper-history', paperId],
    queryFn: ({ signal }) => api.history(paperId, signal),
    enabled: open,
  })
  return (
    <details onToggle={(event) => setOpen(event.currentTarget.open)} className="border-t pt-4">
      <summary className="cursor-pointer text-sm font-medium">修改历史</summary>
      {open && (
        <p className="mt-2 text-xs text-muted-foreground">
          最近 100 次人工修改记录，原始来源保留在来源与证据中。
        </p>
      )}
      {open &&
        (history.isPending ? (
          <Loading />
        ) : history.isError ? (
          <ErrorState error={history.error} retry={() => history.refetch()} />
        ) : (
          <ol className="mt-3 space-y-3 text-xs">
            {!history.data.items.length && <li>暂无人工修改记录。</li>}
            {history.data.items.map((item) => (
              <li key={item.id} className="border-b pb-3">
                <p>
                  {item.kind} {item.topic} · {date(item.createdAt)} · 操作账号 {item.actorId}
                </p>
                <details className="mt-2">
                  <summary className="cursor-pointer">查看修改前后</summary>
                  <pre className="mt-2 whitespace-pre-wrap break-words">
                    {JSON.stringify({ 修改前: item.before, 修改后: item.after }, null, 2)}
                  </pre>
                </details>
              </li>
            ))}
          </ol>
        ))}
    </details>
  )
}
