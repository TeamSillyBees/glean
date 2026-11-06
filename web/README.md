# Glean Web

中文优先的文献阅读与管理工作台，使用 React、TypeScript、Vite、Tailwind、Radix 和 TanStack Query。产品能力统一见[功能清单](../docs/features.md)，操作步骤见[管理指南](../docs/guides/management.md)。

## 本地运行

先按[开发指南](../docs/development.md#本地启动)准备 Go 后端、数据库与账号。前端需要 Node 24 / npm 11：

```sh
cd web
npm ci
npm run dev
```

Vite 默认绑定 `127.0.0.1:5173`，将 `/api` 代理到 `127.0.0.1:8080`。需要修改时参考 [.env.example](.env.example)，通过 `GLEAN_API_PROXY` 指定后端；独立 `VITE_API_BASE_URL` 还需要后端 CORS 白名单。`VITE_*` 不得承载密钥。

生产托管 `dist/`，并在同源反向代理 API。`npm run preview` 用于构建预览，不代替生产服务。容器与 HTTPS 见[部署说明](../docs/deployment/README.md)。

## 数据与交互边界

- 前端只调用 Glean 的目录与监控 API，不自行采集来源、启动扫描、调用模型或推断科学分类。
- token 仅保存在内存，刷新页面后重新登录；退出、刷新与延迟响应按会话代次隔离。
- 默认精选目录，另有待审核、已移出主题和全部记录；筛选与计数来自后端，个人阅读/隐藏和共享审核分开。
- 详情支持 `?paper=<字符串 ID>` 直达链接。复制链接只包含论文 ID；个人隐藏不是访问权限控制。
- 来源证据按转义文本展示，外链仅接受 HTTP(S)。摘要、历史笔记和模型结果分别标识，未取得内容保持为空。
- “采集状态”刷新只读状态；管理员显式点击连通性测试才由服务器探测官方来源。
- 保存主题或检索方案不启动采集。每个来源的实际查询语义见[来源参考](../docs/reference/sources.md)。

API 字段与权限见 [API 契约](../docs/reference/api.md)。

## 界面结构与维护

论文工作台和详情弹窗外壳保留在首屏；详情正文、管理、监控和期刊页面按需加载。加载/失败状态可重试，保持导航、会话和焦点返回；部署后的旧资源失效可能需要刷新并重新登录。

窄屏将导航与筛选放入弹窗，支持 Escape、焦点约束和减少动态效果偏好。界面规范和源码组件归属分别见 [PRODUCT.md](PRODUCT.md)、[DESIGN.md](DESIGN.md) 和 [COMPONENTS.md](COMPONENTS.md)；保留组件许可声明。

## 验证

```sh
npm test
npm run typecheck
npm run lint
npm run build
npm run format:check
npm run build:size
```

`build:size` 统计入口与静态依赖闭包的原始/gzip 字节，不是浏览器延迟成绩。对比旧构建时可传 `npm run build:size -- /path/to/old/dist`。

Vitest / Testing Library 使用明确的网络 mock 验证组件行为。真实桌面/手机验收使用 Go API、PostgreSQL 和完整历史库；依赖与运行方式见 [E2E 说明](e2e/README.md)。列举测试或构建通过不等于实际浏览器执行通过。

## 界面截图

下列截图记录 2026-10-27 的真实历史库阅读界面，不是最新全部功能的截图。原始提交、CI 和数据来源见[截图说明](docs/screenshots/README.md)。

![桌面端论文工作台](docs/screenshots/desktop-catalog.png)

![桌面端论文详情](docs/screenshots/desktop-paper-detail.png)

<img src="docs/screenshots/mobile-catalog.png" alt="手机端论文工作台" width="360" />
