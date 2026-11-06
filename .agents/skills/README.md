# 仓库内技能

这三个技能安装在当前仓库的 `.agents/skills/`，供项目成员共同使用，不安装到用户目录。

| 技能 | 上游目录 | 安装时提交 | 许可 |
| --- | --- | --- | --- |
| [impeccable](impeccable/SKILL.md) | [pbakaus/impeccable/.agents/skills/impeccable](https://github.com/pbakaus/impeccable/tree/c3bb418ee38ee49a6b999b7752c1368f3123a2bd/.agents/skills/impeccable) | `c3bb418ee38ee49a6b999b7752c1368f3123a2bd` | Apache-2.0 |
| [frontend-design](frontend-design/SKILL.md) | [anthropics/skills/skills/frontend-design](https://github.com/anthropics/skills/tree/ca45fc496a3436251f191354091d557da31c15b5/skills/frontend-design) | `ca45fc496a3436251f191354091d557da31c15b5` | Apache-2.0 |
| [ux-writing](ux-writing/SKILL.md) | [content-designer/ux-writing-skill](https://github.com/content-designer/ux-writing-skill/tree/98cacde4ba2dd10ed28df43a8d53eef1e321c539) | `98cacde4ba2dd10ed28df43a8d53eef1e321c539` | MIT |

- `impeccable` 使用上游的 Codex 发行目录，保留指导文档、参考资料和启动脚本；补充仓库根目录的 `LICENSE`。未安装引擎二进制或 hook，也未运行会下载引擎的启动脚本。
- `frontend-design` 保留上游目录中的 `SKILL.md` 和 `LICENSE.txt`。
- `ux-writing` 按上游 `build-skill.sh` 的内容范围保留 `SKILL.md`、`docs/`、`examples/`、`references/`、`templates/`，并保留 `LICENSE`；不复制演示视频、网站或重复的发行包。

使用范围和项目优先约束见 [AGENTS.md](../../AGENTS.md#项目技能与前端工作)。可在项目中通过 `$impeccable`、`$frontend-design`、`$ux-writing` 指定技能。

更新时从对应上游选择提交，替换相同目录范围，保留许可，并更新本表；检查技能入口和本地引用仍存在。不要用全局安装命令替代仓库内更新。
