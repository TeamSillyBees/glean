# 检索方案与覆盖审计

本功能把研究协议映射为可保存、可执行和可审计的主题路线配置。当前执行 Crossref、PubMed、Europe PMC、arXiv OAI、bioRxiv、medRxiv 六源子集；其他来源和引文计划完整保留为 planned。保存、预览和导出不会启动采集或模型。

## 完整协议导入

管理员在“研究主题 → 导入完整检索协议”粘贴协议 JSON，或需求包的 profile/requests JSON。点击“预览协议与语义差异”，核对来源、日期和语义警告后勾选确认，才能保存。`POST /api/v1/catalog/topics/protocol-preview` 是同一离线预览接口；保存主题时同时提交返回的 `protocol` 和 `protocolAcknowledgement`。

- `profile_id/profile_version` 标识研究协议，主题编辑仍使用 `expectedVersion` 防止并发覆盖。改变已保存协议必须提升 profile_version；旧配置和人工操作留在目录历史中。
- 用户要求、系统假设、概念出处、来源权限/许可、种子、质量检查、外部原生查询分别保存。没有确认的语言、文类、载体阈值不会继承内置生物医学规则。
- `mode=plan` 只能保存计划，不能开启主题扫描；外部查询的命中数为 null，不产生成功请求记录。切换 execute 需要明确有效的日期范围、时区和采集预算。
- 协议中的日期按指定时区解释为日历范围，再转 UTC；实际采集仍遵循各源索引/更新日期语义，发表资格由独立证据审核决定。
- 导入上限 120 KiB，拒绝未知字段、凭据、无效词表和超限路线。通道编译使用有限词组模型；不会把外部原生查询直接当成等价已执行语句。
- `quality_checks` 保存抽样框、方法、样本数、漏检/不收录区分、证据、审读人和时间。当前校验并导出这些声明，不自动抽样，也不生成总体召回率。

## 配置与预览

管理员在“研究主题”的多路线检索编辑器粘贴 JSON 数组。一个主题最多20条路线，每条最多10个词组、合计50个词条；每个词条最多300字节。路线之间取并集，组间 AND、组内 OR。

```json
[
  {
    "id": "broad-discovery",
    "name": "主题宽召回",
    "sources": ["pubmed", "europepmc"],
    "fieldMode": "title_abstract",
    "termGroups": [
      ["large language model", "LLM"],
      ["knowledge graph", "ontology"]
    ],
    "backfillFrom": "2020-01-01T00:00:00Z"
  },
  {
    "id": "metadata-supplement",
    "name": "题录补充",
    "sources": ["crossref"],
    "fieldMode": "default",
    "termGroups": [
      ["large language model", "LLM"],
      ["knowledge graph", "ontology"]
    ],
    "backfillFrom": "2020-01-01T00:00:00Z"
  }
]
```

示例用于说明结构，不代表已验证全面性的研究策略。保存不会发送上游请求。保存后在「检索方案」或 `GET /api/v1/monitor/templates` 检查各来源编译预览。预览明确标识尚未执行、来源是否启用、回溯起点和能力限制。

旧 `termGroups` 继续兼容。非空 `searchRoutes` 取代该主题普通发现词组，核心期刊补查仍保留原独立语义。旧客户端省略或传 null 不清空已有路线，显式 `[]` 才清除并回到传统词组配置。自定义主题启用检索时至少有词组或路线。

配置不会自动授予 JIF/CCF、核心期刊或科学纳入资格；命中仅是待审核候选。

## 来源边界

- `title_abstract` 仅支持 PubMed `[tiab]` 与 Europe PMC `TITLE/ABSTRACT`。
- `default` 按来源实际查询能力编译。Crossref 对第一词组逐种子独立请求并取并集，不保证第二词组或完整 Boolean 式在服务端执行。
- arXiv OAI、bioRxiv、medRxiv 按日期窗口取得 feed，不执行主题关键词；记录保留为候选，相关性由后续审核判断。不同路线目前会重复扫描 feed，尚未实现共享采集优化。
- 不支持的字段/来源组合明确拒绝。路线不接受 URL、HTTP 方法或凭据，沿用官方 HTTPS origin、GET、限流和持久退避。
- 不支持任意查询 AST、MeSH 自动映射或邻近算子。OpenAlex、Semantic Scholar、引文扩展、会议目录、订阅数据库等未接入路线仍应留在外部覆盖计划，不能标记为 Glean 已执行。本功能不声明项目拥有 Semantic Scholar 商业许可。

## 回溯、增量与恢复

每条来源查询有语义版本和独立水位。新增或实质改变词组、字段模式、回溯起点时，不沿用旧查询完成水位；从 `backfillFrom` 补采。没有指定起点时仅回看30日，不是全历史检索。名称变化或添加其他来源不会重置当前来源的查询版本。

