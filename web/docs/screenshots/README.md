# 运行截图来源

这些 PNG 是真实 GitHub Actions 浏览器验收的原始视口截图，只做更名复制，未生成、重绘、裁切或修改界面内容。

- 拍摄日期：2026-10-27（UTC）。
- 代码：[4eff45b49b851a8275a3fe3ada37d7e263fc0789](https://github.com/TeamSillyBees/glean/commit/4eff45b49b851a8275a3fe3ada37d7e263fc0789)。
- CI：[运行 #17 / 40472376956](https://github.com/TeamSillyBees/glean/actions/runs/40472376956)。
- artifact：`glean-browser-4eff45b49b851a8275a3fe3ada37d7e263fc0789`（ID `12718837925`）。CI artifact 有保留期限，因此将精选 PNG 随源码保存。
- 测试：`web/e2e/catalog.spec.ts`，真实 Go HTTP API、隔离 PostgreSQL、完整历史数据导入与临时验收账号。
- 浏览器：Playwright 的 Chromium；桌面视口 `1440 × 1080`，手机采用 Pixel 7 设备参数（仿真视口，不是真机拍摄）。

| 当前文件                   | artifact 中的原文件名               | SHA-256                                                            |
| -------------------------- | ----------------------------------- | ------------------------------------------------------------------ |
| `desktop-catalog.png`      | `desktop-catalog-viewport.png`      | `40a7301cf5d313bc3ea253fb8146c93df214978c11408f297a1a8b69fff90797` |
| `desktop-paper-detail.png` | `desktop-paper-detail-viewport.png` | `b3d2cfb39f89e89be90d0c253cd8b5c8b2fafd8fe69ed42964f7e75dca5f32c3` |
| `mobile-catalog.png`       | `mobile-catalog-viewport.png`       | `130f02c9564e3de78a4f33a704a28f9217f44b59b99e2f1d80a8ef4d7aed8042` |

原文件位于 artifact 的 `test-results/historical/catalog-real-historical-ca-61866-state-and-account-isolation-desktop/` 或对应的 `-mobile/` 目录。

## 证据边界

- 该次桌面和手机历史库验收已通过；Go/PostgreSQL、前端及容器任务也已通过。
- 后续 Europe PMC 实时请求返回 HTTP 503，实时记录的浏览器验收未执行，整轮 CI 仍为失败。
- 页面数据是历史文献，不是模拟论文；LLM 调用仍为 mock，截图不证明单篇论文经过重新科学审核。
- 截图显示隔离测试环境，不代表已正式部署；缺失摘要等真实空状态予以保留。
- 已逐图查看，所选画面不包含密码、token、请求头或认证存储；显示的“浏览器验收用户”为临时测试账号名称。
