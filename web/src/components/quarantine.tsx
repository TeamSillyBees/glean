import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { request } from '../lib/api'
import { Badge, Empty, ErrorState, ExternalLink, Loading } from './common'
import { Button } from './ui/button'
import { Select } from './ui/select'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from './ui/dialog'
import type { SourcesData } from './source-health'

export interface QuarantineItem {
  id: string
  source: string
  sourceId: string
  title: string
  doi: string
  arxivId: string
  pmid: string
  topics: string[]
  reason: string
  quarantinedAt: string
  acquiredAt: string
  sourceUpdatedAt: string
  runIds: string[]
}
export interface QuarantineDetail extends QuarantineItem {
  sourceUrl: string
  evidence: {
    format: string
    bytes: number
    sha256: string
    preview: string
    redacted: boolean
    truncated: boolean
  }
}
interface QuarantinePage {
  items: QuarantineItem[]
  total: number
  page: number
  pageSize: number
}
const reasonLabels: Record<string, string> = {
  identity_conflict: '文献身份信息冲突',
  invalid_source_record: '来源记录字段不完整或格式异常',
  invalid_record: '记录校验未通过',
  missing_identity: '缺少可核验的文献标识',
  unsupported_record_type: '暂不支持的来源记录类型',
  mock_source_record: '模拟来源记录不纳入真实目录',
}
function reason(value: string) {
  return reasonLabels[value] ? `${reasonLabels[value]}（${value}）` : value || '未记录'
}
function date(value: string) {
  if (!value || value.startsWith('0001-')) return '未记录'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime())
    ? '时间格式待核验'
    : `${parsed.toLocaleString('zh-CN', { timeZone: 'UTC' })} UTC`
}

