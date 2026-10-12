# AGENTS.md

编码助手的仓库指引（`CLAUDE.md` 只是指向这里）。Go API 在 `apps/server`，React 前端在 `apps/web`，契约在 `api/openapi.yaml`。这里只放必须遵守的规则；各功能的完整设计与理由在 [docs/design/](docs/design/README.md)，动手改某块之前先读对应那份。

## 命令

```bash
make setup      # 安装依赖，创建 .env
make up         # docker compose 起 Postgres + MinIO（可选：DATABASE_URL 指向任意 Postgres 18 即可）
make ser        # API :8080，启动时自动迁移
make web        # Vite :5173，/api 代理到 :8080
make check      # lint + test + build，即 CI
make generate   # 重新生成 sqlc、API 契约、GenUI 目录
```

单个 Go 测试：`cd apps/server && go test ./internal/auth/ -run TestHashAndVerify -v`。

带库的 Go 测试读 `TEST_DATABASE_URL`（`make test` 默认用 `.env` 里 `DATABASE_URL` 的服务器和账号、库名换成 `openchat_test`；没有就跳过），只把它当作"哪台 Postgres、什么账号、库名前缀"：每个测试包用 `internal/db/dbtest` 在同一台服务器上建并迁移自己的库 `<原库名>_<包名>`，原库本身不碰。所以 `go test ./...` 可以并行，不用 `-p 1`。库跨次运行保留，测试不能假设表是空的，需要独占的数据就像 `TestAdminDashboardFigures` 那样要一个专用库（`dbtest.Pool(t, "api_dashboard")`）。

前端用 vitest 测 `src/lib` 里的纯逻辑：`pnpm --filter @openchat/web test`（`make test-web`），配置在 `apps/web/vitest.config.ts`，沿用 vite 配置的别名但不加载它的插件。测试夹具在 `src/lib/testing.ts`。其余靠 `tsc --noEmit`、`eslint .`、`vite build` 验证。

## 生成的代码

`api/openapi.yaml` 同时生成 `internal/api/openapi.gen.go` 和 `packages/api-client/src/schema.d.ts`，改完跑 `make generate-api`。标了 `DO NOT EDIT` 的文件（另有 `internal/db/sqlc/*`、`routeTree.gen.ts`）不要手改，改源头再生成。TypeScript 锁定 5.9.3（openapi-typescript 需要 JS 版编译器）。

## 两套设计系统，不要混用

聊天/登录界面用 `--n-*` 令牌、`n-` 类名、`.prism-next`；后台面板用 shadcn（`components/shadcn/**`，`.shadcn-scope`）。两边共用的东西放 `lib/`、`hooks/`，或者各写一份。以下两条命令应当无输出：

```bash
grep -rn "components/shadcn" apps/web/src/components/next apps/web/src/components/auth
grep -rn -- "--n-\|prism-next" apps/web/src/features/admin apps/web/src/components/shadcn
```

- Tailwind 4 的 `border` 只设宽度，没有默认颜色时画成黑框；两套系统各自在作用域内提供默认边框色。
- Radix portal 渲染在 `<body>` 下、作用域之外，用 `[data-slot]` 和 `[data-slot] *` 选中。
- `shadcn-base.css` 必须在 `@layer base` 里，否则会盖过 Tailwind 工具类。
- shadcn 组件搬自 satnaing/shadcn-admin，偏离上游处要写注释说明原因。

## 后端约定

