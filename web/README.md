# Glean Web

以中文阅读为优先的文献检索与研究工作台。基于 **React 19 + TypeScript + Vite**、**Tailwind CSS v4**、可编辑的 **shadcn/ui 开放源码组件与 Radix 基础组件**，配合 Lucide 图标和 TanStack Query 构建。

## 运行界面

以下截图来自真实运行的前端、Go API 和 PostgreSQL，使用已提交的历史文献数据，不是设计稿或模拟页面。

### 桌面端：论文工作台

三个研究主题、中文优先的论文卡片、多维筛选、目录范围与个人阅读状态集中展示。截图中主题总量为 31,399 条，精选目录为 8,556 篇；两种统计口径不同。

![桌面端论文工作台：主题导航、筛选条件与真实历史论文列表](docs/screenshots/desktop-catalog.png)

### 桌面端：论文详情

详情抽屉将个人阅读状态、原始历史笔记与模型处理结果分开。尚未获取的摘要明确显示为空，不用历史笔记或模型输出冒充原文摘要。

![桌面端论文详情：阅读状态、摘要空状态与保留的历史阅读笔记](docs/screenshots/desktop-paper-detail.png)

### 手机端：文献浏览

窄屏将导航与筛选收进弹窗，保留检索、目录切换和论文卡片。

<img src="docs/screenshots/mobile-catalog.png" alt="手机端论文工作台：搜索、目录范围、筛选入口与论文卡片" width="360" />

