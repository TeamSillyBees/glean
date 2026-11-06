# 论文处理流程与模型配置

真实来源记录先写入 PostgreSQL 与目录，再执行模型处理。未配置模型不妨碍保存和查询新论文，也不生成占位总结。模型默认关闭；当前已实现 Jev 和生成式 LLM 的真实 HTTP adapter。

## 一篇新论文如何处理

| 阶段                 | 作用与边界                                                                                                                          |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `material`           | 获取并保存实际供模型读取的材料、来源 URL、材料块和 SHA-256。无法取得正文时明确降级，不冒称全文。                                    |
| `classification`     | Jev 对记录已关联的发现主题分别分类（相关 / 范围外 / 不确定）和给出 0–1 优先分；不自动遍历全部主题，也不代替科学质量评分或人工纳入。 |
| `summary`            | 生成式 LLM 仅根据实际材料生成中文总结及带材料块 ID、原文引句、数值和引用标识的声明。                                                |
| `independent_review` | 同一生成式服务发起独立请求，重新读取原材料并审查总结/声明，不只检查前次模型意见；不代表独立机构或不同模型的复核。                   |
| `program_validation` | 程序检查证据块、连续原文引句、数值与引用标识是否存在；不能证明语义正确或研究结论成立。                                              |
| `evidence_check`     | Jev 逐条检查声明是否受到材料支持，记录支持分与问题。任何机器检查均不替代人工科学审核。                                              |

完成后默认是 `needs_human_review`。`ready_for_display` 仅表示机器结果可展示，**不等于精选纳入或科学核验**；`monitor.models.auto_display` 默认 false，显式启用也不会变更人工纳入。`scientificReviewed` 不因自动处理变为 true。分类不相关也不会删除真实来源证据。

材料范围：

- `fulltext`：已解析到正文文本；不是宣称附件、图像、公式或每篇论文的全部内容均已读取。
- `partial`：取得部分正文，存在未提取的非文本内容等缺口。
- `abstract_only`：实际仅有摘要与题录；总结必须保留这一限制。
- `metadata_only`：只有题录；不凭标题编造论文总结，摘要阶段报告材料不足。

正文获取限于固定官方路径：Europe PMC 的 PMC `fullTextXML`、带明确版本号的 arXiv HTML。不会任意访问论文中的链接、执行文本指令、绕过付费墙或自动下载任意附件。材料响应上限 4 MiB，共享 30 秒请求预算；没有合适身份、接口不可用（包括 403/404）或材料不完整时保留摘要与原因。未知许可明确记录 `license=unknown`，不冒称已确认再分发权。来源材料是未受信任的数据，不是模型指令。

## 一次配置两个模型端

服务配置位于 `monitor.models.{jev,generative}`，各有 `endpoint`、`model`、`api_key_env`、`timeout`。`monitor.models.enabled` 默认 false，endpoint/model 默认空，不暗中选定付费服务。环境覆盖分别为 `INITRA_MONITOR_MODELS_ENABLED`、`INITRA_MONITOR_MODELS_AUTO_DISPLAY`（机器展示开关，默认 false）。

| 配置               | Jev                                     | 生成式 LLM                                     |
| ------------------ | --------------------------------------- | ---------------------------------------------- |
| endpoint 环境覆盖  | `INITRA_MONITOR_MODELS_JEV_ENDPOINT`    | `INITRA_MONITOR_MODELS_GENERATIVE_ENDPOINT`    |
| model 环境覆盖     | `INITRA_MONITOR_MODELS_JEV_MODEL`       | `INITRA_MONITOR_MODELS_GENERATIVE_MODEL`       |
| key 环境变量名覆盖 | `INITRA_MONITOR_MODELS_JEV_API_KEY_ENV` | `INITRA_MONITOR_MODELS_GENERATIVE_API_KEY_ENV` |
| 默认实际密钥变量   | `INITRA_JEV_API_KEY`                    | `INITRA_GENERATIVE_API_KEY`                    |
| timeout 环境覆盖   | `INITRA_MONITOR_MODELS_JEV_TIMEOUT`     | `INITRA_MONITOR_MODELS_GENERATIVE_TIMEOUT`     |

`endpoint` 是**完整 POST URL**，不是域名或 base URL；Jev 使用 System One 问答协议，生成式端使用 OpenAI 兼容的非流式 chat completions 协议，不是任意 LLM 接口。`model` 使用供应商实际提供的标识，建议固定版本。`api_key_env` 填环境变量的名字，不能填 key 本身。timeout 默认 `60s`，上限 `120s`；请求还受外层任务预算限制，剩余预算不足配置 timeout 加 3 秒时不开始下一次付费调用，也不增加尝试次数。后台任务与真实 CLI 的单次总预算均为 3 分钟；更短的父请求期限仍优先。远端要求 HTTPS，拒绝带凭据、query 或 fragment 的 URL，以及重定向；本机开发例外仅用于显式回环 HTTP。

