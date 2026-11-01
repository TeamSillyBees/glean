import { useQuery } from '@tanstack/react-query'
import { request } from './api'
export interface Checkpoint {
  source: string
  lastAcquiredAt?: string
  quarantined?: number
  handledThrough?: string
  acquiredThrough: string
  reviewedThrough: string
  publishedThrough: string
  nextRetryAt: string
  lastError?: string
  consecutiveFailures: number
  enabled?: boolean
  acquisitionMode?: string
  topicAcquiredThrough?: Record<string, string> | null
  lagSeconds?: number
  blocked?: boolean
  lastStatusCode?: number
  coreBackfillPending?: boolean
}
interface Schedule {
  anchorUtc: string
  nextRunUtc: string
  everyHours: number
  completedRuns: number
  realFetchEnabled: boolean
  schedulerEnabled?: boolean
  acquisitionMode?: string
  reviewMode?: string
}
export interface SourcesData {
  counts?: { quarantinedCandidates?: number }
  items: Checkpoint[]
  schedule: Schedule
}
export const hasDate = (value?: string) =>
  !!value && !value.startsWith('0001-') && Number.isFinite(Date.parse(value))
export function useSources() {
  return useQuery({
    queryKey: ['sources'],
    queryFn: ({ signal }) => request<SourcesData>('/api/v1/monitor/sources', { signal }),
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
}
