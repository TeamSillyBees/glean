# 查询性能诊断

`TestCatalogHistoricalQueryDiagnostics` 在专用 PostgreSQL 中使用完整历史基线，核对查询结果并记录耗时与 SQL 次数。一次导入 39,495 条主题成员关系，所有场景共享快照；不访问外部文献 API。

## 运行

私密设置 `INITRA_TEST_DATABASE_URL` 指向具备建 schema 权限的专用测试数据库，再在仓库根目录执行（Bash）：

```bash
GLEAN_CATALOG_QUERY_DIAGNOSTICS=1 go test ./internal/modules/catalog \
  -run '^TestCatalogHistoricalQueryDiagnostics$' -count=1 -v
```

测试使用真实迁移、独立 schema/事务和 savepoint，结束回滚；不要指向生产库。未设置开关或数据库 URL 时明确 skip。

## 场景与口径

- 记录 Go/PostgreSQL 版本、CPU 数、实际论文/成员数量和导入耗时。
- 对相关表执行 `ANALYZE`，覆盖主题统计、详情、目录列表、组合筛选、深分页、文本搜索、排序与 facets。
- 每场景一次预热，随后三次串行样本，记录 `elapsed_ms`、`sql_queries`、`matched`。
- SQL 计数来自实际 Ent driver Query，包括 Count 和关联加载；不包含导入、ANALYZE 或预热。
- 断言完整导入、查询无错误、数量稳定和结果等价；不设置硬件相关毫秒门槛，也不固定 SQL 次数为 API 契约。

这是服务层、单连接外层事务中的热缓存诊断，包含组装结果的开销，不包含 HTTP/客户端网络。三次样本不能作为 p95，也不能代表生产并发、冷缓存、连接池竞争或大量后续观察/审核的负载。8 分钟 context 是整个测试的资源预算，不是接口延迟目标。

## 关联批次比较

当前 facets 关联批次为 5000，correction、membership、review、observation 和个人 state 共用该批次。初始匹配论文仍整体加载，不能据减少查询次数推断内存下降。

完整快照在同一次导入、同一进程与数据库中比较 1000 和 5000 两种批次，每组均预热并采样；完整 `FacetsVO`（维度、数量和排序）必须等价。小型 PostgreSQL fixture 另覆盖多主题、人工审核、纠错、重复观察和两用户个人状态。

对比时固定提交、数据库版本与数据快照，保留逐样本结果；需要定位 SQL 开销时在隔离库收集 `EXPLAIN (ANALYZE, BUFFERS)`，同时检查过滤与排序语义。

## CI 行为

main push 和手动 CI 执行完整诊断。PR 的完整 Git diff 只有被保守选择器证明符合允许范围时，才可跳过该耗时测试；`query-performance.md`、测试、后端、schema、数据、工作流和未知路径始终要求诊断。缺失历史、浅克隆、无效 SHA、失败或空 diff 不作为跳过依据。

纯文档 PR 是否跳过全部运行时任务由另一选择门决定，规则见[开发指南](../development.md#ci-与外部验收)。只有性能诊断被跳过时，其他历史正确性、数据库、浏览器与恢复测试仍执行。

CI 的 `catalog-query-diagnostics` artifact 保留 14 天，只含版本、数量和测量行，不记录 DSN、题名、SQL 参数或历史原文。浏览器使用独立数据库，不复用该诊断的事务。

实现入口：[诊断测试](../../internal/modules/catalog/catalog.performance_postgres_test.go)、[选择器](../../scripts/ci/catalog_diagnostics.py)。
