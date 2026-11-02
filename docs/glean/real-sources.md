# 真实公开文献源

Glean 的文献发现使用真实公开 API；不需要用户邮箱、来源 API key 或 LLM key。唯一允许的模拟环节是明确标识的 LLM 阅读/总结。API 命中、入库、元数据丰富都**不等于科学审核通过**，也不自动证明 JIF / CCF / 正式卷期资格。

## 来源与增量语义

| 来源 | 官方服务 | 查询与恢复 |
|---|---|---|
| Crossref | `https://api.crossref.org` | 固定 `from-index-date` / `until-index-date`，UTC 秒精度（API 不接受末尾 `Z`）。普通发现逐个 OR seed 查询取并集；核心期刊用 `/journals/{ISSN}/works`，不把全部同义词 AND 在一起。逐 feed 保存 cursor / 已读数量。 |
| PubMed | `https://eutils.ncbi.nlm.nih.gov` | `CRDT OR LR`：首次创建和最后修订，而不是可能回填的 EDAT / 出版日。ESearch 创建 history，EFetch 分页。超过 9,999 条先二分日期；单日仍太大则二分 PMID 区间，最高区间不假定 PMID 上界。 |
| arXiv | `https://oaipmh.arxiv.org/oai` | OAI-PMH `ListRecords` + `arXivRaw`，按元数据修改日逐日采集，保留全部版本日期。无需受 Atom 30,000 条搜索上限限制；旧稿新版本也能发现。每日过期的 token 只重放未完成日。 |
| bioRxiv / medRxiv | `https://api.biorxiv.org/details/{server}/…` | 日期区间包括新稿和修订版。按响应的 cursor / count / total 翻页；当前实际 details 每页 30 条，不能硬编码 100。`no posts found` 只在初始空页被视为明确空区间。 |
| Europe PMC | `https://www.ebi.ac.uk/europepmc/webservices/rest/search` | `FIRST_IDATE OR UPDATE_DATE`，`resultType=core`，cursorMark 深分页。PubMed、PMC、预印本等身份保持 source 前缀；MED 记录另存 PMID 便于去重。 |

Crossref / Europe PMC 的可变索引数量在分页过程中变化时，最多重放当前 feed 三次，绝不在不完整时推进水位；PubMed 过期 history 同样只重放当前分区。仍无法完成会明确失败并保留未完成进度，不声称采集成功。

bioRxiv / medRxiv 的内部 `rx1` 游标保存原始条目 offset、该轮首次响应 total 和重放次数，上游 URL 仍仅传数字 offset。分页总数增加或减少（包括续页变为空区间）时丢弃该页并从原固定窗口 offset 0 重放，最多三次；持续漂移则失败关闭，保留游标和重试上下文，不推进采集水位。旧数字游标没有 total 证据，迁移时先保存一次有界重放，不沿用旧前缀。重放可能重复返回记录，由已有幂等键去重。**total 不变不代表上游提供快照一致性**：等量替换、重排仍可能无法被数量检查发现；固定查询窗口和重叠补漏不能升级为绝对不漏的保证。

核心期刊补查使用登记的 ISSN 全刊范围：Crossref、PubMed、Europe PMC 均不再将主题关键词 AND 到核心期刊查询；普通扩展检索仍使用主题词。核心全刊记录全部保留为候选，不作本地关键词硬过滤；其主题关联仍待审核，不能据此进入精选集合。PubMed / Europe PMC 对排序去重后的 ISSN 按每批最多 40 项查询，游标保存批次索引和登记快照摘要；只在全部批次的分页完成后完成核心补查。单批的 history 过期或索引数量变化只重放当前批次。旧版带关键词、未分批或登记快照不同的核心游标会从原固定窗口重放，不能沿用不兼容的查询后声称全刊完成。

