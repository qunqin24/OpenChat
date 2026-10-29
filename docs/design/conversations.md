# 对话与转录

对话是一条线的转录：发送、改写、重新生成、删除、被审核拒绝的行、上下文压缩与摘要、标题、记忆、临时对话、搜索与导出。规则摘要见 [AGENTS.md](../../AGENTS.md)；下文为完整设计说明。

> 文中「后端约定」「前端约定」「内容审核」等指 AGENTS.md 的章节，细节按主题分在本目录各文件，索引见 [README.md](README.md)。

## 线性转录、改写与重新生成

- 对话是一条线，没有分支和版本：一个对话的所有 `messages` 行按 `(created_at, id)` 排列就是对话记录，发给模型的上下文也是它（`ListMessages`、`ListRecentMessages`、`ListTranscript` 只按对话取，游标沿时间回退）。被替换的东西直接删除，没了就是没了。改写任意一条提问（`POST .../messages/{id}/edit`）：在 `LockConversation` 下的同一个事务里，先在末尾写新提问，用 `MoveAttachments` 把旧提问的附件行交给它，再 `DeleteTranscriptFrom` 删掉旧提问及其之后的所有行，最后建 run；run 随被删的 user 消息级联，但一个 run 的其他行只会 `run_id` 置空，所以是按位置删而不是按 run 删；`DELETE ... RETURNING` 带回被删附件的 `storage_key`，提交后尽力删文件。重新生成（`POST .../regenerate`）只作用于最后一条提问：同一个事务里 `DeleteAnswerTo` 删掉它后面的所有行和它名下全部 run（`ask_user` 续写的 run 也在内），再建新 run；提问在加锁前后不是同一条就返回 409 `not_latest`。两者在有 run 在跑时返回 409 `conversation_busy`，不然会把正在写的 run 删掉（run 只由持有对话锁的请求启动，所以锁下的检查可靠）。发送和回答 `ask_user` 也先 `LockConversation`，才不会写进正被删的对话。新增会删行的操作照这一套来：锁、查 busy、先写后删、收集 `storage_key`、提交后删文件、发 `transcript_changed`。旧数据由迁移 `20261020000001_linear_conversations` 清成一条线：每个对话只留下当时看得到的行，别的版本和它们的子树、被重新生成替掉的回答、对应的 run 都删掉；被删行的附件改成「未被认领的上传」并把 `created_at` 设为 epoch，由清理循环的 `DeleteStaleUploads` 连存储对象（本地盘或桶）一起删。
- 发消息返回 SSE；助手消息在第一个 token 前就写库，取消算 `complete`，provider 报错才算 `failed`。
- 改写和重新生成发出的 `run_started` 批次里同时带 `transcript_changed`（改写时带 `from`，即被改的提问的 id）：行被删了，开着同一对话的其他标签页据此立刻丢掉这些行并重取最新一页。凡是会删「当前显示的对话记录」的行的新操作，都要发这个事件，否则别的标签页会一直显示已经不存在的记录。存下被拒绝的发送（`blocked` 行）也发它（单独成批、没有 `from`、没有 run）：那是新增了一行而没有 run，别的标签页收到后照旧 `refreshNewestMessages`。

## 日期与隔了一段时间的提示（`agent.BuildContextIn`，前端 `lib/time-gap.ts`）

日期不写进系统提示词（每天变会让提示词缓存和回放的思考块失效），而是写在用户消息开头：一天里第一条带 `[Date: 2026-10-28]`；距离上一行（回答、工具结果或上一条提问）**一小时以上**的用户消息改带 `[Time: 2026-10-28 10:44 Asia/Shanghai, 3 hours after the previous message]`，模型因此知道对话隔了多久、现在几点，系统提示词说明这两种行。时区取账号里浏览器上报的 `users.time_zone`，没有就用 UTC；行一旦写出就不变，历史仍是稳定前缀。页面在同样的位置（隔一小时以上的提问上方）显示「今天 10:44」「昨天 21:05」「10月28日 10:44」，门槛两边一致（`agent.TimeGap` ↔ `TIME_GAP_MS`）。