export function QuarantineView({ initialSource = '' }: { initialSource?: string }) {
  const [source, setSource] = useState(initialSource)
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<string | null>(null)
  const sources = useQuery({
    queryKey: ['sources'],
    queryFn: ({ signal }) => request<SourcesData>('/api/v1/monitor/sources', { signal }),
  })
  const query = useQuery({
    queryKey: ['quarantine', source, page],
    queryFn: ({ signal }) => {
      const params = new URLSearchParams({ page: String(page), pageSize: '20' })
      if (source) params.set('source', source)
      return request<QuarantinePage>(`/api/v1/monitor/quarantine?${params}`, { signal })
    },
  })
  const sourceNames = [
    ...new Set([
      ...(sources.data?.items.map((item) => item.source) || []),
      ...(source ? [source] : []),
    ]),
  ].sort()
  return (
    <div className="space-y-5">
      <p className="text-sm leading-6 text-muted-foreground">
        这里只读展示被采集流程隔离的来源记录与证据，不会释放、重试或修改记录。隔离不等于论文撤稿或科学结论有误。
      </p>
      <div className="flex flex-wrap items-center gap-3 border-b pb-4">
        <div className="w-full sm:w-56">
          <Select
            label="隔离记录来源"
            value={source}
            options={sourceNames.map((value) => ({ value, label: value }))}
            onValueChange={(value) => {
              setSource(value)
              setPage(1)
              setSelected(null)
            }}
            placeholder="全部来源"
          />
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => query.refetch()}
        >
          刷新记录
        </Button>
        {query.data && (
          <p role="status" className="text-sm text-muted-foreground">
            共 {query.data.total.toLocaleString('zh-CN')} 条 · 最新隔离优先
          </p>
        )}
      </div>
      {sources.isError && (
        <p role="status" className="text-xs text-amber-800">
          来源筛选项暂时不可用；仍可查看当前记录。
        </p>
      )}
      {query.isPending ? (
        <Loading label="正在读取隔离记录…" />
      ) : !query.data ? (
        <ErrorState error={query.error} retry={() => query.refetch()} />
      ) : (
        <>
          {query.isError && (
            <p role="alert" className="text-sm text-amber-800">
              刷新失败，当前显示上次读取的记录。
            </p>
          )}
          {!query.data.items.length ? (
            <Empty title="没有匹配的隔离记录" description="当前来源或分页下没有隔离记录。">
              {page > 1 && <Button onClick={() => setPage(1)}>返回第一页</Button>}
            </Empty>
          ) : (
            <div className="space-y-3">
              {query.data.items.map((item) => (
                <article key={item.id} className="min-w-0 rounded-xl border bg-card p-4 sm:p-5">
                  <div className="flex flex-wrap gap-2">
                    <Badge>{item.source}</Badge>
                    <Badge tone="amber">隔离待核验</Badge>
                  </div>
                  <h3 className="mt-3 break-words text-base font-semibold leading-7">
                    {item.title || item.sourceId || '无标题来源记录'}
                  </h3>
                  <p className="mt-2 break-words text-sm text-muted-foreground">
                    隔离原因：{reason(item.reason)}
                  </p>
                  <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
                    <p className="text-xs text-muted-foreground">
                      隔离时间：{date(item.quarantinedAt)}
                    </p>
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={`查看隔离证据：${item.title || item.sourceId}`}
                      onClick={() => setSelected(item.id)}
                    >
                      查看证据
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          )}
          <nav
            aria-label="隔离记录分页"
            className="flex flex-wrap items-center justify-between gap-3"
          >
            <Button
              variant="outline"
              disabled={page <= 1}
              onClick={() => {
                setPage((p) => p - 1)
                setSelected(null)
              }}
            >
              上一页
            </Button>
            <span className="text-sm tabular-nums">
              第 {page} 页 / 共 {Math.max(1, Math.ceil(query.data.total / query.data.pageSize))} 页
            </span>
            <Button
              variant="outline"
              disabled={page * query.data.pageSize >= query.data.total}
              onClick={() => {
                setPage((p) => p + 1)
                setSelected(null)
              }}
            >
              下一页
            </Button>
          </nav>
        </>
      )}
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      >
        <DialogContent drawer showCloseButton={false}>
          <header className="sticky top-0 z-10 flex items-start gap-3 border-b bg-background p-5">
            <div className="min-w-0 flex-1">
              <DialogTitle className="text-lg font-semibold">隔离记录证据</DialogTitle>
              <DialogDescription className="mt-1 text-sm text-muted-foreground">
                只读来源元数据与安全预览，不代表目录中的论文状态。
              </DialogDescription>
            </div>
            <DialogClose asChild>
              <Button variant="ghost" size="icon" aria-label="关闭隔离证据">
                <X />
              </Button>
            </DialogClose>
          </header>
          {selected && <EvidenceDetail key={selected} id={selected} />}
        </DialogContent>
      </Dialog>
    </div>
  )
}
function EvidenceDetail({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ['quarantine-detail', id],
    queryFn: ({ signal }) =>
      request<QuarantineDetail>(`/api/v1/monitor/quarantine/${encodeURIComponent(id)}`, { signal }),
  })
  if (query.isPending) return <Loading label="正在读取隔离证据…" />
  if (!query.data) return <ErrorState error={query.error} retry={() => query.refetch()} />
  const item = query.data
  return (
    <div className="space-y-6 p-5 sm:p-7">
      {query.isError && <p role="alert">刷新失败，当前显示上次读取的证据。</p>}
      <div>
        <Badge>{item.source}</Badge>
        <h3 className="mt-3 break-words text-lg font-semibold">
          {item.title || item.sourceId || '无标题来源记录'}
        </h3>
        <p className="mt-2 break-words text-sm">隔离原因：{reason(item.reason)}</p>
      </div>
      <dl className="grid grid-cols-1 gap-x-6 gap-y-4 border-y py-5 text-sm sm:grid-cols-2">
        {Object.entries({
          '来源记录 ID': item.sourceId,
          DOI: item.doi,
          'arXiv ID': item.arxivId,
          PMID: item.pmid,
          主题: item.topics.join('、'),
          '关联扫描 ID': item.runIds.join('、'),
          隔离时间: date(item.quarantinedAt),
          采集时间: date(item.acquiredAt),
          来源更新时间: date(item.sourceUpdatedAt),
        }).map(([label, value]) => (
          <div key={label} className="min-w-0">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="mt-1 break-words">{value || '未记录'}</dd>
          </div>
        ))}
      </dl>
      <ExternalLink href={item.sourceUrl}>查看来源页面</ExternalLink>
      <section aria-label="安全证据预览" className="space-y-3">
        <h4 className="font-semibold">证据预览</h4>
        <p className="text-sm leading-6 text-muted-foreground">
          预览为安全字段投影，并非完整原始内容。字节数和 SHA-256 对应原始证据。
        </p>
        <div className="flex flex-wrap gap-2">
          <Badge>{item.evidence.format}</Badge>
          <Badge>{item.evidence.bytes.toLocaleString('zh-CN')} 字节</Badge>
          {item.evidence.redacted && <Badge tone="amber">已过滤敏感字段</Badge>}
          {item.evidence.truncated && <Badge tone="amber">预览已截断</Badge>}
        </div>
        <p className="break-all font-mono text-xs leading-6 text-muted-foreground">
          SHA-256：{item.evidence.sha256 || '未记录'}
        </p>
        {item.evidence.preview ? (
          <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-secondary p-4 font-mono text-sm leading-6">
            {item.evidence.preview}
          </pre>
        ) : (
          <p className="text-sm text-muted-foreground">没有可安全展示的预览。</p>
        )}
      </section>
    </div>
  )
}
