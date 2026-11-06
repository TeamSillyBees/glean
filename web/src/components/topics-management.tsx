import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../lib/api'
import { exampleSearchRoutes, parseSearchRoutes } from '../lib/search-routes'
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
  const [routes, setRoutes] = useState(JSON.stringify(topic?.searchRoutes || [], null, 2))
  let routesError = ''
  let parsedRoutes: ReturnType<typeof parseSearchRoutes> = []
  try {
    parsedRoutes = parseSearchRoutes(routes)
  } catch (error) {
    routesError = (error as Error).message
  }
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
    if (client.isMutating({ mutationKey: ['topic-write'] }) || routesError) return
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
        searchRoutes: parsedRoutes,
        expectedVersion: topic?.version ?? 0,
      },
    })
  }
  return (
    <form onSubmit={submit} className="space-y-4">
      <DialogTitle>{topic ? '编辑研究主题' : '新增研究主题'}</DialogTitle>
      <DialogDescription>
        每行是一组必须满足的检索词，同组使用 |
        分隔同义词。内置主题留空保留默认检索模板；新主题启用检索时需填写词组或检索路线。
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
              parsedRoutes.length === 0 &&
              !['biomed-ai', 'llm-agent', 'biomed-worldmodel'].includes(topic?.slug || '')
            }
            rows={4}
            className={managementField}
            value={terms}
            onChange={(e) => setTerms(e.target.value)}
            placeholder={'cancer | tumor\nartificial intelligence | machine learning'}
          />
        </label>
        <details
          className="rounded-lg border p-3"
          open={!!topic?.searchRoutes?.length || undefined}
        >
          <summary className="cursor-pointer text-sm font-medium">高级检索路线（JSON）</summary>
          <p id={`${labelId}-routes-help`} className="my-3 text-xs leading-6 text-muted-foreground">
            最多 20 条，id 为 1–80 位小写字母、数字或连字符且唯一稳定。每条路线包含 sources 与
            termGroups；词组之间为 AND，同组为 OR。 每条最多 10 组、50 个词，每词最多 300
            字节。配置路线后使用路线检索，基础词组仍保留。清空为 [] 恢复基础词组或内置模板。
            来源：crossref、pubmed、europepmc、arxiv、biorxiv、medrxiv。title_abstract 仅用于 pubmed
            / europepmc；其他来源使用 default。 backfillFrom 可选，填写非未来的 ISO 时间戳，例如
            2020-01-01T00:00:00Z。 Crossref
            以首组各词分别检索后取并集，不执行服务端布尔词组匹配；arXiv、bioRxiv、medRxiv
            按时间窗口采集。
            取得后先保留候选，再由后续流程审核主题相关性，不保证关键词已匹配。不同来源的命中数不能直接比较，完成不保证文献穷尽。保存仅更新配置，不会立即启动扫描或回填。
          </p>
          <label className="block text-sm">
            <span id={`${labelId}-routes`}>检索路线 JSON</span>
            <textarea
              aria-labelledby={`${labelId}-routes`}
              aria-describedby={`${labelId}-routes-help`}
              aria-invalid={!!routesError}
              rows={12}
              className={`${managementField} font-mono text-xs`}
              value={routes}
              onChange={(event) => setRoutes(event.target.value)}
              spellCheck={false}
            />
          </label>
          {routesError ? (
            <p role="alert" className="mt-2 text-sm text-red-700">
              {routesError}
            </p>
          ) : (
            <p className="mt-2 text-xs text-muted-foreground">
              {parsedRoutes.length} 条路线 ·{' '}
              {parsedRoutes.reduce((count, route) => count + route.sources.length, 0)} 个来源查询
            </p>
          )}
          <details className="mt-3 text-xs">
            <summary className="cursor-pointer">查看双路线示例（复制到上方编辑）</summary>
            <pre className="mt-2 overflow-auto whitespace-pre-wrap">
              {JSON.stringify(exampleSearchRoutes, null, 2)}
            </pre>
          </details>
        </details>
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
          <Button type="submit" disabled={!!routesError}>
            {pending ? '保存中…' : '保存主题'}
          </Button>
        </div>
      </fieldset>
    </form>
  )
}