有效 JSON 分页中的单条字段类型错误、缺失身份或必要索引时间错误，不再阻塞同页其他记录：原始条目和可辨认来源身份（缺失时使用仅用于隔离的 SHA-256 标识）连同安全错误类别进入隔离队列，不进入 LLM 审核或发布。分页按供应商原始条目数推进，而非通过验证的数量。PubMed 同样逐条解析 XML：合法 `PubmedBookArticle` 目前明确标为不支持的类型并隔离，保留 PMID / 原文且计入分页数量，不猜测为期刊论文；普通文章的坏字段也只影响该条。arXiv OAI 的单条修改日期、身份或版本历史错误同样隔离并保留原始 XML，不丢弃同页有效论文，仍按原 resumptionToken 继续；身份冲突时只使用隔离哈希，不猜测论文归属。损坏的顶层响应、数量或游标仍整页失败并保留进度。缺少或只有年月的发表日期只是未知日期，不隔离整篇论文。

## 日期与证据边界

- 监控窗口是 `[Start, End)`；End 在运行启动时固定。Crossref 索引时间精确到秒，返回记录还需通过半开边界检查。
- 其余来源的日期过滤以 UTC 自然日为粒度，发送 `date(Start)` 到 `date(End−1ns)` 的闭合范围。下界可多取同一天记录，通过幂等键去重；不会因 Start 位于午间而丢掉同日回填。
- 当前日的查询只表示“该次请求可见的数据已完成分页”，**不表示当天所有未来更新已收齐**。运行保留 7 日重叠窗口补漏。前端应显示“检索上界 / 已完成分页”，不是“当天全部收齐”。
- PubMed ESearch history 固定的是命中 PMID 集合，不是元数据的历史版本。EFetch 可能返回已在 End 之后修订的最新元数据；只要 PMID 是权威查询命中，保留该修订证据，不能据 DateRevised 将它丢掉，也不把后来修订日冒充发表日。
- Europe PMC 的 `dateOfRevision` 是引用源的修订日，不等于 Europe PMC `UPDATE_DATE`。真实响应中存在 10 月 23 日首次索引、10 月 24 日修订的记录；不能据后者丢弃服务端已命中的记录。
- 精确正式日期只由明确的 online / issue 出版证据产生。接受日、创建日、索引日不算正式出版。
- PubMed `aheadofprint` 可能在 `JournalIssue/PubDate` 重复电子发表日；不能据它伪造正式卷期。
- Europe PMC 的 `firstPublicationDate` 会补齐缺失月/日；真实响应的 `dateOfPublication: "2026 Oct"` 还会变成 `printPublicationDate: "2026-10-01"`。后者只有在原始显示日期也具有相同完整年月日，且有卷/期信息时才作为 issue 证据。
- arXiv / bioRxiv / medRxiv 版本日期都是预印本事件。bioRxiv 的 `published` 关联 DOI 保留在原始证据中，不能替换预印本 DOI，也不能生成不存在的正式日期。
- `SourceURL` 指向可复取的官方元数据，`URL` 指向论文/出版商页面，两者分开保存。

## 请求边界

业务只使用启动层注入的 `httpclient.Executor`；每次 Fetch 最多一次 HTTP 请求。PubMed ESearch 与 EFetch 是两个可恢复步骤。官方客户端关闭底层自动重试，响应体上限 16 MiB，超时与取消传递到请求。来源层遵循持久退避，429 / 503 的 `Retry-After` 不被即时重试绕过；401 / 403 不尝试更换身份或绕过 WAF。

正式装配的请求 Hook 对每次 RoundTrip（包括重定向）检查来源对应的 HTTPS origin，限制为 GET，不允许用户凭据。请求串行保守节流至少 3 秒；bioRxiv 与 medRxiv 共用 provider 节流门，arXiv 不并发。

## 验证：不要混淆三个层次

1. 合成单元 fixture：只用于错误、边界与恢复测试，文件名/内容明确标注 synthetic。
2. 原样公开 API 捕获：`internal/modules/monitor/testdata/live/`；gzip 解压后的响应字节不可编辑，`manifest.json` 记录请求、UTC 捕获时间、HTTP 状态、字节数与 SHA-256。测试验证摘要后再检查解析。离线 fixture 测试不代表今天网络仍通畅。
3. 真实 Go HTTP canary：仅在 `live` build tag 加显式环境变量双重 opt-in 时编译并运行，不进入默认 `go test ./...` 或常规 CI，使用同一平台 Factory、官方 origin Hook 和来源适配器。每来源最多 2 次请求；支持的 API 请求 1 条，bio/medRxiv 和 OAI 使用供应商固定页大小。只读，不追完所有分页，不运行 LLM、不写生产目录。