截图记录：2026-10-27，[提交 `4eff45b`](https://github.com/TeamSillyBees/glean/commit/4eff45b49b851a8275a3fe3ada37d7e263fc0789) 的 [GitHub Actions 运行 #17](https://github.com/TeamSillyBees/glean/actions/runs/40472376956)，来自 Chromium 桌面端与 Pixel 7 尺寸的移动端浏览器验收。图片为原始视口截图，仅更名复制；详细来源见[截图说明](docs/screenshots/README.md)。该次历史库浏览器测试已通过；截图展示历史库界面，不代表实时采集验收结果，也不代表正式上线。LLM 调用仍为明确标识的 mock，历史笔记不是本次模型生成结果。

## 本地运行

需要 Node 24 和 npm 11；仓库中的锁文件固定已解析的依赖版本。

```bash
cd web
npm ci
cp .env.example .env.local
npm run dev
```

Vite 开发服务器默认绑定 `127.0.0.1:5173`，将 `/api` 代理到 Go 后端 `http://127.0.0.1:8080`。后端端口不同时可修改 `GLEAN_API_PROXY`。生产环境应托管 `dist/` 并在同源下反向代理 `/api`；若使用独立的 `VITE_API_BASE_URL`，必须同时明确配置后端 CORS 白名单。不要在 `VITE_*` 变量中放置密钥、token 或密码。

按照[仓库根目录说明](../README.md#本地启动)完成 Go 后端配置、迁移和账号 seed，再使用该账号登录。前端不内置示例密码。Access token 与 refresh token 仅保存在模块内存中，不写入浏览器存储；刷新页面后需要重新登录，这是有意设置的安全边界。

```bash
npm run typecheck
npm run lint
npm test
npm run build
npm run format:check
npm run preview
```

`npm run preview` 用于预览已构建的 `dist/`，不替代正式生产服务器。

## 架构与数据边界

- 业务数据来自经过认证的 `/api/v1/catalog/*`、`/api/v1/monitor/*`，沿用 initra 的统一响应结构。详见 [API 契约](../docs/glean/api-contract.md)。
- 前端是查询与阅读界面，不自行采集外部来源、启动扫描、调用 LLM、推断科学分类或编造实时计数。
- **不存在静默回退到演示数据的行为。** 后端错误有明确的重试状态。可预测且明确标记的模拟数据只用于测试；模型审核的 mock 标识显示在相应输出旁，不把真实来源论文标成假数据。
- `src/lib/filters.ts` 将各主题原有的筛选维度映射为查询参数。选项值与分类来自后端 facets，不使用前端启发式推断。世界模型主题的 `display_*` 值由后端从原始 HTML 分类中导入。
- 默认范围为后端定义的精选目录（`curated`），另有新发现待审和全部记录；全部记录包含历史背景及范围外条目。主题总数始终标为总记录数，而非精选数量。筛选项加载中、可重试错误和确实缺少标签是不同状态。
- 历史筛选顺序：论文类型 → 方向 → **AI 方法（仅生物医学 AI）** → 期刊/会议范围 → 技术任务 → 模型作用 → 年份 → 收录状态 → 具体期刊/会议。生物医学 AI 的 9 项及另外两主题的 8 项筛选均可见；通过搜索精确后端标签，仍能找到低频选项。**科学审核证据**独立于用户的**未读 / 在读 / 已读**状态。
- 首次查询论文前先加载个人偏好，保存操作需显式执行。个人隐藏的论文默认排除，可在独立的已隐藏视图中恢复；这些操作不修改原始元数据。
- 正式在线日期与卷期日期分别展示。缺失日期保持待核，录用、投稿或索引日期不冒充发表日期。
- 来源证据链接仅接受 HTTP(S)，新窗口使用 `noopener noreferrer`；原始证据按转义文本渲染。
- 会话代次隔离可防止延迟刷新、退出、过期 401 响应及延迟 JSON 解析跨账号污染状态。

## 界面能力

三个主题导航；字面检索与键盘聚焦快捷键（`⌘K` / `Ctrl+K`）；后端计数；已选条件标签；分页；个人阅读与隐藏状态；来源证据抽屉；来源健康；扫描历史；检索模板；版本化期刊与会议登记表；只读隔离记录及安全证据预览。

扫描失败卡片可展开只读恢复诊断，区分暂时失败、退避等待、访问受限与已创建恢复运行，并按唯一运行 ID 跳转关联记录。页面不会发起恢复；显示的 CLI 参数仅供管理员核对，实际执行仍须后端校验。

窄屏使用 Radix 弹窗呈现导航和筛选。弹窗支持 Escape 关闭、焦点约束和关闭后的焦点返回；遵循用户的减少动态效果偏好。

## 组件归属

[组件来源与上游登记说明](COMPONENTS.md)保留了 MIT 许可声明。`src/components/ui/` 使用 shadcn/ui 的开放源码组件模式，按本工作台需求调整；组件实际基于 `radix-ui` 的 Dialog、Select、Tabs、Tooltip 和 Slot，结合 Tailwind 工具类与 CVA 变体。

`components.json` 保留标准 shadcn 路径别名和主题配置。组件是可直接维护的源码，不是封闭的组件封装。

参考文档：

- [shadcn/ui 与 Tailwind v4](https://ui.shadcn.com/docs/tailwind-v4)
- [shadcn/ui 的 Vite 安装说明](https://ui.shadcn.com/docs/installation/vite)
- [Radix Dialog 无障碍支持](https://www.radix-ui.com/primitives/docs/components/dialog)
- [Vite 入门](https://vite.dev/guide/)

## 验证范围

### 组件与接口契约测试

Vitest + Testing Library 覆盖认证查询、精确分类参数传递、已保存偏好的恢复、加载/错误/空状态切换、重复打开和关闭详情、阅读/隐藏状态变更与恢复、只读监控、隔离记录、键盘检索及会话竞态隔离。这些测试中的网络数据均显式模拟，属于 DOM 功能测试，不能代替真实浏览器与后端端到端验收。

`src/test/deployment.test.ts` 检查容器部署文件的不变量，不能单独证明容器已成功运行。真实镜像构建、容器启动与反向代理行为由 CI 的容器任务另行验证。

### 真实 API 浏览器验收

`npm run test:e2e:list` 只列出桌面/手机验收测试，不启动浏览器；列举成功不等于实际执行通过。

`npm run test:e2e` 用于具备隔离依赖的 GitHub Actions runner：使用生产 Vite 构建与 preview、真实 Go API、PostgreSQL 和完整历史数据导入，不伪造 API 响应、不注入 token，也不使用假论文。CI 通过 `GLEAN_E2E_USERNAME`、`GLEAN_E2E_SECOND_USERNAME`、`GLEAN_E2E_PASSWORD` 提供临时测试账号，通过 `GLEAN_API_PROXY` 指定后端代理。

测试截图作为 CI artifact 保存，不采集可能携带 token 的 trace/HAR。不要提交 `test-results/`、`playwright-report/` 或认证状态。本文档中的截图是从 CI artifact 人工检查后选出的少量静态图片，单独保存在 `docs/screenshots/`。

真实浏览器验收区分常规回归与外部服务集成：

1. [**常规 CI**](../.github/workflows/ci.yml)：完整导入 39,495 条主题成员关系，验证真实数据库上的登录、查询、阅读/隐藏持久化、账号隔离，以及桌面/手机交互。测试使用仓库中已固定的历史数据，不依赖外部文献服务当时是否可用。
2. [**手动实时验收**](../.github/workflows/live-source-acceptance.yml)：在 GitHub Actions 中单独触发实时集成工作流，有限请求一条 Europe PMC 真实记录，验证采集、持久化以及浏览器中的来源展示。上游不可用时该验收明确失败，不使用模拟记录替代，也不让这项外部依赖阻断常规 CI。

常规 CI 通过不等于外部来源实时可用，实时验收结果也不能代替历史库回归。最新状态请以相应提交的 [Actions 结果](https://github.com/TeamSillyBees/glean/actions)为准。截图中的内容不能作为单篇论文已重新科学核验的证明。

## 容器运行

从仓库根目录构建，**构建上下文必须为 `web/`**：

```bash
docker build --pull -t glean-web:local ./web
```

多阶段镜像先使用官方 `node:24-bookworm-slim` 构建，再将 `dist/` 复制到官方 `nginx:stable-alpine`。运行阶段使用非 root 的 `nginx` 用户，监听 **8080** 端口；镜像构建过程中通过 `RUN nginx -t` 验证配置。上游镜像标签会接受维护更新，正式发布时还应固定经过验证的镜像摘要。

前端与 Go 服务必须位于同一**用户定义的 Docker/Compose 网络**，Go 服务在网络内需能通过 **`backend:8080`** 访问。Docker 内置 DNS（`127.0.0.11`）在请求时解析，缓存每 30 秒更新；后端稍晚启动不会阻止静态前端启动。API 应留在私有服务网络内，仅向本机发布前端端口。仓库级 Compose 配置另行维护。

假设已经创建名为 `glean` 且包含后端服务的网络，可用以下受限配置本地运行：

```bash
docker run --rm --name glean-web --network glean \
  --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m,mode=1777 \
  --cap-drop ALL --security-opt no-new-privileges \
  -p 127.0.0.1:8088:8080 glean-web:local
```

- 访问 `http://127.0.0.1:8088`。`/api/*` 原样转发至 `backend:8080`，**不移除 `/api` 前缀**，保留 JSON 请求体、查询参数和 Bearer 请求头。API 错误保持为 API 错误，不回退为 HTML。
- SPA 深层链接回退到 `index.html`；缺失的带哈希资源返回 404。HTML 需要重新验证，带哈希的 `/assets/` 文件可以缓存，API 响应不缓存。
- `/healthz` 检查 nginx 和构建后的 `index.html`。Docker 健康检查不代表数据库/API 已就绪，也不代表能成功登录，后端必须独立验证。
- 镜像不接收运行时秘密、token、LLM 密钥或环境变量替换。`.env*`、本地 npm 配置及常见密钥格式从构建上下文中排除；Dockerfile 使用明确的源码复制白名单。容器有意使用同源 `/api`，因此 `VITE_API_BASE_URL` 不是运行时开关。
- nginx 将 PID 和临时文件写入 `/tmp`，支持只读根文件系统。访问日志不记录查询参数或凭据。静态 CSP 仅允许同源连接，论文外链仍通过用户主动导航打开。
- 这是私有本地网络的 HTTP 示例，不是公网生产部署。在公开访问前，应由单独配置的可信入口终止 TLS，并明确配置可信代理及转发请求头，不信任任意客户端传入的代理头。

### 认证接入与验证

根据后端 setup/seed 说明初始化真实本地账号，前端不提供固定生产凭据。浏览器使用以下路由：

| 操作     | 路由                                         | JSON / 认证方式                                                                  |
| -------- | -------------------------------------------- | -------------------------------------------------------------------------------- |
| 登录     | `POST /api/v1/auth/login`                    | `{username,password}`；读取 `data.user`、`data.accessToken`、`data.refreshToken` |
| 刷新令牌 | `POST /api/v1/auth/refresh`                  | `{refreshToken}`；公开端点，轮换 refresh token                                   |
| 查询     | `GET /api/v1/catalog/topics` 及目录/监控视图 | `Authorization: Bearer …`                                                        |
| 退出     | `POST /api/v1/auth/logout`                   | `{refreshToken}` 及 Bearer access token                                          |

`test/e2e/glean_auth_test.go` 中的 `TestGleanFrontendAuthContract` 使用 `httptest` 验证真实 Go HTTP handler、JWT 签发/轮换、刷新重放拒绝、认证后的主题查询、退出及随后的撤销；数据库使用 SQL mock。该测试比前端的模拟传输测试更深入，但本身仍不是 PostgreSQL/浏览器端到端验收。

官方参考：[Node Docker 镜像](https://github.com/nodejs/docker-node)、[nginx Docker 镜像与非 root 配置](https://hub.docker.com/_/nginx)、[nginx 代理 URI 行为](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass)、[Docker 健康检查](https://docs.docker.com/reference/dockerfile/#healthcheck)。
