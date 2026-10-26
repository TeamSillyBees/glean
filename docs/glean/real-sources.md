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
3. 真实 Go HTTP canary：显式 opt-in，使用同一平台 Factory、官方 origin Hook 和来源适配器。每来源最多 2 次请求；支持的 API 请求 1 条，bio/medRxiv 和 OAI 使用供应商固定页大小。只读，不追完所有分页，不运行 LLM、不写生产目录。

```sh
GLEAN_LIVE_SOURCE_CANARY=1 go test ./internal/modules/monitor -run '^TestLiveSourceCanary$' -v -count=1
# 或仅检查一个来源，避免不必要地重复访问全部来源：
GLEAN_LIVE_SOURCE_CANARY=1 GLEAN_LIVE_SOURCE=pubmed go test ./internal/modules/monitor -run '^TestLiveSourceCanary$' -v -count=1
```

Canary 使用固定的小时间窗，不是 30 日全网扫描。捕获的 arXiv 363 条 cs.AI 子集只是该子集的解析证据，不能宣称全 arXiv 覆盖。来源查询、同义词和数据库本身都有覆盖边界；完整分页也不是“全部相关文献”的保证。

## 官方依据

- [Crossref REST API](https://www.crossref.org/documentation/retrieve-metadata/rest-api/) 与 [2026 年 cursor / index-date 变更说明](https://community.crossref.org/t/changes-to-cursors-filtering-and-sorting-in-the-rest-api/16246)
- [NCBI 更新后的 PubMed E-utilities：UID 二分范围策略](https://ncbiinsights.ncbi.nlm.nih.gov/2022/11/22/updated-pubmed-eutilities-live/)；[PubMed 字段说明](https://pubmed.ncbi.nlm.nih.gov/help/)
- [arXiv OAI-PMH：2025 年新端点、datestamp、arXivRaw 和游标](https://info.arxiv.org/help/oa/index.html)
- [bioRxiv / medRxiv 官方 API](https://api.biorxiv.org/)
- [Europe PMC API](https://europepmc.org/RestfulWebService)；[官方开发者论坛对 FIRST_IDATE / UPDATE_DATE 的解释](https://groups.google.com/a/ebi.ac.uk/g/epmc-webservices/c/JShd-dbIIBE)
