# API 契约

本文维护应用接口索引和容易混淆的语义，完整字段与校验以路由和 DTO 为准。目录入口见 [catalog.routes.go](../../internal/modules/catalog/catalog.routes.go)、[catalog.dto.go](../../internal/modules/catalog/catalog.dto.go) 与[管理 DTO](../../internal/modules/catalog/catalog.management.dto.go)；监控入口见 [monitor.routes.go](../../internal/modules/monitor/monitor.routes.go)、[audit.api.go](../../internal/modules/monitor/audit.api.go) 和 [pipeline.api.go](../../internal/modules/monitor/pipeline.api.go)。

## 公共约定

- JSON 响应使用 `{code,message,data,traceId}` 标准 envelope；ZIP 导出直接返回文件。
- 业务 ID 使用十进制字符串，时间使用 RFC3339。缺失正式日期保留 null，不补造月初或年初。
- 目录和监控读取要求有效 Bearer access token；个人状态取当前登录用户。
- 共享目录写入、预算扩容、连通性测试和模型重试要求 `catalog:manage`，服务层同时校验当前有效超级管理员。
- 前端不直接调用来源或模型，没有公共论文导入/来源写入接口。

## 认证与用户

| 方法与路径                                     | 作用                                                                   |
| ---------------------------------------------- | ---------------------------------------------------------------------- |
| `POST /api/v1/auth/login`                      | 公开；提交 `{username,password}`，返回 access/refresh token 和用户信息 |
| `POST /api/v1/auth/refresh`                    | 公开；提交 `{refreshToken}`，原子轮转，旧 refresh token 不可重放       |
| `POST /api/v1/auth/logout`                     | Bearer 加当前会话 `{refreshToken}`，使配对 token 失效                  |
| `POST /api/v1/auth/logout-all`                 | 撤销当前用户的全部会话                                                 |
| `PUT /api/v1/auth/password`                    | 修改自己的密码，同时撤销旧会话                                         |
| `GET /api/v1/auth/me`                          | 读取当前有效身份和用户信息                                             |
| `GET /api/v1/users`                            | 分页查询用户，要求 `system:user:read`                                  |
| `POST /api/v1/users`、`PUT /api/v1/users/{id}` | 创建/更新用户，要求 `system:user:write`，管理员变更有额外服务端约束    |

Web 账号管理使用 `isSuperAdmin` 表示管理员类型，保留原有角色与未提交的联系字段。简化界面不提供注册、重置密码或用户删除。会话和 RBAC 详见[认证与权限](security.md)。

## 目录查询

以下路径省略公共前缀 `/api/v1/catalog`。

| 方法与路径                             | 返回/用途                                                       |
| -------------------------------------- | --------------------------------------------------------------- |
| `GET /topics`                          | `{items}`，包含主题配置、路线、协议、版本和成员统计             |
| `GET /papers`                          | `{items,total,page,pageSize}`，total 为去重论文数               |
| `GET /papers/{id}`                     | 完整论文详情、成员、来源观察与原始证据                          |
| `GET /facets`                          | 当前过滤结果的维度值与去重论文计数                              |
| `GET /venues`                          | `{items,total,page,pageSize}`，版本化期刊/会议证据              |
| `GET /preferences`、`PUT /preferences` | 读取/保存当前用户 `{filters}`                                   |
| `PUT /papers/{id}/state`               | 保存 `{hidden,readStatus}`，readStatus 为 `unread/reading/read` |
| `GET /access`                          | `{canManage}`                                                   |
| `GET /papers/{id}/history`             | `{items}`，最近 100 条共享人工修改，非全量分页历史              |

### 检索与集合

`/papers` 与 `/facets` 共用 `topic`、`collectionScope`、`q`、`year`、`venue`、`publicationType`、`category`、`tier`、`readStatus`、`inclusionStatus`、`source`、`dimension` 和任务/方法等别名。条件间取 AND。文本为不区分大小写的字面子串，搜索有效书目及官方来源观察的明确字段，不搜索任意 raw 或模型输出。

| 参数                       | 语义                                                                      |
| -------------------------- | ------------------------------------------------------------------------- |
| `collectionScope=all`      | API 默认，保留全部分类；仍受其他过滤与个人隐藏限制                        |
| `collectionScope=curated`  | UI 默认；人工 included，或无人工决定时按原历史精选规则                    |
| `collectionScope=pending`  | 人工 pending，或无人工决定时的原 `pending-review`                         |
| `collectionScope=excluded` | 人工 excluded，可恢复                                                     |
| `hidden`                   | `exclude` 默认；另有 `include/only`，个人隐藏跨主题生效                   |
| `page/pageSize`            | 默认 1/20，每页最多 100                                                   |
| `sort`                     | `newest` 默认；另有 `oldest/title/observed/source-updated/model-priority` |

