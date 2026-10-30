# 代码约定与设计取舍

这里放「读代码看不出来、AGENTS.md 又没有展开」的约定和理由：为什么这么写，改动时哪些地方要一起动。必须遵守的规则在 [AGENTS.md](../AGENTS.md)，各功能的完整设计在 [design/](design/README.md)，这里不重复它们；写到的地方会指过去。

本文不放目录地图（会过时，直接看代码），也不放某个功能的流程。每条都对着磁盘上的代码核对过；动到相关代码时顺手更新这里。

## 后端

### 契约、生成物与路由

- `api/openapi.yaml` 是契约的唯一真相源，同时生成 Go 一侧（`internal/api/openapi.gen.go`）和 TypeScript 一侧（`packages/api-client/src/schema.d.ts`）。契约改动在编译期而不是运行期暴露。生成物的规矩见 AGENTS.md 的「生成的代码」；CI 重新生成后 `git diff --exit-code`，不同步即失败。
- 路由在 `internal/api/router.go` 手工挂载，不用 `HandlerFromMux`：每个端点的保护级别（公开 / `Require` / `Require`+`RequireAdmin`）集中可见，新端点必须被有意识地放进某个组，不会因为「忘了挂」而继承公开。生成的 `ServerInterfaceWrapper` 仍负责解析参数，解析失败由 `ErrorHandlerFunc` 统一回 400 `invalid_request`。包装器方法名是 `operationId` 的导出形式（`listMessages` → `wrapper.ListMessages`）。
- `RequireAdmin` 必须挂在 `Require` 里面（它假设上下文里已有用户），非管理员回 404 而不是 403，理由见 AGENTS.md：403 等于确认了管理面板存在。
- 没有全局超时，`http.Server` 也不设 `WriteTimeout`（`cmd/openchat/main.go` 里有注释）：事件流要和页面一样长地开着。别加，会把事件流切断。

### 错误处理

统一的错误形状是契约里的 `ApiError`：

```json
{ "code": "email_taken", "message": "...", "field": "email" }
```

- `httpx.NewError(status, code, message)`：普通错误。
- `httpx.NewFieldError(field, code, message)`：400 加 `field`，前端表单把它显示在对应输入框旁。**一个响应只指一个字段**。
- `httpx.Fail(w, r, err)`：不是 `*APIError` 的错误只记日志、对客户端回通用的 500 `internal_error`，**原始错误细节不会泄露**。
- `dbErr(err)`：`pgx.ErrNoRows` → `ErrNotFound`。按 id 的读写都要过它，否则「不存在」会变成 500。
- 唯一约束冲突：`duplicateError` 按约束名调 `conflictingField`，把 PG `23505` 翻成指向具体字段的错误（`users_email_lower_idx` → `email_taken`，`users_name_lower_idx` → `name_taken`，`channels_name_lower_idx` → `channel_name`）。**新增唯一索引要同步 `conflictingField`**，认不出的约束返回空串，冲突就只会是 500，故意不乱指一个字段。不做「先查再插」：唯一索引本身就是判定，先查照样有竞争。
- 事件流（`GET /conversations/{id}/events`）一旦切成 SSE，状态行已经发出，之后的失败不能再用 `httpx.Fail`，只能结束这条流让客户端重连（见 [pitfalls.md](pitfalls.md)）。创建消息的 `POST` 是普通 JSON 请求（202），照常用 `httpx.Fail`。

code 用 snake_case。前端要给用户看的 code 要在 `features/auth/http.ts` 的 `SERVER_ERRORS` 里登记（见下「API 调用」）。

### sqlc

- 源在 `internal/db/queries/*.sql`，`-- name: Foo :one|:many|:exec|:execrows` 声明生成的方法。schema 来源就是迁移目录本身（`sqlc.yaml` 的 `schema: internal/db/migrations`），**迁移没写好，sqlc 就编不过新查询**。
- 可空参数用 `sqlc.narg('x')`，必填用 `sqlc.arg('x')`；现有查询里的 `LIMIT`/`OFFSET` 参数一律是 `sqlc.arg`。
- `sqlc.yaml` 里值得知道的几项：`emit_pointers_for_null_types`（可空列生成指针）、`emit_empty_slices`（列表返回 `[]` 而不是 `null`）、`emit_interface`（`Querier`）、uuid 映射到 `github.com/google/uuid`（可空的是 `*uuid.UUID`）。
- 改完查询或迁移跑 `make generate-sql`。行到契约模型的转换放 `mapping.go`（`toXxx`）。

