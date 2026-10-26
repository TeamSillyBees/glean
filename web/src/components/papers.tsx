import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowUpRight, BookOpen, CalendarDays, Check, ChevronRight, EyeOff, X } from 'lucide-react'
import type { Paper, ReadStatus, TopicSlug } from '../lib/types'
import { api } from '../lib/api'
import { cn, date, readLabels, reviewLabels, safeLink } from '../lib/utils'
import { Badge, ErrorState, ExternalLink, Loading } from './common'
import { Button } from './ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import { Tooltip } from './ui/tooltip'
import { PaperOverview, PaperEvidence } from './paper-content'
import { readingSnippet } from '../lib/presentation'
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
  const tags = (
    membership?.dimensions?.display_tasks ||
    membership?.dimensions?.technical_tasks ||
    membership?.categories ||
    []
  )
    .filter(
      (tag) =>
        !['其他', '待核', '其他/待核', '其他／待核', 'unknown', '未分类'].includes(tag.trim()),
    )
    .slice(0, 3)
  const snippet = readingSnippet(paper, membership)
  const reviewStatus = membership?.dimensions?.display_review_status?.[0]
  return (
    <article
      data-testid="paper-card"
      data-paper-id={paper.id}
      className="paper-card group relative min-w-0 [overflow-wrap:anywhere] rounded-xl border bg-white px-6 py-5 transition-all hover:border-violet-200 hover:shadow-[0_5px_24px_-15px_rgba(79,70,229,.45)]"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
        <span className="font-semibold text-slate-700">{paper.venue || '来源待核验'}</span>
        {paper.year > 0 && <span className="text-muted-foreground">· {paper.year}</span>}
        {membership?.tier && <Badge tone="violet">{membership.tier}</Badge>}
        <span className="ml-auto flex items-center gap-2">
          {membership?.inclusionStatus === 'pending-review' && (
            <Badge tone="amber">新发现 · 待审</Badge>
          )}
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
      {snippet.text && (
        <p className="mt-3 line-clamp-2 text-[13px] leading-6 text-slate-500">
          <span className="mr-2 text-[10px] font-medium text-slate-400">{snippet.label}</span>
          {snippet.text}
        </p>
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
        {paper.publicationType &&
          !['其他', '待核', '其他/待核', '其他／待核', 'unknown', '未分类'].includes(
            paper.publicationType.trim(),
          ) && <span className="text-[11px] text-muted-foreground">{paper.publicationType}</span>}
        <span className="ml-auto flex items-center gap-3 text-[11px] text-muted-foreground">
          <Tooltip label="只展示正式在线发表 日期；不以接收或索引日期代替">
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
    staleTime: 0,
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
        variables.hidden === true
          ? '已在所有主题中隐藏，可在「已隐藏」中恢复。'
          : '阅读状态已保存。',
      )
    },
  })
  const { reset: resetMutation } = mutation
  useEffect(() => resetMutation(), [id, resetMutation])
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
        showCloseButton={false}
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          restoreFocus?.()
        }}
      >
        <div
          data-testid="paper-detail-header"
          className="sticky top-0 z-10 flex items-start gap-3 border-b bg-white/95 px-5 py-5 backdrop-blur sm:px-7"
        >
          <div className="min-w-0 flex-1">
            <div className="mb-1 flex items-center gap-2 text-xs font-medium text-primary">
              <BookOpen className="size-4" />
              阅读空间
            </div>
            <DialogTitle className="text-lg font-semibold">论文详情</DialogTitle>
            <DialogDescription className="mt-1 text-xs text-muted-foreground">
              原始证据与个人阅读状态分开保存。
            </DialogDescription>
          </div>
          {/* 关闭入口与标题共用 sticky 层，滚动后仍可点击。 */}
          <DialogClose asChild>
            <Button
              variant="ghost"
              size="icon"
              className="size-11 rounded-full text-muted-foreground"
              aria-label="关闭"
            >
              <X />
            </Button>
          </DialogClose>
        </div>
        {query.isLoading ? (
          <Loading />
        ) : query.isError ? (
          <div className="p-7">
            <ErrorState error={query.error} retry={() => query.refetch()} />
          </div>
        ) : (
          paper && (
            <div className="px-5 py-6 sm:px-7">
              <div className="mb-4 flex flex-wrap gap-2">
                <Badge>{paper.venue || '来源待核验'}</Badge>
                {paper.year > 0 && <Badge>{paper.year}</Badge>}
                {paper.review?.isMock && <Badge tone="violet">模型总结为模拟输出</Badge>}
                {paper.hidden && <Badge tone="amber">已隐藏</Badge>}
              </div>
              <h2 className="break-words text-xl font-semibold leading-relaxed tracking-tight sm:text-2xl">
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
                <Tooltip
                  label={paper.hidden ? '在所有主题中恢复显示' : '在所有主题中隐藏，可随时恢复'}
                >
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
              <p className="mt-3 text-[11px] text-muted-foreground">
                隐藏状态适用于所有主题；可在「已隐藏」中恢复。个人阅读状态不会改变原始审读记录。
              </p>
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
                  <PaperOverview paper={paper} />
                </TabsContent>
                <TabsContent value="evidence">
                  <PaperEvidence paper={paper} />
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
