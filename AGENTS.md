# AGENTS.md

编码助手的仓库指引（`CLAUDE.md` 只是指向这里）。Go API 在 `apps/server`，React 前端在 `apps/web`，契约在 `api/openapi.yaml`。

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
- 附件存储在 后台 → 文件存储 里配置（`/admin/storage`，存于 `app_settings`，运行时生效，不用重启，也没有任何存储相关的环境变量）。驱动是本地磁盘或 S3 兼容存储；`storage.Switch` 把新上传交给当前驱动，读取则先看本地磁盘有没有、再找 S3，所以切换驱动不会让旧文件失联。本地驱动由我们自己的 API 签发 HMAC 签名的读写地址 `/api/v1/files/content/{key}`，签名密钥在首次启动时自动生成并存进 `app_settings`（`storage_signing_key`，不经任何 API 暴露）；S3 密钥只写不读。库里的设置应用失败时回退到本地 `./data`，不拒绝启动。
- 对话是一条线，没有分支和版本：一个对话的所有 `messages` 行按 `(created_at, id)` 排列就是对话记录，发给模型的上下文也是它（`ListMessages`、`ListRecentMessages`、`ListTranscript` 只按对话取，游标沿时间回退）。被替换的东西直接删除，没了就是没了。改写任意一条提问（`POST .../messages/{id}/edit`）：在 `LockConversation` 下的同一个事务里，先在末尾写新提问，用 `MoveAttachments` 把旧提问的附件行交给它，再 `DeleteTranscriptFrom` 删掉旧提问及其之后的所有行，最后建 run；run 随被删的 user 消息级联，但一个 run 的其他行只会 `run_id` 置空，所以是按位置删而不是按 run 删；`DELETE ... RETURNING` 带回被删附件的 `storage_key`，提交后尽力删文件。重新生成（`POST .../regenerate`）只作用于最后一条提问：同一个事务里 `DeleteAnswerTo` 删掉它后面的所有行和它名下全部 run（`ask_user` 续写的 run 也在内），再建新 run；提问在加锁前后不是同一条就返回 409 `not_latest`。两者在有 run 在跑时返回 409 `conversation_busy`，不然会把正在写的 run 删掉（run 只由持有对话锁的请求启动，所以锁下的检查可靠）。发送和回答 `ask_user` 也先 `LockConversation`，才不会写进正被删的对话。新增会删行的操作照这一套来：锁、查 busy、先写后删、收集 `storage_key`、提交后删文件、发 `transcript_changed`。旧数据由迁移 `20261020000001_linear_conversations` 清成一条线：每个对话只留下当时看得到的行，别的版本和它们的子树、被重新生成替掉的回答、对应的 run 都删掉；被删行的附件改成「未被认领的上传」并把 `created_at` 设为 epoch，由清理循环的 `DeleteStaleUploads` 连存储对象（本地盘或桶）一起删。
- 删除对话会取消它正在跑的 run：`DELETE /conversations/{id}` 和 `DELETE /me/conversations` 在**一个事务里**先 `LockConversation`（全部删除用 `LockUserConversations` 锁住调用者所有未删除的对话），再 `SoftDeleteConversation`/`SoftDeleteAllConversations`，再用 `CancelConversationRuns` 把这些对话里 `running` 的 run 记上 `cancel_requested`，提交后对每个 id 调 `runner.Cancel`（和停止接口同一套）。不这样做的话 run 会继续调模型、占着账号的并发 run 名额，而停止接口因为对话已删除是 404，没人能停它。另一半是互斥：发送、重新生成、改写、回答 `ask_user`（以及撤销压缩）在锁下一律用 `lockLive` 而不是直接 `LockConversation`，拿到锁后发现对话已被删除就返回 404、什么都不写——所以「已删除的对话里有 run 在跑」不会出现：要么 run 先建，被删除取消；要么删除先到，发送被拒。新增任何会在对话里起 run 的路径都要用 `lockLive`；任何新的软删除路径都要取消 run。测试在 `conversation_delete_runs_pg_test.go`。
- 附件行消失的地方都要先收集 `storage_key`、删行、再尽力删文件（`internal/api/cleanup.go`，查询在 `queries/cleanup.sql`，用 `DELETE ... RETURNING`，多实例安全）：临时对话 24 小时后、软删除的对话 30 天（`deletedRetention`）后连同消息和附件硬删，从未被消息认领的上传 24 小时后删；改写删掉的提问的附件在提交后立即删（见上）。本地磁盘另有孤儿清扫：`uploads/` 下没有附件行且超过 24 小时的文件会被删，所以数据目录必须和数据库配套，不要拿别的库去指同一个 `./data`。`storage.Backend` 有 `Delete`，`Switch` 会同时从磁盘和 S3 删。
- 模型设置里的「向普通用户开放」（`model_overrides.admin_only`）：关掉后非管理员的模型列表里没有它，按 id 发也被拒（`pickChannel` 的 `staff` 参数，用户请求传 `isStaff(ctx)`）；管理员照常用，服务端自己的任务（标题等 `taskModel`、校验任务模型）传 `true`，不受影响。新增挑模型的地方要想清楚传哪个。
- 发消息返回 SSE；助手消息在第一个 token 前就写库，取消算 `complete`，provider 报错才算 `failed`。
- 上下文压缩（`internal/agent/compaction.go`、`summary.go`、`estimate.go`）：每个 run 在**每次模型请求之前**（第一次，以及每轮工具之后）估一下要发的 token——系统提示词 + 行 + 本次提供的工具定义，CJK 字按 1 token、其余 4 字符 1 token，助手行有服务商报的输出用量就用它，再乘 1.25——超过模型窗口（`contextLimit`，未知按 128000）的 50% 就先压缩；对话已有摘要时门槛是 65%，这个落差是滞回，刚压完不会马上又压。压缩由 **run 自己的模型和渠道**写（不用任务模型，小模型的窗口可能装不下要读的历史），用量记进账本，类型 `compression`。开关是 后台 → 系统 的「自动压缩上下文」（`app_settings.compress_context`，默认开，每次请求前读，对运行中的 run 立即生效）；关掉只是不自动压，已有的摘要照常用，用户仍可手动压。摘要写成什么：除了**最新的用户消息**（原样保留，当前指令不能变成历史）之外，到转录最后一行为止的一切，包括本 run 前面的工具调用和结果；边界永远取最后一行，所以调用和结果不会被切开。增量：旧摘要的文字加上之后新增的行（旧摘要保留的那条提问从没写进旧摘要，算历史）合成**一份**新摘要替换旧的，一个对话至多一份；要读的内容超过窗口就切成几段依次折叠进摘要。摘要提示词在 `compaction.go`（英文，模型可见文字）；工具结果和网页是不可信数据，提示词要求只记录、不把里面的指令带进摘要，历史里的闭合标签也会被转义，因为摘要是以**用户**消息发出的。
- 摘要存在 `context_summaries`（`conversation_id` 主键；`through_message_id` 覆盖到的最后一行；可空的 `kept_message_id` 原样保留的提问），两个外键都是 `ON DELETE CASCADE`——这就是和线性编辑的全部一致性：改写提问会删掉它和它之后所有行，摘要的终点或保留的提问在其中就连摘要一起没了，在它之后改写则摘要还在；重新生成只删最后一条提问之后的行，摘要终点是被删的回答就没了（摘要里有那个回答的内容），终点是提问本身则还在（有测试）。**行从不因压缩而改变**，转录页照常显示所有消息。构造请求用 `LoadContextRows` + `BuildRequestContext`：`[系统提示词, 摘要（用户消息，包在 <compressed_history_summary> 里）, 保留的提问, 摘要终点之后的行…]`；系统提示词一个字节不变，前缀只在压缩发生时变（所以 `HistoryLimit` 现在只是 2000 行的安全上限，别拿它管大小）。Anthropic、Gemini 会合并相邻同角色轮次，OpenAI 两套 API 直接接受连续的用户消息，都有测试。压缩失败（摘要请求出错、模型没写出东西）只记日志，不写任何东西，这次请求原样发出，下一次请求前再试；同一 run 里压缩过一次、请求没比上次增长窗口的 10% 就不再压（问题本身大到压不掉时不要每次都花一次摘要）。
- 手动压缩与撤销：`GET/POST/DELETE /conversations/{id}/compaction`。POST 压整个对话（包括最后一条提问，这时没有要保留的），run 在跑时 409 `conversation_busy`，没有新内容 409 `nothing_to_compact`，同一对话已在压 409 `compaction_busy`（`pg_try_advisory_lock`，在专用连接上，关连接即放锁），模型没写出摘要 502 `compaction_failed`。最后一条提问还没有回答（失败的、被停掉的）时，摘要只到那条提问为止并保留它——这样「压缩上下文」之后重新生成（会删它后面的行）不会把刚写的摘要一起删掉。DELETE 只删摘要，原消息本来就在。运行里的压缩发 `compaction_started` / `compaction_finished`（带摘要，失败则不带）进 run 的批次，`runs.live.compacting` 在压缩期间为真，让中途连上的页面也显示状态行；不属于 run 的变化发 `compaction_changed`（run id 为空、version 为 0，和标题事件一个路子）。删行造成的摘要消失不单独发事件，靠 `transcript_changed` 让页面重读摘要。
- 改写和重新生成发出的 `run_started` 批次里同时带 `transcript_changed`（改写时带 `from`，即被改的提问的 id）：行被删了，开着同一对话的其他标签页据此立刻丢掉这些行并重取最新一页。凡是会删「当前显示的对话记录」的行的新操作，都要发这个事件，否则别的标签页会一直显示已经不存在的记录。
- 头像：上传由 后台 → 系统 的「允许上传头像」控制（`app_settings.avatar_uploads`，默认关；关时 `PUT /me/avatar` 返回 403 `avatar_uploads_off`，前端从 `ChatFeatures.avatarUploads` 读，按钮显示「暂不可用」；移除头像始终可以）。用户上传图片本身（`PUT /me/avatar`，png/jpeg/webp，≤1 MiB），服务端解码、取中心正方形、缩到 256 px、重新编码成 PNG 再存，所以文件里夹带的东西不会被保留；对象在 `avatars/<用户>/<uuid>.png`，库里只存 key（`users.avatar_key`），读取用的是附件同一套签名读取地址，`avatarURL` 对同一个 key 在有效期过半前复用同一个地址。`files_local.go` 对 `avatars/` 前缀的 key 查 `users` 而不是 `attachments`，且只允许读；被替换或移除的图片文件立即删，行不再指向它后旧地址也就不再被服务。孤儿清扫只看 `uploads/`，不要把头像放进去。
- 「我的数据」：`GET /me/export` 一次一个对话地流式写出 JSON（只有 user/assistant 文本和附件文件名，不含临时和已删除的对话；写到一半出错就切断连接，不留一份看似完整的残缺文件），`DELETE /me/conversations` 软删除调用者的全部对话（含临时的），之后走和单个删除一样的保留期清理。两者都只认会话里的账号，没有任何参数能指向别人。
- 搜索：`GET /conversations?search=` 在标题和 user/assistant 消息的 `content` 里做 `ILIKE` 子串匹配（中文没有空格分词，所以不用全文检索），只查调用者自己的非临时对话，分页同列表；靠消息命中的结果带 `match`（消息 id、角色、命中处前后的片段）。前端搜索结果的查询键是 `queryKeys.conversations.search(term)`，刻意不在 `list` 下面——`list` 会持久化到 localStorage，而搜索结果引用了消息内容。
- 使用日志用游标分页（`created_at, id`，走 `usage_logs_created_idx`），`nextCursor` 是下一页的 `cursor`，筛选条件在翻页间必须不变；总数是 `summary.requests`，没有页码也没有 `total`。
- 没有服务商用量的请求也要计费（`usage.Meter.settle`，`internal/llm/estimate.go`）：用户按停止、流中途出错、或服务商根本不报用量（OpenAI 兼容服务只在最后一个 chunk 带 usage，Anthropic 在 `message_start` 只给输入、`message_delta` 才给输出），账本以前写的是 0 token、`cost` NULL，等于这些请求不花钱——有人可以反复发大上下文再停掉。现在只要请求**被服务商接手**，行里就是估算值并标 `usage_logs.estimated = true`（界面在费用旁显示「估算」）：输入取 `Call.InputEstimate`（`llm.EstimateInput(request)`，压缩用的同一套 CJK 一字一 token、其余 4 字符一 token，乘 1.25），输出取已流出的文本、推理、工具参数的 `llm.EstimateOutput`，价格走目录价，所以计入上限。**服务商报了的部分保持原样，只补缺的**：只有输入则输出补估算，没有输入才补输入估算，完整上报（即使被停止）一个字都不动、不标估算。「被接手」的判断：被取消一律算（请求已发出，服务商读了上下文就计费）；其余错误要看流有没有开始（`Meter.Wrap` 见过任何事件，或响应里已有内容）——状态码错误、连不上这类没开始的不算，行里仍是 0。写账本用 `WithoutCancel` 的上下文，取消路径照样写。新增的模型请求路径要：先构造 `llm.Request`，`Begin(Call{InputEstimate: llm.EstimateInput(req)})`，`Reserve`，再 `Stream`，再 `End`。对话请求的 `partialStream`（节流 goroutine）要在 `Reserve` 通过之后才创建——先创建的话，被拒的请求会把它留在后面；它的 goroutine 另外随请求的 ctx 退出，漏掉一条 `close()` 的路径也不会泄漏（`TestRefusedReservationLeavesNoGoroutine`）。进程关闭（`errShutdown`）时的在飞请求不写账本，下一个进程会重做这次请求，这是已知缺口。
- 同时进行的回答数有上限（`internal/api/run_limits.go`）：一个账号跨所有对话最多同时有 N 个 `running` 的 run（`app_settings.max_concurrent_runs`，默认 3，0 为不限，上限 1000；后台 → 系统 → 对话）；管理员不受限。游客按网络地址算（`guest_max_concurrent_runs`，默认 2，后台 → 系统 → 访问，和每小时限额同一个地址，`runs.client_ip`）。检查在 `createRun` 里，发送、重新生成、改写、回答 `ask_user`、游客运行都经过它，没有别的路：在建 run 的**同一个事务里**先取 `pg_advisory_xact_lock`（键是账号 id 或 `ip:<地址>`）再数再插，锁持有到提交，所以两个请求抢最后一个名额只有一个过，多实例也成立；超了返回 429 `too_many_runs`，事务回滚，什么都没写（重新生成/改写删的行也回来了）。`awaiting_input` 的 run 不占名额。游客的 `GuestRun` 另在存文件之前无锁预检一次（`guestRunSlotFree`），只为尽早拒绝；游客的 run 只在请求连接期间存在、重启后不会恢复，所以超过 20 分钟没有任何提交的游客 `running` 行不占名额（进程崩溃留下的残余，`sweepGuests` 之后才会删）。新增会起 run 的路径必须走 `createRun`。
- 日志：`logging.WithFields`/`Annotate` 把请求编号、用户等放进上下文，所有带 ctx 的 `slog.*Context` 行都会自动带上（`auth.Require` 标 `user_id`，游客路由标 `guest`，请求日志补 `conversation_id`）；后台运行没有请求上下文，用 `ex.logger()`，它带 run、对话、用户、模型、渠道。响应头 `X-Request-Id` 是请求编号，前端在发送失败的提示里显示它，回答失败的提示显示 run id 前 8 位（`ErrorId`），用户反馈时报这个编号。`LOG_FILE` 设了就把同样的 JSON 日志写一份到文件、按大小轮转（镜像默认 `/app/data/logs/openchat.log`，在数据卷上，换容器也不丢）。新写的日志行尽量带 ctx，别丢掉这些字段。
- 服务端自带前端：`make build` 先建 web（vite 同时预压缩出 `.br`/`.gz`），再拷进 `internal/webui/dist` 嵌入二进制，一个进程、一个源。`/assets/*` 带 `immutable` 长缓存，`index.html` 是 `no-cache`；缺失的 `/assets/x.js` 是真 404，不回落到 index.html。开发仍走 `make web`。
- 没跑过 web 构建时 `webui/dist` 里只有 `.gitkeep`，服务端只提供 API，`/` 返回一行纯文本说明；该目录除 `.gitkeep` 外都被 gitignore，不要提交。
- 个人记忆（`internal/memory`、`tools/memory.go`）：对话在第一次运行时把用户的自定义指令和记忆存成 `conversations.memory_snapshot`，此后每次请求的系统提示词都从这份快照渲染，同一对话内字节不变（提示缓存和思考块校验依赖它）；对话中改动的记忆从下一个对话起生效。模型用短 id（`m1`…）引用快照里的记忆，id 只在本对话的快照里有意义；工具执行时的用户取自 `agent.ScopeFrom(ctx)`，永远不信参数。记忆上限 100 条、每条 200 字，满了拒绝而不淘汰。只有模型的改动写 `memory_events`（撤销和"最近更改"用），用户自己删改记忆时会连带清掉对应日志。记忆接口全在 `/me/*`，没有任何管理员接口能读到别人的记忆或自定义指令；管理员只有 `memory` 总开关。
- 临时对话（`conversations.temporary`）：不进列表、不起标题、没有记忆和 memory 工具（自定义指令照常），创建 24 小时后由 `Server.StartCleanup` 里的单条 `DELETE` 删除（多实例安全）；用量日志的会话外键是 `SET NULL`，记录保留。
- 模型容量上限（`internal/api/model_capacity.go`，列在 `model_overrides`：`capacity` / `capacity_period` / `capacity_admin_exempt` / `capacity_since`）：给单个模型设一个美元上限，周期是 `total` / `monthly` / `daily`。花费是 `usage_logs.cost` 里该模型自周期开始的总和，**按请求的 `model_id` 算，不按 `served_model`**（模型的上架、选择、对话里存的、设置里配的都是请求 id；`served_model` 是服务商回显的字符串，可能是带日期的变体或中转站自己的名字）。所有用户、访客、管理员、`chat` 和 `title` 等各种 kind 的请求都计入。没有价格的模型（元数据没有输入或输出价格）`cost` 是 NULL，总和恒为 0，上限永远不会触发——设置面板会提示。服务商没返回用量不再算「没有价格」：账本会估算（见下一条），估算出的花费照常计入。金额全程是 `numeric`：存成整数微美元（至多 6 位小数）、在 SQL 里和总和比较（`ModelSpends`，一次查询算出一批模型），不要经过 float 比较；达到即触发（`花费 >= 上限`）。
  - 周期按服务器本地时区的零点算（日从当天零点、月从 1 日零点，`time.Date` 构造，所以夏令时的 23/25 小时日也对）。时区来自环境变量 `TZ`（部署默认 `Asia/Shanghai`），`cmd/openchat` 引入 `time/tzdata`，因为生产镜像是 distroless、没有系统时区库，缺了它 Go 会悄悄把 `TZ=Asia/Shanghai` 当成 UTC。`total` 从 `capacity_since` 起算，NULL 表示整个历史；「重新计算」（`POST /admin/models/{id}/capacity/reset`）把它设为现在，使用日志不动。保存设置不会改 `capacity_since`（`SaveModelOverride` 的 `ON CONFLICT` 不更新它）；清掉上限时周期和管理员豁免会被写回默认值（`total`、开），不留看不见的状态。
  - 「恢复默认」（`DELETE .../settings`）删掉整行，所以上限、周期、豁免和起点一并消失；保存时如果什么都没决定（含没有上限）也删行，起点随之丢失。花费记录仍在使用日志里。
  - 上限对所有人生效，除非该模型的 `capacity_admin_exempt` 开着且请求者是管理员（`asker.admin`；管理员的花费照样计入）。访客不是管理员。后台为某人做的事（标题）按那个人算：管理员的对话在豁免下照常起标题。
  - 执行点：①起一次运行之前，`chooseRun` → `pickChannel(ctx, modelID, asker)` 返回 `errModelCapacity`（409 `model_capacity_reached`），此时什么都还没写，发送、重新生成、改写、回答 ask_user 和访客运行都走这条；"随便哪个模型"（空 id）会跳过已满的模型，全满才拒绝。②运行当中，`agent.Options.Capacity`（`api.NewCapacityCheck`）在 `generate` 里**每次模型请求之前**问一次，满了就以 `failWithRow(CodeModelCapacity)` 结束，留下一条带该错误码的空失败助手消息；账本在请求结束时才写，所以光看账本，超出量是检查通过时已在飞的请求，不是整个工具循环；再加上下面的「在飞请求先预留」，同时发起的请求会互相看见。查询出错按失败处理（运行以 `internal` 结束），不放行。③任务模型：配置的任务模型满了就退回对话自己的模型，也满了就跳过标题（`taskModel` 返回 `errModelCapacity`；按需起标题的接口把它原样返回）。④`GET /models`、`/guest/config` 的 `atCapacity` 按请求者算，满的模型仍然列出来。新增任何会用模型的新路径（比如上下文压缩、内容审核）都要在它的请求前过这几个点之一，用量记一个新 kind 就会自动计入花费。
  - 保存任务模型（`UpdateSystemSettings`）不检查容量（`asker.ignoreCap`），只看它能不能被回答。
  - 索引：`usage_logs_model_idx` 是 `(model_id, created_at DESC) INCLUDE (cost)`，上限的求和是 index-only scan；只有设了上限的模型才查花费，没设的只多一次主键查询。`total` 且从不重新计算的上限要扫该模型全部历史，历史很长就用「重新计算」收窄。
  - 在飞请求先预留（`internal/api/spend_reserve.go`，`usage.Reserver`）：对**设了上限的模型**，每次模型请求（对话请求、运行里的压缩、标题、手动压缩，凡是过 `usage.Meter` 的）在发出前调 `meter.Reserve`，先预留「估算输入 token × 目录输入价」这一笔，要求「已花 + 其他在飞请求的预留 + 本次预留 < 上限」，否则拒绝（对话请求以 `failWithRow(CodeModelCapacity)` 结束，标题跳过，手动压缩 409 `model_capacity_reached`）；请求的账本行写完后 `Meter.End` 才释放，调用方另外 `defer meter.Release()` 兜底。预留只含输入（输出事先不知道，同时有多少个回答在写由下面的并发上限管），缓存命中也按全价输入预留，所以偏保守；没有价格的模型不预留。**只在进程内存里、尽力而为**：不建表、不会留下脏行，单实例下是准的，多实例各看各的预留，超出量最多是其他实例上在飞的请求。预留带 1 小时 TTL 防泄漏。比较在 SQL 里按 numeric 做（`ModelWouldReach`），持有互斥锁跨过这次查询，这样「读到的花费」一定包含了已释放预留对应的行。管理员被豁免时不被拒绝，但预留照记（他们的花费对别人算数）。