超过90日的显式路线历史切为连续分区，固定本次目标上界。每个分区分页完成后持久化水位，预算或错误中断保留原分区游标。日期窗口是来源索引/修订/入库日期语义，不是统一的论文发表日期过滤。

正常扫描沿用72小时 UTC 计划、7日重叠和既有核心期刊补查。旧运行没有逐查询账本时按原模板、游标恢复；新配置不会改变已活动运行的冻结意图。

```sh
go run ./cmd/glean-monitor --sources pubmed,europepmc,crossref --max-pages 20
```

沿用项目已有审计账号和数据库配置。单次 CLI 仍受3分钟和1至200次请求预算约束；命令结束不等于整个历史计划完成。未完成时恢复同一运行。

## 同窗新计划

普通显式窗口重复调用保持幂等。改变配置后需要对同一窗口按新版本重扫时：

```sh
go run ./cmd/glean-monitor --new-plan \
  --sources pubmed,europepmc,crossref \
  --since 2026-10-01T00:00:00Z --until 2026-10-31T00:00:00Z \
  --max-pages 20
```

`--new-plan` 必须给出起止时间，且不与 demo、doctor、retry-failed 混用。有活动运行时先恢复，不能覆盖；同版本同窗仍幂等。

新计划因预算中断后，去掉 `--new-plan` 并传相同来源/窗口，或省略窗口恢复活动运行。活动运行优先于旧版本同窗完成回执。

`backfillFrom` 同样限制显式窗口。有交集时从更晚起点开始；不相交时报错且不请求该计划。狭窄手动窗口不建立未扫描历史区间的连续水位，后续自动回溯不会被跳过。

## 覆盖账本与发现路径

`GET /api/v1/monitor/runs` 的来源 `queries` 包含查询 ID、版本、主题、来源、编译式、模式、警告、整体范围、当前分区、请求/分页/记录计数和状态。

- `coverageWindow` 是本次不可变整体范围，`window` 是当前或最后分区。
- `observed` 是适配器返回记录数，不是来源总命中数或唯一论文数。
- `newCandidates` 是监控候选新增数，不是新增唯一论文数。
- `quarantined` 是采集记录校验阶段隔离数，不包含后续目录入库的全部隔离事件。
- `completed`、`completed_empty` 只表示该查询窗口分页完成或合法空结果；`partial_budget`、`retry_wait`、`failed`、`blocked` 不表示完整覆盖。
- `advanceCheckpoint=false` 表示该手动窗口没有建立连续增量水位。
- 计划要求但未启用的来源显示为 `unavailableQueries`；相应终态为 `completed_with_gaps`，不伪装已执行或无结果。

新运行将每次 HTTP 尝试保存到长期请求表，发送前先登记 pending，响应后保存状态、脱敏参数、来源声明总数、返回数、归一化数、游标和响应 SHA256。PubMed 一页的 esearch/efetch 分别留痕；前置 ID 查询不冒充最终论文计数或终页证明。中断后没有持久结果的请求为 uncertain，不补造成成功。

`unique_source_ids` 是本页去重来源 ID；`cumulative_unique` 是同运行、同来源截至该页提交时的唯一来源 ID 数，跨 query 去重，不能当作跨来源独立论文数。失败或前置请求不能取得可靠计数时保留 null。来源总命中数只按单次响应保存，不跨源相加。

显式路线的发现关系写入独立 `glean_search_hit`，记录运行、查询、计划版本、来源、表达式、模式与时间，不改变论文身份或来源内容 hash。重复发现幂等；论文详情每个来源观察最多显示最近100条路径，超过时明确 `discoveriesHasMore`。传统运行不伪造历史发现路径。

热快照仍按既有数量保留；新运行的冻结版本与每次变更同步写入长期归档，和水位 CAS 同一事务提交。旧运行没有请求档案时明确报告缺失。多路线完成不代表“所有相关论文零遗漏”。

## 请求审计与导出

“扫描记录”可以读取更早的归档、逐页查看请求账本、下载完整审计 ZIP。以下接口要求登录；目录个人状态取当前用户，而非采集操作员：

| 接口                                                         | 用途                                           |
| ------------------------------------------------------------ | ---------------------------------------------- |
| `GET /api/v1/monitor/archive/runs?before=&limit=50`          | 长期运行，稳定倒序 ID 游标                     |
| `GET /api/v1/monitor/runs/{id}/requests?after=&limit=100`    | 每次请求、失败和恢复点                         |
| `GET /api/v1/monitor/runs/{id}/occurrences?after=&limit=100` | 全部来源出现，重复出现不丢失                   |
| `GET /api/v1/monitor/runs/{id}/catalog?after=&limit=100`     | 当前目录、全部发现路径、共享修改历史、个人状态 |
| `GET /api/v1/monitor/runs/{id}/export`                       | 完整有界 ZIP                                   |
| `GET /api/v1/monitor/model-budget`                           | 全数据库累计模型配额和使用量                   |

