# CI 效率与完整验证边界

正常 `Glean CI` 保留五个重验证 job，并增加差异判定和始终运行的 `CI validation` 汇总检查：Go/PostgreSQL/历史基线、备份恢复、前端检查、容器 HTTP smoke、真实数据库浏览器验收。没有删除测试、缩减浏览器项目、减少历史记录或改为模拟数据。

## 触发与取消

- `main` 每次 push 运行完整 CI，包括历史查询诊断；发布仍要求同一 SHA 的成功 push CI。
- 功能分支通过 PR 运行，不再同时产生 push 和 PR 两份相同验证。没有 PR 的分支可手动运行 `workflow_dispatch`。
- 同一个 PR 的新提交取消旧 PR 运行。其他事件用唯一 `run_id` 分组，避免运行中和 pending 的 main/手动任务相互取消。
- 没有全工作流 `paths-ignore`。仅 PR 的完整 Git diff 被证明全部是 `README.md` 或 `docs/**/*.md` 时，五个重任务明确显示 skipped，`CI validation` 写明仅文档验证、未执行运行时测试。性能诊断文档、测试目录/文件名、未知路径、数据、配置和工作流不在允许列表；缺失历史、浅克隆、异常或空 diff 均全量运行。重命名同时检查新旧路径。main 的文档提交仍完整验证。
- 建议分支保护要求稳定的 `CI validation` 检查；它要求判定成功且每个预期任务状态正确，失败、取消、缺失输出或意外跳过都不能通过。判定失败时重任务回退到全量执行，汇总仍失败，避免依赖判定job失败而让旧检查仅以skipped结束。没有修改仓库保护设置；不能假定尚未启用的保护已经生效。
- 容器构建只 `load`，从不推送镜像。镜像发布、生产部署和真实外部来源验收仍是分开的手动工作流，本优化不触发它们。

## 缓存与并行

Go module 缓存按操作系统、架构、实际 Go 版本及 `go.sum` 保存，仅完整 verify job 写入，browser 只恢复，避免较少依赖先占据不可变主键；编译缓存另按 job 与提交滚动保存，并恢复同一工具链/job 的最近快照。Go 自身检查编译输入，不缓存测试结果来替代 `-count=1`。原先 setup-go 的不可变 lockfile 主键在源码变化后不更新编译产物，导致重复编译。

GitHub 的缓存访问范围仍生效：PR merge ref 的缓存只供该 PR 使用，main 不能恢复 PR 缓存。不用 `pull_request_target`，不缓存凭据、数据库、浏览器会话或测试结果。module 缓存不随提交重复存储；编译快照与两个独立 Buildx image scope 使用仓库缓存额度，旧项由 GitHub LRU/未使用过期机制回收。没有提高额度或设置付费存储；缓存缺失只重新构建，不跳过验证。高频提交时需观察缓存淘汰，不能把每个提交的快照永久保留。

容器 backend/web 使用不同 GHA layer scope，防止互相覆盖。构建上下文、Dockerfile 和 HTTP smoke 不变。缓存导出故障只影响加速，不掩盖构建或 smoke 失败。浏览器前端安装/构建/Chromium 安装与独立数据库全量导入并行；显式等待两者成功后才启动 API 和浏览器。任一失败均失败，退出时等待后台任务结束，不静默吞错。

## 测量方法

分别报告事件创建至最后 job 完成的墙钟、所有 job 起止时间相加的 runner 秒和首次 runner 等待。缓存准备、服务启动、测试和清理分别分析；runner 秒不是实际计费账单（计费可能逐 job 取整）。