生产配置在对应实例的 **`config/backend.env`** 私密设置上述环境变量及实际 key；按[部署说明](../deployment/README.md)选择正确布局，并重建后端容器加载环境变量，单纯 restart 不会重新读取 env_file。密钥不进入聊天、日志、截图、命令行参数或 Git。CLI 从同名环境变量读取配置，不自动读取服务器 env 文件。

显式设 `INITRA_MONITOR_MODELS_ENABLED=true` 且两个模型配置齐备后，启用已有调度器（`INITRA_MONITOR_SCHEDULER_ENABLED=true`，并配置已有操作员）或再次运行真实 monitor CLI。之前处于 `awaiting_configuration` 的**新候选会自动继续**，无需逐篇点击。调度器的一分钟唤醒检查处理进度，不是每分钟全量采集。禁用或缺配置时不发模型请求，也不额外请求正文、不消耗失败尝试预算；真实来源采集仍继续。

## 查看结果、失败与恢复

目录切到「待审核」查看新论文。详情「论文处理流程」展示材料范围、材料块/哈希、逐主题分类、总结与证据、复审和校验结果；历史笔记仍独立保留。选择「模型阅读优先级」按当前主题的最新来源版本分数排序；未知分数后置，不借用旧版本得分。未选主题时取该版本各主题最高分，分数相同按 ID 稳定排序。这是目录排序，不是按分数调度模型调用预算。

阶段状态：`pending` 待执行、`running` 执行中、`completed` 已完成、`awaiting_configuration` 待配置、`retry_wait` 等退避、`failed` 失败。完成一个阶段即保存其输入哈希、模型/提示/规则版本、时间、尝试次数与输出。来源分页与模型状态分开，模型失败不回滚真实采集水位。

- 可重试的暂时错误按阶段预算与 `Retry-After`/退避续跑；失败或认证/协议问题不无限重试，也不回退 mock。401/403 为 `authentication_failed`，修正凭据并重启后仍需显式重试，不能与缺 key 的自动续跑混淆。执行中断后遗留的 `running` 阶段标为 `failed/interrupted_execution`，需显式重试，避免自动重复收费。未开始调用、仅因本次总预算不足而暂停的待执行阶段可在下次推进。
- 管理员可「重试此阶段」重新排队失败或待配置阶段；不能越过退避。**排队成功不是模型处理完成**，需调度器或 CLI 后续推进。
- 对已完成但非 `fulltext` 的材料，可「重新获取全文并重跑后续阶段」。旧处理档案先归档，新材料会使下游重新处理，**可能再次产生模型费用**；普通失败阶段重试不重跑已完成的上游阶段。
- 目录投影失败保留待投影状态，下次幂等恢复；完整档案确认写入目录后，热状态可移除材料/输出正文并按需回取；未完成或待配置任务仍保留队列状态。已完成收据可按数量预算淘汰，处理档案 GET/重试从目录归档回取，不因热缓存淘汰重复付费处理同一来源版本。

模型流程不直接遍历历史约 3.9 万条主题成员关系或旧 mock 候选来创建任务；但真实来源回溯（包括核心期刊补漏）重新发现已有历史论文的新来源观察/处理键时，仍会建立新流程，并在模型启用后处理。因此不能理解为历史论文永不自动触发模型调用。新观察、机器结果和人工审核分开，不能覆盖原历史科学结论、人工纳入/排除、个人阅读/隐藏状态。

费用控制的当前边界是 `monitor.models.enabled` 总开关、单个 `processPipelines` 处理片段最多 8 个阶段、单次运行 3 分钟时限，以及退避/有限重试；真实 CLI 一次运行可能推进多个片段，不能把 8 个阶段视为每任务调用上限。另有同一数据库共享的累计调用数与保守 token 预留配额，发送前持久扣预留，失败、中断和重启不自动退款；默认 100 次调用、1,000,000 预留 token，单次输入 128 KiB、输出 4096 token。通过 `monitor.models.budget` 及对应 `INITRA_MONITOR_MODELS_BUDGET_*` 环境变量配置，`GET /api/v1/monitor/model-budget` 查询使用量。调整配置不重置累计值。Jev 不提供同等输出 token 限制，因此配额不是供应商金额硬上限；尚未提供自动周期重置或金额结算。启用前请检查来源回溯范围、待处理队列与供应商费用。

接口字段与权限见 [API 契约](../reference/api.md#论文处理档案与阶段重试)。本说明描述实现边界，不是所有论文均取得全文、模型实服可用或科学结论已验证的声明。
