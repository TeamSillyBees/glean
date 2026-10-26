import type { Facets, Paper, Topic } from '../lib/types'
export const topics: Topic[] = [
  {
    slug: 'biomed-ai',
    name: '生物医学 × AI',
    description: '追踪人工智能与生命科学的交汇',
    rule: 'JIF > 10 · CCF A 主会完整论文',
    membershipCount: 2,
  },
  {
    slug: 'llm-agent',
    name: '大模型与智能体',
    description: '方法、任务与智能体系统',
    rule: 'JIF ≥ 4 · CCF A / B / C',
    membershipCount: 1,
  },
  {
    slug: 'biomed-worldmodel',
    name: '生物医学世界模型',
    description: '跨尺度建模与模拟',
    rule: 'JIF ≥ 4 · CCF A / B / C',
    membershipCount: 1,
  },
]
export const paper: Paper = {
  id: '123',
  title: 'A deterministic example for interface testing',
  titleZh: '用于界面测试的模拟论文',
  abstract: '这是明确标记的测试夹具，不是实际研究发现。用于验证筛选、阅读状态和来源证据界面。',
  authors: ['Test Author'],
  year: 2026,
  venue: 'Mock Research Journal',
  publicationType: '研究论文',
  doi: '10.0000/mock-example',
  arxivId: '',
  url: 'https://example.com/paper',
  formalOnlineDate: '2026-10-01T00:00:00Z',
  issueDate: null,
  dateProvenance: '测试夹具中的显式 online 日期',
  readStatus: 'unread',
  hidden: false,
  isMock: true,
  memberships: [
    {
      topic: 'biomed-ai',
      originalId: 'test-1',
      tier: '核心',
      primaryCategory: '模拟方向',
      categories: ['测试分类'],
      dimensions: {
        paper_types: ['原创研究'],
        biomedical_domains: ['模拟方向'],
        display_scope: ['核心库'],
        technical_tasks: ['分类'],
        ai_leadership: ['核心方法'],
        display_review_status: ['尚未审读'],
      },
      readStatus: 'not_reviewed_current',
      source: 'mock-fixture',
      isMock: true,
      raw: { fixture: true },
    },
  ],
  evidence: [
    {
      source: 'Mock source',
      url: 'https://example.com/evidence',
      dateType: 'formal_online',
      date: '2026-10-01T00:00:00Z',
      raw: { kind: 'mock', notRealEvidence: true },
    },
  ],
}
export const facets: Facets = {
  years: [{ value: '2026', count: 1 }],
  venues: [{ value: 'Mock Research Journal', count: 1 }],
  publicationTypes: [{ value: '研究论文', count: 1 }],
  categories: [{ value: '测试分类', count: 1 }],
  tiers: [{ value: '核心', count: 1 }],
  readStatuses: [{ value: 'unread', count: 1 }],
  dimensions: Object.fromEntries(
    Object.entries(paper.memberships[0].dimensions!).map(([key, values]) => [
      key,
      values.map((value) => ({ value, count: 1 })),
    ]),
  ),
}