基线为 main `f8069d86835f031acb4613f877bf5c1f9df7d699` 的 [CI 41661167125](https://github.com/TeamSillyBees/glean/actions/runs/41661167125)：墙钟 369 秒，job 合计 978 秒，首次 runner 等待 3 秒。Go 366 秒、浏览器 335 秒；查询诊断 182.33 秒，39,495 条浏览器导入 154.79 秒。Go/npm 缓存已命中，但 Docker 没有层缓存。总共获取当时全部 47 次 run，而不是虚构 50 个样本；近 10 次成功 CI 墙钟中位数约 439 秒，版本不同，不作为严格因果对照。

优化后的冷缓存与热缓存结果应分别记录。PR merge commit 与 main push、GitHub hosted runner 抖动、诊断样本/导入速度都是混杂因素；单次差值不是稳定收益保证。

## 首轮完整验证（新缓存预热，不作为收益承诺）

[优化分支运行 41674606552](https://github.com/TeamSillyBees/glean/actions/runs/41674606552) 在提交 `ab01399eebb2d63f58be7cf7a6b073c8846f2265` 的 PR 合并结果上，七个 job 全部成功：五项原有重验证、差异判定、最终汇总。200 项前端测试、真实数据库/历史数据/浏览器与容器 smoke 均保留。

| 指标 | 最新 main 基线 | 首轮完整优化运行 |
| --- | ---: | ---: |
| 创建到最后 job 结束 | 369 秒 | 499 秒 |
| 首次 runner 等待 | 3 秒 | 64 秒 |
| 首个 job 开始到最后 job 结束 | 366 秒 | 435 秒 |
| job 时长总和 | 978 秒 | 1095 秒 |
| Go 验证 job | 366 秒 | 399 秒 |
| 浏览器验收 job | 335 秒 | 322 秒 |
| 容器构建/smoke job | 154 秒 | 199 秒 |

这轮处于新 Go 缓存预热，Docker 则部分恢复了取消探针已写出的层，不能称为完全冷缓存，也不能拿它宣称已经加速。64 秒首次等待受前一次运行取消清理影响。下一次使用同一套验证的热缓存结果记录在 PR 测量总结，分别报告排队和执行时长。

两个被新提交替代的 PR 探针 `41673189759`、`41674103819` 最终均为 cancelled，未产生对应 feature push 重复运行；取消清理并非瞬时。被取消样本不纳入成功运行性能对照。main 不取消通过唯一 run ID 分组和回归测试确认；本次没有向 main 推送或部署来做破坏性演示。


## 首次热缓存实测

[运行 41676390147](https://github.com/TeamSillyBees/glean/actions/runs/41676390147)（提交 `f1565bc7e7db8d6e41761f0e7476a694cea35a29`）七项检查全部成功，且该头只有一个 PR run，没有重复 push run。

| 指标 | 最新 main 基线 | 热缓存样本 |
| --- | ---: | ---: |
| 创建至最后 job 完成 | 369 秒 | 326 秒 |
| 首次 runner 等待 | 3 秒 | 2 秒 |
| 所有 job 时长之和 | 978 秒 | 726 秒 |
| Go/PostgreSQL/历史 | 366 秒 | 285 秒 |
| 真实浏览器 | 335 秒 | 226 秒 |
| 容器构建/smoke | 154 秒 | 47 秒 |
| 前端检查 | 78 秒 | 73 秒 |
| 备份恢复 | 45 秒 | 61 秒 |
| 新增差异判定 + 汇总 | 无 | 34 秒 |

墙钟减少43秒（11.7%），runner合计减少252秒（25.8%）。备份恢复反而多16秒，Go checkout本次用了25秒，说明hosted runner存在抖动；不能把单次差值当固定加速保证。早期基线370秒取API的updated_at，该字段比最后job结束晚1秒；表中统一按最后job结束，基线应为369秒。本次说明更正不改变工作流或测试逻辑。

最终热缓存实测保留1,143条Go测试/子测试通过事件、49个Go包通过、200项前端测试、4项真实浏览器测试与39,495条历史成员关系。Go测试仍为`-count=1`，没有靠复用测试结果冒充重跑。模块/编译缓存命中得到确认，Docker共19个层缓存命中。编译快照大小约191MB（verify）/141MB（browser），共享module快照约252MB；这些是缓存体积，不是永久保留或计费承诺。

纯文档PR的轻量路径已通过真实本地Git历史与失败状态回归，独立docs-only PR实跑尚未完成。main不取消也没有通过向main连续推送来演示；使用唯一run ID与工作流回归约束。最新提交的CI状态与后续热缓存样本见[优化PR](https://github.com/TeamSillyBees/glean/pull/1)，避免为每次时间波动继续提交记录而反复触发CI。

参考：[GitHub Actions效率指南](https://awesome-copilot.github.com/skill/github-actions-efficiency/)、[GitHub并发语义](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)、[Docker GHA缓存](https://docs.docker.com/build/ci/github-actions/cache/)。
