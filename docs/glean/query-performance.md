# 历史目录查询诊断

先测量，再决定是否改写 SQL 或增加索引。`TestCatalogHistoricalQueryDiagnostics`
复用目录 PostgreSQL 测试的隔离 schema、真实迁移和 savepoint 驱动；结束后整体回滚。
它核验全部提交分片并通过正常导入服务**仅导入一次 39,495 条主题成员关系**，
所有查询场景共享该快照，不请求任何外部文献 API，也不写入已有应用表。
需要具备建 schema 权限的**专用测试数据库**，不要连接生产数据库。

## 运行

在私有终端设置 `INITRA_TEST_DATABASE_URL` 指向测试 PostgreSQL，然后在仓库根目录执行：

```sh
GLEAN_CATALOG_QUERY_DIAGNOSTICS=1 go test ./internal/modules/catalog \
  -run '^TestCatalogHistoricalQueryDiagnostics$' -count=1 -v
```

未设置开关或数据库 URL 时明确跳过。普通 `go test ./...` 不额外导入全库；
正常 CI 的 `verify` job 显式开启诊断，并强制检查该测试确实通过。
CI 保存 `catalog-query-diagnostics` artifact（14 天），仅含版本、数量和测量行，
不记录 DSN、SQL 参数、题名或历史原始内容。浏览器验收使用另一个数据库，仍独立导入；
此测试不为每个场景重新导入，也不额外启动第二个诊断进程。

## 测量口径

- 输出 Go/PostgreSQL 版本、可见 CPU 数、真实成员/去重论文数及导入耗时。
- 对隔离 schema 的相关表显式 `ANALYZE`，避免新导入且未提交的数据缺少统计信息。
- 场景：主题计数、详情、全库/精选列表、主题+年份+载体+精确 JSONB 分类组合筛选、深分页、文本搜索，
  五种排序，以及全库/精选/主题/组合筛选 facets。
- 每场景一次预热，随后固定三次串行样本；记录 `elapsed_ms`、`sql_queries`、`matched`。
  `matched` 对列表/facets 为匹配论文数，对主题为主题数，对详情为 1。
  SQL 计数拦截实际 Ent driver Query 调用，包括 Count/关联加载，不把导入、ANALYZE 或预热计入。
- 仅断言导入完整、场景无错误、非空、结果数量稳定和确有 SQL；全库列表/facets 还对照数据库论文总数。
  **没有毫秒阈值，也不固定当前 SQL 次数为性能契约**。8 分钟 context 是整个诊断的资源预算，
  不是请求延迟 SLO；CI 原有 job 超时仍有效。
- 这是服务层、单连接外层事务内的热缓存诊断，包含结果组装及轻量断言开销，不含 HTTP/网络客户端耗时；
  不代表生产并发、冷缓存、磁盘大小、连接池竞争或已有大量增量 observation/人工审核的负载。
  三个样本不能冒充 p95。导入时间也不是独立提交事务的生产导入吞吐。

## 已确认的代码结构（非实测结论）

`Topics` 读取主题配置后，对全体成员按主题执行四次分组 Count，共五次 SQL，不随主题数增加。
统计成员行而非去重论文；历史数仍按原始 historical 状态统计，精选/待审沿用原有逐主题人工审核及精确 JSONB 谓词。
此前三个主题的诊断为 13 次 SQL、约 194–198 ms；新版本耗时须等待同口径 CI，不据查询数推算加速。
`Facets` 首先读取所有匹配论文；每批有一次 correction 查询，主循环再查询
membership、review、observation 分组、个人 state，共**五次关联查询/批**，另有主题检查等固定开销。
因此 facets 不受列表的 pageSize 限制。这说明值得测量，但不能仅凭查询次数断言哪一段耗时最大。

第一轮 CI 实测（优化前，1000 条/批）：全库 facets 38,989 篇，196 次 SQL、约 2023–2072 ms；
主题 31,399 篇，161 次 SQL、约 1653–1713 ms；精选 10,844 篇，56 次 SQL、约 708–772 ms；
组合筛选 1 篇，6 次 SQL、约 39–40 ms。这是该次 runner 的三个热样本，不是生产 SLO。

## 有界批次对照

默认关联批次候选调整为 **5000**，不改 SQL 谓词、集合语义、schema 或索引。
correction、membership、review、observation 分组和个人 state 都使用相同批大小；
各查询最多 5000 个论文 ID，加少量固定主题/collection/user 参数，远低于 PostgreSQL 65,535 参数上限。
批大小仅是内部测试入口参数，不是生产配置。初始匹配论文的加载及最终聚合仍与原来相同，
并非流式处理；单次关联结果可能更大，不能将减少往返次数等同于必然降低内存或耗时。

完整快照诊断在**同一进程、同一导入和同一数据库**测量 1000 与 5000 两个候选，
场景名包含 `facets/.../batch-1000` / `batch-5000`；每个候选各一次预热和三次样本。
所有样本断言完整 `FacetsVO` 与 1000 基准一致（含全部维度、数量及每个切片的排序），
不是只比较匹配数量。查询次数如实输出，不设置硬件时间门槛；若实测无收益或回退，继续调小或撤回默认候选。
另有小型 PostgreSQL fixture 覆盖多主题、人工审核/标签、纠正年份/载体、重复 observation、
两用户阅读/隐藏和活跃维度/tier/collection 筛选，并用 1/2 条小批次强制穿过关联边界。
完整快照负责真实 1000/5000 多批次边界。

5000 候选的真实性能以正常 CI artifact 为准；没有 PostgreSQL 的本地通过/跳过不算实测。
后续比较应固定提交、数据库版本、runner 和快照，保留逐样本原始值；再针对高耗时场景用隔离测试库
收集 `EXPLAIN (ANALYZE, BUFFERS)`，同时检查过滤语义与结果一致性，之后才讨论 SQL/索引优化。
