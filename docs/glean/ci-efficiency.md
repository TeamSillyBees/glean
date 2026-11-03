# CI 效率与完整验证边界

正常 `Glean CI` 保留五个相互独立的 job：Go/PostgreSQL/历史基线、备份恢复、前端检查、容器 HTTP smoke、真实数据库浏览器验收。没有删除测试、缩减浏览器项目、减少历史记录或改为模拟数据。

## 触发与取消

- `main` 每次 push 运行完整 CI，包括历史查询诊断；发布仍要求同一 SHA 的成功 push CI。
- 功能分支通过 PR 运行，不再同时产生 push 和 PR 两份相同验证。没有 PR 的分支可手动运行 `workflow_dispatch`。
- 同一个 PR 的新提交取消旧 PR 运行。其他事件用唯一 `run_id` 分组，避免运行中和 pending 的 main/手动任务相互取消。
- 没有全工作流 `paths-ignore`。文档可能承载架构契约，main 的文档提交也必须留下完整发布验证。仅 PR 中原有的保守选择器可以关闭一个可选性能诊断，任何不确定性均保留诊断。
- 容器构建只 `load`，从不推送镜像。镜像发布、生产部署和真实外部来源验收仍是分开的手动工作流，本优化不触发它们。

## 缓存与并行

Go module 缓存按操作系统、架构、实际 Go 版本及 `go.sum` 保存；编译缓存另按 job 与提交滚动保存，并恢复同一工具链/job 的最近快照。Go 自身检查编译输入，不缓存测试结果来替代 `-count=1`。原先 setup-go 的不可变 lockfile 主键在源码变化后不更新编译产物，导致重复编译。

GitHub 的缓存访问范围仍生效：PR merge ref 的缓存只供该 PR 使用，main 不能恢复 PR 缓存。不用 `pull_request_target`，不缓存凭据、数据库、浏览器会话或测试结果。module 缓存不随提交重复存储；编译快照与两个独立 Buildx image scope 使用仓库缓存额度，旧项由 GitHub LRU/未使用过期机制回收。没有提高额度或设置付费存储；缓存缺失只重新构建，不跳过验证。高频提交时需观察缓存淘汰，不能把每个提交的快照永久保留。

容器 backend/web 使用不同 GHA layer scope，防止互相覆盖。构建上下文、Dockerfile 和 HTTP smoke 不变。浏览器前端安装/构建/Chromium 安装与独立数据库全量导入并行；显式等待两者成功后才启动 API 和浏览器。任一失败均失败，退出时等待后台任务结束，不静默吞错。

## 测量方法

分别报告事件创建至最后 job 完成的墙钟、所有 job 起止时间相加的 runner 秒和首次 runner 等待。缓存准备、服务启动、测试和清理分别分析；runner 秒不是实际计费账单（计费可能逐 job 取整）。

基线为 main `f8069d86835f031acb4613f877bf5c1f9df7d699` 的 [CI 41661167125](https://github.com/TeamSillyBees/glean/actions/runs/41661167125)：墙钟 370 秒，job 合计 978 秒，首次 runner 等待 3 秒。Go 366 秒、浏览器 335 秒；查询诊断 182.33 秒，39,495 条浏览器导入 154.79 秒。Go/npm 缓存已命中，但 Docker 没有层缓存。总共获取当时全部 47 次 run，而不是虚构 50 个样本；近 10 次成功 CI 墙钟中位数约 439 秒，版本不同，不作为严格因果对照。

优化后的冷缓存与热缓存结果应分别记录。PR merge commit 与 main push、GitHub hosted runner 抖动、诊断样本/导入速度都是混杂因素；单次差值不是稳定收益保证。