`inclusionStatus=included` 仅选择明确人工纳入，不等于包含历史精选的 `collectionScope=curated`。历史不确定记录不自动进入 pending。所有排序有稳定 ID 次序，缺失的时间/模型分数后置。

`dimension` 是“原始维度键 → 单个精确字符串值”的 JSON 对象，键间取 AND，拒绝 null、数组和非字符串值。例如 URL 编码后的 `{"biomedical_domains":"Oncology"}`。`task/role/direction/method` 是历史维度的便利别名，完整维度由后端 facets 返回，前端不自行推断。

`GET /topics` 的 `membershipCount/historicalCount/candidateCount/curatedCount` 统计成员关系，不受个人搜索/隐藏影响，彼此不是互斥桶，不能求和。列表 total 统计全局论文。主题写入响应不承诺包含最新统计，写入后重新读取列表。

`GET /venues` 接受 `q/topic/kind/page/pageSize/sort`，kind 为 journal/conference，sort 为 `jif_desc` 默认、`jif_asc/name`；缺失指标后置。登记项保存指标年、来源和版本，不代表当前指标已经核验。

### 论文与来源字段

| 层次     | 关键字段及含义                                                                                 |
| -------- | ---------------------------------------------------------------------------------------------- |
| 论文     | 稳定身份、有效书目、`formalOnlineDate/issueDate`、个人状态、`memberships`、`evidence`          |
| 主题成员 | `topic/originalId`、原分类与 dimensions、`manualReview/effectiveStatus`、历史 raw/readingNotes |
| 来源观察 | 来源 ID/URL、内容哈希、版本、题录、出版事件、来源更新时间、采集时间、发现路径和处理档案        |
| 新鲜度   | `origin/importedAt/firstSeenAt/lastObservedAt/latestSourceUpdatedAt/observationCount`          |
| 机器结果 | `review` 及可选 `pipeline`，与人工审核独立                                                     |

列表省略重型 raw、evidence 和 observations 正文；详情返回已保存证据。`firstSeenAt` 只表示平台首次创建的 live 论文，不从历史导入时间推造。`lastObservedAt` 是新内容观察的时间，不是每次无变化轮询；来源轮询进度从监控 API 读取。

`eventType` 区分 `source_observed/metadata_updated/version_updated`；只有明确的预印本版本增加才构成论文版本更新。索引和引用计数更新不等于新版本。`coreDiscovery` 只是全刊发现标记，不保证主题相关性。

同内容再次被不同主题/路线发现可追加关联，不制造重复内容版本。身份冲突隔离；不按标题相似度合并。已有历史书目、日期和科学结论不被新观察静默覆盖。

