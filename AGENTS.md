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

带库的 Go 测试读 `TEST_DATABASE_URL`（`make test` 默认取 `.env` 里 `DATABASE_URL` 的服务器和账号、库名换成 `openchat_test`；没有就跳过），只把它当作「哪台 Postgres、什么账号、库名前缀」：每个测试包用 `internal/db/dbtest` 在同一台服务器上建并迁移自己的库 `<原库名>_<包名>`，原库不碰，所以 `go test ./...` 可以并行。库跨次运行保留，测试不能假设表是空的；要独占数据就用专用库（`dbtest.Pool(t, "api_dashboard")`，见 `TestAdminDashboardFigures`）。

前端用 vitest 测纯逻辑：网页版和 App 共用的在 `packages/chat-core`（`pnpm --filter @openchat/chat-core test`，夹具在 `chat-core/src/testing.ts`，语言文件也在这里），网页版自己的在 `apps/web/src/lib`（`pnpm --filter @openchat/web test`）；`make test-web` 都跑。chat-core 不能依赖 React 或浏览器全局对象（它的 ESLint 拦着），需要的部分留在各自的 App 里（如 `useDisplayTitle`、`newChatState`、store 的 `create`）。其余靠 `tsc --noEmit`、`eslint .`、`vite build` 验证。

## 生成的代码

`api/openapi.yaml` 同时生成 `internal/api/openapi.gen.go` 和 `packages/api-client/src/schema.d.ts`，改完跑 `make generate-api`。标了 `DO NOT EDIT` 的文件（另有 `internal/db/sqlc/*`、`routeTree.gen.ts`）不要手改，改源头再生成。TypeScript 锁定 5.9.3（openapi-typescript 需要 JS 版编译器）。

## 两套设计系统，不要混用

聊天/登录界面用 `--n-*` 令牌、`n-` 类名、`.chat-ui`；后台面板用 shadcn（`components/shadcn/**`，`.shadcn-scope`）。两边共用的东西放 `lib/`、`hooks/`，或者各写一份。以下两条命令应当无输出：

```bash
grep -rn "components/shadcn" apps/web/src/components/chat apps/web/src/components/auth
grep -rn -- "--n-\|chat-ui" apps/web/src/features/admin apps/web/src/components/shadcn
```

- Tailwind 4 的 `border` 只设宽度，没有默认颜色时画成黑框；两套系统各自在作用域内提供默认边框色。
- Radix portal 渲染在 `<body>` 下、作用域之外，用 `[data-slot]` 和 `[data-slot] *` 选中。
- `shadcn-base.css` 必须在 `@layer base` 里，否则会盖过 Tailwind 工具类。
- shadcn 组件搬自 satnaing/shadcn-admin，偏离上游处要写注释说明原因。

## 后端约定

