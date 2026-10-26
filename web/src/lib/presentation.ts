import type { Membership, Paper } from './types'
export function readingSnippet(paper: Paper, membership?: Membership) {
  if (paper.abstractZh) return { text: paper.abstractZh, label: '历史中文摘要' }
  const notes = membership?.readingNotes
  const text = notes?.contribution || notes?.method_summary || notes?.summary
  if (text) return { text, label: '历史阅读笔记' }
  return { text: paper.abstract, label: '原文摘要' }
}