```sh
GLEAN_LIVE_SOURCE_CANARY=1 go test -tags=live ./internal/modules/monitor -run '^TestLiveSourceCanary$' -v -count=1
# 或仅检查一个来源，避免不必要地重复访问全部来源：
GLEAN_LIVE_SOURCE_CANARY=1 GLEAN_LIVE_SOURCE=pubmed go test -tags=live ./internal/modules/monitor -run '^TestLiveSourceCanary$' -v -count=1
```

`.github/workflows/live-source-acceptance.yml` 仅支持 Actions 手动 `workflow_dispatch`，不由 push、PR 或定时触发。两个独立 job 互不依赖，一个来源失败不会阻止另一个 job：

- `six-source-canary`：六来源兼容性探针，直接运行上述 `TestLiveSourceCanary`。固定 `[2026-10-23, 2026-10-24)` UTC 一日窗口；每来源一次 Fetch，PubMed 最多额外一次 EFetch，总计 **最多 7 次 Fetch / HTTP 请求**，间隔至少 3 秒，禁止重试及重定向，不使用 API key、联系人、LLM 或数据库。请求 page size 1，bio/medRxiv 与 OAI 仍为供应商页大小；不追完分页。合法空页可通过兼容性检查（PubMed 已执行 EFetch 时要求至少一条记录），不代表六源均有论文命中。
- `live-source`：**仅 Europe PMC 一来源**，一页、请求一条记录，通过真实监控管线写入临时 PostgreSQL，再在桌面/手机浏览器验证。这里只模拟 LLM 审核；必须真实提交恰好一条观察，零结果也失败。不是六源全链路持久化验收，不导入历史库，不替代常规 CI 的完整历史导入和浏览器验证。

六源 job 从 Go JSON 事件生成来源 pass/fail/skip（未完成为 not-run）及耗时表，写入 job summary 和 7 日保留的安全 Markdown artifact；不发布原始测试 Output、请求头、响应正文、凭据或 stderr。失败时另从受控标记中提取 `SourceError` 的固定 kind、HTTP 状态、retryable 与 Retry-After 秒数（无错误文本）；可区分 503 / 429、响应格式和传输失败，不由诊断元数据推断通过。非法或重复诊断被丢弃，缺少诊断表示原因未知，而非成功。缺失/跳过测试、损坏报告或非零测试退出均失败关闭。上游 503、限流、超时会使相应手动检查诚实失败，但**不自动等于代码回归**；不使用 `continue-on-error`、即时重试或 fixture 替代真实响应，也不影响普通 CI 的离线正确性判定。

Canary 使用固定的小时间窗，不是 30 日全网扫描。捕获的 arXiv 363 条 cs.AI 子集只是该子集的解析证据，不能宣称全 arXiv 覆盖。来源查询、同义词和数据库本身都有覆盖边界；完整分页也不是“全部相关文献”的保证。

## 已完成的手动验收

