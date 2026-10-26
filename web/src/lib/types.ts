export type TopicSlug = 'biomed-ai' | 'llm-agent' | 'biomed-worldmodel'
export type ReadStatus = 'unread' | 'reading' | 'read'
export interface Topic {
  slug: TopicSlug
  name: string
  description: string
  rule: string
  curatedCount?: number
  historicalCount?: number
  candidateCount?: number
  membershipCount: number
}
export interface Facet {
  value: string
  count: number
}
export interface Facets {
  years: Facet[]
  venues: Facet[]
  publicationTypes: Facet[]
  categories: Facet[]
  tiers: Facet[]
  readStatuses: Facet[]
  tasks?: Facet[]
  roles?: Facet[]
  directions?: Facet[]
  methods?: Facet[]
  dimensions?: Record<string, Facet[]>
  sources?: Facet[]
  inclusionStatuses?: Facet[]
}
export interface SourceEvidence {
  source: string
  url: string
  dateType: string
  date: string | null
  raw: Record<string, unknown>
}
export interface Membership {
  topic: TopicSlug
  originalId: string
  tier: string
  primaryCategory: string
  categories: string[]
  dimensions?: Record<string, string[]>
  readStatus: string
  source: string
  isMock: boolean
  raw: Record<string, unknown>
  inclusionStatus?: string
  readingNotes?: Record<string, string>
}
// AbstractEvidence 仅包含可公开的摘要恢复依据，不包含本地缓存路径或原始响应。
export interface AbstractEvidence {
  source: string
  sourceId: string
  sourceUrl: string
  doi: string
  sourceVersion: string
  completeness: string
  verificationStatus: string
  abstractHash: string
  responseHash: string
  parserVersion: string
  sourceField: string
  enrichedAt: string
}
// AbstractEnrichment 将来源摘要作为独立补充，不覆盖历史目录的摘要字段。
export interface AbstractEnrichment extends AbstractEvidence {
  observationId: string
  abstract: string
}
export interface Paper {
  id: string
  title: string
  titleZh: string
  abstract: string
  abstractEnrichment?: AbstractEnrichment | null
  abstractZh?: string
  translationStatus?: string
  origin?: 'historical' | 'live'
  importedAt?: string
  latestSourceUpdatedAt?: string
  firstSeenAt?: string
  lastObservedAt?: string
  observationCount?: number
  review?: Review | null
  observations?: SourceObservation[]
  authors: string[]
  year: number
  venue: string
  publicationType: string
  doi: string
  arxivId: string
  url: string
  formalOnlineDate: string | null
  issueDate: string | null
  dateProvenance: string
  readStatus: ReadStatus
  hidden: boolean
  isMock: boolean
  memberships: Membership[]
  evidence: SourceEvidence[]
}
export interface PaperPage {
  items: Paper[]
  total: number
  page: number
  pageSize: number
}
export interface Venue {
  id: string
  name: string
  kind: string
  jif: number | null
  ccf: string
  metricYear: number
  registryVersion: string
  source: string
  evidence: unknown
  eligibleTopics: TopicSlug[]
  qualificationStatus: string
}
export interface Filters {
  topic: TopicSlug
  collectionScope: 'curated' | 'pending' | 'all'
  q: string
  year: string
  venue: string
  publicationType: string
  category: string
  direction: string
  aiMethod: string
  status: string
  inclusionStatus: string
  source: string
  method: string
  tier: string
  task: string
  role: string
  reviewStatus: string
  readStatus: string
  hidden: 'exclude' | 'include' | 'only'
  sort: 'newest' | 'oldest' | 'title' | 'observed' | 'source-updated'
  page: number
  pageSize: number
}
export interface Preferences {
  filters: Record<string, unknown>
}
export interface Identity {
  userId: string
  username: string
  nickname: string
}
export const defaultFilters: Filters = {
  topic: 'biomed-ai',
  collectionScope: 'curated',
  q: '',
  year: '',
  venue: '',
  publicationType: '',
  category: '',
  direction: '',
  aiMethod: '',
  status: '',
  inclusionStatus: '',
  source: '',
  method: '',
  tier: '',
  task: '',
  role: '',
  reviewStatus: '',
  readStatus: '',
  hidden: 'exclude',
  sort: 'newest',
  page: 1,
  pageSize: 20,
}

export interface Review {
  provider: string
  model: string
  promptVersion: string
  reviewedAt: string
  isMock: boolean
  scientificReviewed: boolean
  inclusionStatus: string
  summary: string
}
export interface PublicationEvent {
  kind: string
  date: string
  evidence: string
}
export interface SourceObservation {
  kind?: 'historical-abstract-enrichment'
  abstractEvidence?: AbstractEvidence | null
  eventType?: 'source_observed' | 'metadata_updated' | 'version_updated'
  sourceUrl?: string
  pmid?: string
  authors?: string[]
  runId?: string
  templateVersion?: string
  publicationType?: string
  id: string
  source: string
  sourceId: string
  version: string
  contentHash: string
  observedAt: string
  sourceUpdatedAt: string
  title: string
  abstract: string
  url: string
  doi: string
  arxivId: string
  venue: string
  issns: string[]
  publicationEvents: PublicationEvent[]
  review?: Review | null
  topics: string[]
  raw: Record<string, unknown>
}
