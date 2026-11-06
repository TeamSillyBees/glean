export type TopicSlug = string
export type ReadStatus = 'unread' | 'reading' | 'read'
export interface SearchProtocol {
  profile_id: string
  profile_version: string
  topic: string
  mode: string
  [key: string]: unknown
}
export interface ProtocolPreview {
  protocol: SearchProtocol
  searchRoutes: SearchRoute[]
  warnings: string[]
  externalSources: string[]
  semanticVersion: string
  acknowledgement: string
  executable: boolean
}
export interface CriterionEvidence {
  decision: 'pass' | 'fail' | 'unknown' | 'not_applicable'
  evidence: string
  source_url: string
  location: string
}
export interface ScreeningEvidence {
  profile_id: string
  profile_version: string
  reading_level: string
  requires_fulltext: boolean
  identity_basis: string
  research_object: string
  method_role: string
  topic_role: string
  criteria: Record<string, CriterionEvidence>
}
export interface SearchRoute {
  id: string
  name?: string
  sources: ('crossref' | 'pubmed' | 'europepmc' | 'arxiv' | 'biorxiv' | 'medrxiv')[]
  termGroups: string[][]
  fieldMode?: '' | 'default' | 'title_abstract'
  backfillFrom?: string
}
export interface Topic {
  searchProtocol?: SearchProtocol
  slug: TopicSlug
  name: string
  description: string
  rule: string
  curatedCount?: number
  historicalCount?: number
  candidateCount?: number
  searchEnabled?: boolean
  termGroups?: string[][]
  searchRoutes?: SearchRoute[]
  version?: number
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
  effectiveStatus?: 'included' | 'excluded' | 'pending'
  manualReview?: ManualReview
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
  correction?: Correction
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
export interface VenueQuery {
  topic: string
  q: string
  page: number
  pageSize: number
  kind: '' | 'journal' | 'conference'
  sort: 'jif_desc' | 'jif_asc' | 'name'
}
export interface VenuePage {
  items: Venue[]
  total: number
  page: number
  pageSize: number
}
export interface Filters {
  topic: TopicSlug
  collectionScope: 'curated' | 'pending' | 'excluded' | 'all'
  q: string
  year: string
  venue: string
  publicationType: string
  category: string
  manualLabel: string
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
  sort: 'newest' | 'oldest' | 'title' | 'observed' | 'source-updated' | 'model-priority'
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
  isSuperAdmin?: boolean
  roleCodes?: string[]
}
export const defaultFilters: Filters = {
  topic: 'biomed-ai',
  collectionScope: 'curated',
  q: '',
  year: '',
  venue: '',
  publicationType: '',
  category: '',
  manualLabel: '',
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

export interface PaperPipeline {
  candidateId?: string
  version: string
  status: string
  material: {
    scope: string
    text?: string
    sourceUrl: string
    sha256: string
    reason?: string
    truncated?: boolean
    blocks?: { id: string; kind: string; text: string; sourceUrl: string; sha256: string }[]
  }
  scientificReviewed: boolean
  stages: PipelineStage[]
}
export interface PipelineStage {
  name: string
  status: string
  attempts: number
  inputHash: string
  provider: string
  model: string
  promptVersion: string
  ruleVersion: string
  nextRetryAt?: string
  error?: string
  result?: {
    topics?: {
      topic: string
      relevant: boolean
      category: string
      score: number
      confidence?: number | null
    }[]
    claimSupport?: number[]
    relevant?: boolean
    category?: string
    score?: number
    confidence?: number
    summary?: string
    supported?: boolean
    issues?: string[]
    claims?: {
      text: string
      quote: string
      evidenceBlockId?: string
      numbers?: string[]
      citations?: string[]
    }[]
  }
}
export interface Review {
  pipeline?: PaperPipeline | null
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
export interface DiscoveryHit {
  runId: string
  queryId: string
  queryVersion: string
  planVersion: string
  source: string
  query: string
  mode: string
  discoveredAt: string
}
export interface SourceObservation {
  discoveries?: DiscoveryHit[]
  discoveriesHasMore?: boolean
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

export interface ManualReview {
  evidence?: ScreeningEvidence
  status: 'pending' | 'included' | 'excluded'
  note: string
  labels: string[]
  version: number
  updatedAt: string
  updatedBy: string | null
}
export type CorrectionFields = Partial<
  Pick<Paper, 'title' | 'titleZh' | 'authors' | 'year' | 'venue' | 'abstract'>
>
export interface Correction {
  fields: CorrectionFields
  reason: string
  version: number
  updatedAt: string
  updatedBy: string | null
}
export interface CatalogHistory {
  id: string
  kind: string
  topic: string
  before: unknown
  after: unknown
  actorId: string | null
  createdAt: string
}
export interface TopicInput {
  searchProtocol?: SearchProtocol
  protocolAcknowledgement?: string
  slug?: string
  name: string
  description: string
  rule: string
  searchEnabled: boolean
  termGroups: string[][]
  searchRoutes?: SearchRoute[]
  expectedVersion: number
}
