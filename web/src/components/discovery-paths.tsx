import type { DiscoveryHit } from '../lib/types'
export function DiscoveryPaths({ items, hasMore }: { items?: DiscoveryHit[]; hasMore?: boolean }) {
  if (!items?.length && !hasMore) return null
  return (
    <section className="mt-4 rounded-lg border p-3" aria-label="检索发现路径">
      <h4 className="text-sm font-semibold">检索发现路径</h4>
      <p className="mt-2 text-xs leading-6 text-muted-foreground">
        记录哪次查询发现了此来源版本；多条路径不代表多篇论文，也不等于科学审核通过。
      </p>
      {hasMore && (
        <p className="mt-2 text-xs text-amber-800">
          仅展示此来源版本最近 100 条发现路径，还有更早记录未展示。
        </p>
      )}
      <ol className="mt-2 divide-y">
        {items?.map((item, index) => (
          <li key={`${item.runId}-${item.queryId}-${index}`} className="space-y-1 py-2 text-xs">
            <p className="break-words font-medium">
              路线 {item.queryId} · 来源 {item.source}
            </p>
            <p className="break-words text-muted-foreground">
              运行 {item.runId} · 模式 {item.mode} · 发现时间 {item.discoveredAt}
            </p>
            <p className="break-all text-muted-foreground">
              查询版本 {item.queryVersion} · 计划版本 {item.planVersion}
            </p>
            {item.query && (
              <pre className="overflow-auto whitespace-pre-wrap break-words rounded bg-secondary p-2">
                {item.query}
              </pre>
            )}
          </li>
        ))}
      </ol>
    </section>
  )
}