详情中的 `abstractEnrichment` 是经过固定来源字节校验的历史摘要补充，独立于 canonical abstract；其本地追加时间不能充当实时来源更新。范围、幂等和证据语义见[历史摘要补全](../guides/baseline.md#历史摘要补全)。

## 共享管理

以下写入使用 `expectedVersion` 防止并发覆盖：首次 0，更新使用读到的版本，冲突返回 409。

| 方法与路径（`/api/v1/catalog` 前缀）    | 请求重点                                                                                                     |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `PUT /papers/{id}/topics/{slug}/review` | `{status,note,labels,expectedVersion,evidence?}`；status 为 pending/included/excluded                        |
| `PUT /papers/{id}/correction`           | `{fields,reason,expectedVersion}`                                                                            |
| `POST /topics`、`PUT /topics/{slug}`    | 主题基本信息、`searchEnabled/termGroups/searchRoutes/searchProtocol/protocolAcknowledgement/expectedVersion` |
| `POST /topics/protocol-preview`         | 离线校验完整协议，返回可保存协议、编译差异与确认摘要                                                         |

纠错 fields 为六项展示字段的**完整覆盖集合**：省略字段表示撤销该项覆盖，空集合撤销全部；不是 PATCH。支持 title/titleZh/authors/year/venue/abstract，不改 DOI、arXiv、日期、URL 或原始证据。查询与排序使用有效值。

审核针对已有成员，按主题独立保存，不创建成员或物理删除论文。协议要求的纳排证据必须符合 profile 版本、理由、实际阅读层级和主源定位；证据不足保留待核。模型输出不代替人工决定。

主题 slug 创建后固定。非空 searchRoutes 覆盖普通发现的旧词组；省略或 null 保留已有路线，显式 `[]` 清空。自定义主题启用时需要有效词组或路线，内置主题无自定义配置时沿用默认。完整协议先预览、确认再保存；plan 模式不能开启执行。保存主题不触发扫描，活动运行仍使用冻结配置。详见[检索方案](../guides/search-plans.md)。

## 监控、审计与预算

以下路径省略公共前缀 `/api/v1/monitor`，GET 均要求登录。

| 方法与路径                                    | 用途                                         |
| --------------------------------------------- | -------------------------------------------- |
| `GET /sources`                                | 来源检查点、失败、覆盖、调度状态与隔离数量   |
| `GET /templates`                              | 已配置主题的实际来源编译预览，不发送外部请求 |
| `GET /runs`                                   | 热状态中的运行记录                           |
| `GET /archive/runs?before=&limit=50`          | 长期归档，稳定倒序 ID 游标                   |
| `GET /runs/{id}/requests?after=&limit=100`    | 每次 HTTP 尝试和失败                         |
| `GET /runs/{id}/occurrences?after=&limit=100` | 全部来源出现，包括重复出现                   |
| `GET /runs/{id}/catalog?after=&limit=100`     | 当前目录状态、全部发现路径与当前用户个人状态 |
| `GET /runs/{id}/export`                       | 完整有界审计 ZIP                             |
| `GET /model-budget`                           | 全数据库共享模型累计配额与用量               |
| `POST /runs/{id}/budget`                      | 管理员提高未完成运行的累计采集预算并记录原因 |
| `GET /quarantine`、`GET /quarantine/{id}`     | 隔离记录列表与安全证据预览                   |
| `POST /sources/test`                          | 管理员显式测试来源连通性，无请求体           |

`lastAcquiredAt` 为来源全部模板分页完成的实际时间，`acquiredThrough` 为固定窗口覆盖上界，旧状态缺失成功时间时保持未知。采集成功不等于目录投影或模型处理完成。

查询账本区分 completed、completed_empty、partial_budget、retry_wait、failed、blocked；未启用来源保留覆盖缺口。请求计数、来源出现数、候选数和唯一论文数不混用。ZIP 以 manifest 的字节数/SHA256 和 CRC 对账，超过 128 MiB 明确拒绝，改用分页接口；正文默认省略，原 HTTP 响应主要保存哈希。字段说明及扩容请求见[检索方案](../guides/search-plans.md#请求审计与导出)。

### 隔离与连通性

隔离列表默认每页 20、最多 100，按时间/ID 稳定倒序，可按六源名称过滤。未知或非隔离 ID 返回 404。详情 evidence 的 `bytes/sha256` 对应原始证据，`preview` 是最多 4 KiB 的安全元数据投影，**不是原响应截取**；始终标记 redacted。按普通文本显示，不执行 HTML。没有释放隔离或触发采集的写入接口。

连通性结果 `data.items` 包含 `source/status/message/statusCode?/elapsedMs/checkedAt/retryAt?`，使用 no-store。整轮最多 15 秒，每源最多一个 GET，无自动重试，同实例拒绝重叠测试。状态区分 connected、timeout、network_error、blocked、rate_limited、unavailable、invalid_response、deferred、skipped；部分来源失败仍可返回 200 和逐源结果。

检测复用官方 HTTPS origin、限速与 Retry-After；有效 API envelope 才算 connected。不创建运行、不写论文、不推进游标、不调用模型，结果不能填入最近成功采集时间。

## 论文处理档案与阶段重试

- `GET /api/v1/monitor/pipeline/{id}`：id 为 **candidateId，不是 paperId**，登录可读。
- `POST /api/v1/monitor/pipeline/{id}/retry`：管理员提交 `{stage}`，只重新排队，不同步执行模型；阶段为 material/classification/summary/independent_review/program_validation/evidence_check。
- 普通重试仅重开允许的失败/待配置/退避阶段，不能越过 Retry-After；已完成但材料非 fulltext 的 material 可归档旧流程并重建后续阶段，可能再次收费。

档案保存 candidateId、运行与模板版本、材料块和哈希、各阶段状态/次数/时间、模型和提示版本、输出及历史。材料范围为 fulltext/partial/abstract_only/metadata_only；阶段为 pending/running/completed/retry_wait/failed/awaiting_configuration。

`scientificReviewed` 不因机器处理变成 true。主题相关性分数为 0–1，confidence 不是科学可信度。`sort=model-priority` 只使用最新真实观察的已完成分类，按所选主题得分或该版本最高主题分数排序，未知分数后置，不回退旧版本。

配置、恢复与费用边界见[论文处理](../guides/paper-pipeline.md)，完整类型见 [pipeline.dto.go](../../internal/modules/monitor/pipeline.dto.go)。
