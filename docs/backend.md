# 后端手册

面向改动 `apps/server/` 的 agent。读这份之前先看
[architecture.md](architecture.md) 了解全局。

## 目录地图

```
apps/server/
  cmd/openchat/main.go        入口：serve(默认) / migrate / rollback
  cmd/adminctl/               管理 CLI
  internal/
    api/
      openapi.gen.go          生成物，DO NOT EDIT
      router.go               手工挂载路由 + 中间件栈（新端点必须来这里登记）
      server.go               Server 结构体、dbErr、duplicateError、conversationExists
      auth.go                 register/login/logout/session/me/password
      admin.go                stats/users CRUD/bulk
      conversations.go        会话 CRUD
      messages.go             消息列表 + SSE 流式回复
      files.go                预签名上传
      cursor.go               keyset 分页游标
      mapping.go              sqlc 行 → 契约模型（toConversation/toMessage/toUser/...）
    auth/
      middleware.go           Require / RequireAdmin
      session.go              token 生成与哈希、cookie 读写
      password.go             argon2id 哈希、VerifyPassword、DummyVerify
      context.go              WithUser / MustUser / WithSessionID / SessionIDFrom
    chat/provider.go          Provider 接口 + Echo 桩
    config/config.go          环境变量 → Config，全部有默认值
    db/
      db.go                   pgx 池 + goose Migrate/Rollback
      migrations/             goose SQL，go:embed，启动时执行
      migrations/embed.go     embed 声明
      queries/*.sql           sqlc 源（-- name: X :one/:many/:exec/:execrows）
      sqlc/                   生成物，DO NOT EDIT
    httpx/
      respond.go              JSON / NoContent / Fail / DecodeJSON / APIError
      sse.go                  SSEWriter（Event / Comment）
      logging.go              RequestLogger
    storage/s3.go             PresignPut / PresignGet / ObjectURL
  sqlc.yaml                   sqlc 配置（uuid → google/uuid 等 override）
  oapi-codegen.yaml           chi-server + models + embedded-spec
```

## 请求生命周期

1. chi 中间件（`router.go` 顶部顺序）：RequestID → RealIP → Recoverer →
   `httpx.RequestLogger` → Compress(5, 仅 `application/json`) → CORS。
   **没有全局 timeout，也没有 `srv.WriteTimeout`**——`CreateMessage` 的 SSE
   要开放到回复结束。
2. `ServerInterfaceWrapper`（生成代码）解析 path/query 参数，解析失败由
   `ErrorHandlerFunc` 统一回 400 `invalid_request`。
3. `mw.Require`：取 `openchat_session` cookie → `HashToken` →
   `GetSessionUser`（联表 users，且要求 `disabled_at IS NULL`）。查不到
   （过期/吊销/账户被禁用）→ 清 cookie + 401。成功后往 context 放
   `user` 和 `sessionID`。
4. `mw.RequireAdmin`（必须挂在 Require 内）：非 admin → **404 不是 403**。
5. handler：`auth.MustUser(ctx)` 取用户 → 走 `s.q`（sqlc）→
   `httpx.JSON`/`NoContent`/`Fail` 回包。

## 错误处理约定

统一错误形状（契约里的 `ApiError`）：

```json
{ "code": "email_taken", "message": "...", "field": "email" }
```

- `httpx.NewError(status, code, message)` — 普通错误。
- `httpx.NewFieldError(field, code, message)` — 400 + `field`，前端表单把
  它渲染在对应输入框旁边。一个响应只 blame 一个字段。
- `httpx.Fail(w, r, err)`：非 `*APIError` 记日志并回 500 `internal_error`
  ——**原始错误细节不会泄露给客户端**。
- `dbErr(err)`：`pgx.ErrNoRows` → `ErrNotFound`。所有按 id 的读/写都要过它。
- `duplicateError(err)`：把 PG `23505` 按约束名映射成 field error
  （`users_email_lower_idx` → `email_taken`，`users_name_lower_idx` →
  `name_taken`）。**新增唯一索引后要同步 `conflictingField`**，否则冲突
  只会报 500。不做"先查再插"——唯一索引本身就是判定，先查照样 race。
- SSE 切换之后的失败只能发 `error` 帧（状态行已提交），见 `messages.go`。

code 用 snake_case；前端 `features/auth/http.ts` 的 `SERVER_ERRORS` 表把
code 映射到 locale key，新增 auth 相关 code 要同步那边。

## 路由注册