### 事务

`s.q` 是连接池上的查询；要多条语句同属一个事务，用 `pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { q := s.q.WithTx(tx); ... })`（例：`answers.go`、`UpdateUser`）。也有手写 `s.pool.Begin` 加 `defer tx.Rollback` 的（`BulkUpdateUsers`），新代码用前者。要先拿锁的事务（`LockAdminMembership`、`lockLive` 等）规则见 AGENTS.md。

### 分页

两套并存，别混用：

- **keyset**（`cursor.go`）：游标是不透明的 `base64url("<RFC3339Nano 时间>|<uuid>")`，在 `(时间, id)` 上翻页，插入新行时页面不抖；坏游标回 400 `invalid_cursor`。对话列表和消息用它，部分索引按 `(user_id, updated_at DESC, id DESC)`。
- **offset**（page/limit/total）：后台用户表用，因为界面要页码和总数（`ListUsers` 的注释说了取舍）。

`clampLimit` 统一限制页大小（默认 50，上限 100）。

### 密码、会话与自助接口

会话的整体设计见 [design/auth.md](design/auth.md)；下面是那里没有说的实现取舍。

- **会话 token 用 SHA-256，不用 argon2**（`auth.HashToken`）：token 是 32 字节随机数，没有可爆破的空间，而这个哈希要在每个已认证请求上算。库里只存哈希，库泄露也回放不成登录。
- **cookie `SameSite=Lax`，不是 Strict**：Strict 会让从外部链接进来的人首屏被当成未登录。`Secure` 由 `cfg.IsProduction()` 决定，不看 `r.TLS`（TLS 在代理侧终止，请求本身是明文）。有效期 30 天（`SessionTTL`）。
- 前端永远拿不到 token，「我登录了吗」靠调 `/auth/session` 回答，所以会话是 TanStack Query 状态而不是 Zustand。
- **密码用 argon2id**，m=64 MiB、t=3、p=4（`auth/password.go`），参数编码在 hash 字符串里，日后调参不影响旧密码的验证。比较用常量时间。
- 登录对所有失败回同一个 `invalid_credentials`；账号不存在时也跑一遍 `auth.DummyVerify`，抹平响应时间，**删掉它就会泄露邮箱是否注册过**。
- `users.last_seen_at` 只在 `GET /auth/session` 里写（`TouchUserSeen`），失败只记警告：给每个请求加一次写入换不来任何好处。
- **自助接口刻意与后台接口分开，而且更窄**：`PATCH /auth/me` 用的 `UpdateOwnProfile` 查询根本没有 `role`、`disabled_at` 参数，无论输入是什么都提升不了调用者自己的权限；头像也是单独的端点和查询。给自助端点加参数前先想清楚：缺字段是权限边界，不是遗漏。修改自己的密码要旧密码，保留发起请求的会话、删掉其余会话。
- `email`、`name` 的唯一性建在 `lower(...)` 索引上，大小写不敏感；别加按原文的 `UNIQUE`。

### 迁移

迁移用 goose，嵌进二进制（`go:embed`）、启动时执行，所以发布不需要单独的迁移步骤；每条迁移在事务里跑，失败回滚。新迁移的编号规则见 AGENTS.md（用 `make migration`，别手写编号）。

### 日志

日志行带 ctx（AGENTS.md）。密码、token 原文不进日志也不进错误信息；`.env` 不入库，新的密钥只以占位形式写进 `.env.example`。

## 前端

### API 调用