## 前端约定

- 服务端数据用 TanStack Query（键在 `lib/query-keys.ts`、`features/admin/use-admin.ts`）；流式中间态和 UI 状态用 Zustand。流结束后让查询失效，重新拉取。查询键含筛选条件时加 `placeholderData: (previous) => previous`。
- 路由守卫挂在无路径布局的 `beforeLoad`：`_authed.tsx` 要求登录，`admin.tsx` 要求管理员。会话用 `currentSession(queryClient)`（`features/auth/use-auth.ts`）取：5 分钟内查过的直接用，更老的重新问服务端（别用 `ensureQueryData`，它不管多旧都返回缓存，被停用的账号会一直半死不活）；重新问失败（断网）时退回上次的答案。
- 会话在页面开着时失效（被停用、改了密码）靠 401 发现：`lib/query.ts` 的 `QueryCache`/`MutationCache` 把每个失败交给 `setErrorListener` 登记的监听者（`main.tsx` 里是 `leaveIfSessionExpired`，`lib/session-expired.ts`），不走缓存的调用（事件流）自己调 `reportError`。账号模式下的 401（`invalid_credentials` 除外——那是登录表单自己的错，而且认证接口抛的是 `AuthFieldError`）且页面此前认为已登录，就清缓存、清 chat store、清本地存的缓存，`replace` 到 `/login?redirect=…`；已在 `/login`、`/register` 不动，游客模式不管。事件流收到 401 就停止重连（重连永远是 401）。新写的不经查询缓存的网络调用，遇到 401 也要 `reportError`。
- 换人要清干净：登录、注册成功和退出都 `queryClient.clear()` + `resetChatStore()` + `forgetQueryCache()`；`adoptQueryCache` 发现缓存属于别人（本页内存里的或上次存的）时，除会话外的查询全部移除（记忆、个性化、后台的也算）并重置 chat store。
- 没有版本翻页器。改写和重新生成从点击起就把要删的行藏起来（store 的 `replacing`：`{from}` 是被改的提问及其后，`{after}` 是最后一条提问之后；`buildTurns` 用它），服务端接受后用 `truncateTranscript` 把这些行从缓存里去掉，再让 `replacing` 结束；run 的输入已在列表里（重新生成）时 `buildTurns` 也会藏掉它后面不属于这个 run 的行。
- 对话记录从尾部分页：`GET .../messages` 首页是最新的一页（页内仍是旧→新），`before` 取更早的页；`hooks/use-messages.ts` 是 `useInfiniteQuery`（`pages[0]` 最新），`NextChat` 只渲染最后 20 个 turn，滚到顶部再多画 20 个，画到已加载的最旧处才请求更早一页。run 结束只用 `refreshNewestMessages` 重取最新一页并拼接（交接逻辑在 `lib/hand-over.ts`，有测试：只有刷新**确认成功**——`refreshConfirmed`，兜底的 `refetchQueries` 也要 `throwOnError`——之后才 `settle` 本地的 live 副本；失败时答案留在屏幕上，隔几秒重试，之后任何一次成功的刷新、重连后的 snapshot 都会放手），不要对该键 `invalidateQueries`（会按游标重下所有已展开的页）。
- 发送：`outbox` 里的气泡由 `useSendMessage` 的 hook 级 `onError` 撤掉（调用级的 `mutate(…, { onError })` 在组件卸载后不执行，发完立刻切对话就会留下永远等回复的气泡）；空白页带过去的 `pendingPrompt` 走同一条路。运行中（`working`、`outbox`、`live` 在跑）不能再发：输入框的回车、`handleSend` 都挡掉，游客的 `runner.start` 对同一对话已有未结束的 run 直接 409 `conversation_busy`，不排队。
- 发送的响应可能晚于事件流：SSE 上 `run_started → run_finished → 重取消息 → settle` 全走完，那个 POST 的响应才回来，它（以及重连时拍得更早的 snapshot）仍写着 `running`。store 的 `finished`（有界，最近 `FINISHED_RUNS_KEPT` 个 run id，`rememberFinished`）记住已结束的 run：`run_finished` 批次、非 running 的 snapshot、`settle` 都会登记；`startRun`（发送、重新生成、改写、回答 ask_user 都走它，游客的响应也一样）、`applySnapshot`、`applyBatch` 里的 `run_started` 遇到登记过的 run 一律不写回 `live`（`startRun` 仍清掉 `outbox`/`replacing`）。run 只前进不后退；新增任何由响应或旧快照驱动、会写 `live` 的路径都要过这道检查。`resetChatStore` 一并清空。
- 对话标题：服务端和游客仓库都把没起名的对话叫 `New chat`（英文，不随语言），显示时一律经 `lib/titles.ts` 的 `displayTitle`/`useDisplayTitle`（侧栏、搜索、对话页顶、删除确认），它把空标题和 `New chat` 换成 `chat.untitled`；重命名框也以显示出来的名字起头。别直接渲染 `conversation.title`。
- 脚本触发的滚动（`scrollTo`/`scrollBy` 的 `behavior`）用 `lib/motion.ts` 的 `smoothScrollBehavior()`，CSS 的 reduced-motion 管不到它。工具结果里来的链接（`ToolActivity`）只允许 `isHttpUrl`（`lib/urls.ts`）通过，和 GenUI 的链接一样。
- 表格是服务端分页/筛选；`getRowId` 用记录 id；列译名同时放进 `meta.title`。
- 游标分页的表格（使用日志）用 `DataTableCursorPagination`：游标只会往前，往回就是回到走过的游标，所以组件里留着已访问的游标栈，换筛选或每页行数就清空重来。
- 管理后台的进度条挂在根（`AdminProgress`），只在目标路径在 `/admin` 下时才渲染——admin 布局在第一次从聊天进入时还没挂载，挂在布局里就晚了；聊天侧用自己就地的加载提示，不要给它加条。
- 侧边栏标记正在回答的对话靠 `GET /conversations/running`（`useRunningConversations`）：有在跑的才每 3 秒轮询，`run_started` 时由事件流让它失效；某个 id 消失时刷新列表（标题、顺序可能变了）。当前打开的对话不标，它自己的页面已经在显示。
- 删除对话：服务端在删除提交后往该对话的事件流发 `conversation_deleted`（nil run id、version 0；全部删除时每个对话各一条），正在看它的页面（任何标签页、任何设备）收到就停止跟随并回到首页；游客由 `hub.announceDeleted` 在本页和其他标签页发同样的事件。同一浏览器的其他标签页靠 `lib/conversation-sync.ts` 的 BroadcastChannel（`announceDeleted` / `followDeletions`）立刻从侧栏和搜索里去掉它。新增删除对话的路径要两样都做。
- 收到 `transcript_changed` 时（`use-conversation-events.ts`）先按事件的 `from`（或本页自己的 `replacing`）截缓存，赶在 `run_started` 结束 `replacing` 之前，再用 `refreshNewestMessages` 重取最新一页，不要 `invalidateQueries` 消息键。`spliceNewest`（`lib/transcript-pages.ts`，有测试）在新一页的第一行不在已持有的行里时整页替换，所以行被删光也对。重新生成时其他标签页没有 `from`，靠 `buildTurns` 里「run 的输入已在列表」那条藏旧回答。
- 压缩上下文：摘要在查询 `queryKeys.conversations.compaction(id)`（`hooks/use-compaction.ts`，不落本地缓存），由事件流维护：`compaction_finished`（带摘要）、`compaction_changed` 直接写进缓存，`transcript_changed` 和 run 结束时重新读。原消息**照常全部显示**，`buildTurns(…, summary)` 只在摘要终点那条消息之后标一个 `after`（整个回复的末尾）或回复内部的 `inside`（run 还在继续、或终点在一轮工具的中间时，分隔线在那条回复的零件之间，不拆回复），`CompactionDivider` 画成细线「以上内容已压缩为摘要」，点开是用聊天同一个 `Markdown` 渲染的摘要和「撤销压缩」（run 在跑时不给）。运行中压缩的状态行是 `CompactingLine`（`ThinkingOrb` + 「正在压缩上下文…」），取代「正在思考…」。上下文圆环（`ContextMeter`）点开是同样的数字加「立即压缩」；圆环读的是上一次请求的用量，所以压完要到下一次请求才降。因上下文过长失败的回答，错误提示不再叫人开新对话，而是带「压缩上下文」按钮：先手动压缩再重新生成（只在最后一条回答、且不在忙时给）。所有字符串走 `t()`。
- 后台表单用 react-hook-form + zod，schema 建在组件内部（文案要翻译）；用 `useWatch` 而不是 `form.watch()`。登录注册页是手写校验。
- 所有面向用户的字符串都走 `t()`（`zh`、`en`，键有类型约束）；服务端 `message` 不直接渲染。
- 动效：全局遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`，用透明度脉动代替位移。
- 设置对话框（通用/个性化/账户/数据）是 `?settings=<tab>` 搜索参数（`lib/settings.ts`），挂在 `_authed` 布局上，渲染进 `.prism-next` 里；样式照搬 Open WebUI 经典设置弹窗，在 `styles/settings.css`。记忆列表是它里面的第二层弹窗（`MemoryManager`）。记忆相关查询键在 `queryKeys.memory`，不写入本地缓存（`query-persist` 只存白名单），临时对话的转录同样不落本地缓存。
- 容量上限的前端：设置面板的「容量」一节（`features/admin/models/capacity.ts` 是纯逻辑，有 vitest）、卡片上的用量条、`ChatModel.atCapacity`。选择器里满的模型**保留在列表里但禁用**，原因写在名字下面（禁用项没有指针事件，tooltip 出不来）；对话自己的模型或用户明确选的模型即使满了也不会被悄悄换掉——新对话的默认模型才会绕开满的（`defaultModelId`）；在满的模型上发送时 `NextChat` 先重取一次模型列表（列表最多旧一分钟、还会本地持久化），仍满就弹提示、什么都不创建，取不到就交给服务器决定。重新生成/改写/回答由服务器拒绝，`useSendError` 弹同一句话并让模型列表失效；运行中途被拦住，错误行按 `chat.error.model_capacity_reached` 显示，`use-conversation-events` 看到 `run_finished` 带这个码也会让模型列表失效。

## 游客模式

管理员在 后台 → 系统 打开「游客模式」（`app_settings.guest_mode`，默认关）后，登录页多一个「以游客身份使用」按钮（登录页读公开的 `GET /auth/options` 里的 `guest`；`/guest` 路由读同一个开关的 `GET /guest/status`，它永远在；其余 `/guest/*` 在开关关着时 404，由路由里的 `requireGuestMode` 分组保证）。游客没有账号、没有会话，服务器**不保存**他们的任何东西：对话、记忆、自定义指令全在浏览器的 IndexedDB 里。限额也在系统设置里：每个 IP 每小时、全体游客每天（最近 24 小时）的请求数，0 为不限，服务端校验范围（`invalid_guest_limit`）；另有每个 IP 同时进行的回答数（`guest_max_concurrent_runs`，默认 2，见后端约定里的并发上限，超了 429 `too_many_runs`）。

### 服务端（`api/guest.go`、`api/guest_run.go`）

- 唯一要模型的请求是 `POST /guest/runs`（公开，SSE）。它不 fork agent：请求体带整个对话、新问题（或「重答」/「回答 ask_user」）、模型和开关、自定义指令、记忆、对话已有的记忆快照；服务端在**一个事务**里建临时用户（`users.guest = true`，无密码无会话）、对话、按客户端给的 id 写下转录、写附件文件和行、塞记忆、建 run，再用和账号完全相同的 `chooseRun`/`createRun`/`startRun`（回答走抽出来的 `answerPart`/`startAnswerRun`）跑，把 hub 里的批次原样流回，最后发一帧 `guest_end`（本次用的记忆快照、跑完后的全部记忆、记忆事件、标题），**然后删掉这个用户**（级联带走一切；存储键先收集，事后删文件）。所有错误（429 两种限额和 `too_many_runs`、`no_models`、`model_unavailable`、附件错误、`invalid_answers`、`not_pending`）都在写任何行之前以 JSON 返回。
- 游客的压缩上下文：请求体带浏览器手里的 `summary`（就是 `ContextSummary`），`messages` 只带摘要要用的行——保留的提问、摘要终点那一行、终点之后的行，中间被摘要取代的不带；服务端在同一个事务里写完行后把摘要写在它们上面（`through`/`kept` 必须是请求里的行，否则 400 `invalid_summary`），agent 照常构造请求。run 里压出的新摘要走事件 `compaction_finished`，`guest_end` 也带跑完后的摘要（浏览器两处都存，幂等）；事务整个随游客用户级联删除。手动压缩没有对话可读，所以是无状态的 `POST /guest/compaction`：浏览器决定覆盖哪些行（和账号 `ModeManual` 同一套规则），服务端只让模型写，记账本、计限额，什么都不存。
- 转录按时间写入（user、assistant、tool 行都没有 run 之外的结构）：对话就是它的所有行，所以改写在服务端没有特别处理——浏览器已把被改的提问及其后的内容删了，请求停在它前面，`input` 是新措辞。时间用客户端的（日期戳是模型看到的日期，轮与轮之间不能变），但保证严格递增且早于现在。回答（`answer`）时会塞一个 `awaiting_input` 的占位 run 当 `continues` 的头，它从不执行。
- 提供方私有数据（推理签名、被遮蔽的推理）账号的浏览器从来拿不到，游客的服务器又不留，所以游客流里 assistant 行带 `providerState`（`messages.parts` 原样 base64），客户端存着、下次请求原样带回；`guestParts` 优先用它。其余行按契约里的块还原。
- 限额计数在 `guest_requests(ip, created_at)`，一条 advisory lock 下「数 + 记」是一步，多实例共享；清理循环删一天前的行。IP 取 `RealIP` 中间件处理过的 `RemoteAddr`（IPv6 按 /64），所以得部署在会设 `X-Forwarded-For` 的代理后面，否则可以伪造。标题请求（`/guest/title`）也计一次。游客的图片代理（`/guest/images`）没有对话可对照「地址出现在工具结果里」，靠受保护的客户端（只拉公网、只认图片）加每客户端每分钟限速。
- 游客必须在这些地方被排除：登录（`GetUserByLogin`）、管理员用户列表和计数、仪表盘总用户数/活跃用户、「第一个注册的成为管理员」的计数、启动时恢复 run（`ListRunningRuns`）、事后标题（`TitleAfterRun`）。新写查全体用户的查询时都要想想 `NOT guest`。
- 用量日志保留游客的请求：用户删除后 `user_id` 变 NULL，而账号从不删除，所以没有主人的行就是游客的，界面标「游客」。游客的 run 进行中，查询的 JOIN 排除 guest 行，同样读作无主人。
- 断开连接就取消 run（`stopGuestRun`）并清理；清理循环里 `sweepGuests` 删超过 2 小时的游客用户（进程中途死掉的残留），文件一并收走。`POST /guest/runs/{id}/cancel` 以 run id 为凭据（只有发起的页面知道）。
- `storage.Backend` 有 `PutObject`（服务器自己持有的字节），游客附件走它，key 是 `uploads/guest/<日期>/<每个文件现生成的 uuid><扩展名>`（`guestFileKey`，写文件时才定）；孤儿清扫只看没有行的文件，游客的行在 run 期间一直在。**key 里绝不放浏览器给的 id**：重复请求（两个标签页、连点两次）带着同样的附件 id，若 key 由它决定，被拒的那个请求在失败清理里删的就是先到的、已被接受的请求正在用的文件。每个请求、每个文件的 key 都是自己的，清理才只可能删到自己写的东西。
- 游客的记忆在 `user_memories` 里的 id 是请求自己的，**不是浏览器的**：`user_memories.id` 是全表主键，同一个浏览器的两个对话同时发请求、带同一份记忆，若用浏览器的 id 插入，第二个会撞主键，被误报成 `conversation_busy`。所以浏览器给的每个记忆 id（记忆列表里的，和它带来的快照里 `memories`/`added` 的）都换成这个请求新生成的 id 再入库（`guestIDs`，`api/guest_ids.go`；按发来的顺序生成 uuid v7，列表按 id 排序时保持原来的次序），凡是离开服务器的帧都把它们换回去：发出前对编码后的 JSON 整体替换（流里的批次、重发的快照、工具结果的 `details.memoryId`、最后一帧的快照/记忆/记忆日志），所以以后哪个字段又带了记忆 id 也不会漏。浏览器看到的永远是它发出去的 id；模型在 run 里新建的记忆没有浏览器的 id，用服务器给的 uuid 原样交给浏览器，浏览器照常存、下次照常带回（到时又被换成那次请求自己的 id）。模型看到的只有 `m1…` 这样的短 id，不含真 id。其余由浏览器指定的 id（对话、消息、附件、`continues` 的 run）是故意留着的：对话 id 撞了就是「同一个对话同时在两处发」，得到 `conversation_busy`；浏览器对每条消息、每个附件都 `crypto.randomUUID()`，不同对话之间不会重复。新增由浏览器指定、又落在全表主键上的 id 时要想清楚：能不能撞上同一浏览器的另一个对话。

### 前端（`lib/guest/`）

- 游客页面在 `/guest`、`/guest/chat/$conversationId`（`routes/guest*.tsx`，不在 `_authed` 下，只检查 `GET /guest/status`）。是否游客模式由根路由 `beforeLoad` 里的 `enterPath(pathname, preload)` 决定（`lib/guest/mode.ts`，有测试），**不要**读 `window.location`，因为从登录页跳 `/guest` 时 loader 先于地址变化执行。**预加载（`defaultPreload: 'intent'`，悬停链接）绝不改模式**：它不是导航，翻转会清掉游客的缓存和会话；另一种模式的页面干脆不预加载（`enterPath` 返回 false，根路由抛 `notFound()`），否则它的 loader 会把另一种模式的答案写进共用的缓存。两种模式共用查询键，所以切换时 `queryClient.clear()` 并清 chat store，进游客时还清 `query-persist`；游客数据从不写 localStorage。
- 接缝是 `lib/api.ts` 的 `appFetch`：账号走网络；游客动态加载 `lib/guest` 并由它本地回答契约里的路由（`router.ts`：对话、消息、事件流、记忆、个性化、导出/删除全部、`/files`、`/models`、`/features`），不认识的转发网络。所以 hooks 和组件**不知道**自己在跟谁说话；`streamConversationEvents` 也接受 `fetch`。新增一个聊天用到的端点，要决定游客怎么答：本地服务（加到 `router.ts`），还是只在服务端（`/guest/*`）。
- 对话规则是纯函数（`transcript.ts`：转录就是全部行按时间排、`deletedBy` 选出改写/重新生成要删的行、分页游标），和服务端 SQL 一一对应并有 vitest；记忆规则在 `memory.ts`（上限 100 条/200 字、重复、撤销只在没被改过时）。`repo.ts` 是读写，`storage.ts` 是接口（IndexedDB 实现 `idb.ts`，测试用内存实现）。**改了对话的规则，两边一起改**。改写和重新生成要等服务端接受了新 run（第一批到了）才在 `repo.runStarted` 里删旧行、旧 run 和不再被引用的文件，先写新的再删旧的；被拒绝则什么都不动。有分支的时代留下的数据（对话带 `currentInputId`）在第一次读取时由 `legacy.ts` 清成一条线，规则和服务端迁移一样：只留当时看得到的行，其余连文件删掉，最后才去掉标记，所以中途被打断会重来且结果相同。
- 运行：`runner.ts` 建请求（`request.ts`，只带最近 300 行（有摘要时见下），文件按「最新优先、总共 20 MB」带字节，更早的只带名字和大小）、发 `/guest/runs`、**先落库再发布**每一批（所以前端收到 `run_finished` 去重取最新页时一定取得到），`guest_end` 之后才写记忆/快照/标题（记忆只应用 `memoryEvents`，即模型这次改的，且仅当那条记忆还是模型看到的样子——`before` 对得上；不要用服务端那份整表覆盖，它是 run 开始时的表，会删掉用户在回答期间新写的、复活刚删的。记忆的读写和这一步在仓库里串行，见 `MEMORIES`），同一对话一次只跑一个 run：上一个还没收到 `run_finished` 时再发是 409 `conversation_busy`（和服务端一样，不排队；`start` 里还有个 `starting` 集合挡住同一瞬间的两次），已经 finished 的只等它的 `guest_end`（记忆快照要带上）。连接断了没有 `guest_end` 就记一条 `connection_lost` 的失败回复；连失败都写不进（存储坏了）也要发一条合成的、版本更新的失败 `run_finished`，否则输入框永远是「停止」。服务端已接受 run 而浏览器存不下（`runStarted` 抛错）、或读流途中存储出错，都要 abort 请求，让服务端的 run 被取消。事件由 `hub.ts` 本地造，页面重连时拿到和服务端一样的 snapshot。停止是 `POST /guest/runs/{id}/cancel`，流继续读到结束，保留已生成的部分。
- 游客的摘要跟着对话记录存（`StoredConversation.summary`），规则是纯函数 `summary.ts`（有 vitest）：`survivingSummary` 就是服务端外键级联的本地版——终点或保留的提问不在剩下的行里，摘要就没了，**每个删行的地方（`repo.runStarted`）和每次读取都要过它**；`rowsToSend` 决定请求带哪些行；`planCompaction` 是手动压缩覆盖什么（和 Go 的 `ModeManual` 一对一，改一边改两边）。`runner.ts` 收到 `compaction_finished` 就先存摘要再发布事件，`guest_end` 的摘要也存。手动压缩和撤销是 `compaction.ts`，由 `router.ts` 里的 `/conversations/{id}/compaction` 三个路由答，POST 调 `/guest/compaction`；run 在跑时 409，等模型写的时候被改写删掉了终点就不存。
- 本地数据没有服务端的权威时钟：消息行的 `createdAt` 全是服务端给的，不要在本地给消息盖客户端时间（`runInterrupted` 例外且取「最新一行之后」）。临时对话 24 小时过期在读取时本地执行；上传了没发的文件 24 小时后清。多个标签页靠 BroadcastChannel 互相通知，收到的页用 `hub.announceChanged` 发 `transcript_changed`（改写时带 `from`）。
- 游客存储：进入 `/guest` 时 `startGuest()` 就探测 IndexedDB（`open-storage.ts`，4 秒超时）；能开就用，开不了（某些隐私模式、站点数据被禁）退回内存存储，`GuestNotice` 改说「刷新后对话会丢失」（`lib/guest/kept.ts`）。`idb.ts` 的 `connect()` 不缓存失败的 open。仓库给文件造的 object URL 在文件、对话删除时 revoke（`dropBlob`/`dropConversation`）。
- 游客界面：`GuestNotice`（「游客模式：对话只保存在这个浏览器里」+ 登录）、侧栏账号菜单没有管理后台和退出、设置里没有「账户」页。共用组件里的链接一律用 `useChatPaths()`（`/` 还是 `/guest`），头像上传不适用。

## 认证

HttpOnly Cookie 会话，库里只存 token 的 SHA-256；前端从不接触 token，靠 `/auth/session` 判断登录状态，API 客户端必须 `credentials: 'include'`。用户名不能含 `@`（数据库约束保证，登录框同时接受用户名和邮箱）。第一个注册的账号成为管理员（见下「注册」）；最后一个启用的管理员不能被降级或停用：凡是可能让管理员减少的改动（`PATCH /admin/users/{id}` 和 `POST /admin/users/bulk` 的改角色、停用）都在**一个事务里先拿 `LockAdminMembership`（事务级 advisory lock）**，再读目标、更新、数启用的管理员（0 个就回滚，409 `last_admin`），不能「先数后写」分成两步，否则两个管理员同时互相降级会都读到「还有两个」而都成功、一个不剩；批准（`approved`）不会减少管理员，不排队。新增任何能降级或停用账号的路径都要走这把锁（测试在 `admin_last_admin_pg_test.go`，用自己的库，因为数的是全库的管理员）。停用账号或改密码会清掉其会话，已开着的页面下一次请求得到 401 就被带去登录页（见前端约定）。用停用或待审批的账号登录（密码对了服务端才说 `account_disabled` / `pending_approval`）时，登录页不显示一行红字，而是盖上全屏的毛玻璃覆盖层（`components/auth/AccountOverlay.tsx`，照搬 OpenWebUI 的 `AccountPending.svelte`）：「你已被封禁」，或「账号等待审批」加「再次检查」（用刚才的账号密码再登录一次，批准了就直接进去）。「返回」或 Esc 回到清空的表单。两种拒绝（和审批模式注册的 202）都带上要联系的管理员（`details.admin` / `RegisterPending.admin`：最早创建、仍启用的管理员的名字和邮箱，`GetContactAdmin`），覆盖层照 OpenWebUI 显示「管理员：名字（邮箱）」；只有证明了密码（或刚注册）的人才拿得到，密码错时什么都不给。自助接口（`PATCH /auth/me`、`POST /auth/password`）刻意不带角色和停用字段。不做账号删除，停用即可。

### 注册与审批

后台 → 系统 有两个开关（`app_settings`：`allow_registration`「允许注册」、`registration_approval`「新用户需审批」，都默认**开**，所以升级后的实例默认是审批模式），组合出三种模式：

- **关闭**：`POST /auth/register` 返回 403 `registration_closed`，只有管理员能在 后台 → 用户 新建账号。
- **开放**（允许注册、不审批）：新账号马上能用，和以前一样。
- **审批**（允许注册、需审批）：新账号 `users.pending = true`，注册返回 **202** `{status: "pending_approval"}`，不发会话；登录时密码对了才返回 403 `pending_approval`（密码错依旧是通用的 401 `invalid_credentials`，不泄露账号是否存在），也不发会话。「需审批」只在允许注册时有意义，界面在关闭注册时把它调暗禁用。

几条不变量：

- **第一个账号**（不算 guest）永远是管理员、永远不 pending、永远不被开关拒绝，否则全新安装会把自己锁在外面。这个判断在 `RegisterUser` 的 INSERT 里做，注册先拿 `LockRegistration` 的 advisory lock，所以两个人同时抢第一个也不会都成为管理员。公开的 `/auth/options` 在还没有账号时报告 `registration: true, approval: false`。
- 管理员新建的账号（`POST /admin/users`）永远不 pending，关闭注册时它就是唯一的加人方式。
- `pending` 默认 false，迁移后所有已有账号都算已批准（`migration_signup_pg_test.go` 测）。
- **pending 的账号什么都碰不到**：登录不发会话，`GetSessionUser` 还多一道 `NOT users.pending` 作为第二把锁，所以 `auth.Middleware.Require`（唯一的登录闸）也会拒绝它——就算有人哪天写出一条给它发会话的路径。新增任何「凭 token 认人」的路径都要走 `GetSessionUser`，别另写。
- 批准只有一个方向：`PATCH /admin/users/{id}` 带 `approved: true`，或 `POST /admin/users/bulk` 带 `approved: true`（和 `role`、`disabled` 三选一）；`approved: false` 是 400 `invalid_approval`，没有「退回待审批」。批准和启用/停用互相独立。非管理员调这些接口得到 404。
- **拒绝 = 停用**（账号从不删除）：待审批的行上「拒绝」就是 `disabled: true`，之后它是个普通的已停用账号（登录说 `account_disabled`），再「启用」会让它回到待审批名单。用户列表的 `status` 过滤（`pending`/`enabled`/`disabled`，可重复）里 disabled 优先于 pending，所以「待审批」名单只含还在等决定的人。
- 登录页和注册页靠公开的 `GET /auth/options`（`registration`、`approval`、`guest`）决定显示什么：注册关闭时隐藏「注册」链接、`/register` 显示「注册已关闭」；审批模式下注册成功后注册页盖上同一个「账号等待审批」覆盖层（「再次检查」用注册时的邮箱和密码登录）而不是跳进聊天。读不到选项时按开放处理（服务端才是裁判）。改了开关要让 `queryKeys.auth.options` 失效。

### 登录与注册的限流

失败次数存在 `auth_attempts(kind, ip, subject, created_at)`，Postgres 里，所以多实例共享；`CleanupNow` 里 `purgeAuthAttempts` 删一小时前的行。IP 用和游客限额同一个 `clientIP`（`RealIP` 处理过的 `RemoteAddr`，IPv6 按 /64），所以同样**必须**部署在会正确设置转发头的代理后面——注意 `RealIP` 优先读 `True-Client-IP`、`X-Real-IP`，再读 `X-Forwarded-For`，`deploy/Caddyfile` 因此会丢掉前者、用连接地址覆盖后者；换别的代理要做同样的事。

- 登录：每个 IP 每 15 分钟 10 次失败，每个（IP，标识）5 次；超过就 429 `login_rate_limited`，带 `Retry-After`（秒，同时在 `details.retryAfter`），且**在检查密码之前**就拒绝，所以限流期间密码对了也进不去。**绝不按标识单独锁账号**，否则任何人都能从别处把管理员锁在外面；也因此换一个 IP 或另一个标识不受影响。标识存的是小写去空格后的 SHA-256，不存原文。
- 先占位后验证：登录先在 advisory lock 下「数 + 记」一行，密码对了再把这行删掉（所以只有失败才占额度，成功的登录不会耗尽它，也不会清掉别人的失败）。这样一批并发的猜测在 argon2 慢慢算的时候就已经被数进去，而不是算完才记。被拒的请求不记录，免得延长等待。密码对但账号待审批/已停用不算失败。
- 注册：每个 IP 每小时 5 次，**每次尝试都算**（包括邮箱已被占用的——那是探测邮箱的办法），在 argon2 之前计数；超过是 429 `register_rate_limited`。关闭注册时的拒绝发生在计数之前，不记录。
- 新增一类需要限流的公开端点，用 `takeAttempt(ctx, attemptLimit{...}, ip, subject)`，别另造一张表。

## 内容审核

用户发的一切都以运营者的上游账号（OpenAI、Anthropic、中转站）名义发出，违规内容会让这些账号被封。所以：用户新发的输入先审核再发给模型，拒绝的记日志，反复违规的账号被停用、游客地址被封禁；上游请求再带一个用户标识，万一被封也能定位到人。代码在 `internal/api/moderation*.go`、`end_user.go`，查询在 `queries/moderation.sql`。

### 检查什么、在哪儿

- 只检查**用户新发的输入**：发送（`CreateMessage`）的文字和图片附件、改写（`EditMessage`）的新文字（附件是第一次发送时审过的）、回答 `ask_user` 时用户**打的字**（`other`，选的选项是模型自己写的，不审）、游客的新消息和回答（`GuestRun` 里的 `moderateGuest`，只看浏览器真的发来字节的图片）。
- **不审**：重新生成（重发的是审过的输入）、工具结果、网页内容、历史对话、标题、摘要——都不是用户新发的。新增会把用户输入送上游的路径要想清楚属于哪一类。
- 位置和容量检查一样：`chooseRun` 之后、事务之前，所以被拒绝的请求不写行、不认领附件、不建 run。游客在 `planGuestRun` 之后、`seedGuest` 之前。拒绝是 **422 `content_blocked`**，`details.categories` 只列命中的类别名，不给分数、不给原文；服务不可用并且要拦时是 **503 `moderation_unavailable`**。幂等重试（`requestId` 已有 run）在检查之前就返回了，不会被重复审。

### 设置

全部在 `app_settings`，每个请求读一次（一条 `GetSettings` 查询），改了立即生效，保存时服务端校验（400 `invalid_moderation_*`、`moderation_key_required`、`moderation_model_required`、`invalid_guest_ban_hours`），任何一项不合法就一项都不存；入口是 `GET/PATCH /admin/moderation`，页面在 后台 → 系统 →「审核」。读不出来的存储值是错误（按"服务不可用"处理），不是默认值，免得一个坏值悄悄把审核关了。

- `moderation_mode`：`off`（默认）/ `openai` / `model`。
- `openai`：`moderation_openai_key`（只写不读，接口只报 `openaiKeySet` 和末 4 位，不进日志，错误信息里也抹掉）、`moderation_openai_base_url`（空 = `https://api.openai.com/v1`）。POST `{base}/moderations`，模型 `omni-moderation-latest`，输入是文字 + 图片（`image_url` data URL，最多 6 张、合计 24 MiB，超了算"检查不了"），超时 5 秒；文字超过 32000 字时取头尾各一半。多个 result 按"任一标记、取高分"合并。
- `model`：`moderation_model`，目录里的模型。按**服务端任务**走 `pickChannel`（`staff=true`，管理员发的请求带 `admin` 以便容量豁免，和 `taskModel` 一样），用量记新类型 `moderation`（`usage_logs_kind` 约束已加，所以花费上限会计入）。提示词是英文、模型可见（`moderationPrompt`），用户文字放在 `<message>` 标签里当不可信数据，里面的同名标签被转义成 `&lt;`；要求只回一个 JSON（`categories` 布尔 + `scores`），容忍代码围栏和前后文字，读不出来就算失败。**只审文字**，图片不给任意模型。`errModelCapacity`（模型满了）按"服务不可用"处理，不去问。
- `moderation_categories`：拦截的类别，OpenAI 的 13 个类别里多选，默认 `sexual/minors`、`harassment/threatening`、`hate/threatening`、`illicit/violent`、`self-harm/instructions`、`violence/graphic`。`moderation_min_score`（默认 0）：为 0 只看接口给的布尔；大于 0 时，选中类别的分数达到它也算命中——**只会更严，不会放过布尔已标记的**。
- `moderation_exempt_admins`（默认**关**：管理员的消息发往同一个上游，同样要审）。
- `moderation_on_error`：`allow`（默认）/ `block`，服务出错、超时、没渠道、模型满了、图片太多时账号用户的消息怎么办。**游客永远拦**（没有账号可以担责）。
- `moderation_strikes`（默认 3，0 = 只记录不处罚）、`moderation_strike_window_hours`（默认 24）、`guest_ban_hours`（默认 168，0 = 永久；共用出口的地址永久封禁风险大）。

### 日志与处罚

- `moderation_events`（迁移 `…_content_moderation.sql`）：只记被拒绝的，不记通过的。`user_id`（账号，`SET NULL`）或 `guest_ip`（游客，IPv6 按 /64，即 `clientIP`），`conversation_id`（游客没有，`SET NULL`）、`source`（`send|edit|answer|guest`）、`mode`、`categories`、`excerpt`（原文前 500 字，运营者自己的记录，只有管理员能读）、`action`（目前只有 `blocked`）、`sanction`（这条事件触发了处罚才有：`disabled` / `banned`）。90 天后由清理循环的 `purgeModeration` 删。**不要给这张表加"user_id 或 guest_ip 必有一个"的约束**：用户行删除时外键置空会撞上它。
- 接口（都在管理员分组，非管理员 404）：`GET /admin/moderation/events`（游标分页 `(created_at, id)`，过滤 `userId`、`guestIp`、`category`，游标只对发出它的那组过滤有效）、`GET/POST/DELETE /admin/moderation/bans`（POST 按日志行的 `eventId` 封那条的游客地址，账号的行是 409 `not_a_guest`；DELETE 的地址放查询参数，因为 IPv6 网段里有 `/`）。
- 处罚在 `recordBlocked`：**一个事务**里按主体（`user:<id>` / `ip:<地址>`）取 advisory lock、写事件、数窗口内的违规、够了就处罚，所以并发的违规请求不会各数少一条。账号：走和管理员停用一样的两步（`AdminUpdateUser` 的 `disabled` + `DeleteUserSessions`），已开着的页面下次请求 401、登录页出现已有的封禁遮罩；**管理员永远不自动停用，只记录**。游客：`guest_bans`（`ip` 主键、`expires_at` 空为永久、`reason`、`event_id`），已封的地址再封只延长不缩短。计数**从该主体上一次被处罚之后**算起，所以管理员启用账号 / 解封地址后重新来过，不会下一条消息又触发。触发处罚的那个请求直接以处罚作答（`moderate` 里 `recordBlocked` 的结果）：游客是 403 `guest_banned`（带 `until`），页面立刻盖上封禁遮罩；账号是 401（会话已删），页面按会话失效去登录页，登录时看到封禁遮罩。
- 过期的封禁无论清理循环删没删都不生效（`GetGuestBan` 带过期条件），清理循环再把它们删掉。

### 游客按地址封禁

被封地址的每个 `/guest/*` 请求（`config`、`runs`、`title`、`compaction`、`images`、`favicons`）都是 **403 `guest_banned`**，`details.until` 总是在，要么是结束时间要么是 `null`（永久）；由 `rejectBannedGuests` 中间件挂在 `requireGuestMode` 之内。`/guest/status` 不拦，但多报 `banned` / `bannedUntil`（页面靠它知道）；`POST /guest/runs/{id}/cancel` 也不拦（运行已经在花钱，停下更省）。地址取 `clientIP`，所以和游客限额一样必须部署在会正确设置转发头的代理后面。

### 用户标识（上游请求里的 end-user id）

- 每个发往模型的请求带 `llm.Request.User`：账号 = `HMAC-SHA256(密钥, 用户 id)`，游客 = `HMAC-SHA256(密钥, "guest:" + clientIP)`，十六进制取前 32 位。密钥 `end_user_key` 在 `app_settings`，首次使用时生成（`InsertSettingIfMissing`，和 `storage_signing_key` 一路），**没有任何接口读或写它**；不含邮箱，别人拿不到密钥就反推不出 id。
- 线上字段：OpenAI Chat Completions 和 Responses 是 `safety_identifier`，Anthropic 是 `metadata.user_id`，Gemini 没有这个字段所以不发。这几个键在 `Options.Extra` 里是保留键。
- 第三方兼容端点可能拒绝不认识的字段，所以每个渠道有开关「发送用户标识」（`channels.send_user_id`，渠道表单里可改）：新建时 base URL（空则取该渠道类型的默认地址）的主机是 `api.openai.com` / `api.anthropic.com` 就默认开，否则关；迁移按同样规则给已有渠道回填。适配器只在开关开着且请求带标识时才发（`Config.SendUser`）。
- 传到哪：run 的每次请求（`Options.EndUser`，游客的 run 取 `runs.client_ip`）、标题（含按需标题和游客标题）、上下文压缩（run 内、手动、游客）、模型审核的请求。新写会用模型的路径也要带上 `User`。

### 前端

- 被拒绝的发送 / 改写 / 回答走已有的发送错误路径（`useSendError`）：提示「这条消息未通过内容审核，没有发送」，**不显示类别**；输入框的文字和文件原样留着（`handleSend` 的 `onError`），改写的输入框在被拒绝后带着打的字重新打开（`NextChat` 的 `refusedEdit` → `UserMessage.restoredDraft`，因为改写期间那条消息被藏起来了，本地 state 保不住）。游客的拒绝同一条路：`GuestError` 带着服务端的 `details`。
- 游客被封：`lib/guest/index.ts` 里所有发往网络的请求都经 `network`，看到 403 `guest_banned` 就 `markGuestBanned`（`stores/guest-ban-store.ts`），`/guest` 路由读到 `status.banned` 也一样；`NextShell` 在游客页上盖满窗的磨砂遮罩（`GuestBannedScreen`，复用 `AccountOverlay`），只留「登录」一条路。
- 后台：系统 →「审核」页签（`features/admin/moderation/`：zod 模式 + vitest、表单、被封地址列表和「解封」），「审核日志」页（`features/admin/moderation-logs/`，`DataTableCursorPagination`，游客行有「封禁此地址」、账号行链到用户页），渠道表单的「发送用户标识」开关。使用日志的类型过滤里有「内容审核」。