2026-11-02 00:41:40 UTC，Starxy 手动触发[运行 41368520709](https://github.com/TeamSillyBees/glean/actions/runs/41368520709)（提交 [`4295f6a`](https://github.com/TeamSillyBees/glean/commit/4295f6a)），两个独立 job 均成功。[安全摘要 artifact 13107988478](https://github.com/TeamSillyBees/glean/actions/runs/41368520709/artifacts/13107988478)记录 Crossref、PubMed、arXiv、bioRxiv、medRxiv、Europe PMC 六项探针均 pass；产物按上述策略仅保留 7 日。Europe PMC 单条真实观察另通过临时 PostgreSQL、真实 Go API 与桌面/手机浏览器，两项浏览器测试通过，只有 LLM 审核为 mock。

这是固定窗口、请求预算内的一次兼容性及单来源全链路证据，不代表六源均有论文命中、六源均已入库、完整分页、持续可达或全部相关文献覆盖；不改变历史科学结论，也不是生产部署验收。常规 CI 通过仍不等于实时来源可达。

## 官方依据

- [Crossref REST API](https://www.crossref.org/documentation/retrieve-metadata/rest-api/) 与 [2026 年 cursor / index-date 变更说明](https://community.crossref.org/t/changes-to-cursors-filtering-and-sorting-in-the-rest-api/16246)
- [NCBI 更新后的 PubMed E-utilities：UID 二分范围策略](https://ncbiinsights.ncbi.nlm.nih.gov/2022/11/22/updated-pubmed-eutilities-live/)；[PubMed 字段说明](https://pubmed.ncbi.nlm.nih.gov/help/)
- [arXiv OAI-PMH：2025 年新端点、datestamp、arXivRaw 和游标](https://info.arxiv.org/help/oa/index.html)
- [bioRxiv / medRxiv 官方 API](https://api.biorxiv.org/)
- [Europe PMC API](https://europepmc.org/RestfulWebService)；[官方开发者论坛对 FIRST_IDATE / UPDATE_DATE 的解释](https://groups.google.com/a/ebi.ac.uk/g/epmc-webservices/c/JShd-dbIIBE)

## 来源数字证据与候选散列

监控候选与目录观察均以 `json.Number` 规范原始 JSON：忽略空白与对象键顺序，但保留大整数、精细小数和指数数字的完整写法，不经 `float64` 舍入。`1`、`1.0`、`1e0` 因来源写法不同可形成不同证据版本；这不是论文身份变化或科学审核结论。缺少 raw 的 nil / 空字节切片具有相同散列语义；非空非法 JSON 仍先进入隔离，不接受有效前缀。

本修复不重写已持久化的候选键、运行引用、分页游标或水位，不需要清库、迁移或重置。待审核/待发布的旧候选按原键继续恢复；使用新规则再次采集相同原文保持幂等。旧版已舍入的大数字或曾改写的数字格式（例如 `1.0` → `1`）再次出现时，可能新增一个候选并重新投影；目录已有独立的精确数字散列，会去重相同的来源观察。普通整数及原本规范的常见数字键不变。此前被旧碰撞丢弃且不再被来源返回的证据不能凭此修复自动恢复；不据此宣称历史采集完整。

## 持续采集恢复与健康诊断

普通 CI 的 `TestDurableRecoveryAfterCancellationAndPartialFailure` 在无外部网络的确定性来源上验证：一个来源完成、另一个来源留下分页游标后取消；重建服务，503 耗尽预算；再次重建并只恢复失败来源。断点、固定窗口和冻结主题配置保持不变，成功来源不重复采集，失败来源不提前推进水位，重复恢复不增加候选或运行。相同矩阵也作为 `TestMonitorPostgresDurableMockRun/CancellationPartialFailureFrozenTemplates` 在真实 PostgreSQL 迁移表和 JSONB 上执行；每次重建新的 Ent client，不借用 Service 内存状态。另有 `TestMonitorPostgresProcessRestart` 使用两个独立测试二进制子进程：第一个以假 Europe PMC HTTP 响应提交分页检查点后退出；第二个重新连接 PostgreSQL，在当前主题配置不可用时按原始冻结查询和游标恢复，并验证重复执行不产生重复候选。该测试使用已提交的独立 schema，结束后清理；不声称进行了生产进程强制 kill 或持续运行 72 小时的现场验收。普通 CI 的 `TestEuropePMCHTTPResumeAfter503` 还用真实适配器和假 HTTP 503 响应验证重建服务后仍遵守持久化 Retry-After，不提前请求或推进水位。

来源健康页面分别显示实际完整采集成功距今、窗口覆盖距今、失败来源名称、隔离记录总数与每来源计数。`lastAcquiredAt` 仅在一个来源所有模板分页完成时写入实际成功时间；它不是论文发表时间，也不是 `acquiredThrough` 固定窗口结束时间。旧状态没有成功时间时显示未知，不从窗口水位推造时间。最近成功的单一来源不代表全部来源成功，也不代表目录写入已完成。刷新失败保留上次诊断，并明确提示可能过期；页面只读刷新不启动采集。

真实公开 API 验收仍只通过独立 `workflow_dispatch` 工作流执行，不进入普通 CI。Europe PMC 等上游临时 503 属于来源可用性/退避证据，不自动归因于代码失败；离线恢复回归和必跑 PostgreSQL 集成测试仍须通过。
