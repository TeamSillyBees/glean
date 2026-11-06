# Glean API contract (v1)

All routes use initra authentication (Bearer access token) and the standard envelope `{ "code": "OK", "message": "success", "data": ..., "traceId": ... }` (actual optional envelope metadata follows initra). IDs are decimal strings. Dates are ISO-8601 UTC; missing formal dates are null, never acceptance dates. Production frontend never calls sources or LLMs directly. All catalog routes require a logged-in user. Personal state/preferences affect only the current user; the administrator-only shared management endpoints below are separate.

## Catalog

- `GET /api/v1/catalog/topics` → `{items: Topic[]}`. Topic `{slug,name,description,rule,searchEnabled,termGroups:string[][],version,membershipCount,historicalCount,candidateCount,curatedCount}`. Includes built-ins `biomed-ai`, `llm-agent`, `biomed-worldmodel` and saved custom topics.
- `GET /api/v1/catalog/papers` → `{items: Paper[],total,page,pageSize}`. Query: `topic`, `collectionScope` (`all` API default, `curated`, `pending`, `excluded`), `q`, `year`, `venue`, `publicationType`, `category`, `tier`, `readStatus`, `hidden` (`exclude` default, `include`, `only`), `page` (1 default), `pageSize` (20 default,100 max), `inclusionStatus` (`historical`, `pending-review`, `pending`, `included`, `excluded`), `source`, `sort` (`newest` default, `oldest`, `title`, `observed`, `source-updated`, `model-priority`). Filters are AND, category matches membership categories, text is literal case-insensitive substring across canonical fields and official observation title/abstract/DOI/arXiv/PMID/venue aliases (not mock review output or arbitrary raw JSON). Sorting always adds ID as stable tie breaker. Total counts unique global papers, not memberships.
- `GET /api/v1/catalog/papers/{id}` → `Paper`; includes all memberships and raw original records as source evidence.
- `GET /api/v1/catalog/facets` (same filters) → `{years: Facet[],venues: Facet[],publicationTypes: Facet[],categories: Facet[],tiers: Facet[],readStatuses: Facet[],sources: Facet[],inclusionStatuses: Facet[],dimensions: Record<string,Facet[]>}`; `Facet={value,count}`. Counts reflect currently filtered distinct global papers.
- `GET /api/v1/catalog/venues` → `{items: Venue[], total, page, pageSize}`; query `q`, `topic`, `kind` (`journal` / `conference`), `page` (default 1), `pageSize` (default 20, max 100), `sort` (`jif_desc` default, `jif_asc`, `name`). Impact-factor sorts keep missing values last; ties use name, metric year, registry version and ID for stable pagination. Venue `{id,name,kind,jif,ccf,metricYear,registryVersion,source,evidence,eligibleTopics,qualificationStatus,issns:string[],aliases:string[]}`. Registry entries are versioned evidence, not invented scores. Historical membership remains unchanged when registry versions change.
- `GET /api/v1/catalog/preferences` → `{filters:object}`.
- `PUT /api/v1/catalog/preferences`, body `{filters:object}` → `{filters:object}`.
- `PUT /api/v1/catalog/papers/{id}/state`, body `{hidden:boolean,readStatus:"unread"|"reading"|"read"}` → `{hidden,readStatus}`. Does not edit historical metadata. Personal hidden/read state is currently global across topics; the UI labels this explicitly. There is no implicit per-topic hide behavior.

`Paper={id,title,titleZh,abstract,authors:string[],year,venue,publicationType,doi,arxivId,url,formalOnlineDate:string|null,issueDate:string|null,dateProvenance,readStatus,hidden,isMock,memberships:Membership[],evidence:SourceEvidence[]}`.

`Membership={topic,originalId,tier,primaryCategory,categories:string[],dimensions,readStatus,source,isMock,inclusionStatus,readingNotes:Record<string,string>,raw:object}`. `SourceEvidence={source,url,dateType,date,raw:object}`. Unknown classification/date evidence remains explicitly unresolved rather than fabricated; this alone does not add a historical record to the pending-review collection. `Paper.isMock` describes source data only. Real API papers have `isMock=false`; the independent `review.isMock` describes model output, not source authenticity. Old mock outputs remain marked; current runtime waits for model configuration rather than fabricating a review. Neither a real HTTP response nor `isMock=false` implies scientific review or formal inclusion.

