# 常见任务手册

每个任务给出改动面。涉及生成物的步骤都不能省，CI 会 `git diff --exit-code`。规则本体在 [AGENTS.md](../AGENTS.md)，改某个功能前先读 [design/](design/README.md) 里对应那份；这里只列步骤，以及步骤里容易漏的接缝。

## 加 API 端点

1. `api/openapi.yaml`：加 path、`operationId`、请求和响应的 schema。错误响应用统一的 `ApiError`。
2. `make generate-api`（重新生成 `openapi.gen.go` 和 `schema.d.ts`）。
3. 在 `internal/api/` 对应文件里实现 `ServerInterface` 的新方法：`httpx.DecodeJSON` 解 body，`httpx.Fail` 报错，`httpx.JSON` 回包。
4. **在 `router.go` 手工挂载**，想清楚放哪个组：公开 / `Require` / `Require`+`RequireAdmin`。没有默认保护：忘挂是 404，挂错组是越权。
5. **归属写进查询**：凡是按 id 读写用户自己的数据，SQL 里带 `user_id`（如 `GetConversation`），别人的数据读出来就是不存在；后台接口对非管理员回 404，不回 403。
6. 需要新 SQL，见下一节。
7. 前端：`api.GET/POST/...`（类型已在 `schema.d.ts` 里）加 `unwrap`，query key 登记进 `lib/query-keys.ts` 或 `adminKeys`。
8. 验证：`cd apps/server && go build ./... && go test ./...`。

## 加数据库查询

1. `internal/db/queries/<domain>.sql` 加 `-- name: Foo :one` 块（写法见 [conventions.md](conventions.md#sqlc)）。
2. `make generate-sql`（相关迁移必须已存在，sqlc 的 schema 来源就是迁移目录）。
3. handler 里 `s.q.Foo(ctx, sqlc.FooParams{...})`，错误过 `dbErr`。
4. 行到契约模型的转换加进 `mapping.go`（`toXxx`）。

## 加迁移

```bash
make migration name=add_something   # 生成带 goose 骨架的 SQL 文件
# 编辑 apps/server/internal/db/migrations/*_add_something.sql
make migrate                        # 本地应用（make ser 启动时也会自动跑）
```

- **编号用 `make migration` 生成，不要手写**：新迁移必须排在所有已有迁移之后，原因和 CI 的 Migration order 任务见 AGENTS.md「后端约定」。
- Up 和 Down 都要写；每条迁移在事务里跑。
- 加了唯一索引，同步 `internal/api/server.go` 的 `conflictingField`。
- 改了表结构，通常要重跑 `make generate-sql` 并检查受影响的查询。

## 新增一条模型请求

不是「实现一个 Provider 接口」，规则在 AGENTS.md 的「新增模型请求路径」：先构造带 `User` 的 `llm.Request`，再 `Begin(Call{InputEstimate: ...})` → `Reserve` → `Stream` → `End`；挑模型走 `pickChannel` 并过容量检查，新 kind 要加 `usage_logs_kind` 约束。完整设计见 [design/usage-and-limits.md](design/usage-and-limits.md) 和 [design/conversations.md](design/conversations.md)；已有的写法可对照 `internal/agent/compaction.go`。

## 加对话事件

一次回答的进展和对话的变化都经事件流 `GET /conversations/{conversationId}/events`（`streamConversationEvents`）送到页面：先一个 `snapshot`，之后是一批批 `batch`，每条都是已提交的事实。新增一种事件：

1. `internal/agent/events.go`：定义事件类型并加 `isEvent()`。
2. 发布：属于某个 run 的在 `execution.commit` 的变更里产生事件（`internal/agent/execution.go`）；不属于任何 run 的（标题、删除对话、子智能体变化）由 handler 调 `s.runner.Hub().Publish(conversationID, agent.Batch{Events: ...})`，不带 run id 和版本（例：`api/titles.go`）。
3. `api/openapi.yaml`：新增 `XxxEvent` schema，加进 `RunEvent` 的 `oneOf` 和判别映射。
4. `make generate-api`。
5. `internal/api/agent_mapping.go` 的 `toRunEvent` 加一个 `case`（没有映射的事件会被丢掉）。
6. 前端：影响转录或运行状态的，改 `lib/run-state.ts` 的 `applyBatch`；触发查询失效或导航的，改 `hooks/use-conversation-events.ts`。事件流的前端规则见 [design/frontend-chat.md](design/frontend-chat.md)。

## 加前端页面/路由

1. `src/routes/` 加文件：文件名即路径，`$id` 是参数，`_` 前缀是无路径布局。聊天页有特殊规则（只有一个 `ChatView` 实例），先读 AGENTS.md「前端约定」。
2. 需要登录放在 `_authed.` 前缀下，需要管理员放在 `admin.` 前缀下。守卫在布局的 `beforeLoad` 里，页面本身不用查会话。
3. `routeTree.gen.ts` 由 vite 插件在开发或构建时自动更新，别手改。
4. 导航入口按需加到 `components/chat/Sidebar*`（聊天侧）或 `components/shadcn/layout/data/sidebar-data.ts` 的 `useSidebarData()`（后台）。

## 加后台表格/页面

参照 `features/admin/users/`：服务端分页，`use-table-url-state` 同步 URL，`getRowId` 用记录 id，列名进 `meta.title`，所有字符串走 `t()`。数据 hook 放 `features/admin/use-admin.ts`，key 用 `adminKeys`。游标分页的表格用 `DataTableCursorPagination`（例：使用日志）。

## 搬一个 shadcn 组件

1. 拷进 `components/shadcn/`，保留上游结构。
2. 硬编码的英文字符串全部换成 `t()`，locale key 加进 `en`、`zh` 两个文件。
3. 组件用到 portal（dialog、dropdown、popover、tooltip、select）时，确认 `shadcn-base.css`、`shadcn-theme.css` 里 `[data-slot]` 和 `[data-slot] *` 的覆盖对它生效。
4. 与上游的偏差写注释说明原因。

## 加环境变量

`internal/config/config.go` 的 `Load()` 里加字段（用 `env` / `envInt` / `envDuration` / `envList`，给安全的本地默认值），根目录 `.env.example` 加样例。前端的用 `VITE_` 前缀，经 `import.meta.env` 读，样例在 `apps/web/.env.example`，并在 `src/vite-env.d.ts` 里声明类型。注意存储配置只在后台，没有环境变量（AGENTS.md）。

## 改用户可见文案

`locales/en/common.json` 和 `locales/zh/common.json` 同步加或改 key。`t()` 的 key 有类型约束，改完 `tsc --noEmit` 会指出漏掉的引用。

## 常见验证命令

```bash
make check                                  # lint + test + build，CI 跑的全套
make generate                               # sqlc + openapi + genui 目录，全部重新生成
cd apps/server && go test -race ./...       # CI 用 -race
pnpm -r typecheck && pnpm --filter @openchat/web lint
pnpm --filter @openchat/web test            # 前端 vitest（等同 make test-web）
```

带库的 Go 测试怎么跑、测试库怎么建，见 AGENTS.md「命令」。
