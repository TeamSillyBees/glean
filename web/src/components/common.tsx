import type { ReactNode } from 'react'
import { AlertCircle, ArrowUpRight, FileSearch, LoaderCircle, RotateCcw } from 'lucide-react'
import { Button } from './ui/button'
import { cn, safeLink } from '../lib/utils'
export function Badge({
  children,
  tone = 'neutral',
  className,
}: {
  children: ReactNode
  tone?: 'neutral' | 'violet' | 'green' | 'amber' | 'red'
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] font-medium leading-none',
        {
          neutral: 'bg-slate-100 text-slate-600',
          violet: 'bg-violet-50 text-violet-700',
          green: 'bg-emerald-50 text-emerald-700',
          amber: 'bg-amber-50 text-amber-800',
          red: 'bg-red-50 text-red-700',
        }[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}
export function Loading({ label = '正在读取目录…' }: { label?: string }) {
  return (
    <div
      role="status"
      className="flex min-h-48 flex-col items-center justify-center gap-3 text-sm text-muted-foreground"
    >
      <LoaderCircle className="size-5 animate-spin text-primary" />
      {label}
    </div>
  )
}
export function ErrorState({ error, retry }: { error: unknown; retry: () => void }) {
  return (
    <div
      role="alert"
      className="flex min-h-48 flex-col items-center justify-center gap-3 rounded-xl border border-dashed bg-white p-8 text-center"
    >
      <AlertCircle className="size-6 text-amber-600" />
      <h3 className="font-semibold">暂时没能读取数据</h3>
      <p className="max-w-md text-sm text-muted-foreground">
        {error instanceof Error ? error.message : '请稍后重试。'}
      </p>
      <Button variant="outline" size="sm" onClick={retry}>
        <RotateCcw />
        重新连接
      </Button>
    </div>
  )
}
export function Empty({
  title = '暂时没有匹配的论文',
  description = '试试减少筛选条件，或换一个关键词。',
  children,
}: {
  title?: string
  description?: string
  children?: ReactNode
}) {
  return (
    <div className="flex min-h-72 flex-col items-center justify-center rounded-2xl border border-dashed bg-white px-6 text-center">
      <span className="mb-5 flex size-14 items-center justify-center rounded-2xl bg-violet-50">
        <FileSearch className="size-6 text-primary" />
      </span>
      <h3 className="text-lg font-semibold">{title}</h3>
      <p className="mb-5 mt-2 max-w-md text-sm leading-6 text-muted-foreground">{description}</p>
      {children}
    </div>
  )
}
export function ExternalLink({
  href,
  children,
  className,
}: {
  href?: string
  children: ReactNode
  className?: string
}) {
  const url = safeLink(href)
  return url ? (
    <a
      href={url}
      target="_blank"
      rel="noreferrer noopener"
      className={cn(
        'inline-flex items-center gap-1.5 text-sm text-primary hover:underline',
        className,
      )}
    >
      {children}
      <ArrowUpRight className="size-3.5" />
    </a>
  ) : (
    <span className="text-sm text-muted-foreground">来源链接待补充</span>
  )
}
