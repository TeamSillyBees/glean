export interface Progress {
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
}
