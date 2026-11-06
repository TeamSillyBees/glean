# Glean

**持续生长的文献库。** 将历史论文、增量发现、来源证据、机器处理和人工审核放在同一个中文优先的工作台中。

Glean 使用 Go 后端、React / TypeScript 前端、PostgreSQL 和 Redis，支持桌面与手机。内置生物医学 AI、LLM / Agent 知识工程、生物医学世界模型三个主题，也可创建自定义研究主题。

## 主要功能

- 检索与阅读：多维筛选、精选与待审核目录、论文详情、来源与出版证据、个人阅读和隐藏状态。
- 持续发现：六个公开文献来源，关键词与多路线检索、历史回溯、分页续跑、重叠补漏和请求审计。
- 论文处理：可选的材料获取、Jev 分类与优先排序、LLM 总结及证据检查；缺少模型配置时先保存真实题录。
- 共享管理：主题内人工审核、六项书目字段纠错、账号和检索方案管理，保留来源与修改历史。
- 运行维护：采集状态、手动连通性测试、隔离记录、模型预算、容器发布与备份恢复工具。

仓库包含 **39,495 条历史主题成员关系**的可核验快照。来源命中、模型处理与人工纳入分别记录；历史导入不重新裁决科学结论。

## 文档

| 入口                                        | 内容                               |
| ------------------------------------------- | ---------------------------------- |
| [文档目录](docs/README.md)                  | 全部文档导航                       |
| [功能清单](docs/features.md)                | 已实现能力与边界                   |
| [部署说明](docs/deployment/README.md)       | 上线、镜像发布、备份恢复及当前实例 |
| [Roadmap](docs/roadmap.md)                  | 后续工作与完成标准                 |
| [Changelog](CHANGELOG.md)                   | 已交付变化与迁移要求               |
| [开发指南](docs/development.md)             | 本地启动、代码组织和验证           |
| [管理指南](docs/guides/management.md)       | 阅读、审核、纠错与主题管理         |
| [检索方案](docs/guides/search-plans.md)     | 关键词、路线、研究协议与覆盖审计   |
| [来源访问与更新](docs/reference/sources.md) | 六源访问政策和增量策略             |

## 本地启动

准备 Go 1.26、Node 24 / npm 11、Docker Compose、Atlas CLI 和 PostgreSQL 客户端后，按[开发指南](docs/development.md#本地启动)依次准备本地配置、迁移数据库、初始化账号并启动前后端。

`tools/setup` 只准备本地文件，迁移、seed 和历史导入均显式执行。默认关闭自动采集与模型调用；开启前需配置操作员、来源范围和预算。应用配置环境变量保留 `INITRA_` 前缀，运行环境使用 `APP_ENV`。

## 线上实例

[Glean](https://glean.starxy.cc) 已部署；当前版本、运行开关、目录和备份位置见[实例记录](docs/deployment/current-instance.md)。仓库服务器部署脚本与线上单 Compose 布局尚未统一，操作前请阅读部署说明。

## 开发与来源

项目基于 [initra](https://github.com/TeamSillyBees/initra) 源码模板，基础能力与业务均在当前单一 Go module 内维护。参阅[贡献指南](CONTRIBUTING.md)、[开发约定](AGENTS.md)、[安全政策](SECURITY.md)与[模板来源](TEMPLATE_ORIGIN.md)。