- 路由在 `internal/api/router.go` 手工挂载，每个新端点都要有意识地放进某个保护分组。
- 归属写进查询（如 `GetConversation` 接收 `user_id`），别人的数据读出来就是不存在；后台接口对非管理员返回 404 而不是 403。
- 迁移嵌进二进制、启动时执行；`make migration name=...` 新建，不要手写编号。新迁移必须排在所有已有迁移之后，否则已升级的服务器上 goose 拒绝启动（全新的库发现不了）；CI 的 Migration order 任务（`scripts/check-migration-order.sh`）会拦下。
- 对话是一条线，没有分支和版本，被替换的直接删除。会删行的新操作照改写的套路：`lockLive` → 查 busy → 先写后删 → 收集 `storage_key` → 提交后删文件 → 发 `transcript_changed`。
- 在对话里起 run 的新路径必须：`lockLive`（不是裸 `LockConversation`）、`requireRevised`、`prepareSnapshot`，并经 `createRun`（并发上限，同事务 advisory lock）。
- 新的软删除对话路径必须在同一事务里 `CancelConversationRuns`，提交后 `runner.Cancel`，并发 `conversation_deleted`。
- 被审核拒绝的行是 `messages.status = 'blocked'`：凡构造模型看到的内容的查询都要 `status <> 'blocked'`，只有 `ListMessages`（显示）和 `GetInput`（改写）看得见它；`agent.BuildContext`、`EstimateRow` 再各跳一次。
- 给模型的上下文用 `LoadContextRows` + `BuildRequestContext`；压缩从不改行（摘要靠 `context_summaries` 的 `ON DELETE CASCADE` 保持一致），`HistoryLimit` 只是安全上限。
- 压缩只有 run 自己会做，用户没有手动入口（只留 `GET` 与撤销 `DELETE` `/conversations/{id}/compaction`），不要加回去。模型因长度拒绝（`context_too_long`）时，开着自动压缩的 run 强制压一次并重试，每个 run 至多一次（`recoverContextLength`）。
- 新增模型请求路径：先构造 `llm.Request`（带 `User`），`Begin(Call{InputEstimate: llm.EstimateInput(req)})` → `Reserve` → `Stream` → `End`；`partialStream` 在 `Reserve` 之后才创建；新 kind 要加 `usage_logs_kind` 约束。
- 挑模型走 `pickChannel(ctx, modelID, asker)`，容量检查在它里面，别绕开：用户请求传 `askerFrom(ctx)`，服务端任务建 `asker{staff: true, …}`（`admin` 只在替管理员办事时为真，`ignoreCap` 只用于「能不能答」的校验）。渠道按优先级、同级按权重选（`servingOrder`），模型还没输出就失败的 run 自动换渠道（`failover`），别改回只取第一个。
- 同一次回复里的工具调用并发执行（`agent.Options.ToolConcurrency`，默认 10）：新工具必须能与其他工具同时运行。
- 容量上限按请求的 `model_id`（不是 `served_model`）、金额全程 `numeric` 在 SQL 里比较，不经 float。
- 读用户上传文件并发给模型必须走 `agent.attachmentResolver`（SHA-256 收据），别自己 `Read`；本地 `PUT` 用 `Local.PutNew`，写一次。
- 文本文件一律以 `text/plain` 存（前端按扩展名认，`TEXT_EXTENSIONS`），发给模型时是文字（`llm.FileBlock`，经 `modelTakes`），不是 UTF-8 就不发；它的字随消息一起审并记 SHA-256 收据（`attachmentTexts`）。超过 `llm.InlineTextBytes` 且模型能调工具时只发概览卡（`agent.fileCard`），模型用 `file_read`/`file_search`（`agent/files.go`）按需读：只认本对话非 `blocked` 用户消息里的文件，必须经 `attachmentResolver.one` 核对收据，不再审。
- Word/Excel/PDF 由 `internal/doctext` 读成文字，原件保留、文字不另存；发送时 `readDocuments` 读出文字送审，收据是原件的摘要；PDF 在模型能读且不超过 `llm.NativePDFPages` 页时发原件，否则发文字。
- 引用其他对话（`conversationIds`，`api/references.go`、`agent/references.go`）是 `parts` 里的 `llm.ConversationMediaType` 零件，不是文件：构建请求时由 `attachmentResolver` 按 run 的用户现读；消息不再审，标题随这条消息审（`withReferenceTitles`），模型只读引用时记下的标题（零件的 `Filename`）。读附件零件的新代码要认得它（`agent.ReferencedConversation`），别把 `Ref` 当 uuid。
- 附件行消失的地方先收集 `storage_key`、`DELETE ... RETURNING`、再尽力删文件（`cleanup.go`）。头像放 `avatars/`，不放 `uploads/`（孤儿清扫只看后者）。新增让用户存文件的路径要在同一事务里过 `roomFor`（`LockUserStorage` 后再数；上限见 `storage_quota.go`），按上传声明的大小预留（本地 `PUT` 与预签名 URL 都把它当上限），发送时仍按实际大小再算。存储配置只在后台，没有环境变量。
- 个人记忆快照在建 run 前取，同对话内字节不变；记忆工具里的用户取自 `agent.ScopeFrom(ctx)`，永不信参数；没有任何管理员接口读得到别人的记忆。
- 日志行带 ctx（`slog.*Context`）；后台 run 用 `ex.logger()`；`X-Request-Id` 由 `httpx.RequestID` 生成，不采用客户端的。
- 新增能降级或停用账号的路径走 `LockAdminMembership`。

细节：[对话与转录](docs/design/conversations.md)、[用量与上限](docs/design/usage-and-limits.md)、[存储与运维](docs/design/storage-and-files.md)。

## 前端约定

