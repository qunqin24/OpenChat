> **历史文档。** 这是项目早期的设计记录，很多内容（比如 Echo 占位模型、只支持 S3 的存储、`internal/chat` 包）已经不存在了。现在的约定以仓库根目录的 [AGENTS.md](../AGENTS.md) 为准，部署见 [部署指南](deployment.md)。

# 常见任务手册

每个任务给出完整改动面。凡是涉及生成物的步骤都不能省——CI 会
diff 检查。

## 加 API 端点

1. `api/openapi.yaml`：加 path + `operationId` + request/response schema。
   错误响应用统一的 `ApiError`。
2. `make generate-api`（重生成 `openapi.gen.go` 和 `schema.d.ts`）。
3. 在 `internal/api/` 对应文件实现 `ServerInterface` 的新方法；handler
   用 `httpx.DecodeJSON` 解 body、`httpx.Fail` 报错、`httpx.JSON` 回包。
4. **在 `router.go` 手工挂载**，并想清楚放哪个组：public / `Require` /
   `Require+RequireAdmin`。不存在"默认保护"——忘挂载是 404，挂错组是
   越权。
5. 需要新 SQL → 见「加数据库查询」。
6. 前端：调 `api.GET/POST/...`（类型已在 `schema.d.ts` 里）+ `unwrap`，
   query key 登记进 `query-keys.ts` 或 `adminKeys`。
7. 验证：`cd apps/server && go build ./... && go test ./...`。

## 加数据库查询

1. `internal/db/queries/<domain>.sql` 加 `-- name: Foo :one` 块。
2. `make generate-sql`（要求相关迁移已存在——sqlc 的 schema 源就是
   migrations 目录）。
3. handler 里 `s.q.Foo(ctx, sqlc.FooParams{...})`。
4. 行 → 契约模型的转换加进 `mapping.go`（`toXxx` 命名）。

## 加迁移

```bash
make migration name=add_something   # 生成带 goose 骨架的 SQL 文件
# 编辑 apps/server/internal/db/migrations/*_add_something.sql
make migrate                        # 本地应用（make ser 启动时也会自动跑）
```

- Up 和 Down 都要写；每条迁移在事务里跑。
- 加了唯一索引 → 同步 `internal/api/server.go` 的 `conflictingField`。
- 改了表结构 → 通常要重跑 `make generate-sql` 并检查受影响的查询。

## 接真实模型

1. 实现 `chat.Provider`（`Name()` + `Stream(ctx, req, emit)`），emit 返回
   error 时立即中止。
2. `cmd/openchat/main.go` 里替换 `chat.Echo{Delay: 25 * time.Millisecond}`。
3. 需要配置（API key 等）→ `config.go` 加 env + `.env.example` 加样例。
4. HTTP 层、SSE、前端都不用动。

## 加前端页面/路由

1. `src/routes/` 加文件（命名即路径，`$id` 是参数，`_` 前缀是无路径
   layout）。
2. 需要登录 → 放 `_authed.` 前缀下；需要 admin → 放 `admin.` 前缀下。
   守卫在 layout 的 `beforeLoad` 里，页面本身不用查会话。
3. `routeTree.gen.ts` 由 vite 插件自动更新（dev 启动或 build 时生成）。
4. 导航入口按需加到 `components/next/Sidebar*`（聊天侧）或
   `features/admin` 的 sidebar data hook（admin 侧）。

## 加 admin 表格/页面

参照 `features/admin/users/`：manual 分页 + `use-table-url-state` 同步
URL + `getRowId` 用实体 id + 列名进 `meta.title` + 所有字符串 `t()`。
数据 hooks 放 `features/admin/use-admin.ts`，key 用 `adminKeys`。

## vendor 一个 shadcn 组件

1. 拷进 `components/shadcn/`，保留上游结构。
2. 硬编码英文字符串全部换成 `t()`，locale key 加进 en/zh 两个文件。
3. 若组件用 portal（dialog/dropdown/popover/tooltip/select）：确认
   `shadcn-base.css`/`shadcn-theme.css` 里 `[data-slot]` 和
   `[data-slot] *` 的覆盖对新组件生效。
4. 与上游的偏差写注释说明原因。

## 加 SSE 事件类型

1. `api/openapi.yaml` 的 `StreamEvent` 判别联合加成员。
2. `make generate-api`。
3. 服务端：`messages.go` 里 `sse.Event(StreamXxx{...})`。
4. 前端：`stream.ts` 解析（通常免改，帧是 `data: <json>`）、
   `use-send-message.ts` 的 switch 加 case。

## 加环境变量

`config.go` 的 `Load()` 里加字段（用 `env`/`envBool`/`envDuration`/
`envList` helper，给安全的本地默认值），`.env.example` 加样例。
前端用 `VITE_` 前缀经 `import.meta.env` 读，`apps/web/.env.example`。

## 改用户可见文案

`locales/en/common.json` + `locales/zh/common.json` 同步加/改 key。
`t()` 的 key 有类型约束，改完 `tsc --noEmit` 会指出遗漏的引用。

## 常见验证命令

```bash
make check          # lint + test + build，CI 跑的全套
make generate       # sqlc + openapi 双侧重生成
cd apps/server && go test -race ./...
pnpm -r typecheck && pnpm --filter @openchat/web lint
```
