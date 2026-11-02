import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi, afterEach } from 'vitest'
import { PaperPipelinePanel } from './paper-pipeline'
import type { PaperPipeline } from '../lib/types'
import * as api from '../lib/api'

const pipeline: PaperPipeline = {
  candidateId: '123',
  version: 'paper-pipeline-v1',
  status: 'awaiting_configuration',
  scientificReviewed: false,
  material: { scope: 'abstract', sourceUrl: 'https://example.org/paper', sha256: 'abc' },
  stages: [
    {
      name: 'classification',
      status: 'completed',
      attempts: 1,
      inputHash: 'a',
      provider: 'jev',
      model: 'jev-test',
      promptVersion: 'v1',
      ruleVersion: '',
      result: { category: 'biomed-ai', score: 0.8 },
    },
    {
      name: 'summary',
      status: 'completed',
      attempts: 1,
      inputHash: 'a',
      provider: 'llm',
      model: 'reader',
      promptVersion: 'v1',
      ruleVersion: '',
      result: {
        summary: '仅依据真实摘要的总结',
        claims: [{ text: '研究声明', quote: '原文证据' }],
      },
    },
    {
      name: 'independent_review',
      status: 'awaiting_configuration',
      attempts: 0,
      inputHash: 'a',
      provider: '',
      model: '',
      promptVersion: '',
      ruleVersion: '',
      error: 'model_not_configured',
    },
  ],
}
function show(canManage = false, value = pipeline) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <PaperPipelinePanel pipeline={value} paperId="42" canManage={canManage} />
    </QueryClientProvider>,
  )
}
afterEach(() => vi.restoreAllMocks())
describe('论文处理流程', () => {
  it('显示实际阅读范围、不确定分类、真实总结与缺配置状态，不伪称人工纳入', () => {
    show()
    expect(screen.getByText('阅读范围：仅摘要')).toBeInTheDocument()
    expect(screen.getByText(/置信度：不确定/)).toBeInTheDocument()
    expect(screen.getByText('仅依据真实摘要的总结')).toBeInTheDocument()
    expect(screen.getByText(/不自动改变科学纳入结论/)).toBeInTheDocument()
    expect(screen.getAllByText('缺少模型配置')).toHaveLength(2)
    expect(screen.queryByRole('button', { name: '重试此阶段' })).not.toBeInTheDocument()
  })
  it('仅管理员可重试受阻阶段，按钮实际提交并防重复点击', async () => {
    let resolve!: (value: PaperPipeline) => void
    const send = vi.spyOn(api, 'request').mockReturnValue(
      new Promise<PaperPipeline>((done) => {
        resolve = done
      }),
    )
    show(true)
    const button = screen.getByRole('button', { name: '重试此阶段' })
    fireEvent.click(button)
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith('/api/v1/monitor/pipeline/123/retry', {
        method: 'POST',
        body: JSON.stringify({ stage: 'independent_review' }),
      }),
    )
    expect(screen.getByRole('button', { name: '提交中…' })).toBeDisabled()
    resolve(pipeline)
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('重试请求已排队'))
  })
  it('显示逐主题分类、实际材料锚点与截断边界', () => {
    show(false, {
      ...pipeline,
      material: {
        ...pipeline.material,
        scope: 'partial',
        truncated: true,
        blocks: [
          {
            id: 'abstract:1',
            kind: 'abstract',
            text: '真实原文片段',
            sourceUrl: 'https://example.org/paper',
            sha256: 'block-hash',
          },
        ],
      },
      stages: [
        {
          ...pipeline.stages[0],
          result: {
            topics: [
              {
                topic: 'biomed-ai',
                relevant: true,
                category: 'clinical',
                score: 0.7,
                confidence: null,
              },
              {
                topic: 'llm-agent',
                relevant: false,
                category: 'uncertain',
                score: 0.1,
                confidence: 0.9,
              },
            ],
          },
        },
      ],
    })
    expect(screen.getByText('阅读范围：部分材料')).toBeInTheDocument()
    expect(screen.getByText(/材料已截断/)).toBeInTheDocument()
    expect(screen.getByText('真实原文片段')).toBeInTheDocument()
    expect(screen.getByText(/SHA-256：block-hash/)).toBeInTheDocument()
    expect(screen.getByText(/分类待定 · 不确定/)).toBeInTheDocument()
    expect(screen.getByText(/clinical · 相关 · 优先分：0.7 · 置信度：不确定/)).toBeInTheDocument()
  })
  it('材料补全操作明确提示重跑下游，而已完成模型阶段不可重复付费重跑', () => {
    show(true, {
      ...pipeline,
      stages: [{ ...pipeline.stages[0], name: 'material' }, pipeline.stages[1]],
    })
    expect(screen.getByRole('button', { name: '重新获取全文并重跑后续阶段' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试此阶段' })).not.toBeInTheDocument()
  })
})