`router.go` 手工分组（public / Require / Require+RequireAdmin）。加端点的
完整流程见 [tasks.md](tasks.md#加-api-端点)。要点：

- 保护级别是唯一靠"放进哪个组"决定的——没有默认拒绝的兜底。
- 生成的 wrapper 方法名 = openapi.yaml 里的 `operationId` 的导出形式
  （`listMessages` → `wrapper.ListMessages`）。

## sqlc 约定

- 源文件在 `internal/db/queries/*.sql`，`-- name: Foo :one|:many|:exec|:execrows`
  注释声明生成方法。
- 可空参数用 `sqlc.narg('x')`，必填用 `sqlc.arg('x')`；`LIMIT` 参数必须
  `sqlc.arg`。
- schema 来源是 `migrations/` 目录本身（`sqlc.yaml` 里 `schema:
  internal/db/migrations`），**迁移没写好 sqlc 就编不过新查询**。
- 生成配置要点：`emit_pointers_for_null_types`（可空列生成指针）、
  `emit_empty_slices`（列表返回 `[]` 不是 null）、uuid override 到
  `github.com/google/uuid`、可空 uuid 生成 `*uuid.UUID`。
- 改完查询或 schema 跑 `make generate-sql`。
- 事务用 `s.pool.Begin` + `s.q.WithTx(tx)`（参考 `BulkUpdateUsers`），
  `defer tx.Rollback` 兜底。

## 分页

两套并存，别混用：

- **keyset**（`cursor.go`）：conversations 和 messages 用。游标 =
  `base64url("<RFC3339Nano>|<uuid>")`，取 `limit+1` 行判断有没有下一页。
  对应的部分索引按 `(user_id, updated_at DESC, id DESC)`。
- **offset**（page/limit/total）：admin 用户表用，因为 UI 要页码和总数。

`clampLimit` 统一限制 page size（默认 50，上限 100）。

## SSE 写消息（`messages.go`）

时序（改这里之前先读 `CreateMessage` 注释）：

1. 校验 + 写 user 消息（`complete`）→ `AttachToMessage` 绑附件。
2. 拉历史（`historyLimit = 100` 条）。
3. **先写 assistant 行**（`streaming`，空 content）——崩溃也有记录。
4. `httpx.NewSSEWriter` 切换响应为 SSE（此后只能发帧）。
5. `start` 帧带 `messageId` → `provider.Stream` 逐 delta 发 `delta` 帧。
6. 落库用 `context.WithoutCancel(ctx)` + 5s 超时——客户端断了也要保存。
7. `ctx.Err() != nil`（Stop/关页）→ `complete`；provider 返回错误 →
   `failed` + `error` 帧；正常 → `done` 帧带最终 Message。
8. `TouchConversation` 刷 `updated_at` 让列表排序更新。

`Provider` 接口见 `internal/chat/provider.go`：`Name()` +
`Stream(ctx, req, emit)`。接真实模型只要实现它并改 `main.go` 里一行
`chat.Echo{Delay: 25ms}`。

## 鉴权细节

- cookie `openchat_session`：HttpOnly、SameSite=Lax、30 天、`Secure` 由
  `cfg.IsProduction()` 决定（TLS 在代理侧终止时 `r.TLS` 不可靠）。
- token：32 字节随机 → base64url；库里只存 `sha256(token)`。查找在
  每个已认证请求上跑，所以用 SHA-256 不是 argon2。
- 密码：argon2id（m=64MiB, t=3, p=4，OWASP 推荐），参数编码进 hash
  字符串，日后调参向后兼容。
- `Login` 对所有失败回同一个 `invalid_credentials`，且对不存在的邮箱
  跑 `DummyVerify` 抹平时序。
- 第一个注册用户是 admin：`RegisterUser` 的 INSERT 里 `CASE WHEN
  (SELECT count(*) FROM users) = 0`，并发安全。
- 会话吊销时机：禁用账户、改密码（保留发起请求的会话）、admin 重置他人
  密码/禁用（全删，包括 admin 改自己的密码——旧会话的浏览器不可信）。
- `last_seen_at` 只在 `GET /auth/session` 里写，不给每个请求加写放大。

## 存储（`storage/s3.go`）

- `CreateUpload`：先建 `attachments` 行（`message_id` 为 NULL 的孤儿），
  返回预签名 PUT（15 分钟有效）+ `uploads/YYYY/MM/DD/<uuid><ext>` key。
  客户端直传 S3，之后 `CreateMessage` 的 `attachmentIds` 认领。
- 读侧：`ListMessages` 用 `PresignGet` 签 1 小时 URL。
- 上限 25 MiB（`maxUploadBytes`）。
- 没有 MinIO 也能跑：只有 `CreateUpload`/附件解析会失败。

## 配置（`config.Load`）

全部环境变量在 `.env.example` 里有样例；`config.go` 里每个都有默认值，
唯一硬要求是 `DATABASE_URL` 非空。`.env` 加载在 `main.go`：依次尝试
`.env` 和 `../../.env`（因为从 `apps/server` 启动时根 `.env` 不在 cwd）。

## 测试与验证

```bash
cd apps/server && go test ./...            # 全部
go test ./internal/auth/ -run TestXxx -v   # 单个
go vet ./...                               # lint
```

CI（`.github/workflows/ci.yml`）跑：`go vet`、`go test -race`、
`go build`、**生成物新鲜度检查**（`sqlc generate` + `oapi-codegen` 后
`git diff --exit-code`）——改了 `openapi.yaml` 或 `queries/*.sql` 必须
提交重新生成的代码。

## 改后端时的自检清单

- [ ] 端点进了 `router.go` 正确的保护组
- [ ] 按 id 的读写都带 `user_id` 条件（或先 `conversationExists`）
- [ ] `pgx.ErrNoRows` 过了 `dbErr`
- [ ] 新唯一索引登记进 `conflictingField`
- [ ] 新 auth 错误 code 登记进前端 `SERVER_ERRORS`
- [ ] 生成物已重新生成并提交
- [ ] `go vet` + `go test -race` 过
