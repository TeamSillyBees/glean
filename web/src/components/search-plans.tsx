import { useIsMutating, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Pencil, History } from 'lucide-react'
import { request } from '../lib/api'
import type { SearchRoute, Topic, TopicSlug } from '../lib/types'
import { topicLabels } from '../lib/utils'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'
import { Dialog, DialogContent } from './ui/dialog'
import { TopicEditor } from './topics-management'

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
  topic: string
  query: string
  terms: string[]
  termGroups?: string[][]
  routes?: SearchRoute[]
  policy: {
    jifMinimum: number
    jifStrict: boolean
    ccf: string[]
    fullPaperRequired: boolean
    mainTrackRequired: boolean
  }
}
const sourceNames: Record<string, string> = {
  pubmed: 'PubMed',
  crossref: 'Crossref',
  europepmc: 'Europe PMC',
  arxiv: 'arXiv',
  biorxiv: 'bioRxiv',
  medrxiv: 'medRxiv',
}

// TemplatesView 展示实际生效的关键词和来源，将编译细节保留在折叠区域。
export function TemplatesView({
  topic,
  topicInfo,
  canManage = false,
  notify = () => {},
  onOpenRuns,
}: {
  topic: TopicSlug
  topicInfo?: Topic
  canManage?: boolean
  notify?: (message: string) => void
  onOpenRuns?: () => void
}) {
  const [editor, setEditor] = useState<Topic | null>(null)
  const pending = useIsMutating({ mutationKey: ['topic-write'] }) > 0
  const query = useQuery({
    queryKey: ['templates'],
    queryFn: ({ signal }) =>
      request<{ items: SearchTemplate[]; version: string; previews?: QueryPreview[] }>(
        '/api/v1/monitor/templates',
        { signal },
      ),
  })
  if (query.isPending) return <Loading label="正在读取检索方案…" />
  if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const templates = query.data.items.filter((item) => item.topic === topic)
  const plans = templates.flatMap((template) =>
    template.routes?.length
      ? template.routes.map((route) => ({
          ...template,
          id: `${template.id}/${route.id}`,
          name: route.name || '',
          sources: route.sources,
          termGroups: route.termGroups,
        }))
      : [{ ...template, name: '', sources: [], termGroups: template.termGroups || [] }],
  )
  const previews = (query.data.previews || []).filter((item) => item.topic === topic)
  const sources = [...new Set(previews.map((item) => item.source))]
  const enabled = topicInfo?.searchEnabled !== false
  const name = topicInfo?.name || topicLabels[topic] || topic
  // Built-in defaults live on the server. Populate the editor with the actual saved/effective groups.
  const editableTopic = topicInfo && {
    ...topicInfo,
    termGroups:
      topicInfo.termGroups?.length || topicInfo.searchRoutes?.length
        ? topicInfo.termGroups
        : templates[0]?.termGroups,
  }
  return (
    <div className="space-y-5">
      <section className="rounded-xl border bg-card p-5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="font-semibold">{name}</h2>
            <div className="mt-2">
              <Badge tone={enabled ? 'green' : 'neutral'}>
                {enabled ? '参与后续采集' : '已暂停检索'}
              </Badge>
            </div>
          </div>
          {canManage && editableTopic && (
            <Button onClick={() => setEditor(editableTopic)}>
              <Pencil />
              编辑检索方案
            </Button>
          )}
        </div>
        <div className="mt-5 border-t pt-4">
          <h3 className="text-xs font-medium text-muted-foreground">检索来源</h3>
          <div className="mt-2 flex flex-wrap gap-2">
            {sources.length ? (
              sources.map((source) => (
                <Badge
                  key={source}
                  tone={
                    previews.some((p) => p.source === source && p.enabled) ? 'violet' : 'neutral'
                  }
                >
                  {sourceNames[source] || source}
                  {!previews.some((p) => p.source === source && p.enabled) && ' · 未启用'}
                </Badge>
              ))
            ) : (
              <span className="text-sm text-muted-foreground">暂无可执行来源</span>
            )}
          </div>
        </div>
      </section>
      {!templates.length ? (
        <Empty
          title="暂无该主题的检索方案"
          description={
            enabled
              ? '添加关键词后，后续采集会使用此方案。'
              : '此主题已暂停，启用后可查看实际查询。'
          }
        />
      ) : (
        plans.map((template, index) => (
          <section key={template.id} className="rounded-xl border bg-card p-5">
            <h3 className="text-sm font-semibold">
              {template.name || (plans.length > 1 ? `检索路线 ${index + 1}` : '检索关键词')}
            </h3>
            {!!template.sources.length && (
              <div className="mt-2 flex flex-wrap gap-2">
                {template.sources.map((source) => (
                  <Badge key={source}>{sourceNames[source] || source}</Badge>
                ))}
              </div>
            )}
            <div className="mt-3 space-y-3">
              {(template.termGroups?.length ? template.termGroups : [template.terms])
                .filter((g) => g?.length)
                .map((group, groupIndex) => (
                  <div key={groupIndex}>
                    {groupIndex > 0 && (
                      <p className="mb-2 text-xs text-muted-foreground">同时包含</p>
                    )}
                    <div className="flex flex-wrap items-center gap-2 rounded-lg bg-secondary/50 p-3">
                      <span className="text-xs text-muted-foreground">任一词</span>
                      {group.map((term) => (
                        <Badge key={term}>{term}</Badge>
                      ))}
                    </div>
                  </div>
                ))}
            </div>
            <details className="mt-4 border-t pt-3 text-xs">
              <summary className="cursor-pointer text-muted-foreground">查看纳入条件</summary>
              <div className="mt-3 flex flex-wrap gap-2">
                <Badge>
                  期刊影响因子 {template.policy.jifStrict ? '>' : '≥'} {template.policy.jifMinimum}
                </Badge>
                {!!template.policy.ccf?.length && (
                  <Badge>CCF {template.policy.ccf.join(' / ')}</Badge>
                )}
                {template.policy.fullPaperRequired && <Badge>完整论文</Badge>}
                {template.policy.mainTrackRequired && <Badge>主会 / 主轨</Badge>}
              </div>
            </details>
          </section>
        ))
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">保存的方案用于后续采集。</p>
        {onOpenRuns && (
          <Button variant="outline" onClick={onOpenRuns}>
            <History />
            查看采集记录
          </Button>
        )}
      </div>
      <details className="min-w-0 rounded-xl border bg-card p-4">
        <summary className="cursor-pointer text-sm font-medium">高级：查看各来源查询语句</summary>
        <section aria-label="来源查询编译预览" className="mt-4 space-y-3">
          <p className="break-all text-xs text-muted-foreground">
            仅编译预览，尚未执行 · 版本 {query.data.version}
          </p>
          {!previews.length ? (
            <p className="text-xs text-muted-foreground">暂无该主题的逐来源编译预览</p>
          ) : (
            previews.map((preview, index) => (
              <article
                key={`${preview.queryId}-${preview.source}-${index}`}
                className="min-w-0 space-y-2 rounded-lg border p-3"
              >
                <h3 className="break-all text-sm font-medium">
                  {sourceNames[preview.source] || preview.source} · {preview.queryId}
                </h3>
                {!preview.enabled && <p className="text-xs text-amber-800">当前配置未启用此来源</p>}
                {preview.backfillFrom && (
                  <p className="text-xs">历史起点：{preview.backfillFrom}</p>
                )}
                {preview.mode === 'seed_union' && (
                  <p className="text-xs text-muted-foreground">各关键词独立查询后合并结果。</p>
                )}
                {preview.mode === 'full_window' && (
                  <p className="text-xs text-amber-800">按时间采集，不在来源端按关键词过滤。</p>
                )}
                {preview.queries?.length
                  ? preview.queries.map((seed, i) => (
                      <div key={i}>
                        <p className="mb-1 text-xs">独立查询 {i + 1}</p>
                        <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-secondary p-3 text-xs">
                          {seed}
                        </pre>
                      </div>
                    ))
                  : preview.query && (
                      <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-secondary p-3 text-xs">
                        {preview.query}
                      </pre>
                    )}
                {preview.warnings?.map((warning, i) => (
                  <p key={i} className="break-words text-xs text-amber-800">
                    来源限制：{warning}
                  </p>
                ))}
              </article>
            ))
          )}
        </section>
      </details>
      <Dialog
        open={!!editor}
        onOpenChange={(open) => {
          if (!open && !pending) setEditor(null)
        }}
      >
        <DialogContent className="max-h-[90dvh] overflow-y-auto" showCloseButton={!pending}>
          {editor && (
            <TopicEditor
              key={topic}
              topic={editor}
              searchOnly
              onClose={() => setEditor(null)}
              notify={notify}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
