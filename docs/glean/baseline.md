# 历史基线：来源、快照与可重放导入

## 已交付的真实历史记录

本仓库包含用户此前整理、于 2026-10-26 交付的三个最新版 HTML 的**全部 39,495 条主题成员关系**，不是演示数据，也不只保留正式纳入记录。

| 主题 | 主题标识 | 成员关系 |
|---|---|---:|
| 生物医学 AI | `biomed-ai` | 31,399 |
| LLM / Agent 知识工程 | `llm-agent` | 4,059 |
| 生物医学世界模型 | `biomed-worldmodel` | 4,037 |

`data/baselines/manifest.json` 是机器可验证清单，记录权威 HTML SHA-256、原始 `catalog-data` JSON SHA-256、逐主题原始 ID 集合 SHA-256、压缩/解压分片 SHA-256、数量、原分类分布、日级正式日期数量和隐私来源字段脱敏次数。HTML 文件本身不修改、不纳入仓库。

38,989 是按显式 DOI/arXiv 得出的**候选全局身份数**，不是已写入数据库数量。全局身份冲突数和逐条导入结果分别记录；仅导入报告的 `created` / `existing` / `quarantined` 能说明一次实际数据库写入的结果。相同题名不自动合并，跨主题原始 ID 均保留。

## 保存边界

- 可提交：全部规范化记录的 `*.ndjson.gz` 分片，单片不超过 2,000,000 字节；精确复用旧阅读器的世界模型显示分组表；清单；少量真实来源测试样本。
- 不提交：默认 `.baseline/archive/*.catalog.json.gz` 保存的**字节级原始 catalog-data JSON**，以及 `.baseline/` 下的导入报告、隔离记录。目录已被根 `.gitignore` 忽略，归档以限制权限创建。离开当前执行环境前应按项目私有数据保留策略另行备份；Git 中的脱敏快照不能替代完整归档。
- `raw` 保留原科学内容、原 ID、原分类、原核读旗标、原证据 URL、相对缓存出处和未知值。绝对私人路径、内部追踪 URL/引用、Library ID、凭据字段替换为显式 `[REDACTED:…]`。清单记录脱敏数量；不宣称脱敏后的 `raw` 与私有归档逐字一致。
- 生物医学 AI 的原 HTML 使用 `packed.strings/columns/rows`：负整数是字符串索引，真实负数由 `{"$number":…}` 转义。提取器按原阅读器结构解包、检查索引与行宽，不执行 HTML/JavaScript。

## 规范化不重新裁决科学事实

1. LLM / 生物医学使用 `title`；世界模型使用 `title_en`。作者、中文题名、DOI、arXiv、原刊会、年份、原文类和 URL 均映射为查询字段，原值仍在 `raw`。
2. 全局去重只使用规范化后的显式 DOI 或 arXiv（可去除 arXiv 版本号）；无标识时使用 `(topic, originalId)`。同题异文不会被合并。不同 DOI/arXiv 对同一身份产生冲突时，数据库服务拒绝并隔离，不以题名或“最可能”强行覆盖。
3. 只有已有明确核验标记、日级精度、类型为正式在线/见刊的日期进入 `formalOnlineDate` / `issueDate`。月、年精度及歧义值只留在证据中，不补齐为月初/年初；同类已核事件有多个不同日值且无原明确选定日时不擅自选最早一天（例如 LLM `P3422` 的两个在线日期均保留、typed 日期为空）；投稿、录用、索引时间不当作正式发表时间。年份查询字段不等价于已通过正式日期审核。
4. `abstract_fetched`、`abstract_read`、`method_checked`、`fulltext_accessed` 和 pending/false/unknown 原值不提升为 true。历史科学核读状态与用户个人 `unread/reading/read` 分离；个人状态默认 unread。导入不执行任何 LLM 调用，历史记录 `isMock=false` 不代表由本次导入重新阅读过。
5. `dimensions` 保存各主题原筛选字段。世界模型的 `display_type` / `display_scale` / `display_tasks` / `display_roles` 精确使用交付 HTML 中的 `UI_TAXONOMY` JSON 映射；`display_scope` 复用旧阅读器的显示分桶；`display_review_status` 复用原状态字段或旧阅读器基于明确旗标的标签。它们仅供显示分组，不覆盖 `raw`，不改变纳入资格。其映射表哈希单独记录。
6. 载体注册表保留原 JIF/CCF、原指标年、出处和证据。每主题/证据摘要形成稳定版本，避免跨主题先写值覆盖后写证据。没有原显式载体达标旗标时仍为 pending；JIF 数字不被自动当作新核验。`evidence.sourceTopics` 可让主题筛选显示待核载体。

## 使用

在仓库根目录执行，Go 版本以 `go.mod` 为准：

```sh
# 无数据库副作用：核验所有 39,495 条记录与分片哈希
 go run ./tools/importbaseline -mode verify

# 重新提取：三个参数必须指向经授权的最新版原文件；程序核对固定哈希和数量。
 go run ./tools/importbaseline -mode extract \
   -source 'llm-agent=/path/to/llm-latest.html' \
   -source 'biomed-ai=/path/to/biomed-latest.html' \
   -source 'biomed-worldmodel=/path/to/worldmodel-latest.html'
```

数据库须由项目标准流程先完成迁移。工具**不自动迁移、建管理员、启动抓取或修改历史来源文件**。

```sh
# 在私有环境中设置 GLEAN_BASELINE_DATABASE_URL；不要把带密码 URL 写入命令、文档或日志。
# 共享数据库要求 sslmode=verify-full。node 必须与所有运行实例不同。
 go run ./tools/importbaseline -mode import -node 17 -operator '<existing-user-id>'

# 仅回环地址开发数据库可显式允许 sslmode=disable：
 go run ./tools/importbaseline -mode import -node 17 -operator '<existing-user-id>' -allow-local-insecure
```

默认先核验所有分片，再逐条调用 catalog 服务。一条记录的论文/主题成员关系由一个事务提交；相同 `(topic, originalId)` 重放为 no-op，不覆盖历史元数据。载体注册也是稳定版本幂等写入。

默认结果在 `.baseline/import-report.json`，失败记录在 `.baseline/quarantine.ndjson`。失败不会静默丢弃：`attempted = created + existing + quarantined`（未中断时），有任何隔离记录则进程非零退出、`complete=false`。修复数据库或人工确认冲突后重跑，同一来源安全跳过已成功记录。用 `-report` / `-quarantine` 指定独立私有路径可保留每次执行的报告。报告仅记录安全的失败类别，不写数据库连接细节；隔离记录本身仍按私有数据处理。

## 验证范围

```sh
 go test ./internal/baseline ./tools/importbaseline ./internal/modules/catalog -count=1
 go vet ./internal/baseline ./tools/importbaseline
```

测试覆盖真实完整快照数量/哈希/原 ID 集、packed 解码、日/月精度和录用日期隔离、原 false/pending 保留、隐私来源脱敏、同题不去重、标识冲突、分片篡改、失败隔离、重复运行和部分失败后重跑。小型真实样本只用于方便业务测试；完整历史快照才是基线。无真实 PostgreSQL 连接的测试不等于已向某个数据库导入，实际数据库成功数量必须引用相应导入报告。