## 被审核拒绝的行（`blocked`）

- **被审核拒绝的发送是转录里的一行**（`messages.status = 'blocked'`，role `user`，内容是被拒的原文，迁移 `20261025000004_blocked_messages`；写入见下「内容审核」）。它只是被显示、从不发给模型，所以凡是**构造模型看到的东西**的查询都要跳过它：`ListRecentMessages`、`ListMessagesAfter`、`ListMessagesFrom`（因此 `LoadContextRows`、压缩的读取范围、摘要的 `through_message_id` / `kept_message_id`、标题的素材、`ask_user` 的 `pendingQuestion` 都看不见它），`ListTranscript`（导出）和搜索的命中也不含它；`agent.BuildContext` 和 `EstimateRow` 再各跳过一次，免得哪条新路径把它漏进请求。**只有 `ListMessages`（显示用）和 `GetInput`（改写用）看得见它**。它不是提问：重新生成的「最后一条提问」（`GetLastInput`、`not_latest`）是最后一条**没被拒绝的** user 行，`DeleteAnswerTo` 删掉那条提问之后的所有行：条件是 `role <> 'user' OR status = 'blocked' OR origin IS NOT NULL`，所以被拒绝的 user 行和子智能体的报告行（`origin` 非空，见 [sub-agents.md](sub-agents.md)）也算回答，一并删掉。`GetLastInput`、`GetInput`、`ListTranscript`、`ListTranscriptUpTo` 都带 `origin IS NULL`：报告行不是用户的提问，不能被重新生成、改写，也不进导出和引用。改写可以改它，和改任何提问一样：新措辞写在末尾、重新审核，`DeleteTranscriptFrom` 把它和之后的行删掉；再次被拒绝的处理见「内容审核」的「被拒绝的消息要先改写」。它是对话的终点：**末行是被拒绝的消息时，对话不能往下走**，直到它被改写（同上一节）。新增读转录的查询时想清楚：给模型看的要 `status <> 'blocked'`，给人看的不要。

## 删除对话会取消 run

- 删除对话会取消它正在跑的 run：`DELETE /conversations/{id}` 和 `DELETE /me/conversations` 在**一个事务里**先 `LockConversation`（全部删除用 `LockUserConversations` 锁住调用者所有未删除的对话），再 `SoftDeleteConversation`/`SoftDeleteAllConversations`，再用 `CancelConversationRuns` 把这些对话里 `running` 的 run 记上 `cancel_requested`，提交后对每个 id 调 `runner.Cancel`（和停止接口同一套）。不这样做的话 run 会继续调模型、占着账号的并发 run 名额，而停止接口因为对话已删除是 404，没人能停它。另一半是互斥：发送、重新生成、改写、回答 `ask_user`（以及撤销压缩）在锁下一律用 `lockLive` 而不是直接 `LockConversation`，拿到锁后发现对话已被删除就返回 404、什么都不写——所以「已删除的对话里有 run 在跑」不会出现：要么 run 先建，被删除取消；要么删除先到，发送被拒。新增任何会在对话里起 run 的路径都要用 `lockLive`；任何新的软删除路径都要取消 run。测试在 `conversation_delete_runs_pg_test.go`。

## 上下文压缩与摘要

