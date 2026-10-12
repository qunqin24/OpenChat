> **历史文档。** 这是项目早期的设计记录，很多内容（比如 Echo 占位模型、只支持 S3 的存储、`internal/chat` 包）已经不存在了。现在的约定以仓库根目录的 [AGENTS.md](../AGENTS.md) 为准，部署见 [部署指南](deployment.md)。

# docs/ — 给 agent 的协作文档

这组文档的读者是改这个仓库的 agent（和人）。目标：不看代码也能判断
"改动该落在哪、有哪些隐形约束、怎么验证"。

## 读哪份

| 你要做什么 | 读 |
| --- | --- |
| 第一次接触这个仓库 / 需要全局图景 | [architecture.md](architecture.md) |
| 改 `apps/server/`（端点、查询、迁移、auth、SSE、存储） | [backend.md](backend.md) |
| 改 `apps/web/` 或 `packages/api-client/` | [frontend.md](frontend.md) |
| 执行一个具体任务（加端点/迁移/页面/模型…） | [tasks.md](tasks.md) |
| 改完自检 / 排查"为什么这么写" | [pitfalls.md](pitfalls.md) |
| 改某个功能（对话/转录、压缩、用量与上限、存储、聊天页、游客、认证、内容审核）前读它的完整设计 | [design/README.md](design/README.md) |
| 研究自研 Chatbot 内核与成熟开源项目 | [research/README.md](research/README.md) |
| 设计模型目录、能力解析和自动更新 | [research/models-dev.md](research/models-dev.md) |

`research/` 保存固定版本的源码研究与未来设计建议，不是已实现 API 的说明。
主要参考 Pi AI；DeepSeek Harness、Hermes、OpenCode、Cherry Studio 提供补充案例，OpenWebUI 侧重
UI/UX、多端同步和流式体验。当前系统行为仍以本目录现状手册与源码为准。
扩展方式已明确为仓库内模块、稳定接口与显式装配，不做插件系统。

## 给 agent 的硬性规则速览

1. `api/openapi.yaml` 是契约唯一真相源；所有 `DO NOT EDIT` 生成物改源头
   再生成并提交（CI diff 检查）。
2. 端点保护级别只看它在 `router.go` 哪个组里——没有默认拒绝。
3. 越权访问回 404，不回 403。
4. 前端：server state 归 TanStack Query，高频/ephemeral state 归 Zustand；
   带过滤 key 的 query 必须有 `placeholderData: (previous) => previous`。
5. 两套设计系统不混用：`.prism-next`/`.n-auth` 用 `--n-*`，
   `.shadcn-scope` 用 shadcn token；portal 里靠 `[data-slot]` 命中。
6. 一切用户可见字符串走 `t()`（en + zh 两个 locale 文件同步改）。
7. 验证：`make check`；前端没有测试框架，验证 = tsc + eslint + build。

## 仓库速查

```bash
make setup && make up          # 依赖 + Postgres/MinIO
make ser                       # API :8080（启动即迁移）
make web                       # Vite :5173，/api 代理到 :8080
make check                     # CI 全套
```

入口速览：`api/openapi.yaml`（契约）、`apps/server/internal/api/router.go`
（端点与保护级别一览）、`apps/web/src/routes/`（页面与守卫）、
`apps/web/src/lib/query-keys.ts`（缓存键）。
