import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, LoaderCircle } from 'lucide-react'
import { api } from '../lib/api'
import type { TopicSlug, Venue, VenueQuery } from '../lib/types'
import { number } from '../lib/utils'
import { Badge, Empty, ErrorState, Loading } from './common'
import { Button } from './ui/button'

const statusLabels: Record<string, string> = {
  verified: '已核验',
  pending: '待核验',
  'historical-verified': '历史已核验 · 当前待核',
  'historical-qualified': '历史达标 · 当前待核',
  'historical-curated-discovery': '历史精选发现目标 · 当前资格待核',
}
const kindLabels: Record<string, string> = { journal: '期刊', conference: '会议' }
function VenueStatus({ venue }: { venue: Venue }) {
  return (
    <Badge tone={venue.qualificationStatus === 'verified' ? 'green' : 'neutral'}>
      {statusLabels[venue.qualificationStatus] || '待核验'}
    </Badge>
  )
}

export function VenuesView({ topic, q }: { topic: TopicSlug; q: string }) {
  const [pageSize, setPageSize] = useState(20)
  const [kind, setKind] = useState<VenueQuery['kind']>('')
  const [sort, setSort] = useState<VenueQuery['sort']>('jif_desc')
  return (
    <VenueList
      key={JSON.stringify([topic, q, pageSize, kind, sort])}
      {...{ topic, q, pageSize, setPageSize, kind, setKind, sort, setSort }}
    />
  )
}
function VenueList({
  topic,
  q,
  pageSize,
  setPageSize,
  kind,
  setKind,
  sort,
  setSort,
}: {
  topic: TopicSlug
  q: string
  pageSize: number
  setPageSize: (value: number) => void
  kind: VenueQuery['kind']
  setKind: (value: VenueQuery['kind']) => void
  sort: VenueQuery['sort']
  setSort: (value: VenueQuery['sort']) => void
}) {
  const [page, setPage] = useState(1)
  const list = useRef<HTMLElement>(null)
  function changePage(next: number) {
    setPage(next)
    list.current?.scrollIntoView?.({ block: 'start' })
    list.current?.focus({ preventScroll: true })
  }
  const filters = { topic, q, page, pageSize, kind, sort }
  const query = useQuery({
    queryKey: ['venues', filters],
    queryFn: ({ signal }) => api.venues(filters, signal),
    placeholderData: keepPreviousData,
  })
  const total = query.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (query.isSuccess && !query.isFetching && page > pages) setPage(pages)
  const items = query.data?.items || []
  return (
    <section
      ref={list}
      tabIndex={-1}
      className="space-y-5 scroll-mt-5 focus:outline-none"
      aria-label="期刊会议列表"
      aria-busy={query.isFetching}
    >
      <div className="flex flex-wrap items-end gap-3">
        <label className="text-xs text-muted-foreground">
          类型
          <select
            className="form-input mt-1.5 min-w-32"
            aria-label="期刊会议类型"
            value={kind}
            onChange={(event) => {
              setKind(event.target.value as VenueQuery['kind'])
            }}
          >
            <option value="">全部类型</option>
            <option value="journal">期刊</option>
            <option value="conference">会议</option>
          </select>
        </label>
        <label className="text-xs text-muted-foreground">
          排序
          <select
            className="form-input mt-1.5"
            aria-label="期刊会议排序"
            value={sort}
            onChange={(event) => {
              setSort(event.target.value as VenueQuery['sort'])
            }}
          >
            <option value="jif_desc">影响因子：从高到低</option>
            <option value="jif_asc">影响因子：从低到高</option>
            <option value="name">名称：A–Z</option>
          </select>
        </label>
        <span
          role="status"
          className="flex items-center gap-2 pb-2 text-xs text-muted-foreground sm:ml-auto"
        >
          {query.isFetching ? (
            <>
              <LoaderCircle className="size-3.5 animate-spin" />
              正在加载…
            </>
          ) : (
            `${number(total)} 条指标记录`
          )}
        </span>
      </div>
      {query.isError ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : query.isPending ? (
        <Loading label="正在读取期刊与会议…" />
      ) : !items.length ? (
        <Empty title="没有找到期刊或会议" description="试试其他名称，或切换类型与研究主题。" />
      ) : (
        <>
          <div className="space-y-3 md:hidden">
            {items.map((venue) => (
              <article key={venue.id} className="rounded-xl border bg-card p-4">
                <div className="flex items-start justify-between gap-3">
                  <h2 className="text-sm font-semibold leading-6">{venue.name}</h2>
                  <Badge>{kindLabels[venue.kind] || '未标注类型'}</Badge>
                </div>
                <div className="my-3 flex flex-wrap gap-2">
                  {venue.jif != null && <Badge tone="violet">期刊影响因子 {venue.jif}</Badge>}
                  {venue.ccf && <Badge tone="violet">CCF {venue.ccf}</Badge>}
                  {venue.jif == null && !venue.ccf && <Badge>暂无指标</Badge>}
                  <Badge>
                    {venue.metricYear ? `${venue.metricYear} 年指标` : '指标年份待核验'}
                  </Badge>
                </div>
                <VenueStatus venue={venue} />
                <details className="mt-3 text-xs text-muted-foreground">
                  <summary className="cursor-pointer">指标来源</summary>
                  <p className="mt-2 break-words">
                    {venue.source || '来源待补充'} · {venue.registryVersion || '版本待补充'}
                  </p>
                </details>
              </article>
            ))}
          </div>
          <div className="hidden overflow-x-auto rounded-xl border bg-card md:block">
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-secondary/50 text-xs text-muted-foreground">
                <tr>
                  {['期刊 / 会议', '类型', '影响因子', 'CCF', '指标年份', '核验状态'].map(
                    (label) => (
                      <th key={label} className="px-4 py-3 font-medium">
                        {label}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody>
                {items.map((venue) => (
                  <tr key={venue.id} className="border-b last:border-0 hover:bg-secondary/30">
                    <td className="max-w-80 break-words px-4 py-4">
                      <span className="font-medium">{venue.name}</span>
                      <details className="mt-1 text-xs text-muted-foreground">
                        <summary className="cursor-pointer">指标来源</summary>
                        <p className="mt-1">
                          {venue.source || '来源待补充'} · {venue.registryVersion || '版本待补充'}
                        </p>
                      </details>
                    </td>
                    <td className="px-4 py-3 text-xs text-muted-foreground">
                      {kindLabels[venue.kind] || '未标注类型'}
                    </td>
                    <td className="px-4 py-3 font-semibold tabular-nums">{venue.jif ?? '—'}</td>
                    <td className="px-4 py-3">{venue.ccf || '—'}</td>
                    <td className="px-4 py-3 tabular-nums">{venue.metricYear || '待核验'}</td>
                    <td className="px-4 py-3">
                      <VenueStatus venue={venue} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <nav
        aria-label="期刊会议分页"
        className="flex flex-wrap items-center justify-between gap-3 border-t pt-4"
      >
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          每页
          <select
            className="rounded border bg-card px-2 py-1.5"
            aria-label="每页记录数"
            value={pageSize}
            onChange={(event) => {
              setPageSize(Number(event.target.value))
            }}
          >
            {[20, 50, 100].map((size) => (
              <option key={size} value={size}>
                {size} 条
              </option>
            ))}
          </select>
        </label>
        <div className="flex items-center gap-3">
          <span className="text-xs text-muted-foreground">
            第 {page} / {pages} 页
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={page === 1 || query.isFetching || query.isError}
            onClick={() => changePage(page - 1)}
          >
            <ChevronLeft />
            上一页
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= pages || query.isFetching || query.isError}
            onClick={() => changePage(page + 1)}
          >
            下一页
            <ChevronRight />
          </Button>
        </div>
      </nav>
      <p className="text-xs text-muted-foreground">
        影响因子按记录所列年份展示，未提供指标的记录排在最后。
      </p>
    </section>
  )
}