- 一律走 `lib/api.ts`：`api` 单例加 `unwrap()`。openapi-fetch 返回 `{ data, error, response }` 而不抛错，`unwrap` 把错误变成 `OpenChatError`，TanStack Query 才能感知；漏掉 `unwrap` 时 4xx 会静默通过。
- `api` 创建时带 `credentials: 'include'`，这是浏览器发送和保存 HttpOnly 会话 cookie 的唯一原因，新建 client 时别丢（丢了所有请求都表现为未登录）。`fetch` 换成了 `appFetch`，它对每个响应调 `noteBuild`（版本提示，见 [design/storage-and-files.md](design/storage-and-files.md)）。
- 基础地址是 `VITE_API_BASE_URL`，缺省 `/api/v1`（`lib/api-base.ts`）。开发时 Vite 把 `/api` 代理到 `VITE_PROXY_TARGET`（缺省 `http://localhost:8080`），浏览器始终同源，cookie 和事件流都不需要 CORS。
- **事件流不走 openapi-fetch**（它建不了流式响应模型）：用 `packages/api-client/src/stream.ts` 里手写的 `streamConversationEvents`，解析 `data: <json>` 帧、跳过 `:` 注释帧（保活），产出契约里的 `ConversationEvent`。前端怎么用它见 [design/frontend-chat.md](design/frontend-chat.md)。
- 错误怎么显示：`OpenChatError.field` 有值 ⇒ 表单内联在对应输入框；否则 banner 或 toast。**服务端的 `message` 不直接渲染**（没有本地化）。登录注册相关的调用经 `features/auth/http.ts`：服务端 code 在 `SERVER_ERRORS` 里映射成 locale key（`AuthErrorKey`），整次尝试失败的 code（`FORM_LEVEL`）显示成表单顶部的横幅。新增面向用户的 auth 错误 code 要同步这两处。

### QueryClient 默认值

`lib/query.ts`：`staleTime` 30 秒、`gcTime` 5 分钟、`refetchOnWindowFocus: false`；重试：服务端明确拒绝的请求（`OpenChatError` 且状态码 < 500）永不重试，其余最多重试 2 次；mutation 不重试。所有查询和变更的失败都会经 `setErrorListener` 登记的一个监听器（`main.tsx`），所以会话结束这类全局性的事只在一处处理；不经查询缓存的网络调用要自己 `reportError`。

### 表单与表格

- 登录注册页手写校验（`components/auth/validation.ts`），校验器返回 locale key 而不是文字。
- 后台表单用 react-hook-form + zod（`ui/form.tsx` 一套）。zod schema 在组件内构建：错误消息要 `t()`，且规则可能依赖组件状态（`users-action-dialog` 的密码规则取决于新建还是编辑）。监听字段用 `useWatch`，不用 `form.watch()`（源码注释的理由：后者每次返回新函数，React compiler 没法围着它做 memo）。
- 服务端的字段错误进表单：后台直接调 `/admin/...` 的表单读 `OpenChatError.field`，按字段挑译文再 `form.setError(field, ...)`（`users-action-dialog.tsx`，译文不取服务端 `message`）；设置页的资料、密码表单走 `features/auth` 的 `AuthFieldError.field` 和 `key`。没有字段的错误走 toast。
- 后台表格是服务端分页：`getRowId` 必须是记录 id，选中状态才能挺过重新获取；列的译名要同时放在 header 渲染里和 `meta.title`（列显隐菜单读不到 header，会回退成裸列 id）；表格状态与 URL 同步靠 `hooks/use-table-url-state.ts`。

### 两套设计系统的补充

规则本体在 AGENTS.md「两套设计系统」。补充两点：

- **`focus.css` 的 `:focus-visible` 只作用于聊天/登录/菜单这些 `--n-*` 表面**（`.chat-ui`、`.n-auth`、`.n-menu`）。shadcn 组件自带 focus ring，全局化就会叠成双环。
- `chat-theme.css` 现在是本地文件，就地修改；其他聊天样式表在它之后导入，同一属性只放在一处（该文件头部的说明），不要在两处各写一遍。

### i18n