## 简单管理接口

操作步骤见[管理指南](management.md)。以下 JSON 是标准响应 envelope 内的 `data`。共享目录写入路由登记 `catalog:manage`，服务层另要求当前有效 `isSuperAdmin=true`，并在事务内检查操作者；普通登录用户不能仅凭前端隐藏/显示按钮获得权限。

| 方法与路径                                             | 请求 / 返回                                                                                                                                           |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/catalog/access`                           | 返回 `{canManage:boolean}`，登录可用                                                                                                                  |
| `PUT /api/v1/catalog/papers/{id}/topics/{slug}/review` | 请求 `{status:"pending"\|"included"\|"excluded",note,labels:string[],expectedVersion}`；返回 `{status,note,labels,version,updatedAt,updatedBy}`       |
| `PUT /api/v1/catalog/papers/{id}/correction`           | 请求 `{fields:{title?,titleZh?,authors?:string[],year?,venue?,abstract?},reason,expectedVersion}`；返回 `{fields,reason,version,updatedAt,updatedBy}` |
| `GET /api/v1/catalog/papers/{id}/history`              | 返回 `{items:[{id,kind,topic,before,after,actorId,createdAt}]}`，登录可用，按新到旧最多 100 条；不是全量分页历史接口                                  |
| `POST /api/v1/catalog/topics`                          | 请求 `TopicBody`；返回 `Topic`                                                                                                                        |
| `PUT /api/v1/catalog/topics/{slug}`                    | 请求 `TopicBody`；返回 `Topic`                                                                                                                        |

`TopicBody={slug?,name,description,rule,searchEnabled,termGroups:string[][],expectedVersion}`。创建时可省略 slug，由服务端生成；slug 创建后不可变。词组内 OR、词组间 AND。新建自定义主题启用检索时必须提供非空词组；内置主题空词组保留原检索模板。未覆盖的内置主题配置版本为 0。

首次写入使用 `expectedVersion=0`，更新使用读到的版本；冲突返回 HTTP 409，不静默覆盖。前端自动处理版本传递，用户只需在冲突提示后重新读取。审核针对已有主题成员关系；不创建成员或物理删除论文。手动标签是共享审核标签，与个人阅读状态分开，可通过 `dimension={"manual_labels":"标签"}` 查询。

纠错 `fields` 是**完整覆盖集合而不是部分 PATCH**：省略某字段即撤销该字段覆盖、恢复来源值；空集合撤销全部覆盖。仅上述六字段可编辑，年份为 0–9999；UI 要求填写纠错原因。DOI/arXiv、日期、URL 和 raw/evidence 不可在此编辑。`Paper.correction` 返回当前覆盖和版本，正常 Paper 字段返回有效值；查询、对应筛选/分面与排序也使用有效值，原来源仍可在详情追溯。

`Membership` 增加可选 `manualReview`（上述审核响应结构）与 `effectiveStatus`。有人工审核时响应 `inclusionStatus`/`effectiveStatus` 反映人工决定，但数据库原始成员分类及 raw 不被重写；无人工决定时继续保留原始分类语义。LLM `review` 与 `manualReview` 不可混用。

主题保存不触发扫描或启用全局 scheduler。采集 provider 在新运行开始时冻结模板内容与版本，恢复/重试使用旧快照；暂停只影响后续新运行。旧静态版本运行没有快照时仅允许匹配原静态模板，不用编辑后的配置猜测恢复。

### 简化账号管理使用的既有接口

Web 管理入口仅对管理员展示，沿用既有 `/api/v1/users`，不新增注册或密码重置 API，也不替换既有 RBAC。

- `GET /api/v1/users?keyword=&page=1&pageSize=20`：`{items:User[],total,page,pageSize,totalPages}`，要求 `system:user:read`。
- `POST /api/v1/users`：表单发送 `{username,password,nickname,isEnable,isSuperAdmin}`，要求 `system:user:write`；返回 `User`。
- `PUT /api/v1/users/{id}`：表单发送 `{nickname,isEnable,isSuperAdmin}`，当前账号仅发送昵称；要求 `system:user:write`；返回 `User`。
- `User={id,username,nickname,phone,email,avatarUrl,roleCodes,isSuperAdmin,isEnable,sortId}`。界面“管理员 / 普通用户”映射 `isSuperAdmin`，**不是新增 `role` 字段或覆盖 `roleCodes`**；未发送的既有角色与联系字段保持不变。

既有用户 API 的权限体系保留；创建/提升管理员、显式分配角色及修改已有管理员另受当前数据库管理员校验。不能禁用、降级或删除当前账号。后端既有用户软删除接口不代表此简化界面提供删除功能；论文没有对应删除接口。

管理数据库变更见 `20261027163000_catalog_management.sql`，升级沿用现有 Atlas 迁移流程，不新增自动初始化或清库步骤。

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

## Real-source observations and freshness

Every page/detail paper adds `origin` (`historical` or `live`), optional `importedAt`, `firstSeenAt`, `lastObservedAt`, `latestSourceUpdatedAt`, `observationCount`, optional `review`, and `observations` (empty in list, populated in detail). Historical import time is **not** historical first discovery. `firstSeenAt` is only populated for papers first created by the live platform. `lastObservedAt` is the latest new immutable source-content observation, not every unchanged polling check; monitor source health separately reports polling progress. `sort=observed` sorts these new observations, while `sort=source-updated` sorts source-reported metadata change timestamps. Both use nulls-last and stable ID ties; neither implies a newly published paper. `newest/oldest` retain formal-date/year ordering, not invented January 1 dates.

`review={provider,model,promptVersion,reviewedAt,isMock,scientificReviewed,inclusionStatus,summary,pipeline?}`. Current machine processing remains `scientificReviewed=false,inclusionStatus="pending"`; new query-hit memberships remain pending even though their real paper is visible in ordinary catalog results. Historical memberships, scientific notes, IDs and canonical metadata are never overwritten by new observations.

`SourceObservation={id,contentHash,coreDiscovery,eventType,source,sourceId,sourceUrl,pmid?,runId,version,title,abstract,url,doi,arxivId,venue,issns,authors,publicationType,observedAt,sourceUpdatedAt,publicationEvents,review,topics,templateVersion,raw}`. Events preserve `{kind,date,evidence}` independently. For a newly created live paper, conflicting formal dates stay null in its canonical typed date fields; existing historical canonical dates are never overwritten, and all subsequent conflicting source facts remain visible in observations. `eventType` is `source_observed`, `metadata_updated`, or `version_updated`; only an explicit numeric arXiv/bioRxiv/medRxiv version advance is a paper version update. Citation/index metadata changes are not paper versions. Source URL is the official source landing page, not an internal cache trace.

`catalog.IngestObservation(ctx, ObservationInput)` requires an explicit active audit operator and real source data. It appends immutable content-addressed observations transactionally, with exact DOI/arXiv/PMID identity matching; no fuzzy title merge. The same source content can gain additional topic query-hit associations without creating a duplicate observation. Conflicting established identifiers reject for review. Poll/retry times and LLM output are excluded from content hashes. There is no frontend ingestion endpoint. Explicit shared human decisions and bibliographic display corrections use the separate management endpoints below; they never replace immutable source observations.

`abstractZh` and `translationStatus="historical-source-text"` appear only when actual historical Chinese abstract text exists. `membership.readingNotes` contains exact nonempty source text under `contribution`, `method_summary`, `evidence_note`, `uncertainties`, `summary`, `review_notes`; absence is not inferred from `abstract_fetched` or availability flags. List notes are lightweight source excerpts, not generated summaries.

### Verified historical abstract overlay

Detail responses may include `abstractEnrichment={observationId,abstract,source,sourceId,sourceUrl,doi,sourceVersion,completeness,verificationStatus,abstractHash,responseHash,parserVersion,sourceField,enrichedAt}`. It is separate from canonical `abstract`; neither that field nor the baseline membership/raw/scientific/personal-reading state is rewritten. The initial scope is exactly three pinned source responses, validated by `tools/enrichabstract` against original response bytes and the immutable baseline. See [bounded enrichment workflow](abstract-enrichment.md). No public ingestion endpoint is added.

`verificationStatus="source-response-verified"` with `completeness="source-field-complete"` means exact correspondence to the saved source abstract field/section, **not** independent publisher completeness certification or scientific review. `sourceVersion` is honestly `cached-source-field` or `cached-publisher-section` where a concrete publication version number was not established. `enrichedAt` is the local append time, not a fabricated source acquisition/publication date. The overview may show this explicitly labeled source text when canonical `abstract` is empty; notes, translations and mock output cannot become fallback abstracts.

The backing immutable observation has `kind="historical-abstract-enrichment"` and `abstractEvidence` containing the same provenance (without `observationId`/`abstract`). Its `observedAt` equals the append audit time. Such records count toward `observationCount` and are searchable as source evidence, but are excluded from `lastObservedAt`, `latestSourceUpdatedAt`, live-observation sorting and latest `review` selection. They also cannot make the next genuine source poll appear to be a publication/version update. Detail `observations` include them; list `observations` remain empty and list payloads do not load recovered abstract bodies.

`catalog.EnrichHistoricalAbstract` requires an active audit actor, a positive batch/run ID and a verified allowlisted record, resolves only an existing historical `(topic,originalId)` and checks its established DOI. It never creates a paper or membership. Missing targets, source-ID/DOI conflicts, altered text/provenance and THPFF are rejected. Repeating identical verified content is idempotent even with a new batch/time. Only public baseline IDs, source links and provenance appear in metadata/raw; private response paths and request headers never enter this API.

The LLM original HTML collection selector is backend dimension `display_collection`. Its exact precedence is: publication eligible + explicitly related → 正式纳入; original numeric year below 2025 → 窗口外背景; original status contains 预印本 → 预印本候选; special track → 特殊分轨; out-of-scope/method control → 条件外或方法对照; otherwise 待核实. Import projection and an additive migration implement this display rule without changing original evidence.

## Core journal discovery registry

`data/journals` preserves the original208 historical registry rows and adds362 discovery-only evidence groups compiled from all actual curated journal memberships with recorded valid issuer ISSNs: BioAI7376/LLM316/World63, no unexplained misses. These570 rows are versioned evidence, not570 unique journals. `coverage.json` enumerates conference/series and missing-recorded-identity exclusions; `tools/journalregistry/compile.py --check` independently reproduces the artifact from pinned baseline shards. Discovery-only entries carry explicit discoveryTopics, source/metadata hashes and metricVerified=false; Medical Image Analysis retains JIF14 with metricYear0/unknown and still enters core discovery. No current metric qualification or new-paper inclusion is inferred from historic curation, and no old-name JIF is transferred through an ISSN to a renamed journal. Topic-specific ISSN routing preserves each actual target scope. Existing historically verified seed metrics still use BioAI JIF>10 vs other-topic JIF≥4 for discovery; expanded keyword feeds remain unrestricted.

## Collection scope and preserved default catalog

The query UI starts with `collectionScope=curated`; `all` remains the backwards-compatible API default. No scope physically deletes papers or memberships. Shared manual review takes precedence per `(paper, topic)`:

- `curated`: manual `included`, or no manual decision plus historical curated rules (BioAI `collection=正式纳入`, LLM `display_collection=正式纳入`, World Model `eligibility_status=正式纳入`).
- `pending`: manual `pending`, or no manual decision plus original `pending-review`. Historical uncertain/out-of-scope records are not automatically pending.
- `excluded`: manual `excluded`; it can be changed back to pending/included.
- `all`: all classifications, still subject to the request's other filters, including personal hidden state.

`inclusionStatus=pending` and `pending-review` both use effective pending; `included`/`excluded` select explicit manual decisions, while `historical` selects original historical memberships with no manual decision. Thus `inclusionStatus=included` is not a synonym for the broader `collectionScope=curated`.

These predicates apply to both paper queries and facets. The immutable baseline has **8,556 / 1,204 / 1,210** original curated memberships; current counts may differ after explicit manual decisions. Source hits and machine output never imply approval.

`GET /topics` counts memberships from the database, independently of a user's search/hidden filters: `membershipCount` is all memberships, `historicalCount` is original `inclusion_status=historical`, `candidateCount` follows effective pending, and `curatedCount` follows effective curated. These are not four disjoint buckets and must not be summed. Paper list totals instead count distinct global papers. Topic write responses return the saved configuration; reload `GET /topics` for current counts.

### Per-record ingestion quarantine

`catalog.ObservationRejectionKind` distinguishes only explicit `invalid_source_record` and `identity_conflict` failures. The boot publisher maps these to the monitor typed permanent rejection, so one bad source record can be retained in quarantine while valid records continue to publish. Missing/inactive audit actors, database outages and other transient failures are not classified as bad records and do not advance successful publication. Quarantine retains original source evidence; it is not a hard delete or a scientific rejection. Observation-aware search uses parameterized, fixed-field `EXISTS` matching, so duplicate source versions never duplicate result rows or totals.

`SourceObservation.coreDiscovery` records whether that immutable observation was first obtained through a whole-core-journal scan. It does **not** assert topic relevance or formal inclusion: core journal articles are retained as candidates without local keyword rejection. Routing provenance (core/expanded, like topic/template) is excluded from the scientific-content hash, so finding the same official content through another query does not manufacture a source revision; monitor run/template history records the subsequent route.

### 隔离记录只读检查

- `GET /api/v1/monitor/quarantine?page=1&pageSize=20&source=crossref`：返回 `data.items/total/page/pageSize`。页码 1–1000000，每页 1–100（默认 20）；省略 source 表示全部，否则仅接受 crossref、pubmed、arxiv、biorxiv、medrxiv、europepmc。无效参数返回 4xx。按隔离时间倒序、ID 倒序稳定排序，超出末页返回空数组。
- `GET /api/v1/monitor/quarantine/{id}`：仅查询隔离候选，未知 ID 或非隔离候选返回 404。ID 为既有稳定 Snowflake 十进制字符串，不使用来源 URL 或原文作为路径标识。
- 两接口与其他 monitor GET 使用相同的认证访问边界：匿名、失效会话、禁用账号均不可访问；这是共享运行状态，而不是某用户的个人论文库。没有新增修改、释放隔离或触发采集的接口。
- 列表字段：`id, source, sourceId, title, doi, arxivId, pmid, topics, reason, quarantinedAt, acquiredAt, sourceUpdatedAt, runIds`。空标题有中文占位；时间为 RFC3339（缺失可能是零时间）。`reason` 仅为受控拒绝类型，未知错误归为 `record_rejected`，不回显异常原文。`runIds` 只关联仍保留的运行历史（含失败恢复运行保留的候选关联，同一运行去重），不是完整历史溯源；空数组不表示记录从未属于一次运行。
- 详情额外包含 `sourceUrl`（仅由安全 DOI 构建，其他情况为空）和 `evidence: {format, bytes, sha256, preview, redacted, truncated}`。format 为原证据 `json/text/empty`；bytes 与 SHA-256 对应服务端原始证据。**preview 是安全元数据的 JSON 文本投影，不是原始响应的截取**，只包含有界标题、来源、来源 ID 和受控原因；永不返回任意原始 JSON/XML、嵌套字段、请求头或错误页。redacted 恒为 true，表示原文字段已被排除；truncated 表示预览标题/标识超出上限。预览最大 4 KiB；标题上限 512 字节，来源标识上限 256 字节，疑似凭据文本保守隐藏。
- 合法 JSON 原文及非法原文的 RawEvidence 继续完整保留于持久化快照，检查不会修改原证据、审核结论或科学分类。客户端必须按普通文本呈现 preview，不能解释为 HTML；服务端不会抓取任何来源地址。

### Monitor source success timing

`GET /api/v1/monitor/sources` checkpoint `lastAcquiredAt` is the actual UTC time when all templates/pages for that source finished acquisition. It is persisted independently of the fixed `acquiredThrough` coverage watermark and is unchanged on partial failure, canceled requests, or idempotent replay. A missing/zero value in older persisted state means unknown; clients must not synthesize it from the coverage watermark. This does not assert publication, scientific review, or successful catalog projection. Source errors and quarantine counts remain separate diagnostics.

### 手动来源连通性测试

`POST /api/v1/monitor/sources/test` 无请求体，要求 `catalog:manage` 且当前身份为超级管理员。返回标准 envelope 的 `data.items`，每项包含 `source/status/message/statusCode?/elapsedMs/checkedAt/retryAt?`；响应使用 `Cache-Control: no-store`。

检测从服务器向已配置的官方来源发起，每来源最多一个 GET，整轮最多 15 秒，不自动重试。同一服务实例拒绝重叠测试；复用采集 Executor 的官方 HTTPS 域名限制、同源串行限速和 Retry-After，不允许客户端提交 URL。Crossref 请求零条记录、PubMed 查询数据库信息、arXiv 使用 OAI Identify、bioRxiv/medRxiv 请求最近一篇、Europe PMC 请求一个 ID。仅收到有效 API envelope 才返回 `connected`，HTTP 200 错误网页不算成功。

来源状态可为 `connected/timeout/network_error/blocked/rate_limited/unavailable/invalid_response/deferred/skipped`。部分失败仍返回 HTTP 200 和各来源结果；`deferred` 或已有访问封锁会明确说明本次没有发出请求。错误摘要不含原始上游正文、完整异常或凭据。未授权、会话失效和并发测试返回对应 4xx。

测试不创建采集运行，不加载或保存采集快照、不写论文、不推进游标，不调用模型或启用调度；测试结果不冒充最近成功采集时间。进入页面、刷新状态及普通 CI 均不会触发外部检测。


## 论文处理档案与阶段重试

操作与配置见[论文处理流程](paper-pipeline.md)。沿用标准响应 envelope，ID 均为十进制字符串。

- `GET /api/v1/monitor/pipeline/{id}`：登录可读，`id` 是 **candidateId，不是 paperId**；返回 `PaperPipeline`。不存在的候选/档案返回 4xx。
- `POST /api/v1/monitor/pipeline/{id}/retry`，body `{"stage":"summary"}`：要求 `catalog:manage` 且服务端检查当前有效超级管理员。返回重新排队后的 `PaperPipeline`，不在该请求中同步运行模型。阶段只允许 `material/classification/summary/independent_review/program_validation/evidence_check`；不可重试状态或退避未结束返回 4xx。
- 普通重试仅重开 `failed/awaiting_configuration/retry_wait` 阶段，不重置来源检查点。特殊情况：已完成且材料非 `fulltext` 的 `material` 可归档旧档案并重建全流程，后续可能再次付费；它不是无成本刷新。

`PaperPipeline={candidateId,key,runId?,templateVersion,version,status,material,stages,scientificReviewed,projectionError?,templates?,history?}`。版本为 `paper-pipeline-v1`；顶层状态为 `processing/awaiting_configuration/retry_wait/failed/needs_human_review/ready_for_display`。`scientificReviewed` 恒不因自动处理转 true。

`material={scope,text,sourceUrl,sha256,blocks?,reason?,retryable,rawSha256?,truncated,license?}`；scope 为 `fulltext/partial/abstract_only/metadata_only`。`blocks[]={id,kind,text,sourceUrl,sha256}` 保存实际阅读原文。`retryable` 是材料获取诊断，不代表自动授权无限重抓全文。

`stages[]={name,status,attempts,inputHash,provider,model,promptVersion,ruleVersion,startedAt,finishedAt,nextRetryAt,error?,result?}`。阶段状态为 `pending/running/completed/retry_wait/failed/awaiting_configuration`；错误字段使用安全分类，不含上游正文或密钥。`result` 按阶段含 `topics`、`summary/claims`、`supported/issues`、`claimSupport` 与 `call`。主题结果为 `{topic,relevant,category,score,confidence}`；score 为 0–1，confidence 不能等同科学可信度。`claims[]={evidenceBlockId,text,quote,numbers,citations}`；`call={model,usage,inputHash}` 记录实际响应元数据，供应商未报告用量时不能虚构。

目录 `Paper.review.pipeline` 返回对应机器档案；详情 `SourceObservation.pipeline` / `SourceObservation.review.pipeline` 保留该来源观察的档案。读取处理 GET 获取当前完整档案（必要时从目录归档回取）；补取材料前的旧档案保存在 `history`，冻结主题规则保存在 `templates`。`GET /catalog/papers?sort=model-priority` 只使用最新真实来源观察的已完成分类，按当前 `topic` 的分数降序；未指定 topic 时取该版本最高主题分数。未知/异常分数后置，不回退旧版本；最终按 ID 降序稳定排序。此排序不改变成员分类、人工审核或个人状态。
