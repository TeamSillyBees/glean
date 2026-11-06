import { CalendarDays, ChevronRight } from 'lucide-react'
import type { Paper, TopicSlug } from '../lib/types'
import { cn, date, manualReviewLabels, readLabels, reviewLabels } from '../lib/utils'
import { PipelineBadge } from './paper-pipeline'
import { Badge } from './common'
import { Tooltip } from './ui/tooltip'
import { readingSnippet } from '../lib/presentation'
import { createPaperDetail } from './create-paper-detail'
import type { ComponentProps } from 'react'

const DeferredPaperDetail = createPaperDetail(() => import('./paper-detail-body'))

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
    <article data-testid="paper-card" data-paper-id={paper.id} className="paper-card group">
      <div className="paper-card-meta">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <span className="truncate" title={paper.venue || undefined}>
            {paper.venue || '期刊 / 会议待补充'}
          </span>
          {paper.year > 0 && <span className="shrink-0 tabular-nums">{paper.year}</span>}
        </div>
        <span className="flex shrink-0 items-center gap-2">
          {membership?.manualReview && (
            <Badge>{manualReviewLabels[membership.manualReview.status]}</Badge>
          )}
          {!membership?.manualReview && membership?.inclusionStatus === 'pending-review' && (
            <Badge tone="amber">新发现 · 待审</Badge>
          )}
          <span
            className={cn(
              'flex items-center gap-1.5 text-xs',
              paper.readStatus === 'read' ? 'text-emerald-700' : 'text-muted-foreground',
            )}
          >
            <span
              className={cn(
                'size-1.5 rounded-full',
                paper.readStatus === 'read'
                  ? 'bg-emerald-500'
                  : paper.readStatus === 'reading'
                    ? 'bg-primary'
                    : 'bg-slate-300',
              )}
            />
            {readLabels[paper.readStatus] || '未读'}
          </span>
        </span>
      </div>
      <button
        type="button"
        onClick={onOpen}
        className="block min-h-11 w-full rounded-sm text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <h3 className="paper-card-title text-foreground transition-colors group-hover:text-primary">
          {paper.titleZh || paper.title}
        </h3>
        {paper.titleZh && paper.title !== paper.titleZh && (
          <p className="mt-1.5 line-clamp-2 text-[13px] leading-6 text-muted-foreground">
            {paper.title}
          </p>
        )}
      </button>
      {snippet.text && (
        <p className="mt-2.5 line-clamp-2 text-[13px] leading-6 text-muted-foreground">
          <span className="mr-2 text-xs font-medium text-foreground">{snippet.label}</span>
          {snippet.text}
        </p>
      )}
      <div className="paper-card-footer">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {membership?.tier && (
            <Badge tone="violet">
              {membership.manualReview ? `原目录：${membership.tier}` : membership.tier}
            </Badge>
          )}
          {paper.review?.pipeline && <PipelineBadge pipeline={paper.review.pipeline} />}
          {tags.map((tag) => (
            <Badge key={tag}>{tag}</Badge>
          ))}
          {reviewStatus && (
            <span>
              {membership?.manualReview && '原核验：'}
              {reviewLabels[reviewStatus] || reviewStatus}
            </span>
          )}
          {paper.publicationType &&
            !['其他', '待核', '其他/待核', '其他／待核', 'unknown', '未分类'].includes(
              paper.publicationType.trim(),
            ) && <span>{paper.publicationType}</span>}
        </div>
        <span className="paper-card-actions">
          <Tooltip label="正式在线发表日期，不以接收或索引日期代替">
            <span className="inline-flex items-center gap-1.5">
              <CalendarDays className="size-3.5" />
              {paper.formalOnlineDate ? date(paper.formalOnlineDate) : '正式日期待核验'}
            </span>
          </Tooltip>
          <button
            type="button"
            onClick={onOpen}
            aria-label={`查看 ${paper.titleZh || paper.title} 的详情`}
            className="flex min-h-11 items-center gap-1 rounded-sm px-2 py-1 text-primary hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            详情
            <ChevronRight className="size-3.5" />
          </button>
        </span>
      </div>
    </article>
  )
}

export function PaperDetail(props: ComponentProps<typeof DeferredPaperDetail>) {
  return <DeferredPaperDetail {...props} />
}