- 路由在 `internal/api/router.go` 手工挂载，每个新端点都要有意识地放进某个保护分组。
- 归属写进查询（如 `GetConversation` 接收 `user_id`），别人的数据读出来就是不存在；后台接口对非管理员返回 404 而不是 403。
- 迁移嵌进二进制、启动时执行；`make migration name=...` 新建。新迁移的编号必须排在所有已有迁移之后：goose 遇到「库里已执行过更大的版本、却有一个更小的没执行」会拒绝启动，已升级的服务器起不来，而全新的库（CI）发现不了。`make migration` 取「当前 UTC 时间」和「最新编号 + 1」中较大者；不要手写编号。CI 的 Migration order 任务（`scripts/check-migration-order.sh`）会拦下排在已发布迁移之前的新文件。
- 对话是一条线，没有分支和版本，被替换的直接删除。会删行的新操作照改写的套路：`lockLive` → 查 busy → 先写后删 → 收集 `storage_key` → 提交后删文件 → 发 `transcript_changed`。
- 在对话里起 run 的新路径必须：`lockLive`（不是裸 `LockConversation`）、`requireRevised`、`prepareSnapshot`，并经 `createRun`（并发上限，同事务 advisory lock）。
- 新的软删除对话路径必须在同一事务里 `CancelConversationRuns`，提交后 `runner.Cancel`，并发 `conversation_deleted`。
- 被审核拒绝的行是 `messages.status = 'blocked'`：凡构造模型看到的内容的查询都要 `status <> 'blocked'`，只有 `ListMessages`（显示）和 `GetInput`（改写）看得见它；`agent.BuildContext`、`EstimateRow` 再各跳一次。
- 给模型的上下文用 `LoadContextRows` + `BuildRequestContext`；压缩从不改行，摘要的一致性靠 `context_summaries` 的 `ON DELETE CASCADE`；`HistoryLimit` 只是安全上限。
- 新增模型请求路径：先构造 `llm.Request`（带 `User`），`Begin(Call{InputEstimate: llm.EstimateInput(req)})` → `Reserve` → `Stream` → `End`；`partialStream` 在 `Reserve` 之后才创建；挑模型走 `pickChannel`（用户请求传 `isStaff(ctx)`，服务端任务传 `true`）并过容量检查；新 kind 要加 `usage_logs_kind` 约束。
- 容量上限按请求的 `model_id`（不是 `served_model`）、金额全程 `numeric` 在 SQL 里比较，不经 float。
- 读用户上传文件并发给模型必须走 `agent.attachmentResolver`（SHA-256 收据），别自己 `Read`；本地 `PUT` 用 `Local.PutNew`，写一次。
- 附件行消失的地方先收集 `storage_key`、`DELETE ... RETURNING`、再尽力删文件（`cleanup.go`）。头像放 `avatars/`，不放 `uploads/`（孤儿清扫只看后者）。存储配置只在后台，没有环境变量。
- 个人记忆快照在建 run 前取，同对话内字节不变；记忆工具里的用户取自 `agent.ScopeFrom(ctx)`，永不信参数；没有任何管理员接口读得到别人的记忆。
- 日志行带 ctx（`slog.*Context`）；后台 run 用 `ex.logger()`；`X-Request-Id` 由 `httpx.RequestID` 生成，不采用客户端的。
- 新增「查全体用户」的查询想想 `NOT guest`；新增能降级或停用账号的路径走 `LockAdminMembership`。

细节：[对话与转录](docs/design/conversations.md)、[用量与上限](docs/design/usage-and-limits.md)、[存储与运维](docs/design/storage-and-files.md)。

## 前端约定