- 服务端数据用 TanStack Query（键在 `chat-core/src/query-keys.ts`、`features/admin/use-admin.ts`），流式中间态和 UI 状态用 Zustand；含筛选条件的键加 `placeholderData: (previous) => previous`。会话用 `currentSession(queryClient)`，别用 `ensureQueryData`。
- 路由守卫挂在无路径布局的 `beforeLoad`（`_authed.tsx`、`admin.tsx`）。不经查询缓存的网络调用遇 401 或 403 `account_disabled` 要 `reportError`（`main.tsx` 的 `setErrorListener` 据此离开会话、经 `showIfAccountDisabled` 去 `/disabled`）；换人要 `queryClient.clear()` + `resetChatStore()` + `forgetQueryCache()`。
- 聊天页只有一个 `ChatView` 实例（`components/chat/ChatPage.tsx`，规则在 `chat-core/src/chat-view.ts`）：子路由不渲染组件；首条消息的流程只在服务器答复后才 `navigate`；在该路径上写页面状态要过 `alive`；新增「新对话」入口必须带 `newChatState`。
- 消息列表从尾部分页：对消息键不要 `invalidateQueries`，用 `refreshNewestMessages`（`transcript_changed` 先截缓存再重取）；交接走 `chat-core/src/hand-over.ts`，只有 `refreshConfirmed` 之后才 `settle`。跟随事件流的循环只有一份：`chat-core/src/conversation-follow.ts`（`followConversation`），网页和 App 各自只传连接方式和「对话没了去哪」；发送前后的缓存动作在 `chat-core/src/send.ts`。
- 新增由响应或旧快照驱动、会写 `live` 的路径，必须过 `finished` 检查（`rememberFinished`）；`outbox` 由 `useSendMessage` 的 hook 级 `onError` 撤掉，别用调用级回调。
- 流式文字用 `useReveal`（`MarkdownText.tsx`，节奏在 `chat-core/src/reveal.ts`）逐步显示前缀，**不要给每个字建元素或动画**（v1.1.0 这样做，长思维链把 CPU 拖到 90%）；回复零件的 key 按位置 `r<n>` 起，不按消息 id，别改回去（显现依赖同一元素）。
- 删除对话的新路径要发/处理 `conversation_deleted`，并走 `lib/conversation-sync.ts` 的 BroadcastChannel（`announceDeleted`）。
- 标题一律经 `displayTitle`/`useDisplayTitle`，不要直接渲染 `conversation.title`；搜索结果键 `queryKeys.conversations.search(term)` 不放在 `list` 下；记忆、摘要查询不落本地缓存。
- 手机上禁止缩放（viewport、`lib/no-zoom.ts`、`html` 的 `touch-action`）、安全区由根元素/输入框/侧栏各自让开；`content-visibility` 在 WebKit 上不用（`data-webkit`，有消息变空白的 bug）。
- 脚本触发的滚动用 `smoothScrollBehavior()`（`lib/motion.ts`）；工具结果里的链接只放行 `isHttpUrl`。
- 满额的模型在选择器里保留但禁用（`atCapacity`）；新增挑模型的前端入口要想清楚是否绕开它。
- 所有面向用户的字符串走 `t()`（`zh`、`en`）；动效遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`。
- 后台表单用 react-hook-form + zod（schema 建在组件内，用 `useWatch`）；表格服务端分页，`getRowId` 用记录 id；游标分页表格用 `DataTableCursorPagination`。

细节：[聊天页与前端约定](docs/design/frontend-chat.md)。

## 定时任务

到点以用户名义把 `prompt` 发进任务专属对话（`api/tasks.go`、`task_scheduler.go`、`task_tool.go`，周期在 `internal/schedule`）。

- 起 run 只走 `startTaskRun`（即 `CreateMessage` 的路径，带 `auth.WithUser(owner)`），别另写。
- 指令按自定义指令的套路审：保存时 `task`（算违规）、运行前哈希不符或模型写的用 `task_run`（不算违规）；被拒的任务自动暂停。
- 新建/恢复要过 `LockTaskOwner` + 数量上限；调度器用 `SKIP LOCKED` 认领并在同一事务推进 `next_run_at`。
- 工具和入口都受后台开关 `scheduled_tasks` 管，临时对话没有。

细节：[定时任务](docs/design/scheduled-tasks.md)。

## 写作块

模型把要反复修改的文字写进文档（`api/documents.go`、`document_tool.go`，前端 `DocumentCard`/`DocumentPanel`）。

- 文档和模型的版本挂在 run 上（`ON DELETE CASCADE`），回答被改写/重新生成时一起消失；用户的版本不挂 run。
- 模型修改用定点替换，`find` 必须恰好出现一次；写入前锁文档并核对版本，用户保存也带 `baseVersion`（409 `version_conflict`）。
- 用户写的版本在模型读取时才审（来源 `document`，算违规），模型写的直接记为已检查。
- 选中文字「提出修改请求」发出的消息以 `<document-selection id title>` 包着原文开头（`chat-core/src/document-selection.ts`），显示、编辑、复制都要先拆开；格式改了要同步系统提示词。
- 用户的改动经轮次说明（`DocumentNote`，`agent.TurnNote`，每个 run 算一次、挂在输入消息末尾、不入库）告诉模型；整篇重写（`content`）遇到这个 run 没看过的用户版本要拒绝并附改动，「看过」记在 `Scope.State`。新增让模型读到用户文档文本的路径照样先过 `documentText`。
- 修改中的段落标记按卡片实例（`useId`）登记在 `document-activity-store`，不要改回按工具调用 id；面板里的弹出层画在浮条内，不用 portal。

细节：[写作块](docs/design/writing-blocks.md)。

## 子智能体

主 Agent 用 `subagent` 把独立的活交给子 Agent：子 Agent 是一个隐藏的子对话（`conversations.kind = 'subagent'`），每段工作是那里的一次普通 run（`api/subagents.go`）。

- 新增「列出/查全体对话」的查询要加 `kind = 'chat'`；`GetConversation` 已经带上，所以对话接口找不到子对话。不要给子对话开任何接受用户文字的接口：用户只能经主 Agent 转达，审核只在用户和主 Agent 之间。
- 子 run 不走 `createRun`、不占 `max_concurrent_runs`，走 `admitSubagent`（同事务 advisory lock，按对话/用户计数）；`CountUserRunningRuns` 只数 `chat` 对话。前台子 Agent 没名额时是 `queued`（有子对话、没有 run，`startQueued` 按派出顺序放行）；等待没开始就结束要删掉它（`DeleteQueuedSubagent`）。
- 报告一次 run 一份，在结束子 run 的事务里写（`agent.Subagents.Ended`），以子 run 为主键；送达只在两处：运行中的 run 在请求边界的自己的 commit 里（`Take`，持对话锁），空闲时 `deliverReports`（`lockLive` → `requireIdle` → `requireRevised` → `createRun`）。新增结束 run 的路径要让 `AfterRun` 照常触发，否则报告会晾到清扫。
- 报告行（`messages.origin = 'subagent'`）不是用户的提问：重新生成的「最后一条提问」、改写、搜索、导出、标题素材都要跳过 `origin` 非空的行；`DeleteAnswerTo` 把它当回答删，送达行被删报告就回到待送达（`ON DELETE SET NULL`）。
- 删除路径：子对话随 `launched_by` 的 run 级联删除；会删 run 的新路径在事务前 `runningSubagents`、提交后 `cancelGoneRuns`；新的软删除路径用 `CancelConversationRuns`（已包含子对话的 run）。
- 子 run 的工具、提示词看 `agent.Scope.Parent`：新工具要决定子 Agent 能不能用（默认不给），不给嵌套、`ask_user`、记忆、定时任务、写作块写操作。

细节：[子智能体](docs/design/sub-agents.md)。

## 联网

`web_search`/`web_fetch` 在 `internal/tools`，只在对话开着联网时提供（`UsesWeb`）。

- `web_fetch` 的地址来自模型输出，只能经 `GuardedClient` 连接（每次拨号都检查目标是公网地址，每一跳重定向同样，不读环境代理）；不要新增绕开它的抓取路径。
- 屏蔽站点（`Blocklist`）对请求的地址和重定向后的地址各查一次。
- 读取输出有上限：每次最多 `fetchMaxChars`，用 `offset` 续读；`find` 的段数和总量同样封顶。不要新增一次返回整篇长页面的路径。
- 搜索的时间范围（`recency`）和限定网站（`sites`）：后端能过滤的交给后端，回来的结果在 `WebSearch.Run` 里再按同样条件过一遍。
- 写给模型的文字（工具说明、提示词、结果）一律英文；联网提示词是 `api/agent_setup.go` 的 `webGuide`，主 Agent 和子 Agent 共用。
- 模型用错工具，改提示词或工具描述，不要在服务端改写它的输出。

细节：[联网：搜索与读网页](docs/design/web-tools.md)。

## 认证

- HttpOnly Cookie 或 `Authorization: Bearer`（只有带 `X-OpenChat-Client: app` 且不带 `Origin` 的登录才在响应体里给 token，`SignInResult`），库里只存 token 的 SHA-256；任何「凭 token 认人」的路径都走 `auth.TokenFromRequest` + `GetSessionUser`，别另写（它还挡 `pending`）。App 的会话由 `GET /auth/session` 续期（`RenewSession`）；Android 通行密钥的来源和 `assetlinks.json` 都由后台登记的 App 得出（`android_passkeys.go`）。
- 停用不让人退出登录：会话保留，`Require` 经 `GetDisabledSessionUser` 认出后一律回 403 `account_disabled`（`api/account_disabled.go`）。新增停用账号的路径要记 `users.disabled_reason`/`disabled_note`、别删会话；`/auth/logout` 不在登录分组里。
- 最后一个启用的管理员不能被降级或停用：改角色/停用在一个事务里先 `LockAdminMembership`，再读、写、数（409 `last_admin`），测试在 `admin_last_admin_pg_test.go`。
- 第一个账号永远是管理员（`RegisterUser` + `LockRegistration`）；批准单向（`approved: true`）；拒绝 = 停用。删除账号只有管理员在后台显式操作（`DeleteUser`）：不能删自己，过 `LockAdminMembership` 和最后一个管理员检查，先 `CancelUserRuns`、给用量行打 `deleted_user` 标签，`DeleteAccount` 交回文件 key，提交后删文件。
- 通行密钥登录（`api/passkeys.go`）验证通过后走和密码登录同一段：停用/待审批的拒绝、`issueSession`；失败用 `passkeyLimit` 计数；挑战存 `webauthn_challenges`，一次性取用。新的身份验证方式放进后台「身份验证」页的新 Tab。
- 登录/注册限流用 `takeAttempt(ctx, attemptLimit{...}, ip, subject)`，先占位后验证，绝不按标识单独锁账号；IP 取 `clientIP`，必须部署在会正确设置转发头的代理后（见 `deploy/Caddyfile`）。

细节：[认证、注册与限流](docs/design/auth.md)。

## 内容审核

用户新发的输入先审后发（`internal/api/moderation*.go`）；拒绝是 422 `content_blocked`，服务不可用是 503 `moderation_unavailable`。

- 位置：`chooseRun` 之后、事务之前。新增把用户输入送上游的路径要想清楚属于哪一类（审 / 不审 / 靠收据）。
- 因大小而检查不了，必须包 `errTooLargeToCheck`（413 `message_too_large_for_moderation`），绝不当作服务出错（`on_error=allow` 会放行）；判定要完整（`verdictFrom`），缺字段算「回答不可用」，绝不当「没问题」。
- 被拒绝的发送由 `keepRefused` 写成 `blocked` 行；新增往转录末尾写行、起 run 的路径都要过 `requireRevised`（409 `revise_refused`）。
- 处罚在 `recordBlocked`：一个事务里先主体锁 `LockModerationSubject`、再 `LockAdminMembership`；管理员只记录不停用；`moderation_events.user_id` 可以为空（账号删除后 `ON DELETE SET NULL`），不要加非空约束。
- 自定义指令和手写记忆保存时和取快照时都要审；「审核过」只看哈希，不看 `source`。
- 上游请求都带用户标识 `llm.Request.User`（`end_user.go`）；新写会用模型的路径也要带。
- 前端：被拒绝走 `useSendError`，输入框随即是被拒绝消息的编辑器（`composerMode`，`chat-core/src/revise.ts`）；新增起 run 的入口要处理末行被拒绝的情形。

细节：[内容审核](docs/design/moderation.md)。

---

设计文档索引与各章节的完整说明：[docs/design/](docs/design/README.md)。代码约定与设计取舍、常见改动的步骤、易错清单、部署和发布说明等见 [docs/README.md](docs/README.md) 的索引；与 `docs/design` 或代码不一致时以后两者为准。
