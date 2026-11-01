# 开发进度检查点

截至 2026-11-01 06:08 UTC；记录自 [`daaa529`](https://github.com/TeamSillyBees/glean/commit/daaa529)（10 月 28 日）以来的推进，**不是项目完成或上线声明**。后续继续按[产品目标](product-goals.md)开发与验收。

## 已完成与已验证

- 界面：次级页面按需加载；区分空主题、无精选、筛选无结果与查询失败。访问权限查询遇 503 可重试；已有成功授权的正常后台复查不会丢失编辑草稿。
- 查询：增加完整历史库诊断；facets 关联批次调为 5000，主题计数改为分组统计，保持精确过滤语义。[运行 41276375649](https://github.com/TeamSillyBees/glean/actions/runs/41276375649)（[`844d93f`](https://github.com/TeamSillyBees/glean/commit/844d93f)）五项全通过，含 24 个 fixture 与完整历史结果等价性检查；实测中位数、SQL 次数及限制见[查询诊断](query-performance.md)，不将减少 SQL 次数冒充普遍加速。
- 可靠性：bioRxiv / medRxiv total 漂移有界重放，不完整不推进水位；数据库显式端口 0 被拒绝。[运行 41277577603](https://github.com/TeamSillyBees/glean/actions/runs/41277577603)（[`29706d2`](https://github.com/TeamSillyBees/glean/commit/29706d2)）五项全通过，含真实后端访问故障恢复和端口检查。

- 原始来源证据的候选键保留精确数字；详情正文延迟加载，无障碍弹窗外壳常驻。完整静态 JS 依赖图可复测：同口径干净基线 512,469 → 475,740 字节，gzip 160,258 → 151,168 字节；不是网络耗时。方法见 [Web 构建记录](../../web/README.md#按需加载与构建记录)。本地及 CI 的 171 项测试通过。
- [`4d521a6`](https://github.com/TeamSillyBees/glean/commit/4d521a6) 的[运行 41278404583](https://github.com/TeamSillyBees/glean/actions/runs/41278404583)五项全通过，含四项真实后端浏览器测试及详情资源首次使用加载检查；桌面/手机实际截图复核未发现阻塞问题。

## 当前待验证

历史查询诊断的保守 CI 选择门在 [`8a9bf5c`](https://github.com/TeamSillyBees/glean/commit/8a9bf5c) 的[运行 41279002580](https://github.com/TeamSillyBees/glean/actions/runs/41279002580)五项全通过：相关变更分支实际选择 RUN，12 项选择器/通过断言测试通过，完整诊断产物已生成。**仅文档变更的跳过分支仍待验收**：本检查点的独立文档提交用于验证该分支，须检查 CI 实际结果，其他常规验收保持执行。

## 不变边界与后续条件

历史基线仍为 39,495 条主题成员关系、38,989 个身份候选，不是重新科学核验。六个真实来源已实现，但离线 CI、手动实时 API 验收与完整覆盖不同，未宣称本轮重新验证全部外部请求；LLM 仍明确标记 mock。细节见[来源边界](real-sources.md)与 [API 契约](api-contract.md)。

本轮网络允许范围不含官方来源 API，未尝试外部请求、未重跑真实来源验收；live-tag 离线编译通过，但未启用的 canary 已跳过，不是实源成功证据。网络可用后应通过 `workflow_dispatch` 手动运行[独立实源验收](../../.github/workflows/live-source-acceptance.yml)；常规 CI 通过不代表实源可达。

- [ ] 完成 CI 选择门的仅文档跳过分支验收，修复发现的问题，再推进后续开发。
- [ ] 实际服务器尚未部署；上线前需用户提供目标、访问条件、私有配置/密钥及生产授权，并按[生产指南](production.md)演练。
- [ ] 真实 LLM 接入需确定提供方/模型及密钥；秘密不得进入仓库。这些条件不阻断其他开发。