- 所有面向用户的字符串走 `t()`，包括搬来的 shadcn 组件（搬进来先翻译字符串）。语言 `zh`、`en`，单一 namespace `common`（`locales/{en,zh}/common.json`），顶层分区见文件本身。
- `types/i18next.d.ts` 让 `t()` 的 key 有类型：改名会让构建报错，新增 key 两个文件都要加。`locales/locales.test.ts` 还按文本检查同一个对象里重复写的 key（`JSON.parse` 会静默保留后一个）。
- `t` 只在组件和 hook 里可用，模块级常量不能用：所以侧栏数据是 `useSidebarData()`（`components/shadcn/layout/data/sidebar-data.ts`），不是常量。
- 语言探测顺序：`localStorage`（键 `openchat.lang`）→ 浏览器语言，兜底 `en`。

### 字体与主题

- 字体自托管（`@fontsource-variable/inter`、`manrope`，`config/fonts.ts` 列出选项，另有 `system`），设置页的字体选择离线可用，也不向第三方发请求。选择写成 `<html>` 上的 `font-<name>` 类，**只作用于 `.shadcn-scope` 和 `[data-slot]`**（`shadcn-base.css`）：`body` 自己设了 `font-family`，从 `<html>` 继承来的值压不过它；聊天界面的字体是聊天设计的一部分，不归读者选。
- 主题：`stores/ui-store.ts` 的 `theme`（`light`/`dark`/`system`，持久化在 `localStorage` 键 `openchat.ui`）经 `applyTheme` 切换 `<html>` 上的 `.dark`；`--n-*` 令牌和 shadcn 令牌都挂在这个类上。
- 动效：`globals.css` 全局遵守 `prefers-reduced-motion`；`data-motion="essential"` 的元素自己处理（加载指示器：完全静止的进度条和卡死的页面无法区分，所以用透明度脉冲代替位移）。

### 启动画面

`index.html` 内联了 `#boot` 的标记和样式，JS 起来之前就画出来。`lib/boot.ts` 的 `dismissBootSplash` 在**路由空闲后**才撤下（根布局在首个路由还在查会话时就渲染了，挂载时撤会露出空壳）；移除靠 `transitionend` 加 600 ms 兜底超时（减少动效或后台标签页不触发 `transitionend`，元素会无形地盖住整个应用吞掉点击）。改画面只改 `index.html`：`boot.ts` 的预览用的标记是从活元素读的，没有第二份拷贝。

## 设计取舍速查

| 决策 | 取舍 |
| --- | --- |
| 单份 OpenAPI 契约生成两侧代码 | 契约变更在编译期暴露 |
| 路由手工挂载 | 每个端点的保护级别集中可见，不能「被遗忘成公开」 |
| cookie 会话，库里只存哈希 | 库泄露不能回放；前端读不到凭证 |
| 所有权写进 SQL 条件，越权回 404 | 不泄露资源存在性（AGENTS.md） |
| 自助接口比后台接口窄 | 缺字段就是权限边界，输入无法提升自己 |
| 迁移内嵌、启动执行 | 部署无额外步骤，每条迁移事务内执行 |
| 流式中间态放 Zustand，历史放 Query | 流式更新每秒多次，会冲垮查询缓存（design/frontend-chat.md） |
| 前端双设计系统并存 | 聊天与后台各自保真，用作用域隔离 |

## 改完自检

后端：

- [ ] 端点进了 `router.go` 正确的保护组
- [ ] 按 id 的读写带 `user_id` 条件（归属在查询里）
- [ ] `pgx.ErrNoRows` 过了 `dbErr`
- [ ] 新唯一索引登记进 `conflictingField`
- [ ] 新的 auth 错误 code 登记进前端 `SERVER_ERRORS`
- [ ] 生成物已重新生成并提交
- [ ] `make check` 通过

前端：

- [ ] 新字符串进了 `zh` 和 `en` 两个 locale 文件
- [ ] 带过滤条件的 query 有 `placeholderData: (previous) => previous`，key 登记进 `chat-core/src/query-keys.ts` 或 `adminKeys`
- [ ] 没有把服务端 `error.message` 直接渲染到界面
- [ ] 改动属于哪套设计系统，样式写在对应作用域里；portal 里的覆盖同时写了 `[data-slot]` 和 `[data-slot] *`
- [ ] `tsc --noEmit`、`eslint`、`pnpm --filter @openchat/web test` 通过
