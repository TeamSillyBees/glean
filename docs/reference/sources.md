# 公开文献 API 访问与更新策略

核对日期：2026-11-07（Asia/Shanghai）。范围为 Glean 已实现的 Crossref、PubMed、Europe PMC、arXiv、bioRxiv、medRxiv，另列尚未接入的 OpenAlex。本文区分官方要求、当前代码行为和后续建议；建议不代表已经修改运行配置。

当前六源共用 72 小时扫描计划。来源选择、关键词和回溯范围由[检索方案](../guides/search-plans.md)配置，有界运行与恢复见[采集 CLI](../../cmd/glean-monitor/README.md)；线上开关见[当前实例](../deployment/current-instance.md)。

## 发现范围与入口

新运行读取已启用主题的保存配置：有路线时使用各路线的来源与词组；无路线时使用主题词组；内置主题无自定义配置时采用默认模板。配置在运行开始时冻结，恢复继续沿用原快照。

| 采集路径                     | 上游筛选范围                                                                | 返回后的主题处理                       |
| ---------------------------- | --------------------------------------------------------------------------- | -------------------------------------- |
| Crossref 普通发现            | 第一组关键词逐个 `query.bibliographic` 请求，加索引更新时间窗口，结果取并集 | 不强制完整布尔式，保留候选供相关性判断 |
| PubMed / Europe PMC 普通发现 | 组内 OR、组间 AND，加创建/修订或索引/更新窗口                               | 命中仍是候选，不等于实质相关或正式纳入 |
| 核心期刊补查                 | 上述三个期刊来源按登记 ISSN 获取全刊更新，不叠加主题关键词                  | 无关键词的全刊记录也保留待审           |
| arXiv / bioRxiv / medRxiv    | 日期窗口；当前不发送主题关键词或类别限制                                    | 保存有效来源记录，后续判断主题相关性   |

Crossref 普通发现没有订阅全站全部更新；它带有关键词条件。核心补查也只覆盖已登记 ISSN，不是全部期刊。模型启用后仅评估记录已经关联的发现主题；一篇被多个主题发现时合并这些关联，不自动遍历所有主题。

实现入口：[主题装配](../../internal/boot/monitor_templates.go)、[路线编译](../../internal/retrieval/routes.go)、[来源请求](../../internal/modules/monitor/adapters.go)、[模型分类](../../internal/modules/monitor/pipeline_provider.go)。详细配置与审计见[检索方案](../guides/search-plans.md)。

## API 访问要求

