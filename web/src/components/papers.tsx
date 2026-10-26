import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowUpRight,
  BookOpen,
  CalendarDays,
  Check,
  ChevronRight,
  EyeOff,
  FileText,
  Link2,
  Quote,
} from 'lucide-react'
import type { Paper, ReadStatus, TopicSlug } from '../lib/types'
import { api } from '../lib/api'
import { cn, date, readLabels, reviewLabels, safeLink, topicLabels } from '../lib/utils'
import { Badge, Empty, ErrorState, ExternalLink, Loading } from './common'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import { Tooltip } from './ui/tooltip'
export function PaperCard({
  paper,
  topic,
  onOpen,
}: {
  paper: Paper
  topic: TopicSlug
  onOpen: () => void
}) {
  const membership = paper.memberships?.find((item) => item.topic === topic)
  const tags = membership?.categories?.slice(0, 3) || []
  const reviewStatus = membership?.dimensions?.display_review_status?.[0]
  return (
    <article className="paper-card group relative rounded-xl border bg-white px-6 py-5 transition-all hover:border-violet-200 hover:shadow-[0_5px_24px_-15px_rgba(79,70,229,.45)]">
      <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
        <span className="font-semibold text-slate-700">{paper.venue || '来源待核验'}</span>
        {paper.year > 0 && <span className="text-muted-foreground">· {paper.year}</span>}
        {membership?.tier && <Badge tone="violet">{membership.tier}</Badge>}
        <span className="ml-auto flex items-center gap-2">
          {paper.isMock && <Badge tone="violet">MOCK</Badge>}
          <span
            className={cn(
              'flex items-center gap-1.5 text-[11px]',
              paper.readStatus === 'read' ? 'text-emerald-700' : 'text-muted-foreground',
            )}
          >
            <span
              className={cn(
                'size-1.5 rounded-full',
                paper.readStatus === 'read'
                  ? 'bg-emerald-500'
                  : paper.readStatus === 'reading'
                    ? 'bg-violet-500'
                    : 'bg-slate-300',
              )}
            />
            {readLabels[paper.readStatus] || '未读'}
          </span>
        </span>
      </div>
      <button
        onClick={onOpen}
        className="block w-full rounded-sm text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <h3 className="pr-5 text-[17px] font-semibold leading-[1.7] tracking-tight text-slate-800 transition-colors group-hover:text-primary">
          {paper.titleZh || paper.title}
        </h3>
        {paper.titleZh && paper.title !== paper.titleZh && (
          <p className="mt-1 line-clamp-1 text-[13px] leading-6 text-slate-500">{paper.title}</p>
        )}
      </button>
      {paper.abstract && (
        <p className="mt-3 line-clamp-2 text-[13px] leading-6 text-slate-500">{paper.abstract}</p>
      )}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        {tags.map((tag) => (
          <Badge key={tag}>{tag}</Badge>
        ))}
        {reviewStatus && (
          <span className="text-[11px] text-muted-foreground">
            {reviewLabels[reviewStatus] || reviewStatus}
          </span>
        )}
        {paper.publicationType && (
          <span className="text-[11px] text-muted-foreground">{paper.publicationType}</span>
        )}
        <span className="ml-auto flex items-center gap-3 text-[11px] text-muted-foreground">
          <Tooltip label="只展示正式 online 日期；不以接收或索引日期代替">
            <span className="inline-flex items-center gap-1.5">
              <CalendarDays className="size-3.5" />
              {paper.formalOnlineDate ? date(paper.formalOnlineDate) : '正式日期待核验'}
            </span>
          </Tooltip>
          <button
            onClick={onOpen}
            aria-label={`查看 ${paper.titleZh || paper.title} 的详情`}
            className="flex items-center gap-1 rounded p-1 text-primary hover:bg-violet-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            详情
            <ChevronRight className="size-3.5" />
          </button>
        </span>
      </div>
    </article>
  )
}
export function PaperDetail({
  id,
  onClose,
  notify,
  restoreFocus,
}: {
  id: string | null
  onClose: () => void
  notify: (message: string) => void
  restoreFocus?: () => void
}) {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ['paper', id],
    queryFn: ({ signal }) => api.paper(id!, signal),
    enabled: !!id,
  })
  const mutation = useMutation({
    mutationFn: ({
      paper,
      hidden = paper.hidden,
      readStatus = paper.readStatus,
    }: {
      paper: Paper
      hidden?: boolean
      readStatus?: ReadStatus
    }) => api.state(paper.id, hidden, readStatus),
    onSuccess: (_, variables) => {
      client.invalidateQueries({ queryKey: ['papers'] })
      client.invalidateQueries({ queryKey: ['paper', variables.paper.id] })
      client.invalidateQueries({ queryKey: ['facets'] })
      notify(
        variables.hidden === true ? '已加入隐藏列表，可在「已隐藏」中恢复。' : '阅读状态已保存。',
      )
    },
  })
  const paper = query.data
  return (
    <Dialog
      open={!!id}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent
        drawer
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          restoreFocus?.()
        }}
      >
        <div className="border-b px-7 py-6">
          <div className="mb-1 flex items-center gap-2 text-xs font-medium text-primary">
            <BookOpen className="size-4" />
            阅读空间
          </div>
          <DialogTitle className="pr-10 text-lg font-semibold">论文详情</DialogTitle>
          <DialogDescription className="mt-1 text-xs text-muted-foreground">
            原始证据与个人阅读状态分开保存。
          </DialogDescription>
        </div>
        {query.isLoading ? (
          <Loading />
        ) : query.isError ? (
          <div className="p-7">
            <ErrorState error={query.error} retry={() => query.refetch()} />
          </div>
        ) : (
          paper && (
            <div className="px-7 py-7">
              <div className="mb-4 flex flex-wrap gap-2">
                <Badge>{paper.venue || '来源待核验'}</Badge>
                {paper.year > 0 && <Badge>{paper.year}</Badge>}
                {paper.isMock && <Badge tone="violet">MOCK · 非真实审读</Badge>}
                {paper.hidden && <Badge tone="amber">已隐藏</Badge>}
              </div>
              <h2 className="text-2xl font-semibold leading-relaxed tracking-tight">
                {paper.titleZh || paper.title}
              </h2>
              {paper.titleZh && (
                <p className="mt-3 text-sm leading-6 text-muted-foreground">{paper.title}</p>
              )}
              <p className="mt-4 text-xs leading-6 text-muted-foreground">
                {paper.authors?.length ? paper.authors.join(' · ') : '作者信息待补充'}
              </p>
              <div className="mt-6 flex flex-wrap items-center gap-2">
                {(['unread', 'reading', 'read'] as ReadStatus[]).map((status) => (
                  <Button
                    key={status}
                    size="sm"
                    variant={paper.readStatus === status ? 'default' : 'outline'}
                    disabled={mutation.isPending && mutation.variables?.paper.id === id}
                    onClick={() => mutation.mutate({ paper, readStatus: status })}
                  >
                    {paper.readStatus === status && <Check />}
                    {readLabels[status]}
                  </Button>
                ))}
                <Tooltip label={paper.hidden ? '移回论文目录' : '从默认列表隐藏，可随时恢复'}>
                  <Button
                    className="ml-auto"
                    variant="ghost"
                    size="sm"
                    disabled={mutation.isPending && mutation.variables?.paper.id === id}
                    onClick={() => mutation.mutate({ paper, hidden: !paper.hidden })}
                  >
                    <EyeOff />
                    {paper.hidden ? '恢复显示' : '隐藏'}
                  </Button>
                </Tooltip>
              </div>
              {mutation.isError && mutation.variables?.paper.id === id && (
                <p role="alert" className="mt-3 text-sm text-red-700">
                  {mutation.error.message}
                </p>
              )}
              <Tabs defaultValue="overview" className="mt-8">
                <TabsList className="w-full">
                  <TabsTrigger className="flex-1" value="overview">
                    内容概览
                  </TabsTrigger>
                  <TabsTrigger className="flex-1" value="evidence">
                    来源与证据
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="overview">
                  <section>
                    <h3 className="section-title">
                      <Quote className="size-4" />
                      摘要 / 历史摘要
                    </h3>
                    <p className="mt-3 whitespace-pre-line text-sm leading-8 text-slate-600">
                      {paper.abstract || '这篇论文暂未提供摘要。可通过下方原文链接继续阅读。'}
                    </p>
                  </section>
                  <section className="mt-8">
                    <h3 className="section-title">
                      <CalendarDays className="size-4" />
                      正式发表时间
                    </h3>
                    <div className="mt-4 grid grid-cols-2 gap-3">
                      <div className="rounded-xl bg-secondary/60 p-4">
                        <p className="text-xs text-muted-foreground">正式 online</p>
                        <p className="mt-2 font-semibold">{date(paper.formalOnlineDate)}</p>
                      </div>
                      <div className="rounded-xl bg-secondary/60 p-4">
                        <p className="text-xs text-muted-foreground">卷期发表</p>
                        <p className="mt-2 font-semibold">{date(paper.issueDate)}</p>
                      </div>
                    </div>
                    <p className="mt-3 text-xs leading-6 text-muted-foreground">
                      {paper.dateProvenance ||
                        '日期依据待核验。接收、提交和索引日期不视为正式发表日期。'}
                    </p>
                  </section>
                  <section className="mt-8">
                    <h3 className="section-title">
                      <FileText className="size-4" />
                      主题归属
                    </h3>
                    <div className="mt-4 space-y-3">
                      {paper.memberships?.map((m) => (
                        <div key={`${m.topic}-${m.originalId}`} className="rounded-xl border p-4">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="text-sm font-medium">
                              {topicLabels[m.topic] || m.topic}
                            </span>
                            {m.tier && <Badge tone="violet">{m.tier}</Badge>}
                            {m.isMock && <Badge tone="violet">MOCK</Badge>}
                            {m.dimensions?.display_review_status?.map((status) => (
                              <Badge key={status}>{reviewLabels[status] || status}</Badge>
                            ))}
                          </div>
                          <div className="mt-3 flex flex-wrap gap-2">
                            {m.categories?.map((category) => (
                              <Badge key={category}>{category}</Badge>
                            ))}
                          </div>
                          <p className="mt-3 text-xs text-muted-foreground">
                            历史 ID：{m.originalId} · {m.source || '历史目录'}
                          </p>
                        </div>
                      ))}
                    </div>
                  </section>
                </TabsContent>
                <TabsContent value="evidence">
                  <h3 className="section-title">
                    <Link2 className="size-4" />
                    可追溯的来源
                  </h3>
                  <p className="mt-2 text-xs leading-6 text-muted-foreground">
                    保留原始记录，不在前端推断分类、指标或发表日期。
                  </p>
                  {!paper.evidence?.length && (
                    <div className="mt-4">
                      <Empty
                        title="暂无结构化证据"
                        description="原始历史记录仍可在下方查看；缺失证据不会被模拟补全。"
                      />
                    </div>
                  )}
                  <div className="mt-5 space-y-4">
                    {paper.evidence?.map((evidence, index) => (
                      <section
                        key={`${evidence.source}-${index}`}
                        className="rounded-xl border p-4"
                      >
                        <div className="flex items-start justify-between gap-3">
                          <strong className="text-sm">{evidence.source || '原始来源'}</strong>
                          <ExternalLink href={evidence.url}>访问来源</ExternalLink>
                        </div>
                        <p className="mt-3 text-xs text-muted-foreground">
                          事件：{evidence.dateType || '未标记'} · {date(evidence.date)}
                        </p>
                        <details className="mt-4">
                          <summary className="cursor-pointer text-xs font-medium text-primary">
                            查看原始证据
                          </summary>
                          <pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-secondary p-3 text-[11px] leading-6">
                            {JSON.stringify(evidence.raw, null, 2)}
                          </pre>
                        </details>
                      </section>
                    ))}
                    {paper.memberships?.map((m) => (
                      <details key={m.originalId} className="rounded-xl border p-4">
                        <summary className="cursor-pointer text-sm font-medium">
                          {topicLabels[m.topic]} · 原始历史记录
                        </summary>
                        <pre className="mt-3 max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-secondary p-3 text-[11px] leading-6">
                          {JSON.stringify(m.raw, null, 2)}
                        </pre>
                      </details>
                    ))}
                  </div>
                </TabsContent>
              </Tabs>
              <div className="mt-8 flex flex-wrap items-center gap-4 border-t pt-6">
                {safeLink(paper.url) && (
                  <Button asChild>
                    <a href={safeLink(paper.url)} target="_blank" rel="noreferrer noopener">
                      打开论文原文
                      <ArrowUpRight />
                    </a>
                  </Button>
                )}
                {paper.doi && (
                  <ExternalLink href={`https://doi.org/${encodeURIComponent(paper.doi)}`}>
                    DOI
                  </ExternalLink>
                )}
                {paper.arxivId && (
                  <ExternalLink href={`https://arxiv.org/abs/${encodeURIComponent(paper.arxivId)}`}>
                    arXiv
                  </ExternalLink>
                )}
              </div>
            </div>
          )
        )}
      </DialogContent>
    </Dialog>
  )
}
