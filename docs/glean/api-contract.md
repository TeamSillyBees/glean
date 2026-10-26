# Glean API contract (v1)

All routes use initra authentication (Bearer access token) and the standard envelope `{ "code": "OK", "message": "success", "data": ..., "traceId": ... }` (actual optional envelope metadata follows initra). IDs are decimal strings. Dates are ISO-8601 UTC; missing formal dates are null, never acceptance dates. Production frontend never calls sources or LLMs directly. All catalog routes require a logged-in user; mutations only affect that user's presentation preferences.

## Catalog

- `GET /api/v1/catalog/topics` → `{items: Topic[]}`. Topic `{slug,name,description,rule,membershipCount}`. Slugs `biomed-ai`, `llm-agent`, `biomed-worldmodel`.
- `GET /api/v1/catalog/papers` → `{items: Paper[],total,page,pageSize}`. Query: `topic`, `q`, `year`, `venue`, `publicationType`, `category`, `tier`, `readStatus`, `hidden` (`exclude` default, `include`, `only`), `page` (1 default), `pageSize` (20 default,100 max), `sort` (`newest` default, `oldest`, `title`). Filters are AND, category matches membership categories, text is literal case-insensitive substring. Sorting always adds ID as stable tie breaker. Total counts unique global papers, not memberships.
- `GET /api/v1/catalog/papers/{id}` → `Paper`; includes all memberships and raw original records as source evidence.
- `GET /api/v1/catalog/facets` (same filters) → `{years: Facet[],venues: Facet[],publicationTypes: Facet[],categories: Facet[],tiers: Facet[],readStatuses: Facet[]}`; `Facet={value,count}`. Counts reflect currently filtered distinct global papers.
- `GET /api/v1/catalog/venues` → `{items: Venue[]}`; query `q`, `topic`. Venue `{id,name,kind,jif,ccf,metricYear,registryVersion,source,evidence,eligibleTopics,qualificationStatus}`. Registry entries are versioned evidence, not invented scores. Historical membership remains unchanged when registry versions change.
- `GET /api/v1/catalog/preferences` → `{filters:object}`.
- `PUT /api/v1/catalog/preferences`, body `{filters:object}` → `{filters:object}`.
- `PUT /api/v1/catalog/papers/{id}/state`, body `{hidden:boolean,readStatus:"unread"|"reading"|"read"}` → `{hidden,readStatus}`. Does not edit historical metadata.

`Paper={id,title,titleZh,abstract,authors:string[],year,venue,publicationType,doi,arxivId,url,formalOnlineDate:string|null,issueDate:string|null,dateProvenance,readStatus,hidden,isMock,memberships:Membership[],evidence:SourceEvidence[]}`.

`Membership={topic,originalId,tier,primaryCategory,categories:string[],readStatus,source,isMock,raw:object}`. `SourceEvidence={source,url,dateType,date,raw:object}`. Unknown classification/date evidence remains pending rather than fabricated. `isMock` is always persisted and visible; a mock review does not imply genuine paper reading.

## Baseline importer contract

`catalog.ImportRecord` is one JSONL object. Required `topic`, `originalId`, `title`. Other fields: `titleZh`, `abstract`, `authors:[]`, `year`, `venue`, `publicationType`, `doi`, `arxivId`, `url`, `formalOnlineDate` and `issueDate` (RFC3339 or null), `dateProvenance`, `tier`, `primaryCategory`, `categories:[]`, `dimensions:{key:[values]}`, `readStatus`, `source`, `isMock`, `raw:{}`, `evidence:[]` (SourceEvidence). `raw` preserves original scientific fields; documented private-provenance/credential redactions are permitted, and original source archives are retained outside Git with checksums. Only formal online/issue dates go in those typed date fields. Legacy ambiguous dates remain in raw evidence.

Go: `catalog.NewService(client *ent.Client)`; `Import(ctx context.Context, record ImportRecord) (ImportResult,error)` where `ImportResult={paperId,membershipId,created}`. Same `(topic, originalId)` import is a no-op (historical record never overwritten). DOI/arXiv exact normalized identifiers can unify global papers; no title-only merge. Conflicting identifier matches reject for human review. No HTTP import endpoint; local explicit CLI only. Streaming and compression handled by importer tool.

The three HTML baseline membership totals are 31399 / 4059 / 4037. These are expected provenance counts, not fabricated database counts.

## Dynamic historical filter dimensions

Query `dimension` is a JSON object of exact original dimension key to one exact string value (AND across keys), e.g. `dimension={"biomedical_domains":"Oncology"}` URL-encoded. `null`, arrays and non-string values are rejected. Convenience aliases: `task` matches `technical_tasks` or `tasks`; `role` matches `llm_role`, `ai_leadership` or `roles`; `direction` matches `relation_direction`; `method` matches `ai_methods`. `facets.dimensions` preserves every supplied dimension key; convenience arrays `tasks`, `roles`, `directions`, `methods` use the same unions. `Membership.dimensions` and import `dimensions` are `map[string][]string`. Paper-level `readStatus` is personal unread/reading/read; source scientific-review evidence remains independently in membership/read-status and original dimensions. List responses omit heavy raw/evidence content (empty objects/arrays); detail returns complete stored evidence.

## Authentication used by the query frontend

- `POST /api/v1/auth/login` JSON `{username,password}` is public; `data` contains `{accessToken,refreshToken,user:{userId,username,nickname,roleCodes,isSuperAdmin,isEnable}}`.
- `POST /api/v1/auth/refresh` JSON `{refreshToken}` is public and does not require an expired Bearer token. It atomically rotates both tokens; the old refresh token cannot be replayed.
- `POST /api/v1/auth/logout` requires `Authorization: Bearer <accessToken>` plus JSON `{refreshToken}` from the same current session. Both tokens are invalid after successful logout.
- Every catalog and monitor query uses the Bearer access token and the standard `code/message/data` envelope. The frontend keeps tokens in memory only and serializes concurrent refresh attempts.

`TestGleanFrontendAuthContract` exercises this exact HTTP flow with real JWT/token-store behavior and a SQLmock database. This is not a browser-with-live-PostgreSQL test.
