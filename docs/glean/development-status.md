# 开发进度检查点

截至 2026-11-02 00:57 UTC；记录自 [`daaa529`](https://github.com/TeamSillyBees/glean/commit/daaa529)（10 月 28 日）以来的推进，**不是项目完成或上线声明**。后续继续按[产品目标](product-goals.md)开发与验收。

## 已完成与已验证

- 界面：次级页面按需加载；区分空主题、无精选、筛选无结果与查询失败。访问权限查询遇 503 可重试；已有成功授权的正常后台复查不会丢失编辑草稿。
- 查询：增加完整历史库诊断；facets 关联批次调为 5000，主题计数改为分组统计，保持精确过滤语义。[运行 41276375649](https://github.com/TeamSillyBees/glean/actions/runs/41276375649)（[`844d93f`](https://github.com/TeamSillyBees/glean/commit/844d93f)）五项全通过，含 24 个 fixture 与完整历史结果等价性检查；实测中位数、SQL 次数及限制见[查询诊断](query-performance.md)，不将减少 SQL 次数冒充普遍加速。
- 可靠性：bioRxiv / medRxiv total 漂移有界重放，不完整不推进水位；数据库显式端口 0 被拒绝。[运行 41277577603](https://github.com/TeamSillyBees/glean/actions/runs/41277577603)（[`29706d2`](https://github.com/TeamSillyBees/glean/commit/29706d2)）五项全通过，含真实后端访问故障恢复和端口检查。

- 原始来源证据的候选键保留精确数字；详情正文延迟加载，无障碍弹窗外壳常驻。完整静态 JS 依赖图可复测：同口径干净基线 512,469 → 475,740 字节，gzip 160,258 → 151,168 字节；不是网络耗时。方法见 [Web 构建记录](../../web/README.md#按需加载与构建记录)。本地及 CI 的 171 项测试通过。
- [`4d521a6`](https://github.com/TeamSillyBees/glean/commit/4d521a6) 的[运行 41278404583](https://github.com/TeamSillyBees/glean/actions/runs/41278404583)五项全通过，含四项真实后端浏览器测试及详情资源首次使用加载检查；桌面/手机实际截图复核未发现阻塞问题。

## CI 选择门验收

历史查询诊断的保守 CI 选择门在 [`8a9bf5c`](https://github.com/TeamSillyBees/glean/commit/8a9bf5c) 的[运行 41279002580](https://github.com/TeamSillyBees/glean/actions/runs/41279002580)五项全通过：相关变更分支实际选择 RUN，12 项选择器/通过断言测试通过，完整诊断产物已生成。仅文档变更分支也已验证：[`4e3db8b`](https://github.com/TeamSillyBees/glean/commit/4e3db8b) 的[运行 41279691424](https://github.com/TeamSillyBees/glean/actions/runs/41279691424)实际选择 SKIP，仅跳过全历史性能诊断；五个常规任务均成功，完整历史正确性与浏览器验收仍执行。

## 不变边界与后续条件

历史基线仍为 39,495 条主题成员关系、38,989 个身份候选，不是重新科学核验。六个真实来源已实现，但离线 CI、手动实时 API 验收与完整覆盖不同；LLM 仍明确标记 mock。细节见[来源边界](real-sources.md)与 [API 契约](api-contract.md)。

2026-11-02 00:41:40 UTC，Starxy 通过 `workflow_dispatch` 触发[独立实源运行 41368520709](https://github.com/TeamSillyBees/glean/actions/runs/41368520709)（[`4295f6a`](https://github.com/TeamSillyBees/glean/commit/4295f6a)），两个任务成功：六源有界兼容性探针均通过；Europe PMC 一条真实观察经 PostgreSQL、API 到桌面/手机，两项浏览器测试通过。此结果取代此前未执行实源验收的状态，不代表六源均有论文命中、六源全链路入库、完整分页或持续可达；LLM 仍为 mock，实际服务器未部署。安全产物与范围见[来源验证记录](real-sources.md#已完成的手动验收)。

- [x] CI 选择门的相关变更 RUN 与仅文档 SKIP 两条分支均已验收。
- [ ] 实际服务器尚未部署；上线前需用户提供目标、访问条件、私有配置/密钥及生产授权，并按[生产指南](production.md)演练。
- [ ] 真实 LLM 接入需确定提供方/模型及密钥；秘密不得进入仓库。这些条件不阻断其他开发。
