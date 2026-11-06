import { ArrowUpRight, Radio } from 'lucide-react'
import type { TopicSlug } from '../lib/types'
import { hasDate, useSources } from '../lib/source-query'
export function FreshnessStrip({ topic, onOpen }: { topic: TopicSlug; onOpen: () => void }) {
  const query = useSources()
  const items = query.data?.items || []
  const enabled = items.filter((item) => item.enabled !== false)
  const completed = enabled.filter((item) => hasDate(item.topicAcquiredThrough?.[topic]))
  const failures = enabled.filter(
    (item) => item.blocked || item.consecutiveFailures > 0 || (item.quarantined || 0) > 0,
  ).length
  return (
    <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2 border-b py-3 text-xs text-muted-foreground">
      <span className="flex items-center gap-1.5">
        <Radio className="size-3.5 text-primary" />
        {query.isLoading
          ? '正在读取来源进度…'
          : query.isError
            ? '来源进度暂不可用'
            : completed.length
              ? `${completed.length} / ${enabled.length} 个启用来源已完成本主题采集窗口`
              : '本主题尚无完整采集窗口'}
      </span>
      {failures > 0 && <span className="text-amber-700">{failures} 个来源待处理</span>}
      <button
        type="button"
        onClick={onOpen}
        className="ml-auto inline-flex min-h-8 items-center gap-1 rounded px-1 text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        查看来源进度
        <ArrowUpRight className="size-3" />
      </button>
    </div>
  )
}