- 上下文压缩（`internal/agent/compaction.go`、`summary.go`、`estimate.go`）：每个 run 在**每次模型请求之前**（第一次，以及每轮工具之后）估一下要发的 token——系统提示词 + 行 + 本次提供的工具定义，CJK 字按 1 token、其余 4 字符 1 token，助手行有服务商报的输出用量就用它，再乘 1.25——超过模型窗口（`contextLimit`，未知按 128000）的 50% 就先压缩；对话已有摘要时门槛是 65%，这个落差是滞回，刚压完不会马上又压。压缩由 **run 自己的模型和渠道**写（不用任务模型，小模型的窗口可能装不下要读的历史），用量记进账本，类型 `compression`。开关是 后台 → 系统 的「自动压缩上下文」（`app_settings.compress_context`，默认开，每次请求前读，对运行中的 run 立即生效）；关掉就一律不压（连下面的强制压缩也不做），已有的摘要照常用。**压缩只有 run 自己会做，用户不能手动压**（早先的「立即压缩」按钮、`POST /conversations/{id}/compaction`、`ModeManual` 都已删除）。摘要写成什么：除了**最新的用户消息**（原样保留，当前指令不能变成历史）之外，到转录最后一行为止的一切，包括本 run 前面的工具调用和结果；边界永远取最后一行，所以调用和结果不会被切开。增量：旧摘要的文字加上之后新增的行（旧摘要保留的那条提问从没写进旧摘要，算历史）合成**一份**新摘要替换旧的，一个对话至多一份；要读的内容超过窗口就切成几段依次折叠进摘要。摘要提示词在 `compaction.go`（英文，模型可见文字）；工具结果和网页是不可信数据，提示词要求只记录、不把里面的指令带进摘要，历史里的闭合标签也会被转义，因为摘要是以**用户**消息发出的。被审核拒绝的 `blocked` 行不在压缩的世界里（见上）：不被读、不被总结，摘要永远不会止于它或保留它；转录末尾只有被拒绝的行时没有可压的内容（`ErrNothingToCompact`，run 里当作没压成）。
- 摘要存在 `context_summaries`（`conversation_id` 主键；`through_message_id` 覆盖到的最后一行；可空的 `kept_message_id` 原样保留的提问），两个外键都是 `ON DELETE CASCADE`——这就是和线性编辑的全部一致性：改写提问会删掉它和它之后所有行，摘要的终点或保留的提问在其中就连摘要一起没了，在它之后改写则摘要还在；重新生成只删最后一条提问之后的行，摘要终点是被删的回答就没了（摘要里有那个回答的内容），终点是提问本身则还在（有测试）。**行从不因压缩而改变**，转录页照常显示所有消息。构造请求用 `LoadContextRows` + `BuildRequestContext`：`[系统提示词, 摘要（用户消息，包在 <compressed_history_summary> 里）, 保留的提问, 摘要终点之后的行…]`；系统提示词一个字节不变，前缀只在压缩发生时变（所以 `HistoryLimit` 现在只是 2000 行的安全上限，别拿它管大小）。Anthropic、Gemini 会合并相邻同角色轮次，OpenAI 两套 API 直接接受连续的用户消息，都有测试。压缩失败（摘要请求出错、模型没写出东西）只记日志，不写任何东西，这次请求原样发出，下一次请求前再试；同一 run 里压缩过一次、请求没比上次增长窗口的 10% 就不再压（问题本身大到压不掉时不要每次都花一次摘要）；摘要请求失败过的，请求要长大窗口的 10% 才再试，失败满 `maxCompressionFailures`（2）次后这个 run 不再试。
- 模型因长度拒绝请求时的强制压缩（`execution.recoverContextLength`，`compress(…, force=true)`）：估算低估了（图片、工具定义、服务商自己的分词），模型回 `llm.ErrorContextLength`（`context_too_long`）。这时若自动压缩开着，run 不记失败、不退避，直接压一次——跳过「估算超过门槛」和「比上次长大 10%」两道检查，其余照旧（`failedRound` 与失败次数的保护都还在，本轮已经写过摘要则没有新内容可折，`ErrNothingToCompact`）——然后立即重发请求（`generate` 返回 `done=false`，循环再来一次，不进 `RetryScheduled`）。**每个 run 至多一次**（`runCompression.forced`，在内存里，进程重启后可能再多一次），不论结果；压缩没写出摘要（失败、没有新内容、开关关着）或重发又被拒绝，就照旧以 `context_too_long` 结束，那条失败的助手行照常写。拒绝的那次请求在账本里是 `chat` 的一行错误，压缩是 `compression` 一行（run 的 `compaction_started` / `compaction_finished` 与 `live.compacting` 和自动压缩完全一样）。只在拒绝没带出任何内容时才这样做。前端的 `context_too_long` 提示因此不带按钮：压过了仍然装不下，建议新开对话或换窗口更大的模型。
- 读取与撤销：`GET/DELETE /conversations/{id}/compaction`（没有 POST）。同一对话不会同时压两次（`Compactor.lock`：`pg_try_advisory_lock`，在专用连接上，关连接即放锁；撞上了是 `ErrCompactionBusy`，run 里当作没压成）。DELETE 只删摘要，原消息本来就在；run 在跑时 409 `conversation_busy`。运行里的压缩发 `compaction_started` / `compaction_finished`（带摘要，失败则不带）进 run 的批次，`runs.live.compacting` 在压缩期间为真，让中途连上的页面也显示状态行；不属于 run 的变化（只剩撤销）发 `compaction_changed`（run id 为空、version 为 0，和标题事件一个路子）。删行造成的摘要消失不单独发事件，靠 `transcript_changed` 让页面重读摘要。

