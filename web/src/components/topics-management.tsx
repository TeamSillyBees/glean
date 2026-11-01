import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../lib/api'
import type { Topic, TopicInput } from '../lib/types'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'
import { ErrorState, Loading } from './common'

import { managementField } from './management-field'
export function TopicsView({
  currentUserId,
  notify,
}: {
  currentUserId: string
  notify: (message: string) => void
}) {
  const topics = useQuery({ queryKey: ['topics'], queryFn: ({ signal }) => api.topics(signal) })
  const pending = useIsMutating({ mutationKey: ['topic-write'] }) > 0
  const [editor, setEditor] = useState<Topic | 'new' | null>(null)
  return (
    <section className="space-y-6">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[27px] font-semibold">研究主题</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            统一管理主题说明与检索词。保存后用于后续扫描，不自动改变已有论文的审核结论。
          </p>
        </div>
        <Button onClick={() => setEditor('new')}>新增研究主题</Button>
      </header>
      {topics.isPending ? (
        <Loading />
      ) : topics.isError ? (
        <ErrorState error={topics.error} retry={() => topics.refetch()} />
      ) : (
        <div className="divide-y border-y">
          {topics.data.items.map((topic) => (
            <div key={topic.slug} className="flex items-center justify-between gap-4 py-5">
              <div>
                <h2 className="font-medium">{topic.name}</h2>
                <p className="mt-1 text-sm text-muted-foreground">{topic.description}</p>
                <p className="mt-2 text-xs text-muted-foreground">
                  {topic.searchEnabled ? '参与后续扫描' : '暂停检索'} · {topic.slug}
                </p>
              </div>
              <Button
                variant="outline"
                aria-label={`编辑主题 ${topic.name}`}
                onClick={() => setEditor(topic)}
              >
                编辑
              </Button>
            </div>
          ))}
        </div>
      )}
      <Dialog
        open={!!editor}
        onOpenChange={(open) => {
          if (!open && !pending) setEditor(null)
        }}
      >
        <DialogContent className="max-h-[90dvh] overflow-y-auto" showCloseButton={!pending}>
          {editor && (
            <TopicEditor
              key={`${currentUserId}-${editor === 'new' ? 'new' : editor.slug}`}
              topic={editor === 'new' ? undefined : editor}
              onClose={() => setEditor(null)}
              notify={notify}
            />
          )}
        </DialogContent>
      </Dialog>
    </section>
  )
}
function TopicEditor({
  topic,
  onClose,
  notify,
}: {
  topic?: Topic
  onClose: () => void
  notify: (message: string) => void
}) {
  const client = useQueryClient()
  const labelId = useId()
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const [name, setName] = useState(topic?.name || '')
  const [description, setDescription] = useState(topic?.description || '')
  const [rule, setRule] = useState(topic?.rule || '')
  const [enabled, setEnabled] = useState(topic?.searchEnabled ?? true)
  const [terms, setTerms] = useState(
    topic?.termGroups?.map((group) => group.join(' | ')).join('\n') || '',
  )
  const pending = useIsMutating({ mutationKey: ['topic-write'] }) > 0
  const mutation = useMutation({
    mutationKey: ['topic-write'],
    mutationFn: ({ input, original }: { input: TopicInput; original: unknown }) => {
      if (client.getQueryCache().find({ queryKey: ['topics'], exact: true }) !== original)
        throw new Error('会话已切换，已取消旧操作。')
      return api.saveTopic(input, topic?.slug)
    },
    onSuccess: async (_, variables) => {
      if (client.getQueryCache().find({ queryKey: ['topics'], exact: true }) !== variables.original)
        return
      await client.invalidateQueries({ queryKey: ['topics'] })
      if (client.getQueryCache().find({ queryKey: ['topics'], exact: true }) !== variables.original)
        return
      if (mounted.current) {
        notify('研究主题已保存。后续扫描使用新配置。')
        onClose()
      }
    },
  })
  function submit(event: FormEvent) {
    event.preventDefault()
    if (client.isMutating({ mutationKey: ['topic-write'] })) return
    mutation.mutate({
      original: client.getQueryCache().find({ queryKey: ['topics'], exact: true }),
      input: {
        name: name.trim(),
        description: description.trim(),
        rule: rule.trim(),
        searchEnabled: enabled,
        termGroups: terms
          .split('\n')
          .map((line) =>
            line
              .split('|')
              .map((term) => term.trim())
              .filter(Boolean),
          )
          .filter((group) => group.length),
        expectedVersion: topic?.version ?? 0,
      },
    })
  }
  return (
    <form onSubmit={submit} className="space-y-4">
      <DialogTitle>{topic ? '编辑研究主题' : '新增研究主题'}</DialogTitle>
      <DialogDescription>
        每行是一组必须满足的检索词，同组使用 |
        分隔同义词。内置主题留空保留默认检索模板；新主题启用检索时需填写词组。
      </DialogDescription>
      <fieldset disabled={pending} className="space-y-4">
        <label className="block text-sm">
          主题名称
          <input
            required
            maxLength={100}
            className={managementField}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="block text-sm">
          <span id={`${labelId}-description`}>主题说明</span>
          <textarea
            aria-labelledby={`${labelId}-description`}
            className={managementField}
            maxLength={2000}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </label>
        <label className="block text-sm">
          <span id={`${labelId}-rule`}>纳入规则</span>
          <textarea
            aria-labelledby={`${labelId}-rule`}
            className={managementField}
            maxLength={4000}
            value={rule}
            onChange={(e) => setRule(e.target.value)}
          />
        </label>
        <label className="block text-sm">
          <span id={`${labelId}-terms`}>检索词组</span>
          <textarea
            aria-labelledby={`${labelId}-terms`}
            required={
              enabled &&
              !['biomed-ai', 'llm-agent', 'biomed-worldmodel'].includes(topic?.slug || '')
            }
            rows={4}
            className={managementField}
            value={terms}
            onChange={(e) => setTerms(e.target.value)}
            placeholder={'cancer | tumor\nartificial intelligence | machine learning'}
          />
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          参与后续扫描
        </label>
        {mutation.error && (
          <p role="alert" className="text-sm text-red-700">
            {mutation.error instanceof ApiError && mutation.error.status === 409
              ? '主题已被修改，请重新读取后编辑。'
              : mutation.error.message}
          </p>
        )}
        {mutation.error instanceof ApiError && mutation.error.status === 409 && (
          <Button
            type="button"
            variant="outline"
            onClick={async () => {
              await client.invalidateQueries({ queryKey: ['topics'] })
              if (mounted.current) onClose()
            }}
          >
            重新读取主题
          </Button>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button type="submit">{pending ? '保存中…' : '保存主题'}</Button>
        </div>
      </fieldset>
    </form>
  )
}
