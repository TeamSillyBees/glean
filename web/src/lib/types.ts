export type TopicSlug = 'biomed-ai' | 'llm-agent' | 'biomed-worldmodel'
export type ReadStatus = 'unread' | 'reading' | 'read'
export interface Topic {
  slug: TopicSlug
  name: string
  description: string
  rule: string
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
}
export interface Paper {
  id: string
  title: string
  titleZh: string
  abstract: string
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
  q: string
  year: string
  venue: string
  publicationType: string
  category: string
  direction: string
  method: string
  tier: string
  task: string
  role: string
  reviewStatus: string
  readStatus: string
  hidden: 'exclude' | 'include' | 'only'
  sort: 'newest' | 'oldest' | 'title'
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
  q: '',
  year: '',
  venue: '',
  publicationType: '',
  category: '',
  direction: '',
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
