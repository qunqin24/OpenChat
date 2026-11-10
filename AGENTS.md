# AGENTS.md

编码助手的仓库指引（`CLAUDE.md` 只是指向这里）。Go API 在 `apps/server`，React 前端在 `apps/web`，契约在 `api/openapi.yaml`。这里只放必须遵守的规则；各功能的完整设计与理由在 [docs/design/](docs/design/README.md)，动手改某块之前先读对应那份。

## 命令

```bash
make setup      # 安装依赖，创建 .env
make up         # docker compose 起 Postgres + MinIO（可选：DATABASE_URL 指向任意 Postgres 18 即可）
make ser        # API :8080，启动时自动迁移
make web        # Vite :5173，/api 代理到 :8080
make check      # lint + test + build（CI 另查：生成码是否最新、go test -race、迁移顺序）
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
- 给模型的上下文用 `LoadContextRows` + `BuildRequestContext`；压缩从不改行（摘要靠 `context_summaries` 的 `ON DELETE CASCADE` 保持一致），`agent.HistoryLimit` 只是安全上限。
- 压缩只有 run 自己会做，用户没有手动入口（只留 `GET` 与撤销 `DELETE` `/conversations/{id}/compaction`），不要加回去。模型因长度拒绝（`context_too_long`）时，开着自动压缩的 run 强制压一次并重试，每个 run 至多一次（`recoverContextLength`）。
- 新增模型请求路径：先构造 `llm.Request`（带 `User`），`Begin(Call{InputEstimate: llm.EstimateInput(req)})` → `Reserve` → `Stream` → `End`；`partialStream` 在 `Reserve` 之后才创建；新 kind 要加 `usage_logs_kind` 约束。
- Claude 的思考块只在产生它的那段对话里有效：系统提示词、工具、之前的消息变了，API 就拒绝整个请求（400，对话从此每条都失败）。Anthropic 适配器给每个思考块记下当时的前缀指纹（`ReasoningPart.Binding`，`providers/anthropic/binding.go`），回放时对不上的不发，仍被拒就去掉全部思考重试一次。会在两次请求之间改动这些内容的新做法（换提示词、按 run 增减工具、只在一次 run 里加的说明）照样能用，代价是之前的思考丢掉、缓存失效，能只往后追加就别改前面。
- 挑模型走 `pickChannel(ctx, modelID, asker)`，容量检查在它里面，别绕开：用户请求传 `askerFrom(ctx)`，服务端任务建 `asker{staff: true, …}`（`admin` 只在替管理员办事时为真，`ignoreCap` 只用于「能不能答」的校验）。渠道按优先级、同级按权重选（`servingOrder`），模型还没输出就失败的 run 自动换渠道（`failover`），别改回只取第一个。
- 从 App（bearer token）发起的 run 记 `runs.from_app`（`chooseRun` 读 `auth.FromApp`），这条消息的轮次说明里因此多一段 `phoneGuide`（不放系统提示词：在 App 和网页之间换着发会改掉之前的前缀）；新的起 run 路径经 `chooseRun` 自然带上，服务端自己起的不带。
- 同一次回复里的工具调用并发执行（`agent.ToolConcurrency`，10 个）：新工具必须能与其他工具同时运行。作用于同一份状态、必须按顺序来的工具（如 `browser`）实现 `agent.SequentialTool`：它在一轮里的调用按模型给的顺序逐个跑，别的工具照常并发。
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
- 服务端每 50ms 推一次流式增量（`PublishInterval`，不落库，`Batch.Step` 记第几次），每 150ms 才落库（`PartialInterval`）；快照经 `Runner.LiveOf` 取内存里的最新状态，去重按 (version, step)（服务端 `inSnapshot`、客户端 `applyBatch`），新增发布或订阅路径照此。
- 流式文字一到就排版，每批按此前的出字速度（估计值 `rate`，正常时约一个批间隔）从上到下分段扫入、每段 0.2 秒淡入；上游停顿后一次到的一大段同样按停顿前的速度流出，但最晚在到达后 `MAX_CATCH_UP_MS`（2.5 秒）内全部出完，挨得近的几批接成一段；回答停住超过 `STILL_WRITING_MS`（2 秒）显示「仍在生成」（`mayStallWriting`，网页和 App 各自的 `useStillWriting`）（分段和淡入曲线在 `chat-core/src/arrival.ts`；网页 `useArrivalFade`/`arrival-fade.ts` 按 `Range` 每行盖浮层，App `FadingText`/`ArrivalFade.tsx` 按 `onTextLayout` 的行盖）：揭开和淡入靠盖在文字上的浮层，没到时候的段浮层不透明，不碰 React 渲染的文字。**不要建淡完不回收的逐字/逐词元素或动画**（v1.1.0 用 Streamdown 的 `animated` 给每个字一个永不回收的动画 span，长思维链上万个，CPU 拖到 90%）。回复零件的 key 按位置 `r<n>` 起，不按消息 id，别改回去（淡入依赖同一元素）。
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
- 选中文字「提出修改请求」发出的消息以 `<document-selection id title>` 包着原文开头（`chat-core/src/document-selection.ts`），显示、编辑、复制都要先拆开（`parseUserMessage`，见「技能」）；格式改了要同步系统提示词。
- 用户的改动经轮次说明（`DocumentNote`，`agent.TurnNote`，每个 run 算一次、挂在输入消息末尾，存在 `runs.turn_note`，之后每次请求都附回原处；续接的 run 沿用原来的、有新内容才接在后面；告诉过的版本记在 `run_document_notes`，不重复说）告诉模型；整篇重写（`content`）遇到这个 run 没看过的用户版本要拒绝并附改动，「看过」记在 `Scope.State`。新增让模型读到用户文档文本的路径照样先过 `documentText`。
- 修改中的段落标记按卡片实例（`useId`）登记在 `document-activity-store`，不要改回按工具调用 id；面板里的弹出层画在浮条内，不用 portal。

细节：[写作块](docs/design/writing-blocks.md)。

## 技能

模型按需读的「做事方法」（Agent Skills 格式：`SKILL.md` 的 frontmatter 给 `name`、`description`），站点的（管理员写，`owner_id` 为空）或个人的（`internal/skills`、`api/skills.go`、`tools/skill.go`）。

- 每次保存是一个新版本（只留最近 20 个）；对话在快照里记下可用技能的**版本**（`memory.Snapshot.Skills`，和记忆快照同一份），`skill` 工具只认快照里的名字和版本，所以改技能只影响新对话，提示词字节不变。
- 个人技能照自定义指令审：保存时审（来源 `skill`，算违规），保存时没审过的在取快照时审（`snapshot`，不算违规，被拒的版本记 `blocked` 不进快照）；站点技能是管理员的，不审。一个技能的全部文字不超过 `skills.MaxTextChars`，保证一次审得完。
- 查询都按 owner 限定（个人 = 会话用户，站点 = 空），`/admin/skills` 碰不到任何人的个人技能；同名时个人的盖过站点的。
- 内置技能在 `internal/skills/builtin/<name>/`（`go:embed`，启动时解析，不合法就 panic，测试会拦），管理员只能关（`builtin_skills_off`）；同名时站点盖过内置、个人盖过两者。
- 输入框打 `/` 选的技能（附件、深度研究等在「+」里）以 `<skill name="…"/>` 行放在消息开头（`chat-core/src/skill-mention.ts`），服务端不展开，系统提示词让模型先用 `skill` 打开；显示、编辑、复制都经 `parseUserMessage`/`rewordUserMessage`（`chat-core/src/user-message.ts`，技能、深度研究、图片引用、引用段落一并拆开和拼回，网页和 App 共用），别各自只拆认得的那一层。
- 子 Agent 只能打开派它时 `skills` 参数点名的技能（`conversations.skills`，必须在主对话的清单里），读主对话快照里的版本；不点名就没有 `skill`。
- 现在只收文本文件；脚本只是给模型看的参考，等沙箱来了才能跑。

细节：[技能](docs/design/skills-plan.md)。

## 子智能体

主 Agent 用 `subagent` 把独立的活交给子 Agent：子 Agent 是一个隐藏的子对话（`conversations.kind = 'subagent'`），每段工作是那里的一次普通 run（`api/subagents.go`）。

- 新增「列出/查全体对话」的查询要加 `kind = 'chat'`；`GetConversation` 已经带上，所以对话接口找不到子对话。不要给子对话开任何接受用户文字的接口：用户只能经主 Agent 转达，审核只在用户和主 Agent 之间。
- 子 run 不走 `createRun`、不占 `max_concurrent_runs`，走 `admitSubagent`（同事务 advisory lock，按对话/用户计数）；`CountUserRunningRuns` 只数 `chat` 对话。前台子 Agent 没名额时是 `queued`（有子对话、没有 run，`startQueued` 按派出顺序放行）；等待没开始就结束要删掉它（`DeleteQueuedSubagent`）。
- 报告一次 run 一份，在结束子 run 的事务里写（`agent.Subagents.Ended`），以子 run 为主键；送达只在两处：运行中的 run 在请求边界的自己的 commit 里（`Take`，持对话锁），空闲时 `deliverReports`（`lockLive` → `requireIdle` → `requireRevised` → `createRun`）。新增结束 run 的路径要让 `AfterRun` 照常触发，否则报告会晾到清扫。
- 报告行（`messages.origin = 'subagent'`）不是用户的提问：重新生成的「最后一条提问」、改写、搜索、导出、标题素材都要跳过 `origin` 非空的行；`DeleteAnswerTo` 把它当回答删，送达行被删报告就回到待送达（`ON DELETE SET NULL`）。
- 删除路径：子对话随 `launched_by` 的 run 级联删除；会删 run 的新路径在事务前 `runningSubagents`、提交后 `cancelGoneRuns`；新的软删除路径用 `CancelConversationRuns`（已包含子对话的 run）。
- 子 run 的工具、提示词看 `agent.Scope.Parent`：新工具要决定子 Agent 能不能用（默认不给），不给嵌套、`ask_user`、记忆、定时任务、写作块写操作。

细节：[子智能体](docs/design/sub-agents.md)。

## 深度研究

只有用户能开：「+」里选「深度研究」，消息开头是 `<deep-research/>` 一行（在技能行之前，`skillMessage`/`parseSkills`；服务端 `splitResearch`/`personWords`），先出计划、用户确认才研究（`api/research.go`）。

- 计划 run 只多一个 `research_plan`（和 `ask_user` 一样是用户回答的工具，`pendingCall`）；回应走 `POST /conversations/{id}/research-plans/{callId}`：`start` 起 `runs.research = true` 的 run（continues 计划 run），`revise` 起新的计划 run，`cancel` 只写结果不起 run。
- 每人每月次数（`deep_research_monthly`，默认 10，管理员不限）在 `start` 的事务里 `LockResearchStarts` 后数，记一行 `research_starts`（run 删了不退）；发起计划前先查，用完 429 `research_limit`。搜索、读网页不设上限。
- 阶段由 `researchPhaseOf` 认（计划 / 研究 / 研究员），按阶段给工具和提示词：研究 run 不看子智能体开关、单 run 至多派 `researchMaxSubagents` 个且都在前台；研究员是发起 run 为研究 run 的子 Agent。计划、研究和研究员 run 都强制联网（`forceWeb`，不改对话记住的开关）。
- 研究员的搜索结果只给短摘要（不管后台的压缩设置），提示词要求先少量搜索、再完整读 5 到 8 个一手来源；报告由主 Agent 分节写（`document` 的 `append`），边写边附来源。
- 免费搜索全进程过 `tools.DefaultFreeGate`（每家并发上限、失败后冷却），结果经 `tools.CachedSearch` 存 `search_cache` 24 小时（限最近一天的 1 小时），跨用户共享。

细节：[深度研究](docs/design/deep-research-plan.md)。

## 插件

插件 = 技能 + 远程 MCP 服务，管理员在后台添加，用户在侧栏「插件」页安装（`api/plugins.go`、`api/plugins_admin.go`、`internal/plugins`）。

- 只接远程 Streamable HTTP（官方 Go SDK），不跑 stdio；管理员填的地址可以是内网（不过 `GuardedClient`）。没有现成服务的内置插件在进程里跑：地址是 `openchat:<名字>`（`plugins.LocalScheme`，`plugins/local.go` 登记），内存里连接；arXiv 就是这样（`plugins/arxiv`，进程内每 3 秒至多一个对 arXiv 的请求，按 arXiv 的使用条款，别去掉），OpenAlex、天气（Open-Meteo）、维基百科、Hacker News 也是（共用 `plugins/localkit`：经 `GuardedClient`、自报 User-Agent、每个服务一个进程级的请求间隔；插件的请求头值作为 `Options.Secret` 传给服务，OpenAlex 用它当 API key）。新的进程内服务只放只读、公开、免账号或管理员一个 key 就够的接口，别做要绕过人机验证的。
- 凭证（统一请求头的值、每人的 OAuth 令牌和自己填的令牌）经 `internal/secrets` 加密入库。密钥首次启动时自动生成在数据目录 `data/secret.key`（0600，已 gitignore），设了 `OPENCHAT_SECRET_KEY` 就用它；绝不存进数据库、绝不随响应返回或写日志。拿不到密钥时相关功能 409 `secret_key_missing`，其他照常。
- 用户能看到的插件 = 启用的、面向所有人或面向管理员且本人是管理员的；看不到的一律 404。
- 工具权限三档 `allow`/`confirm`/`blocked`；重新拉取工具只更新描述，不动管理员设过的权限。工具自动拉取：添加插件、改了地址或认证时（`autoFetchPluginTools`），要账号的插件在管理员第一次连上账号时，进程内的内置插件每次启动时（`ListBuiltinPluginTools`）；拉不到不影响保存，后台仍可手动拉。
- 内置插件带的技能放在 `plugins/builtin_skills/<键>/<技能>/`，启动时写进插件（`EnsureBuiltinSkills`）：没写过的补上，管理员没改过的随新版本更新，改过或删掉的不动。插件的技能只在它有没被禁用的工具（或本来就是纯技能插件）时进对话快照。
- 每人自己填令牌（认证方式 `token`，`api/plugins_token.go`，如麦当劳）：保存前先用它连一次服务，被拒（`plugins.ErrRefused`）不存；存在安装记录的 `access_token`、没有刷新，调用时被拒就标 `needs_reconnect`。判断「以本人身份」一律用 `actsAsPerson`（oauth 或 token），别只比 `oauth`。个人的令牌绑定在取得它时的认证方式和地址上（`plugin_installs.token_for`），新的取令牌或列工具的路径要经 `pluginInstall`/`tokenFor`；连服务只跟同源重定向，`plugins` 包出口的错误先脱敏（`scrub.go`），别绕开。
- 每人授权（OAuth 2.1 + PKCE）：`connect` 发起、`/plugins/oauth/callback` 收尾，state 一次性且只认发起人，令牌加密存在本人的 `plugin_installs` 行；取令牌一律走 `pluginToken`（会刷新、刷新失败标 `needs_reconnect`），别直接读列。服务返回的授权地址只跟 https 或同主机。
- 对话里调插件走 CodeMode（`api/plugin_code.go`、`internal/codemode`）：模型只有一个 `plugin_code`，写 JS 调 `tools.<短名>.<工具>()`，脚本在 goja 里跑，只注入 `tools`、`search`、`console`，不给 `fetch`、定时器、模块；时长、调用次数、并发、输出、内存都有上限（共享服务器，别去掉）。开了「直接列出」（`plugins.direct_tools`）的插件工具按 `<短名>__<工具>` 逐个给模型，走同一个 `pluginHost.Call`。
- 每次调用都现查 `GetUsablePluginTool`（权限、安装、连接）、按 `inputSchema` 校验，再取令牌；需确认的停在调用处等 `AnswerPluginConfirmation`（run 不结束，`codemode.Pause` 停表，最多 10 分钟），子 Agent 和没有调用 id 的直接拒绝。审计只写 `plugin_calls`，绝不记参数和结果。
- 系统提示词的插件目录随对话快照（`conversations.plugin_catalog`，第一次提供 `plugin_code` 时取），之后装卸插件只经 `PluginNote` 告诉模型，别改提示词。
- 子 Agent 只拿到派出时 `plugins` 点名的插件里「始终允许」的工具（`scopedPluginTools`，`conversations.plugins`）；调度器起的 run 是 `runs.unattended`，需确认的调用立即拒绝。插件的技能是 `skills.plugin_id` 不为空的行，只给装了插件的人（`ListSnapshotSkills`），管理员写的、不审；`skills.Store.ForPlugin` 之外的查询都按 `plugin_id IS NULL` 隔开。
- 工具返回的图片存 `tool_images`（随 run 删），结果里只放引用（`ToolResultPart.Images`）；`agent.BuildContext` 把它们放在这一轮结果之后的用户消息里，由 `attachmentResolver.picture` 读进来。
- 运行中的工具给页面报进度用 `agent.ReportProgress`（`ToolSlot.progress`、`tool_progress` 事件，先提交再推送），只给页面，不进模型上下文。

细节：[插件](docs/design/plugins-plan.md)。

## 联网

`web_search`/`web_fetch` 在 `internal/tools`，只在对话开着联网时提供（`UsesWeb`）。

- `web_fetch` 的地址来自模型输出，只能经 `GuardedClient` 连接（每次拨号都检查目标是公网地址，每一跳重定向同样，不读环境代理）；不要新增绕开它的抓取路径。被拒时可改请 Jina Reader 代读（后台「网页读取」三种模式，`tools.ReaderMode`，`tools/reader.go`），连 Jina 同样走 `GuardedClient`，人机验证页按失败处理。
- 转录行写入前经 `agent.EncodeParts`/`PlainText` 去掉 NUL（Postgres 的 text/jsonb 都不收，网页文字里偶尔有），别绕开它们直接写 `parts`/`content`。
- 屏蔽站点（`Blocklist`）对请求的地址和重定向后的地址各查一次。
- 读取输出有上限：每次最多 `fetchMaxChars`，用 `offset` 续读；`find` 的段数和总量同样封顶。不要新增一次返回整篇长页面的路径。
- 搜索的时间范围（`recency`）和限定网站（`sites`）：后端能过滤的交给后端，回来的结果在 `WebSearch.Run` 里再按同样条件过一遍。
- 写给模型的文字（工具说明、提示词、结果）一律英文；联网提示词是 `api/agent_setup.go` 的 `webGuide`，主 Agent 和子 Agent 共用。
- 模型用错工具，改提示词或工具描述，不要在服务端改写它的输出。

细节：[联网：搜索与读网页](docs/design/web-tools.md)。

## 浏览器

`browser` 工具驱动一个 headless Chromium（`internal/browser`，`api/browser.go`、`browser_tool.go`）：用户在「+」菜单打开（`conversations.browser`），第一期只给管理员（`browserOffered`），子 Agent、深度研究不给；开着时 run 也有联网（`runChoice.browserOn`）。有没有 `browser` 在 run 的第一次请求时定下、存在 `Scope.State`，到 run 结束不变（中途变了工具或提示词，模型的思考块签名会失效、请求被拒），中途关掉只影响下一个 run。

- 浏览器的一切流量只经 `browser.Proxy`：拨号时查公网地址（`tools.PublicAddress`）、屏蔽站点、本站域名，只放 80/443；只收 `browserd` 地址和后台登记网段的连接。新建浏览器上下文一律带 `ProxyServer` 和 `<-loopback>`，别绕开。
- 快照的 `ref` 只在最新一份快照里有效（`Session.keep`），旧的报 `ErrStaleRef`；动作结果附新快照，超过 `SnapshotLimit` 截断，用 `find`/子树看其余。
- 会改东西的动作先问人（`askReason`：模型标 `consequential`、提交 POST 表单、带密码或银行卡的表单、付款页、按钮文字），走 `plugin_confirmations`（`site` 不为空、`plugin_id` 为空）和 `pluginWaits`，「本对话这个网站都允许」记 `browser_site_grants`；无人值守直接拒。每次都记 `browser_actions`，不记输入的文字。
- 模型永不在密码框里输入（`ErrPassword`）；不下载、不上传。

细节：[浏览器工具](docs/design/browser-plan.md)。

## 图片生成

聊天模型用 `image_generate` 出图，背后是后台选定的图片模型（`model_overrides.kind = 'image'`；`api/image_tool.go`、`image_settings.go`，适配器 `providers/openai/images.go`、`providers/gemini/images.go`）。

- 生成的图是用户的附件（`attachments.origin = 'generated'`、`run_id`）：写之前按实际大小过 `roomFor`，工具结果行写入时在同一个 commit 里认领（`ToolResult.Files` → `ClaimRunFiles`）；没被认领的当未认领上传，24 小时后清掉。上传的认领（`ListPendingAttachments`）不收生成的图。会删工具结果行的新路径要收 `storage_key`（`DeleteAnswerTo` 已返回）。
- 出图请求走 `imageChannels`（kind 必须是 `image`，`admin_only`、容量、`servingOrder` 同 `pickChannel`），`Begin(Call{Kind: image, Images, ImageTokens})` → `Reserve`（含按张的输出预留）→ `Generate` → `EndImages`；填了每张价格（`model_overrides.image_price`）就按张计费。服务商的安全拒绝不换渠道（`refusal`）。
- 提示词送审，来源 `image_prompt`，不算违规；被编辑的原图核对收据（`receiptOf`：生成的看 `attachments.sha256`，用户发的看消息零件里的摘要）。生成结果不审。
- 张数上限三道：每次、每个 run（`Scope.State`）、每人每天（`image_requests`，`LockImageQuota` 下数，失败退回；个人配额 `users.image_daily` 盖过全站，管理员不限）。
- 是否提供在 run 第一次请求时定下（`Scope.State`）；子 Agent、深度研究不给，定时任务给。
- 用户发的图片在上下文里前面有一行 `[Image img_…]`（`agent.labelPictures`、`ImageHandle`），所有对话都加，别只在有工具时加。

细节：[图片生成](docs/design/image-generation-plan.md)。

## 参考聊天记录

新对话的快照里带一份近期对话清单（`memory.Snapshot.Recent`，`memory/recent.go`），模型另有 `chat_history` 工具搜和读用户自己的历史对话（`tools/chat_history.go`）。开关独立于记忆：`users.chat_history` 和后台的 `chat_history`，都默认开，都开才算开（`Store.ChatHistoryAvailable`，按请求现查）。

- 清单和快照一起取、之后不重排；渲染时看现在的开关，快照本身不改。临时对话没有清单、没有工具，也不出现在别人的清单和搜索里。
- 清单和工具只读：本人的、`kind = 'chat'`、非临时、未删除、不是当前对话的对话里 `status <> 'blocked'`、`origin IS NULL` 的消息（清单只要用户消息，且不收定时任务的对话）。新增查询照此，归属写进语句。用户消息一律先过 `words.Typed`（去掉技能、深度研究、图片引用行和引用段落的包装）。
- 标题只有检查过才给模型看（`Store.TitleChecked`，哈希在 `conversations.title_checked_hash`）：模型起的写入时记为已检查，用户改的在取快照时和指令、记忆一起审。新增把标题放进提示词或工具结果的路径照此；其余读到的内容不审。
- 新的软删除对话路径必须在事务开头、锁任何对话行之前取 `LockRecentChats`，并在同一事务里 `RemoveFromRecentChats`；保存带清单的快照只走 `Store.saveSnapshot`（同一把锁）。别改回各锁各的行：互相列着对方的对话同时删除会死锁。
- `chat_history` 不给子 Agent 和深度研究的任何阶段，定时任务的 run 可以用；这个 run 里搜到过的对话记在 `Scope.State`，默认不再返回。搜索是子串匹配加 Go 里排序，不要引入向量库或全文检索引擎。

细节：[参考聊天记录](docs/design/chat-history-plan.md)。

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