## 个人记忆

- 个人记忆（`internal/memory`、`tools/memory.go`）：对话在第一次运行时把用户的自定义指令和记忆存成 `conversations.memory_snapshot`（快照由发起运行的请求在建 run **之前**取，`prepareSnapshot`，四个起 run 的入口都调：发送、改写、重新生成、回答 `ask_user`；这里同时给没审核过的文字做内容审核，见「内容审核」→「自己写下的文字」；`personalPrompt` 里的 `EnsureSnapshot(…, nil)` 只是没走这条路的运行的兜底，不审核），此后每次请求的系统提示词都从这份快照渲染，同一对话内字节不变（提示缓存和思考块校验依赖它）；对话中改动的记忆从下一个对话起生效。模型用短 id（`m1`…）引用快照里的记忆，id 只在本对话的快照里有意义；工具执行时的用户取自 `agent.ScopeFrom(ctx)`，永远不信参数。记忆上限 100 条、每条 200 字，满了拒绝而不淘汰。只有模型的改动写 `memory_events`（撤销和"最近更改"用），用户自己删改记忆时会连带清掉对应日志。记忆接口全在 `/me/*`，没有任何管理员接口能读到别人的记忆或自定义指令；管理员只有 `memory` 总开关。

## 对话标题

- 对话标题（`api/titles.go`）：run **结束后**（不管怎么结束：完成、等提问、被用户停止、失败，`titleAfter`），标题还是 `New chat` 的对话由任务模型（没有就用对话自己的模型）起名，后台 goroutine，不挡回答。模型起不出来（出错、只写了推理没写字、容量满了）就**用提问的第一行当标题**（`questionTitle`，和 OpenWebUI 一样），不再留着 `New chat` 等下一次回答——以前只在正常完成后起名、失败就放弃，停掉的、失败的、标题模型出过错的对话会一直叫「新对话」。用户自己改过的名字永远不覆盖（`SetGeneratedTitle`）。只有被拒绝的消息、没有 run 的对话不起名（不拿违规原文当标题）。

## 临时对话

- 临时对话（`conversations.temporary`）：不进列表、不起标题、没有记忆和 memory 工具（自定义指令照常），创建 24 小时后由 `Server.StartCleanup` 里的单条 `DELETE` 删除（多实例安全）；用量日志的会话外键是 `SET NULL`，记录保留。

## 搜索

