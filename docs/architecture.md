> **历史文档。** 这是项目早期的设计记录，很多内容（比如 Echo 占位模型、只支持 S3 的存储、`internal/chat` 包）已经不存在了。现在的约定以仓库根目录的 [AGENTS.md](../AGENTS.md) 为准，部署见 [部署指南](deployment.md)。

# OpenChat 架构

OpenChat 是一个流式聊天应用：React 19 SPA 前端 + Go API 后端，HTTP 契约由
一份 OpenAPI 文档双向生成。本文描述整体结构、关键数据流和设计决策；
开发命令见根目录 `README.md` 与 `make help`。

配套文档：[backend.md](backend.md)（后端手册）、[frontend.md](frontend.md)
（前端手册）、[tasks.md](tasks.md)（常见任务步骤）、
[pitfalls.md](pitfalls.md)（已知坑清单）。

## 总览

```
浏览器 (SPA, :5173 dev)
   │  /api/* 经 Vite 代理，同源，无 CORS 预检
   ▼
Go API (:8080, Chi)
   ├── PostgreSQL 18 ── pgx 连接池 + sqlc 生成查询 + goose 嵌入式迁移
   ├── S3 (本地 MinIO) ── 附件的预签名上传/下载 URL
   └── chat.Provider ── 模型抽象，默认 Echo 桩，可换成真实 LLM
```

部署形态是单个 Go 二进制 + 静态前端产物：迁移用 `go:embed` 打进二进制、
启动时自动执行，因此发布不需要单独的迁移步骤。Tauri 桌面端在路线图上，
Web 构建本身已是纯静态 SPA。

## 契约是唯一的真相源

`api/openapi.yaml` 同时生成两侧代码（`make generate-api`）：

- `apps/server/internal/api/openapi.gen.go` — Chi handler 包装与模型（oapi-codegen）
- `packages/api-client/src/schema.d.ts` — TypeScript 类型（openapi-typescript）

改契约后必须重新生成并提交生成物，CI 会校验。所有标了 `DO NOT EDIT`
的文件（`internal/db/sqlc/*`、`openapi.gen.go`、`routeTree.gen.ts`、
`schema.d.ts`）一律改源头再生成，不手改。

TypeScript 仓库范围锁定 5.9.3：`openapi-typescript` 依赖 JS 编译器 API，
TS 7 原生移植版不提供。

生成物清单（全部 `DO NOT EDIT`，改源头再生成）：

| 生成物 | 源头 | 命令 |
| --- | --- | --- |
| `apps/server/internal/api/openapi.gen.go` | `api/openapi.yaml` | `make generate-api` |
| `packages/api-client/src/schema.d.ts` | `api/openapi.yaml` | `make generate-api` |
| `apps/server/internal/db/sqlc/*` | `internal/db/queries/*.sql` + migrations | `make generate-sql` |
| `apps/web/src/routeTree.gen.ts` | `apps/web/src/routes/*` | vite 插件自动生成 |

CI 会跑生成命令后 `git diff --exit-code`，不同步即失败。

## 后端

`apps/server/` 内部按职责分包：

| 包 | 职责 |
| --- | --- |
| `cmd/openchat` | 入口：`serve`（默认）/ `migrate` / `rollback` |
| `cmd/adminctl` | 管理 CLI |
| `internal/api` | 生成的 `ServerInterface` + 手写实现（`Server`） |
| `internal/auth` | 会话、密码哈希、`Require`/`RequireAdmin` 中间件 |
| `internal/chat` | `Provider` 接口与 `Echo` 桩 |
| `internal/config` | 环境变量加载 |
| `internal/db` | pgx 连接池、goose 迁移、`queries/`(sqlc 源） 与 `sqlc/`（生成物） |
| `internal/httpx` | JSON 编解码、SSE writer、错误、请求日志 |
| `internal/storage` | S3 预签名 URL |

### 路由手工挂载

路由在 `internal/api/router.go` 手工注册，而不是用 `HandlerFromMux`：
每个端点的保护级别（public / 登录 / admin）集中可见，新增端点必须被
显式放进某个组，不会因为"忘了挂"而继承 public。生成的 wrapper 仍负责
参数解析。

中间件栈：RequestID → RealIP → Recoverer → 请求日志 → Compress（仅 JSON)
→ CORS（允许凭证，供跨源时携带会话 cookie）。没有全局超时和
`WriteTimeout`——SSE 响应要保持开放到回复结束。

### 所有权在查询里，不是事后检查

`GetConversation` 这类查询把 `user_id` 作为条件：读别人的行得到的就是
"not found"。admin API 对非管理员同样回 404 而非 403——403 等于确认了
管理面板的存在。

### 迁移

Goose 迁移内嵌在二进制里（`internal/db/migrations/`，`go:embed`），
服务启动时执行；每条迁移在事务中运行，失败自动回滚。
新建：`make migration name=xxx`。

