import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { PaperPipeline } from '../lib/types'
import { request } from '../lib/api'
import { topicLabels } from '../lib/utils'
import { Badge, ExternalLink } from './common'
import { Button } from './ui/button'

const statuses: Record<string, string> = {
  pending: '待处理',
  running: '处理中',
  processing: '处理中',
  completed: '已完成',
  failed: '处理失败',
  awaiting_configuration: '缺少模型配置',
  retry_wait: '等待重试',
  needs_human_review: '等待人工审核',
  ready_for_display: '模型处理完成 · 非人工纳入',
}
const stages: Record<string, string> = {
  material: '获取可用阅读材料',
  classification: 'Jev 分类与排序',
  summary: '生成式模型阅读总结',
  independent_review: '独立模型复审',
  program_validation: '程序证据校验',
  evidence_check: 'Jev 证据检查',
}
const categories: Record<string, string> = {
  relevant: '主题相关',
  out_of_scope: '范围外',
  uncertain: '分类待定',
}
const scopes: Record<string, string> = {
  abstract: '仅摘要',
  abstract_only: '仅摘要',
  fulltext: '全文',
  partial: '部分材料',
  metadata: '仅题录',
  metadata_only: '仅题录',
}
export function PipelineBadge({ pipeline }: { pipeline: PaperPipeline }) {
  return <Badge tone="amber">{statuses[pipeline.status] || '模型处理状态待确认'}</Badge>
}
export function PaperPipelinePanel({
  pipeline: projectedPipeline,
  paperId,
  canManage = false,
}: {
  pipeline: PaperPipeline
  paperId: string
  canManage?: boolean
}) {
  const client = useQueryClient()
  const pipelineKey = ['paper-pipeline', projectedPipeline.candidateId || paperId]
  const live = useQuery({
    queryKey: pipelineKey,
    queryFn: () =>
      request<PaperPipeline>(
        `/api/v1/monitor/pipeline/${encodeURIComponent(projectedPipeline.candidateId!)}`,
      ),
    enabled: !!projectedPipeline.candidateId,
    initialData: projectedPipeline,
    staleTime: 5000,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      if (status === 'processing' || status === 'retry_wait') return 5000
      if (status === 'awaiting_configuration') return 30000
      return false
    },
  })
  const pipeline = live.data

  const retry = useMutation({
    mutationFn: (stage: string) =>
      request<PaperPipeline>(
        `/api/v1/monitor/pipeline/${encodeURIComponent(pipeline.candidateId!)}/retry`,
        { method: 'POST', body: JSON.stringify({ stage }) },
      ),
    onSuccess: async (updated) => {
      client.setQueryData(pipelineKey, updated)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['paper', paperId] }),
        client.invalidateQueries({ queryKey: ['papers'] }),
        client.invalidateQueries({ queryKey: ['monitor'] }),
      ])
    },
  })
  return (
    <section
      aria-label="论文处理流程"
      className="mt-7 min-w-0 space-y-4 border-t pt-6 [overflow-wrap:anywhere]"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="section-title">论文处理流程</h3>
        <PipelineBadge pipeline={pipeline} />
        <Badge>阅读范围：{scopes[pipeline.material.scope] || '范围未确认'}</Badge>
      </div>
      <p className="text-xs leading-6 text-muted-foreground">
        分类、总结和证据检查均为模型辅助结果，不自动改变科学纳入结论，也不改变个人阅读或隐藏状态。仅摘要或部分材料不能视作阅读全文。
      </p>
      {pipeline.material.sourceUrl && (
        <ExternalLink href={pipeline.material.sourceUrl}>查看阅读材料来源</ExternalLink>
      )}
      {pipeline.material.reason && (
        <p className="text-xs text-muted-foreground">材料范围说明：{pipeline.material.reason}</p>
      )}
      {pipeline.material.truncated && (
        <p className="text-sm text-amber-800">材料已截断；总结与校验仅覆盖实际提交部分。</p>
      )}
      {!!pipeline.material.blocks?.length && (
        <details>
          <summary className="cursor-pointer text-sm text-primary">
            查看实际阅读材料与证据锚点
          </summary>
          <ul className="mt-3 space-y-3">
            {pipeline.material.blocks.map((block) => (
              <li key={block.id} className="rounded border p-3">
                <p className="text-xs text-muted-foreground">
                  {block.id} · {block.kind}
                </p>
                <p className="mt-2 whitespace-pre-wrap text-sm leading-6">{block.text}</p>
                <p className="mt-2 text-xs text-muted-foreground">SHA-256：{block.sha256}</p>
              </li>
            ))}
          </ul>
        </details>
      )}
      <ol className="space-y-3">
        {pipeline.stages.map((stage) => (
          <li key={stage.name} className="rounded-lg border p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h4 className="text-sm font-semibold">{stages[stage.name] || stage.name}</h4>
              <Badge>{statuses[stage.status] || '状态未确认'}</Badge>
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              {[stage.provider, stage.model, stage.promptVersion, stage.ruleVersion]
                .filter(Boolean)
                .join(' · ') || '尚未执行'}{' '}
              · 尝试 {stage.attempts} 次
            </p>
            {stage.error && <p className="mt-2 text-sm text-amber-800">失败原因：{stage.error}</p>}
            {stage.result && (
              <div className="mt-3 space-y-2 text-sm leading-6">
                {stage.name === 'classification' && !stage.result.topics?.length && (
                  <p>
                    分类：{stage.result.category || '未分类'} · 相关性：
                    {stage.result.relevant === true
                      ? '相关'
                      : stage.result.relevant === false
                        ? '不相关'
                        : '不确定'}{' '}
                    · 分数：{stage.result.score ?? '未提供'} · 置信度：
                    {stage.result.confidence ?? '不确定'}
                  </p>
                )}
                {!!stage.result.topics?.length && (
                  <ul className="space-y-2">
                    {stage.result.topics.map((topic) => (
                      <li key={topic.topic}>
                        {topicLabels[topic.topic] || topic.topic}：
                        {categories[topic.category] || topic.category || '未分类'} ·{' '}
                        {topic.category === 'uncertain'
                          ? '不确定'
                          : topic.relevant
                            ? '相关'
                            : topic.category === 'out_of_scope'
                              ? '不相关'
                              : '不确定'}{' '}
                        · 优先分：{topic.score} · 置信度：
                        {topic.confidence ?? '不确定'}
                      </li>
                    ))}
                  </ul>
                )}
                {!!stage.result.claimSupport?.length && (
                  <p>逐项声明支持度：{stage.result.claimSupport.join('、')}</p>
                )}
                {stage.result.summary && (
                  <p className="whitespace-pre-wrap">{stage.result.summary}</p>
                )}
                {stage.result.supported !== undefined && (
                  <p>
                    证据支持：{stage.result.supported ? '支持（仍需人工判断）' : '不支持 / 需复核'}
                  </p>
                )}
                {!!stage.result.issues?.length && (
                  <ul className="list-disc pl-5">
                    {stage.result.issues.map((issue, index) => (
                      <li key={index}>{issue}</li>
                    ))}
                  </ul>
                )}
                {!!stage.result.claims?.length && (
                  <details>
                    <summary className="cursor-pointer text-primary">查看声明与原文证据</summary>
                    <ul className="mt-2 space-y-3">
                      {stage.result.claims.map((claim, index) => (
                        <li key={index}>
                          <p>{claim.text}</p>
                          <blockquote className="mt-1 border-l-2 pl-3 text-muted-foreground">
                            {claim.quote || '缺少原文证据'}
                          </blockquote>
                          {claim.evidenceBlockId && (
                            <p className="text-xs text-muted-foreground">
                              材料锚点：{claim.evidenceBlockId}
                            </p>
                          )}
                          {!!claim.numbers?.length && <p>数值：{claim.numbers.join('、')}</p>}
                          {!!claim.citations?.length && <p>引用：{claim.citations.join('、')}</p>}
                        </li>
                      ))}
                    </ul>
                  </details>
                )}
              </div>
            )}
            {canManage &&
              pipeline.candidateId &&
              (['failed', 'awaiting_configuration', 'retry_wait'].includes(stage.status) ||
                (stage.name === 'material' &&
                  stage.status === 'completed' &&
                  pipeline.material.scope !== 'fulltext')) && (
                <Button
                  className="mt-3"
                  variant="outline"
                  disabled={retry.isPending}
                  onClick={() => retry.mutate(stage.name)}
                >
                  {retry.isPending
                    ? '提交中…'
                    : stage.name === 'material' && stage.status === 'completed'
                      ? '重新获取全文并重跑后续阶段'
                      : '重试此阶段'}
                </Button>
              )}
          </li>
        ))}
      </ol>
      {live.error && (
        <p role="alert" className="text-sm text-amber-800">
          暂时无法读取最新处理状态，当前展示已保存记录。
        </p>
      )}
      {retry.error && (
        <p role="alert" className="text-sm text-red-700">
          {retry.error.message}
        </p>
      )}
      {retry.isSuccess && (
        <p role="status" className="text-sm">
          重试请求已排队，等待后续处理；这不代表模型已完成。
        </p>
      )}
    </section>
  )
}