ZIP 含 protocol.json、concepts.json、query-plan.json、requests.jsonl、occurrences.jsonl、records.jsonl、identity-edges.jsonl、screening.jsonl、coverage.json、report.md，另有 manifest.json 核对各文件字节数和 SHA256，并验证 ZIP CRC。`complete_export` 仅指账本完整导出，不代表检索或科学审读完成。

目录记录包含所有 profile 决定、范围外理由、当前用户隐藏/阅读状态、字段来源、该运行全部发现路径以及共享修改历史。详情 100 条路径限制不会影响导出。目录是导出时的当前状态；冻结采集协议在 protocol.json 中。身份关系仅输出已保存标识关联及官方预印本 API 明示的版本关系；未知研究家族数保持 null。

默认不再分发摘要/全文正文，纠错历史中的正文也只导出省略说明和哈希。原始 HTTP 响应只保留 SHA256，`raw_path` 为空表示没有对应文件；不能把哈希说成已保存原文。PubMed 临时会话值、密钥及联系信息脱敏。超过 128 MiB 的账本明确拒绝生成 ZIP，改用稳定游标分页接口，不静默截断。

## 预算与证据门槛

执行协议必须显式设置 `max_requests/max_pages/max_records/max_queue/max_history_days`。请求额度在每次 HTTP 前检查；页数、记录数和队列按完整来源页检查，后几项可能超过限额最多一页，不能称为逐条硬截断。预算耗尽保留 partial_budget、原查询、分区和游标，不换新运行绕过额度。

当前有效超级管理员可在运行中提高请求/页数/记录/队列预算，或调用 `POST /api/v1/monitor/runs/{id}/budget`，提交 topic、expectedRevision、limits 和 reason。该操作只允许增加未结束运行的额度，记录操作者、时间、前后限额；不改变冻结查询、日期或已消费额度。完成后按原 CLI 来源/窗口继续运行。扩大研究日期范围仍需新协议版本和新计划。

真实模型默认累计配额：100 次调用、1,000,000 保守预留 token、128 KiB 单次输入、4096 单次输出。配置位于 `monitor.models.budget`，对应 `INITRA_MONITOR_MODELS_BUDGET_MAX_CALLS/MAX_RESERVED_TOKENS/MAX_INPUT_BYTES/MAX_OUTPUT_TOKENS`。数据库共享账本在发送前扣预留，失败、中断和重启不会自动退款；调整配置后仍累计原使用量。Jev 没有同等输出 token 限制，因此这些配额不是供应商金额硬上限。

人工纳入/排除必须填写原因、profile 版本、实际阅读层级、身份/研究对象/方法角色，以及主题、日期、载体、文类、版本等短证据和主源定位。未知不等于否定；仅取得摘要不等于已读；模型阴性不自动彻底排除。证据不足保存待核。重新采集不覆盖人工结论，个人隐藏也不改变共享决定。

## 升级与验收

运行新代码前显式应用 `20261105000000_topic_search_routes.sql` 和 `20261106000000_retrieval_protocol_audit.sql`。前者增加路线与发现路径，后者增加协议、人工证据和长期运行/请求/出现账本。Ent 生成代码与 atlas.sum 随源码提交。迁移不清库、不删除旧证据、不引入物理外键。不要重复初始化管理员或把测试指向生产数据库。

离线测试覆盖去重、历史分区、冻结恢复、扩词回溯、新计划同窗、活动运行优先、手动窗口水位、来源缺口、字段编译、旧客户端兼容和路径幂等/截断。真实 PostgreSQL、真实浏览器与上游 canary 是独立验收层；缺少测试数据库时的 skip 不算数据库通过。

PgBouncer 等事务池不能依赖会话级 `SET search_path`。跨进程测试已改为每 phase 显式事务、`SET LOCAL`、schema断言与真实提交；其他数据库测试也应使用唯一隔离 schema，不要裸跑生产迁移来验收。浏览器测试原有 loopback/专用数据库防护继续保留，不能把外部测试库硬塞入受保护的本地 seed 流程。

已交付变化与关键验证见 [Changelog](../../CHANGELOG.md#2026-11-06)，剩余工作统一维护在 [Roadmap](../roadmap.md)。启用模型前检查历史范围、待处理队列和预算。
