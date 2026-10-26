# 历史摘要补全：三个已核验来源记录

这是独立于历史基线的、只追加的摘要证据层。首批固定三个身份：

| 主题 | 原始 ID | 来源 |
| --- | --- | --- |
| 生物医学 AI | `BAIE4C9614D01` | Europe PMC `MED:42823348` |
| 生物医学世界模型 | `BWE4E4BDE173` | Europe PMC `MED:39984300` |
| LLM Agent | `P1286` | Springer 原始 DOI 章节页 |

工具直接重新读取固定的三个本地原始响应，核对压缩响应/HTML 字节 SHA-256、原始来源 DOI、题名和精确记录位置，再用保留统计比较符号与内联公式的解析器提取摘要，核对摘要 SHA-256。随后绑定固定基线清单、分片哈希及三个 `(topic, originalId, doi, title)` 成员身份。不会扫描历史 raw 树，不使用模型或外部文献 API。

`source-field-complete` 只表示摘要与该缓存来源完整字段/Abstract 节一致，**不是**人工科学审读，也不是对出版社所有版本进行独立完整性确认。`cached-source-field` / `cached-publisher-section` 不冒充已经查明的出版版本号。未取得的原始抓取时间保持未知。

## 只读校验（默认）

在私有文件中提供三个 ID 到实际本地响应文件的映射；不要将此映射提交到 Git 或交给前端。工具不内置私人目录路径。

```json
{
  "BAIE4C9614D01": "<local response file>",
  "BWE4E4BDE173": "<local response file>",
  "P1286": "<local HTML file>"
}
```

```sh
go run ./tools/enrichabstract -sources '<private source-map.json>'
```

默认仅输出计数、原始 ID 和基线哈希；不连接数据库。可用 `-out '<new verified-records.json>'` 输出只含公开来源信息、哈希和摘要的记录。输出必须是新文件，不会覆盖现有基线或缓存。`internal/abstractenrichment/testdata/verified_records.json` 是已核验记录的回归测试夹具，不是可绕过原始响应验证的导入入口。

## 显式追加到已有目录

仅在原始历史基线已经导入且管理员明确决定执行时使用。设置私有 `GLEAN_ENRICHMENT_DATABASE_URL`；共享数据库必须使用 `sslmode=verify-full`。来源验证全部完成后才建立数据库连接。

```sh
go run ./tools/enrichabstract -mode apply \
  -sources '<private source-map.json>' \
  -operator '<existing-active-user-id>' -node 18
```

回环开发数据库可显式增加 `-allow-local-insecure`，且仅允许 `sslmode=disable`；节点必须与同时运行的实例不同。工具禁止 URL 查询参数和 `PGHOSTADDR`、`PGSERVICE`、`PGSERVICEFILE` 间接改变数据库目标。

数据库服务只向已有精确成员追加不可变来源观察，不创建论文或成员，不改 canonical abstract、历史 raw、分类、正式纳入、科研核验、个人阅读或隐藏状态。重复执行同一来源正文只返回已有观察。缺失成员、身份冲突或无效审计操作员会停止并返回非零退出；报告保留已完成数量，修正后可幂等重试。新摘要不冒充新的实时来源获取时间，也不覆盖模型审读记录。

## 隔离和范围

- `BAI2E6C229787`（THPFF）因内容对应关系未解决而硬排除，即使 DOI/题名相同。
- 不接受未知 ID、摘要片段、二级转录、题名近似匹配、来源 DOI 冲突或未经复核的新解析结果。
- 现有可用性审计中的大规模候选数字不是导入允许列表。扩大范围必须增加逐来源响应验证、修复优先级和身份/版本核验后再更新固定规范。
- 方法简述、贡献笔记、题名翻译、已取得/已读标记均不能代替原文摘要；本阶段不生成中文摘要译文。
- 私有缓存路径、请求头、内部追踪与认证信息不得进入公开 `raw` 或前端响应。

## 验证

```sh
go test ./internal/abstractenrichment ./tools/enrichabstract ./internal/modules/catalog -count=1
go vet ./internal/abstractenrichment ./tools/enrichabstract ./internal/modules/catalog
```

真实 PostgreSQL 行为沿用 `INITRA_TEST_DATABASE_URL` 集成测试；缺少该变量的跳过不等于通过。运行 `-mode verify` 验证本地真实来源字节；运行测试不会自动执行真实导入。
