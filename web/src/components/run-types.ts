export interface Progress {
  queries?: Record<string, QueryProgress>
  acquired: boolean
  retryableFailure?: boolean
  retryNotBefore?: string
  handled?: boolean
  quarantined?: number
  reviewed: boolean
  published: boolean
  failed: boolean
  attempts: number
  pages: number
  observed?: number
  projected?: number
  lastError?: string
  window: { start: string; end: string }
}
export interface Run {
  id: string
  retryOf?: string
  retriedBy?: string
  number: number
  startedAt: string
  fixedEnd: string
  finishedAt: string
  status: string
  mode: string
  templateVersion: string
  sources: Record<string, Progress>
  unavailableQueries?: QueryProgress[]
}
export interface QueryProgress {
  queryId: string
  queryVersion: string
  topic: string
  source: string
  mode: string
  query: string
  queries?: string[]
  warnings?: string[]
  window: { start: string; end: string }
  coverageWindow?: { start: string; end: string }
  advanceCheckpoint?: boolean
  targetEnd: string
  status:
    | 'unavailable'
    | 'not_started'
    | 'running'
    | 'retry_wait'
    | 'partial_budget'
    | 'completed'
    | 'completed_empty'
    | 'blocked'
    | 'failed'
  requests: number
  pages: number
  observed: number
  quarantined: number
  newCandidates: number
  completedPartitions?: number
  lastError?: string
  completedAt?: string
}
