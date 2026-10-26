import { CalendarDays, FileText, History, Link2, Quote, Sparkles } from 'lucide-react'
import type { Paper, SourceObservation } from '../lib/types'
import { date, number, reviewLabels, topicLabels } from '../lib/utils'
import { Badge, ExternalLink } from './common'

const noteLabels: Record<string, string> = {
  contribution: '主要贡献',
  method_summary: '方法笔记',
  evidence_note: '证据说明',
  uncertainties: '待核问题',
  summary: '历史总结',
  review_notes: '审读笔记',
}
const eventLabels: Record<string, string> = {
  formal_online: '正式在线发表',
  online: '正式在线发表',
  issue: '卷期发表',
  issued: '出版日期',
  published: '来源标记的发表日期',
  accepted: '接收日期',
  submitted: '提交日期',
  indexed: '索引日期',
  updated: '来源更新',
  preprint: '预印本发布',
  deposited: '元数据提交',
}
const dimensionLabels: Record<string, string> = {
  paper_types: '论文类型',
  display_type: '论文类型',
  biomedical_domains: '生物医学方向',
  relation_direction: '构建/应用方向',
  display_scale: '研究尺度',
  ai_methods: 'AI 方法',
  technical_tasks: '技术任务',
  display_tasks: '技术任务',
  llm_role: 'LLM / Agent 的作用',
  ai_leadership: 'AI 的作用',
  display_roles: '世界模型的作用',
  display_scope: '期刊/会议范围',
  scope: '期刊/会议范围',
  collection: '收录状态',
  display_collection: '收录状态',
  eligibility_status: '资格状态',
  publication_eligibility: '发表资格',
}
function Paragraph({ children }: { children: string }) {
  return (
    <p className="mt-3 whitespace-pre-line break-words text-[13px] leading-7 text-slate-600">
      {children}
    </p>
  )
}
function RawRecord({ value, label = '查看原始字段' }: { value: unknown; label?: string }) {
  return (
    <details className="mt-4 rounded-lg border border-dashed px-3 py-2.5">
      <summary className="cursor-pointer text-xs text-muted-foreground">{label}</summary>
      <pre className="mt-3 max-h-72 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-secondary/60 p-3 text-[11px] leading-6">
        {JSON.stringify(value, null, 2)}
      </pre>
    </details>
  )
}
export function PaperOverview({ paper }: { paper: Paper }) {
  const notes = (paper.memberships || []).filter((m) =>
    Object.values(m.readingNotes || {}).some(Boolean),
  )
  return (
    <div className="space-y-8">
      <section>
        <h3 className="section-title">
          <Quote className="size-4" />
          摘要
        </h3>
        {paper.abstractZh && (
          <div className="mt-4 rounded-xl bg-violet-50/50 p-4">
            <Badge tone="violet">历史来源中文摘要</Badge>
            <Paragraph>{paper.abstractZh}</Paragraph>
          </div>
        )}
        {paper.abstract ? (
          <div className="mt-4">
            <p className="text-[11px] font-medium text-muted-foreground">原文摘要</p>
            <Paragraph>{paper.abstract}</Paragraph>
          </div>
        ) : (
          <p className="mt-3 rounded-xl bg-secondary/50 p-4 text-xs leading-6 text-muted-foreground">
            当前未获取摘要。
            {notes.length
              ? '可先阅读下方保留的历史笔记，或打开论文原文。'
              : '可打开论文原文继续阅读；摘要不会由模拟内容补齐。'}
          </p>
        )}
        {!paper.abstractZh && paper.abstract && (
          <p className="mt-3 text-[11px] text-muted-foreground">
            当前未获取中文摘要；模型翻译未接入。
          </p>
        )}
      </section>
      {notes.length > 0 && (
        <section>
          <h3 className="section-title">
            <FileText className="size-4" />
            历史阅读笔记
          </h3>
          <p className="mt-2 text-xs leading-6 text-muted-foreground">
            原目录中已有的笔记，按主题保留；不是本次模型生成。
          </p>
          <div className="mt-4 space-y-4">
            {notes.map((m) => (
              <div key={`${m.topic}-${m.originalId}`} className="rounded-xl border p-4">
                <div className="mb-3 flex flex-wrap items-center gap-2">
                  <Badge tone="violet">{topicLabels[m.topic] || m.topic}</Badge>
                  {m.dimensions?.display_review_status?.map((status) => (
                    <Badge key={status}>{reviewLabels[status] || status}</Badge>
                  ))}
                </div>
                <dl className="space-y-4">
                  {Object.entries(m.readingNotes || {})
                    .filter(([, value]) => value.trim())
                    .map(([key, value]) => (
                      <div key={key}>
                        <dt className="text-xs font-semibold text-slate-700">
                          {noteLabels[key] || key}
                        </dt>
                        <dd>
                          <Paragraph>{value}</Paragraph>
                        </dd>
                      </div>
                    ))}
                </dl>
              </div>
            ))}
          </div>
        </section>
      )}
      <section className="rounded-xl border border-violet-100 bg-violet-50/30 p-4">
        <h3 className="section-title">
          <Sparkles className="size-4 text-primary" />
          {paper.review?.isMock ? '模型模拟输出' : paper.review ? '模型阅读记录' : '模型总结待接入'}
        </h3>
        <p className="mt-2 text-xs leading-6 text-muted-foreground">
          {paper.review?.isMock
            ? '仅用于验证阅读流程，不是科学审读，也不改变真实来源和历史分类。'
            : paper.review
              ? '模型结果与来源记录分开展示，是否完成科学审读以记录为准。'
              : '不影响真实论文检索。已有的原文摘要、历史笔记和来源证据仍可阅读。'}
        </p>
        {paper.review?.summary && (
          <details className="mt-3">
            <summary className="cursor-pointer text-xs font-medium text-primary">
              {paper.review.isMock ? '查看明确标记的模拟总结' : '查看模型总结'}
            </summary>
            <Paragraph>{paper.review.summary}</Paragraph>
            <p className="mt-3 text-[11px] text-muted-foreground">
              {paper.review.provider} · {paper.review.model} · {paper.review.promptVersion} ·{' '}
              {date(paper.review.reviewedAt)}
            </p>
          </details>
        )}
      </section>
      <section>
        <h3 className="section-title">
          <CalendarDays className="size-4" />
          正式发表时间
        </h3>
        <div className="mt-4 grid grid-cols-2 gap-3">
          {[
            ['正式在线发表', paper.formalOnlineDate],
            ['卷期发表', paper.issueDate],
          ].map(([label, value]) => (
            <div key={label} className="rounded-xl bg-secondary/60 p-4">
              <p className="text-xs text-muted-foreground">{label}</p>
              <p className="mt-2 text-sm font-semibold">{date(value)}</p>
            </div>
          ))}
        </div>
        <p className="mt-3 text-xs leading-6 text-muted-foreground">
          正式在线发表与卷期发表分别记录；缺失时保持待核验，不以接收、提交或索引日期代替。
        </p>
        {(paper.firstSeenAt ||
          paper.lastObservedAt ||
          paper.importedAt ||
          paper.latestSourceUpdatedAt) && (
          <dl className="mt-4 space-y-2 border-t pt-4 text-xs">
            {paper.origin === 'historical' && paper.importedAt && (
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">历史目录导入时间</dt>
                <dd>{date(paper.importedAt)}</dd>
              </div>
            )}
            {paper.latestSourceUpdatedAt && (
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">来源最近更新时间</dt>
                <dd>{date(paper.latestSourceUpdatedAt)}</dd>
              </div>
            )}
            {paper.firstSeenAt && (
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">目录首次发现</dt>
                <dd>{date(paper.firstSeenAt)}</dd>
              </div>
            )}
            {paper.lastObservedAt && (
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">最近来源记录</dt>
                <dd>{date(paper.lastObservedAt)}</dd>
              </div>
            )}
          </dl>
        )}
      </section>
      <section>
        <h3 className="section-title">
          <FileText className="size-4" />
          主题与收录记录
        </h3>
        <div className="mt-4 space-y-3">
          {paper.memberships?.map((m) => (
            <div key={`${m.topic}-${m.originalId}`} className="rounded-xl border p-4">
              <div className="flex flex-wrap items-center gap-2">
                <strong className="text-sm">{topicLabels[m.topic] || m.topic}</strong>
                {m.inclusionStatus === 'pending-review' && (
                  <Badge tone="amber">新发现 · 待审</Badge>
                )}
                {m.dimensions?.display_review_status?.map((status) => (
                  <Badge key={status}>{reviewLabels[status] || status}</Badge>
                ))}
              </div>
              <dl className="mt-4 space-y-3">
                {Object.entries(dimensionLabels)
                  .filter(([key]) => m.dimensions?.[key]?.length)
                  .map(([key, label]) => (
                    <div key={key} className="grid grid-cols-[100px_minmax(0,1fr)] gap-2 text-xs">
                      <dt className="text-muted-foreground">{label}</dt>
                      <dd className="break-words leading-5">{m.dimensions![key].join(' · ')}</dd>
                    </div>
                  ))}
              </dl>
              <p className="mt-4 border-t pt-3 text-[11px] text-muted-foreground">
                {m.source || '历史目录'} · 原始 ID {m.originalId}
              </p>
            </div>
          ))}
        </div>
      </section>
    </div>
  )
}
function Observation({ item }: { item: SourceObservation }) {
  return (
    <details
      data-testid="source-observation"
      data-observation-id={item.id}
      className="group rounded-xl border bg-white p-4"
    >
      <summary className="cursor-pointer list-none">
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="violet">{item.source}</Badge>
          {item.eventType && (
            <Badge>
              {{
                source_observed: '首次来源记录',
                metadata_updated: '元数据更新',
                version_updated: '版本更新',
              }[item.eventType] || item.eventType}
            </Badge>
          )}
          {item.version && <Badge>{item.version}</Badge>}
          <span className="ml-auto text-xs text-muted-foreground">{date(item.observedAt)}</span>
        </div>
        <p className="mt-3 break-words text-sm font-medium leading-6">
          {item.title || '来源版本记录'}
        </p>
        <p className="mt-2 text-[11px] text-primary">展开版本内容与日期依据</p>
      </summary>
      <div className="mt-4 border-t pt-4">
        <div className="flex flex-wrap gap-4">
          <ExternalLink href={item.url}>访问该来源版本</ExternalLink>
          {item.sourceUrl && item.sourceUrl !== item.url && (
            <ExternalLink href={item.sourceUrl}>官方来源记录</ExternalLink>
          )}
        </div>
        {item.authors?.length ? (
          <p className="mt-3 text-xs leading-6 text-muted-foreground">{item.authors.join(' · ')}</p>
        ) : null}
        {item.sourceUpdatedAt && (
          <p className="mt-3 text-xs text-muted-foreground">
            来源更新时间：{date(item.sourceUpdatedAt)}
          </p>
        )}
        {item.abstract && (
          <div className="mt-4">
            <h4 className="text-xs font-semibold">该版本原文摘要</h4>
            <Paragraph>{item.abstract}</Paragraph>
          </div>
        )}
        {item.publicationEvents?.length > 0 && (
          <dl className="mt-4 space-y-3">
            {item.publicationEvents.map((event, i) => (
              <div key={i} className="rounded-lg bg-secondary/50 p-3">
                <dt className="flex justify-between gap-3 text-xs font-medium">
                  <span>{eventLabels[event.kind] || event.kind}</span>
                  <span>{date(event.date)}</span>
                </dt>
                {event.evidence && (
                  <dd className="mt-2 break-words text-xs leading-6 text-muted-foreground">
                    {event.evidence}
                  </dd>
                )}
              </div>
            ))}
          </dl>
        )}
        {item.review?.isMock && (
          <p className="mt-4 text-xs leading-6 text-muted-foreground">
            此版本的模型阅读为模拟输出；来源内容是真实采集记录。
          </p>
        )}
        <RawRecord value={item.raw} label="查看该版本原始字段" />
      </div>
    </details>
  )
}
export function PaperEvidence({ paper }: { paper: Paper }) {
  return (
    <div className="space-y-8">
      <section>
        <h3 className="section-title">
          <History className="size-4" />
          来源版本记录
          {paper.observationCount != null && <Badge>{number(paper.observationCount)}</Badge>}
        </h3>
        <p className="mt-2 text-xs leading-6 text-muted-foreground">
          记录内容变化，不把重复抓取当作新版本；历史目录信息不会被新观察覆盖。
        </p>
        <div className="mt-4 space-y-3">
          {paper.observations?.length ? (
            paper.observations.map((item) => <Observation key={item.id} item={item} />)
          ) : (
            <p className="rounded-xl bg-secondary/50 p-4 text-xs leading-6 text-muted-foreground">
              暂无新增来源版本。下方仍保留导入目录中的证据记录。
            </p>
          )}
        </div>
      </section>
      <section>
        <h3 className="section-title">
          <Link2 className="size-4" />
          可追溯的来源
        </h3>
        <div className="mt-4 space-y-3">
          {paper.dateProvenance && (
            <RawRecord value={paper.dateProvenance} label="查看原始日期依据" />
          )}
          {paper.evidence?.length ? (
            paper.evidence.map((evidence, index) => (
              <section key={`${evidence.source}-${index}`} className="rounded-xl border p-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <strong className="text-sm">{evidence.source || '原始来源'}</strong>
                  <ExternalLink href={evidence.url}>访问来源</ExternalLink>
                </div>
                <dl className="mt-4 flex flex-wrap justify-between gap-3 rounded-lg bg-secondary/50 p-3 text-xs">
                  <dt className="text-muted-foreground">
                    {eventLabels[evidence.dateType] || evidence.dateType || '未标记日期类型'}
                  </dt>
                  <dd>{date(evidence.date)}</dd>
                </dl>
                <RawRecord value={evidence.raw} label="查看原始证据字段" />
              </section>
            ))
          ) : (
            <p className="rounded-xl bg-secondary/50 p-4 text-xs leading-6 text-muted-foreground">
              暂无结构化来源证据。缺失的日期和链接保持待核验，不以模型结果补齐。
            </p>
          )}
        </div>
      </section>
      <section>
        <h3 className="section-title">
          <FileText className="size-4" />
          原始历史记录
        </h3>
        <p className="mt-2 text-xs leading-6 text-muted-foreground">
          需要核对时展开完整字段；摘要与阅读笔记已在内容概览中呈现。
        </p>
        {paper.memberships?.map((m) => (
          <RawRecord
            key={`${m.topic}-${m.originalId}`}
            value={m.raw}
            label={`${topicLabels[m.topic] || m.topic} · ${m.originalId}`}
          />
        ))}
      </section>
    </div>
  )
}