### 模型抽象

`internal/chat.Provider` 只有两个方法：

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, req Request, emit func(delta string) error) error
}
```

`emit` 返回 error（比如客户端断开）会中止流。默认 `Echo` 把用户最后一条
消息逐词回显，让整套栈在没有模型 key 时也能跑。接真实模型只需实现该
接口并替换 `cmd/openchat/main.go` 里的 `chat.Echo{}`，HTTP 层不动。

## 数据模型

```
users ──< sessions            （会话：只存 token 的 SHA-256）
  │  └─ role: user|admin, disabled_at, password_hash
  └─< conversations ──< messages ──< attachments
         (软删除 deleted_at)     (role, status, metadata jsonb)
```

- `conversations` 软删除，列表用 `(user_id, updated_at DESC, id DESC)`
  部分索引做 keyset 分页（见 `internal/api/cursor.go`）。
- `messages.status`：`pending | streaming | complete | failed`。
- `email` 的唯一性建在 `lower(email)` 上，大小写不敏感。
- 不删用户：禁用（`disabled_at`）是可逆等价物，删用户会级联带走会话。

## 鉴权

会话是 HttpOnly cookie（`openchat_session`），后端 `sessions` 表支撑。
cookie 携带 32 字节随机熵，数据库只存它的 SHA-256——库泄露也不能回放
成登录。SHA-256 而非 argon2 是有意的：token 本身熵足够、无法爆破，而
这个哈希要在每个已认证请求上跑。cookie 用 `SameSite=Lax`（Strict 会让
用户从外链进来时首屏表现为未登录），`Secure` 仅生产开启。

由此塑造的几个决定：

- 前端永远拿不到 token。"我登录了吗"通过调 `/auth/session` 回答——所以
  会话是 TanStack Query 而非 Zustand 状态。
- API client 必须带 `credentials: 'include'`（`lib/api.ts`），否则浏览器
  静默丢弃 cookie。
- 第一个注册的账户成为管理员，判定在 INSERT 内部完成，并发注册不会
  同时抢到。管理员不能移除自己的权限；最后一个可用管理员不能被降级
  或禁用。
- 管理员可创建账户（`POST /admin/users`）：显式指定角色、不签发会话，
  密码由管理员线下转交。禁用账户或重置密码会吊销该账户所有会话。
- 自助端点（`PATCH /auth/me`、`POST /auth/password`）与 admin 端点刻意
  分开且更窄：`UpdateOwnProfile` 根本没有 `role`/`disabled` 参数，任何
  输入都无法提升调用者自己的权限。改自己密码要提供旧密码、保留发起
  请求的会话、吊销其余会话。

## 流式消息链路

`POST /conversations/{id}/messages` 返回 `text/event-stream`，帧为 JSON
`StreamEvent`：`start → delta… → done | error`。

1. 先持久化 user 消息（status=`complete`），绑定附件。
2. **在第一个 token 之前**写入 assistant 行（status=`streaming`）——
   中途崩溃也有一条可恢复的回复记录。
3. 切换到 SSE 后，所有失败以流内 `error` 帧上报（状态行已提交）。
4. `provider.Stream` 每个增量同时写入 `strings.Builder` 和 `delta` 帧。
5. 结束写库用 `context.WithoutCancel` + 5s 超时的新 context：客户端断开
   也要把已收到的部分落库。
6. context 被取消（用户按 Stop、关标签页）→ 已收部分存为 `complete`；
   只有 provider 真实错误才存 `failed`。

前端对应的分工：流中的临时 token 放 Zustand（`stores/chat-store.ts`，
每秒变多次，会打爆 query 缓存），已持久化历史放 TanStack Query。流结束
后 `use-send-message.ts` invalidate 消息列表，Query 重新拉取权威版本——
这是"server state / client state 分家"的范式，新功能照此办理。

带过滤条件（分页、搜索、时间窗）的 query 都要加
`placeholderData: (previous) => previous`，否则每次改条件 UI 会清空。

## 前端

`apps/web/`：React 19 + Vite + TanStack Router（文件路由）+ Query +
Zustand + Radix + Tailwind 4 + i18next（en/zh）。

### 路由与守卫

`src/routes/` 文件路由，无路径 layout 承担守卫：`_authed.tsx` 要求会话，
`admin.tsx` 再要求 admin 角色。守卫跑在 `beforeLoad` 里，未登录访客不会
看到 shell 闪一下；用 `context.queryClient.ensureQueryData`，在受保护
路由间跳转不产生额外请求。

### 表格与表单

- 表格全部 manual 模式（`manualPagination`/`manualFiltering`）：分页和
  搜索在服务端。`getRowId` 必须是账户 id，选中状态才能挺过 re-fetch。
  列的译名同时放 `meta.title` 和 header renderer——列显隐菜单读不到
  header，会回退到裸列 id。
- 表单分两半：auth 页手写校验（`components/auth/validation.ts`，校验器
  返回 locale key）；admin 面板用 react-hook-form + zod（vendor 的
  `ui/form.tsx`）。zod schema 在组件内构建——错误消息要翻译，且
  `users-action-dialog` 的密码规则取决于新建/编辑。监听字段用
  `useWatch`，React compiler 不会给 `form.watch()` 做 memo。
- 字段级 API 错误经 `OpenChatError.field` 进表单（契约里一个输入对应
  一个 field）；其余错误走 banner/toast。auth 调用例外：经
  `features/auth/` 把服务端 code 映射成 `AuthErrorKey` locale key，
  服务端的 `message` 从不直接渲染（没有本地化）。

### 两套设计系统，刻意为之

这是本仓库最大的坑源。聊天 UI 和 admin 面板**不共享**视觉语言：

| | 聊天 / auth | admin 面板 |
| --- | --- | --- |
| 设计 token | `:root` 上的 `--n-*` | shadcn 的 `--background`、`--border` 等 |
| scope class | `.prism-next`、`.n-auth` | `.shadcn-scope` |
| 样式文件 | `next-theme.css`（prism Less 编译产物，原样保留以便 re-sync）、`auth.css`、`menu.css` | `shadcn-theme.css`、`shadcn-base.css` |

三条规则把它们隔开：

1. **Tailwind 4 里 `border` 只设宽度，颜色另有来源。** 没有默认值时回退
   `currentColor`（即文字颜色），渲染成黑框。两套系统各自在 scope 内
   提供默认值。
2. **Radix portal 渲染在 `<body>` 下，在所有 scope class 之外。** shadcn
   组件在那里靠 `[data-slot]` 和 `[data-slot] *` 命中——漏掉后代选择器
   就是下拉框里出现黑框的原因。
3. **`shadcn-base.css` 必须留在 `@layer base`。** 无 layer 的规则胜过所有
   cascade layer，会让默认边框色压过 Tailwind 的 `border-*` 工具类，
   `border-transparent` 预留的空间就被画成可见的线。

`focus.css` 的 `:focus-visible` 只作用于 prism 表面；shadcn 组件自带
focus ring，全局化会叠成双环。

`components/shadcn/**` vendor 自 satnaing/shadcn-admin（MIT）。与上游的
偏差要注释原因，便于将来 re-sync 区分意图和漂移。已知偏差：theme
provider 代理到本仓 store；灰色零色度；批量操作栏已本地化且 hover 不再
缩放；危险菜单项是红字而非红行。

### 其余约定

- **字体自托管**（`@fontsource-variable/*`）：设置页的字体选择器离线
  可用、不向第三方发请求。选择只作用于 `.shadcn-scope`——`body` 自己
  设了 `font-family`，`<html>` 上继承的类压不过它。
- **i18n**：所有用户可见字符串走 `t()`，包括 vendor 组件。locale 为
  `zh`/`en`，key 对 locale 文件有类型约束，重命名会破坏构建。vendor
  新组件前先翻译其字符串；`sidebar-data.ts` 因此不得不变成
  `useSidebarData()` hook（`t` 只在组件内可用）。
- **动效**：`globals.css` 全局遵守 `prefers-reduced-motion`；
  `data-motion="essential"` 的元素自行处理（加载指示器——完全静止的
  进度条和挂死的页面无法区分，用透明度脉冲代替位移动画）。

## 关键决策速查

| 决策 | 取舍 |
| --- | --- |
| 单份 OpenAPI 契约双向生成 | 契约变更在编译期而非运行期暴露 |
| 路由手工挂载 | 每个端点的保护级别集中可见，不能"被遗忘成 public" |
| cookie 会话 + 只存哈希 | 无 token 泄露面；前端无法读取凭证 |
| 所有权写进 SQL 条件 | 越权访问表现为 404，不泄露资源存在性 |
| assistant 行先于首 token 落库 | 崩溃可恢复；取消 ≠ 失败的语义清晰 |
| 迁移内嵌、启动执行 | 部署无额外步骤；每条迁移事务内执行 |
| 前端双设计系统并存 | prism 移植 UI 与 shadcn admin 各自保真，用 scope 隔离 |
| 禁用而非删除账户 | 可逆、保留会话历史；不提供账户删除 |

## 目前明确不做

无账户删除（禁用即可逆等价物）、无邀请/邮箱验证、设置页只移植上游
五个分区中的三个（Notifications 无事可通知，Display 与配置抽屉重复）、
无模型供应商目录（移植的 UI 预留了位置：按供应商的 key、模型列表、
reasoning effort、web-search 开关）、无附件 UI、无工具调用与推理块。