- 服务端数据用 TanStack Query（键在 `lib/query-keys.ts`、`features/admin/use-admin.ts`），流式中间态和 UI 状态用 Zustand；含筛选条件的键加 `placeholderData: (previous) => previous`。会话用 `currentSession(queryClient)`，别用 `ensureQueryData`。
- 路由守卫挂在无路径布局的 `beforeLoad`（`_authed.tsx`、`admin.tsx`）。不经查询缓存的网络调用遇 401 要 `reportError`；换人要 `queryClient.clear()` + `resetChatStore()` + `forgetQueryCache()`。
- 聊天页只有一个 `NextChat` 实例（`components/next/ChatPage.tsx`，规则在 `lib/chat-view.ts`）：子路由不渲染组件；首条消息的流程只在服务器答复后才 `navigate`；在该路径上写页面状态要过 `alive`；新增「新对话」入口必须带 `newChatState`。
- 消息列表从尾部分页：对消息键不要 `invalidateQueries`，用 `refreshNewestMessages`（`transcript_changed` 先截缓存再重取）；交接走 `lib/hand-over.ts`，只有 `refreshConfirmed` 之后才 `settle`。
- 新增由响应或旧快照驱动、会写 `live` 的路径，必须过 `finished` 检查（`rememberFinished`）；`outbox` 由 `useSendMessage` 的 hook 级 `onError` 撤掉，别用调用级回调。
- 回复零件的 key 按位置 `r<n>` 起，不按消息 id，别改回去（流式显现依赖同一元素）。
- 删除对话的新路径要发/处理 `conversation_deleted`，并走 `lib/conversation-sync.ts` 的 BroadcastChannel（`announceDeleted`）。
- 标题一律经 `displayTitle`/`useDisplayTitle`，不要直接渲染 `conversation.title`；搜索结果键 `queryKeys.conversations.search(term)` 不放在 `list` 下；记忆、摘要查询不落本地缓存。
- 脚本触发的滚动用 `smoothScrollBehavior()`（`lib/motion.ts`）；工具结果里的链接只放行 `isHttpUrl`。
- 满额的模型在选择器里保留但禁用（`atCapacity`）；新增挑模型的前端入口要想清楚是否绕开它。
- 所有面向用户的字符串走 `t()`（`zh`、`en`）；动效遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`。
- 后台表单用 react-hook-form + zod（schema 建在组件内，用 `useWatch`）；表格服务端分页，`getRowId` 用记录 id；游标分页表格用 `DataTableCursorPagination`。

细节：[聊天页与前端约定](docs/design/frontend-chat.md)。

## 游客模式

游客没有账号、没有会话，服务端不保存任何东西，对话/记忆/指令都在浏览器 IndexedDB 里（`lib/guest/`）。

- 唯一要模型的请求是 `POST /guest/runs`（`api/guest_run.go`），复用 `chooseRun`/`createRun`/`startRun`，跑完删临时用户；其余 `/guest/*` 在开关关着时 404（`requireGuestMode`）。
- 新增聊天用的端点必须决定游客怎么答：本地服务（`lib/guest/router.ts`）还是服务端（`/guest/*`）；hooks 和组件不知道在跟谁说话（`appFetch`）。
- 对话规则在两边各有一份，要一起改：`transcript.ts`/`summary.ts`/`memory.ts` ↔ Go 的 SQL 与 `ModeManual`；每个删行的地方过 `survivingSummary`。
- 浏览器带来的一切都要有服务端收据：新增把浏览器内容送给模型的路径必须过 `verifyGuest`；带收据的东西先 `guestIDs` 翻译、后签名。
- 浏览器指定、又落在全表主键上的 id 要换成请求自己的（`guestIDs`）；文件 key 里绝不放浏览器给的 id（`guestFileKey`）。
- 本地消息行的时间只用服务端的或 `stampAfter`，绝不用浏览器的 `now()`；浏览器绝不能把本地 `blocked` 行发给服务端。
- 模式由根路由 `beforeLoad` 的 `enterPath` 决定，不读 `window.location`；预加载绝不改模式。

细节：[游客模式](docs/design/guest-mode.md)。

## 认证

- HttpOnly Cookie 会话，库里只存 token 的 SHA-256；任何「凭 token 认人」的路径都走 `GetSessionUser`，别另写（它还挡 `pending`）。
- 最后一个启用的管理员不能被降级或停用：改角色/停用在一个事务里先 `LockAdminMembership`，再读、写、数（409 `last_admin`），测试在 `admin_last_admin_pg_test.go`。
- 第一个账号永远是管理员（`RegisterUser` + `LockRegistration`）；批准单向（`approved: true`）；拒绝 = 停用，账号从不删除。
- 登录/注册限流用 `takeAttempt(ctx, attemptLimit{...}, ip, subject)`，先占位后验证，绝不按标识单独锁账号；IP 取 `clientIP`，必须部署在会正确设置转发头的代理后（见 `deploy/Caddyfile`）。

细节：[认证、注册与限流](docs/design/auth.md)。

## 内容审核

用户新发的输入先审后发（`internal/api/moderation*.go`）；拒绝是 422 `content_blocked`，服务不可用是 503 `moderation_unavailable`。

- 位置：`chooseRun` 之后、事务之前。新增把用户输入送上游的路径要想清楚属于哪一类（审 / 不审 / 靠收据）。
- 因大小而检查不了，必须包 `errTooLargeToCheck`（413 `message_too_large_for_moderation`），绝不当作服务出错（`on_error=allow` 会放行）；判定要完整（`verdictFrom`），缺字段算「回答不可用」，绝不当「没问题」。
- 被拒绝的发送由 `keepRefused` 写成 `blocked` 行；新增往转录末尾写行、起 run 的路径都要过 `requireRevised`（409 `revise_refused`）。
- 处罚在 `recordBlocked`：一个事务里先主体锁 `LockModerationSubject`、再 `LockAdminMembership`；管理员只记录不停用；不要给 `moderation_events` 加「user_id 或 guest_ip 必有一个」的约束。
- 自定义指令和手写记忆保存时和取快照时都要审；「审核过」只看哈希，不看 `source`。
- 上游请求都带用户标识 `llm.Request.User`（`end_user.go`）；新写会用模型的路径也要带。
- 前端：被拒绝走 `useSendError`，输入框随即是被拒绝消息的编辑器（`composerMode`，`lib/revise.ts`）；新增起 run 的入口要处理末行被拒绝的情形。

细节：[内容审核](docs/design/moderation.md)。

---

设计文档索引与各章节的完整说明：[docs/design/](docs/design/README.md)。更早的、已部分过时的手册见 [docs/README.md](docs/README.md)。