| 来源               | 身份与额度                                                                                                                                     | 请求与分页策略                                                                                                                                                                                                                                                                                                   |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Crossref           | 公共访问无需 key；可提供联系邮箱进入 Polite，Metadata Plus 使用专用 key。实际速率和并发按响应头。此次匿名列表请求返回 **1 次/秒、并发 1**。    | 使用固定过滤条件和最新 `next-cursor`；最多 1000 条/页。缓存结果，大范围拆小窗口。[认证与限流](https://www.crossref.org/documentation/retrieve-metadata/rest-api/access-and-authentication/)；[分页说明](https://www.crossref.org/documentation/retrieve-metadata/rest-api/tips-for-using-the-crossref-rest-api/) |
| PubMed E-utilities | 无 key 默认 **3 次/秒/IP**；使用 key 默认 **10 次/秒**。规范要求 `tool`、`email`；当前 Glean 发送 `tool=glean`，未接入来源 key 或 email 参数。 | 使用 ESearch history 与批量 EFetch；较大任务按官方建议安排在周末或美国东部工作日夜间。共享出口与共享 key 的流量一起计算。[使用规范](https://www.ncbi.nlm.nih.gov/books/NBK25497/)；[key 配额](https://support.nlm.nih.gov/kbArticle/?fromlegacy=true&pn=KA-05317)                                                |
| Europe PMC         | 免费，无须 key。团队 2020 年说明为 **10 次/秒及 500 次/分钟**；2024 年确认按 IP 限流。该历史说明不作为当前容量承诺。                           | `resultType=core` 获取摘要等字段，使用 `cursorMark`；Glean 保持串行、至少间隔 3 秒。[团队限流说明](https://groups.google.com/a/ebi.ac.uk/g/epmc-webservices/c/MfxQ8nvIT5Q/m/u_72r8e1BQAJ)；[按 IP 计数](https://groups.google.com/a/ebi.ac.uk/g/epmc-webservices/c/cZLnV1JhCj8)                                  |
| arXiv OAI-PMH      | 公共读取无需 key；官方要求 **最多每 3 秒一次、同时一个连接**，合并计算同一使用方控制的所有机器。                                               | 使用 OAI 专用地址、逐日分区；续页 token 每日失效，过期后重放未完成日。不能通过增加实例提高总速率。[使用条款](https://info.arxiv.org/help/api/tou.html)；[OAI 说明](https://info.arxiv.org/help/oa/index.html)                                                                                                    |
| bioRxiv            | 公开 details 接口不要求 key；所查官方说明未列固定 QPS。                                                                                        | `/details/biorxiv/...` 每页 30 条，按返回 cursor/count/total 推进；支持 category 参数，但 Glean 尚未使用。与 medRxiv 共用限速。[官方 API](https://api.biorxiv.org/)                                                                                                                                              |
| medRxiv            | 与 bioRxiv 使用同一公开 API 服务；所查官方说明未列固定 QPS。                                                                                   | `/details/medrxiv/...` 每页 30 条；共享上游并发与退避，不能按两个域名配额分别计算。[官方 API](https://api.biorxiv.org/)                                                                                                                                                                                          |

Crossref 的官网通用表列 Public 5、Polite 10，但不同请求池的运行值可能更低。2026-11-06 17:24:56 UTC，从本地发起一次无 key、无联系人的 `GET /works?rows=1&select=DOI`，HTTP 200 返回：

```text
x-api-pool: public-array
x-rate-limit-limit: 1
x-rate-limit-interval: 1s
x-concurrency-limit: 1
```

这是该次列表接口响应，不代表所有接口、身份和时间均采用同一额度。未来动态限速应读取这些头，并与本地保守上限取较小值。

Crossref 于 2026-08-24 上线新游标机制：游标不再过期，但每页必须携带全部原查询参数，并更新 `next-cursor`；分页结果集合可能变化。因此固定结束时间、较小窗口、计数核对和幂等去重都需要保留。[官方变更公告](https://community.crossref.org/t/changes-to-cursors-filtering-and-sorting-in-the-rest-api/16246)

## Glean 当前统一访问控制

- 仅访问装配好的官方 HTTPS origin，GET 请求，拒绝重定向；单请求默认 45 秒、响应体上限 16 MiB。
- 每个 provider 串行、请求开始至少相隔 3 秒；bioRxiv/medRxiv 共用门控。当前门控保存在进程内，尚未在服务器、CLI、测试进程之间共享。
- HTTP 客户端不即时自动重试。来源层持久化退避，遵守 `Retry-After`；无允许重试信息的 403 会阻断来源，等待处理。
- 后台任务单次 3 分钟、默认最多 12 个来源步骤；CLI 的 `--max-pages` 控制 1–200 次步骤预算。步骤可能是查询或游标转换，不能直接等同最终论文页数。
- `INITRA_MONITOR_CONTACT` 目前仅加入 User-Agent，不能据此认定已经正确发送 Crossref `mailto` 或 NCBI `email`。联系人需显式配置，不从用户账户推断。
- 记录脱敏参数、状态、游标、计数及响应哈希；密钥、邮箱、PubMed 临时会话值不进入公开审计内容。

实现入口：[来源客户端与门控](../../internal/modules/monitor/source_http.go)、[请求审计](../../internal/modules/monitor/request_audit.persistence.go)、[预算与调度](../../internal/modules/monitor/monitor.task.go)。

## 各来源的数据更新策略

上游数据更新频率与 Glean 发起扫描的频率分别管理。更新日期用于发现变化，发表日期用于研究范围和资格判断，不能互相替代。

| 来源       | 上游更新语义                                                                            | 当前 Glean 增量依据                                                                                         | 需要单独处理的变化                                                                                            |
| ---------- | --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Crossref   | 成员登记、重投元数据及 Crossref 补充关系/引用计数都会产生更新；入库到 API 可有延迟。    | 固定 `from-index-date` / `until-index-date`，覆盖新增及各种重新索引；普通种子查询与 ISSN 全刊补查分别执行。 | 仅引用计数等变化也会触发召回；新来源证据与需要重新阅读的材料变化应分开判断。更正/撤稿关系尚未形成完整关系图。 |
| PubMed     | 新增、修订持续入库；官方另提供年度全量基线和每日新增、修订、删除文件。                  | `CRDT OR LR` 发现创建/修订，ESearch 冻结 PMID 集合，EFetch 取当前记录；超过 9999 条拆日期，再拆 PMID。      | 当前未消费每日删除文件；主题查询不能充当完整删除账本。EFetch 返回的是当前元数据，不是检索时间点的历史版本。   |
| Europe PMC | 团队论文说明内容每日更新；首次入库、首次索引和后续更新含义不同。                        | `FIRST_IDATE OR UPDATE_DATE`，游标分页；MED 记录保留 PMID，其他来源保留 source 前缀。                       | 后续开放全文、书目修改需要 UPDATE_DATE；`dateOfRevision` 不是 Europe PMC 的 UPDATE_DATE。                     |
| arXiv      | 公告后提供元数据，通常为美国东部周日至周四约 22:30；其他时候也可能有少量管理/书目更新。 | OAI `datestamp` 修改日，`arXivRaw` 保存版本日期，逐日完成后推进。                                           | 最新文本与版本日期需要对应；若返回 OAI deleted 标记，当前代码跳过，尚未投影为本地生命周期事件。               |
| bioRxiv    | 日期接口包含新帖和修订版；官方说明没有给出固定 API 刷新时刻承诺。                       | 按帖子/版本日期取窗口，保存 DOI、version 和原始记录；总数漂移时有界重放。                                   | 旧稿以后新增正式发表关联，未必再次落入最近版本日期窗口。官方另有 `/pubs/biorxiv/...`，Glean 尚未接入。        |
| medRxiv    | 与 bioRxiv 同类，以其服务器实际新帖和修订为准。                                         | `/details/medrxiv/...` 日期窗口，版本与内容证据幂等写入。                                                   | 可用 `/pubs/medrxiv/...` 跟踪正式发表关联；当前尚未接入，不能把预印本日期或关联 DOI 当成正式发表日期。        |

上述上游语义依据：[Crossref 日期过滤](https://www.crossref.org/documentation/retrieve-metadata/rest-api/rest-api-filters/)、[Crossref 同步建议](https://community.crossref.org/t/get-and-sync-all-crossref-metadata/15562)、[PubMed 下载与更新文件](https://pubmed.ncbi.nlm.nih.gov/download/)、[Europe PMC 团队论文](https://pmc.ncbi.nlm.nih.gov/articles/PMC10767826/)、[Europe PMC 日期解释](https://groups.google.com/a/ebi.ac.uk/g/epmc-webservices/c/JShd-dbIIBE)、[arXiv 更新节奏](https://info.arxiv.org/help/oa/index.html)、[bioRxiv/medRxiv API](https://api.biorxiv.org/)。

当前公共编排规则：

1. 默认每 72 小时到期扫描；每分钟唤醒用于续页、退避和处理待办，不是每分钟全量请求。
2. 无明确历史起点时首次回看 30 天；后续从成功位置向前重叠 7 天。每第 10 轮扩大核心期刊补查到 90 天，旧模板的非期刊来源也保留 90 天补扫规则。
3. 冻结运行终点、主题计划和查询版本，分页失败不推进相应水位。修改路线后，新查询版本按自己的历史起点补采。
4. Crossref、Europe PMC 和 bio/medRxiv 的数量变化最多重放三次；PubMed history 失效只重放当前分区，arXiv token 失效只重放当前日。数量不变仍不能证明上游快照一致。
5. 记录先入库，再处理模型；按 DOI、PMID、arXiv ID、来源 ID 识别身份，来源版本和发现路径分别保存。人工审核和历史结论不因来源更新被覆盖。

## 分页、证据与恢复边界

- 内部窗口为 `[Start, End)`。Crossref 发送秒精度时间并检查半开边界；其他来源按 `date(Start)` 到 `date(End−1ns)` 的闭合日范围请求。日边界允许重复取数，通过幂等写入去重。
- PubMed history 固定 PMID 集合，不冻结文章正文；EFetch 可能返回结束时间之后修订的元数据。Europe PMC 的 `dateOfRevision` 不能替代其查询使用的 UPDATE_DATE。
- arXiv 初始日查询的 `noRecordsMatch` 可表示空日；未完成续页出现同一错误时按异常保留断点，不跳日。`badResumptionToken` 才触发未完成日重放。
- bio/medRxiv 内部游标保存 offset、首次 total 和重放次数；旧数字游标先有界重放。数量不变不能排除等量替换或重排，上游未提供稳定快照时不宣称绝对不漏。
- 单条坏字段、缺失身份或非法必要索引时间进入隔离，同页其他合法记录继续；游标/顶层响应错误整页失败。PubMed BookArticle 当前明确隔离为不支持类型。
- 原始 JSON 用精确数字表示计算内容哈希，不经浮点舍入；对象键序与空白不影响哈希，数字写法差异可形成新的来源证据。身份、内容版本和发现路径分别管理。
- `lastAcquiredAt` 表示来源全部模板分页完成的实际时间，覆盖上界不等于发表时间；来源成功也不代表模型或目录投影完成。

离线 fixture、原样 API 捕获和实时 canary 分别验证不同层次，运行方式见[开发指南](../development.md#ci-与外部验收)。有限请求成功不证明全部分页、持续可达或全部相关文献覆盖。

## OpenAlex 待接入对照

以下采用 2026-11-03 更新的官方说明。Glean 尚无 OpenAlex 适配器，不能把此表理解为运行配置。

| 能力                | 当前官方政策                                                                                                                                                | 对接入的影响                                                                       |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| 普通 API 检索       | 无 key 每日 $0.10 用量；免费且邮箱验证的 key 每日 $1，用量在 UTC 零点重置。无 key 10 次/秒，免费 key/预付额度 30 次/秒，年度计划 50 次/秒；重查询另有限制。 | 需要独立用量预算，不按固定“每日请求数”估算所有查询。                               |
| 分页                | `per_page` 最大 100；基础分页最多 10000 结果，cursor 后续页需要有效免费 key。                                                                               | 持续主题采集应准备有效 key，并保存 cursor；无需先购买同步套餐才能普通翻页。        |
| 按创建/更新日期同步 | `from/to_created_date`、`from/to_updated_date` 及对应日期排序需要 Member+ / Partner 或符合条件的旧合同；缺套餐返回 `429 Plan upgrade required`。            | 付费同步条件不能按普通瞬时限流无限重试，也不能用发表日期窗口声称等价覆盖全部修订。 |
| 批量快照            | 免费公开全量快照按季度发布；相应套餐提供每日全量快照及增量同步过滤器。                                                                                      | 大规模历史建库可评估快照；免费普通 API 查询、免费快照和持续同步服务分别选型。      |

新免费账户还有首次 $1 的消耗节奏限制，应遵守 `Retry-After`。后续接入需分别识别瞬时限流、日预算耗尽、首次用量节奏和套餐缺失；响应头/`meta` 中的用量应进入安全监控。依据：[认证与限流](https://help.openalex.org/api/authentication/)、[用量示例](https://help.openalex.org/access/example-costs/)、[同步政策](https://help.openalex.org/access/sync/)。

## 后续工作与材料许可

共享限流、动态配额、每日增量、已知论文复核、删除/撤稿与正式发表关联、共享 feed 等工作统一维护在 [Roadmap](../roadmap.md)。当前代码仍使用 72 小时计划；本文建议不改变运行配置。

元数据访问许可与论文全文的存储、展示、再分发许可分别处理；保留来源及许可字段，未知许可不推定为允许公开全文。现有材料获取范围见[论文处理](../guides/paper-pipeline.md)。