- 搜索：`GET /conversations?search=` 在标题和 user/assistant 消息的 `content` 里做 `ILIKE` 子串匹配（中文没有空格分词，所以不用全文检索），只查调用者自己的非临时对话，分页同列表；靠消息命中的结果带 `match`（消息 id、角色、命中处前后的片段）；被审核拒绝的消息（`blocked`）不参与匹配。前端搜索结果的查询键是 `queryKeys.conversations.search(term)`，刻意不在 `list` 下面——`list` 会持久化到 localStorage，而搜索结果引用了消息内容。

## 引用其他对话

照 OpenWebUI 的 Reference Chats：一条消息可以引用调用者自己的其他对话（`conversationIds`，最多 5 个，`api/references.go`），引用的是对话而不是一份拷贝。

- 发送（和被拒消息的改写）时检查：自己的、`kind = 'chat'` 的（`GetConversation`）、不是当前对话、里面已有问答；否则 400 `reference_unavailable` / `reference_empty` / `too_many_references`，普通消息的改写不能带（`references_not_editable`）。每个引用记成消息 `parts` 里一个媒体类型为 `application/x-openchat-conversation` 的附件零件，`Ref` 是 `conversation:<id>@<当时最后一条可引用消息的时间>`，`Filename` 是当时的标题；`Message.references` 由它得出。
- 不存文件、不建附件行。每次构建模型请求时由 `attachmentResolver.reference`（`agent/references.go`）现读：`GetConversation`（按 run 的用户）+ `ListTranscriptUpTo`（`role <> 'tool'`、非 `blocked`、`origin IS NULL`、不晚于记下的时间），写成 Markdown（标题、说明行、`## User` / `## Assistant (模型)`、问题带的文件名），上限 `doctext.MaxTextBytes`。之后再往那个对话里说的话不会带进来；对话被删了，模型看到的是「已不可用」的说明。
- 读出来后和文本文件走同一条路：短的整篇发，超过 `llm.InlineTextBytes` 且模型能调工具时发概览卡，由 `file_read` / `file_search` 按需读（`ListConversationTextFileMessages` 认得这种零件）。
- 对话的内容不再审核：那是用户自己的对话，提问发出时审过，回答本就是对话历史会发给模型的那类文字。**标题要审**：标题是用户随手改的、改名时不审，而模型会读到它（正文开头和概览卡的名字），所以引用时把标题并进这条消息一起送审（`withReferenceTitles`），模型读到的一律是引用时记下的标题（零件的 `Filename`），之后再改名不影响。

## 「我的数据」：导出与全部删除

- 「我的数据」：`GET /me/export` 一次一个对话地流式写出 JSON（只有 user/assistant 文本和附件文件名，不含临时和已删除的对话，也不含被审核拒绝的消息；写到一半出错就切断连接，不留一份看似完整的残缺文件），`DELETE /me/conversations` 软删除调用者的全部对话（含临时的），之后走和单个删除一样的保留期清理。两者都只认会话里的账号，没有任何参数能指向别人。

### 模型什么时候记（`api/agent_setup.go` 的 Memory 一节）

克制为主。只在两种情况下存：用户要求记住 / 忘掉 / 改正；用户明确说出关于自己的长期事实（身份、工作、所在地、对回答方式的固定偏好），且它会改变其他话题下的回答。**不从提问推断兴趣**：问过某个话题、说「最近在关注 / 挺感兴趣 / 追了很久」都不记，记忆不能用来总结或追踪用户在聊什么；拿不准就不存。新事实单独一条，不追加进已有记忆、不合并成长条；只有用户纠正或事实本身变了才改。v1.1.2 之前的提示词是「像私人秘书一样主动记录」（仿 Kelivo），会出现一条记忆被反复改写、记下用户最近聊过的话题的情况。用 DeepSeek V4.1 Flash 和 Claude Haiku 5.5 实测（临时库）：话题类提问新旧都不记，明确的两条事实新版各存一条（改之前第一版会被 DeepSeek 合并成一条）。

