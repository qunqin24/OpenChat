# OpenChat

一个流式聊天应用：React 19 单页应用跑在 Go API 之上，HTTP 契约由同一份
OpenAPI 文档生成。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 前端 | React 19 + TypeScript + Vite |
| 路由 | TanStack Router（文件式路由） |
| 服务端状态 | TanStack Query |
| 本地 / 流式状态 | Zustand |
| UI 原语 | Radix UI |
| 样式 | Tailwind CSS 4 + CSS 变量 |
| 动效 | Motion |
| 图标 | Lucide |
| Markdown | Streamdown |
| 国际化 | i18next（en、zh） |
| 后端 | Go + net/http + Chi |
| 数据库 | PostgreSQL 18 |
| 数据访问 | pgx + sqlc |
| 迁移 | **Goose**（嵌入二进制） |
| API 契约 | OpenAPI 3.1 → Go 服务端 + TypeScript 客户端 |
| 文件 | 本地磁盘，或兼容 S3 的对象存储（在后台 → 文件存储里配置，运行时生效） |
| Redis | 不使用 |
| 桌面端 | Tauri 2（以后再做；Web 构建产物已经是静态 SPA） |

## 快速开始

```bash
make setup   # 安装依赖，创建 .env 文件
make up      # 通过 docker compose 起 Postgres + MinIO
make ser     # 终端 1 —— API 跑在 :8080，启动时自动迁移
make web     # 终端 2 —— Vite 跑在 :5173
```

`http://localhost:5173` 把 `/api` 代理到 Go 服务，因此浏览器始终是同源访问，
SSE 无需 CORS 预检即可工作。

没有 Docker 也能跑：把 `DATABASE_URL` 指向任意一个 Postgres 18 实例即可，
MinIO 是可选的：附件默认存在服务端本地磁盘（`./data`），要用 S3 兼容存储时在后台 → 文件存储里配置。

## 目录结构

```
api/openapi.yaml          契约 —— 改动从这里开始
apps/server/              Go API
  cmd/openchat/           入口（serve | migrate | rollback）
  internal/api/           生成的 ServerInterface + 处理器
  internal/chat/          模型 provider 的接缝（默认装的是 echo 桩）
  internal/db/            pgx 连接池、goose 迁移、sqlc 查询
  internal/httpx/         JSON / SSE / 错误 / 日志辅助函数
  internal/storage/       附件存储：本地磁盘 / S3 预签名上传
apps/web/                 React 单页应用
  src/routes/             文件式路由 → routeTree.gen.ts
  src/hooks/              TanStack Query hooks
  src/stores/             Zustand（界面偏好、实时流）
  src/components/         ui/ 原语、layout/、chat/
packages/api-client/      由契约生成的类型化客户端
```

设计决策、前后端各自的手册、任务食谱和踩坑清单都在
[docs/](docs/README.md)（面向协作 agent）。

## 开发指南

### 修改 API

`api/openapi.yaml` 是唯一事实来源。改完之后：

```bash
make generate-api
```

这会重新生成 `apps/server/internal/api/openapi.gen.go`（Chi 处理器与模型）
和 `packages/api-client/src/schema.d.ts`（TypeScript 类型），因此契约一旦变化，
两端都会在编译期报错，而不是拖到运行时。

### 修改数据库

```bash
make migration name=add_something   # 新建 goose 迁移文件
# 编辑 apps/server/internal/db/migrations/*_add_something.sql
make migrate                        # 执行迁移
make generate-sql                   # 改了查询或 schema 后重新生成 sqlc
```

迁移通过 `go:embed` 嵌入，并在服务启动时自动执行，所以部署永远不需要单独的
迁移步骤。

### 流式响应

`POST /conversations/{id}/messages` 以 `text/event-stream` 响应；每一帧都是一个
JSON 格式的 `StreamEvent`（`start` → `delta`… → `done` | `error`）。

- 服务端在第一个 token **之前**就写入助手消息那一行，所以流到一半崩溃，
  回复也还在。
- 客户端把实时 token 放在 Zustand 里，把持久化的历史放在 TanStack Query 里
  —— 服务端状态和临时状态从不共用一个缓存。
- 点击停止会中断 fetch，已生成的部分存为 `complete`。只有 provider 真正报错
  才存为 `failed`。

### 接入真实模型

`internal/chat.Provider` 是一个两方法的接口。实现它，然后替换
`cmd/openchat/main.go` 里 `chat.Echo{}` 那一行即可，HTTP 层一行都不用改。

## 命令

`make help` 列出全部命令。常用的几个：

| 命令 | 作用 |
| --- | --- |
| `make check` | lint + test + build（CI 跑的就是这个） |
| `make generate` | 重新生成 sqlc 和 API 契约 |
| `make reset` | 清空本地 Postgres / MinIO 数据卷并重启 |
