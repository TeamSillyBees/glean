# 运行截图来源

这些 PNG 是真实 GitHub Actions 浏览器验收的原始视口截图，只做更名复制，未生成、重绘、裁切或修改界面内容。

- 拍摄日期：2026-10-27（UTC）。
- 代码：[bd20fb4a5be5cae300a1c8cf6f15cdfc07db5d2f](https://github.com/TeamSillyBees/glean/commit/bd20fb4a5be5cae300a1c8cf6f15cdfc07db5d2f)。
- CI：[运行 40531139041](https://github.com/TeamSillyBees/glean/actions/runs/40531139041)。
- artifact：`glean-browser-bd20fb4a5be5cae300a1c8cf6f15cdfc07db5d2f`（ID `12746422482`）。CI artifact 有保留期限，因此将精选 PNG 随源码保存。
- 测试：`web/e2e/catalog.spec.ts`，真实 Go HTTP API、隔离 PostgreSQL、完整历史数据导入与临时验收账号。
- 浏览器：Playwright 的 Chromium；桌面视口 `1440 × 1080`，手机采用 Pixel 7 设备参数（仿真视口，不是真机拍摄）。

| 当前文件                   | artifact 中的原文件名               | SHA-256                                                            |
| -------------------------- | ----------------------------------- | ------------------------------------------------------------------ |
| `desktop-catalog.png`      | `desktop-catalog-viewport.png`      | `8949da4fc952a35690bd0b487a14082ec604432238905682f619c23d8373f8a7` |
| `desktop-paper-detail.png` | `desktop-paper-detail-viewport.png` | `e490e2f53bd1b82f3b1e463712fcd34515e762cab761c4b72c91e5ffba236611` |
| `mobile-catalog.png`       | `mobile-catalog-viewport.png`       | `28021a2703566dc078b1980a0d253163dd11dd4d9fcfa34527675d691340f584` |

原文件位于 artifact 的 `test-results/historical/catalog-real-historical-ca-61866-state-and-account-isolation-desktop/` 或对应的 `-mobile/` 目录。

## 证据边界

- 该次桌面和手机历史库验收已通过；Go/PostgreSQL、前端及容器任务也已通过。
- 整轮常规 CI 通过；目录额外验证 320、375、768、1024、1280、1440、1920px 的布局边界。
- 常规 CI 不访问外部文献来源；独立手动实时验收未在此轮触发，不混入通过范围。
- 页面数据是历史文献，不是模拟论文；LLM 调用仍为 mock，截图不证明单篇论文经过重新科学审核。
- 截图显示隔离测试环境，不代表已正式部署；缺失摘要等真实空状态予以保留。
- 已逐图查看，所选画面不包含密码、token、请求头或认证存储；显示的“浏览器验收用户”为临时测试账号名称。
