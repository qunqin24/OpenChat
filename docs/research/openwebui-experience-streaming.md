# Open WebUI 体验与流式同步研究

> 研究对象：[open-webui/open-webui](https://github.com/open-webui/open-webui)（SvelteKit 前端 + FastAPI 后端，Socket.IO 实时推送，可选 Redis 共享状态）。
> 固定版本：commit `0a7c15832fb30b1903753e83f81dc7d27e5b0944`，提交标题 `ci: run the external regression suite on release pull requests (#29313)`，提交时间 2026-09-04；前端 `package.json` 版本 `0.11.3`。研究日期 2026-09-13。
> 许可证：自定义 **Open WebUI License**（GitHub metadata 为 `NOASSERTION`/`Other`），包含品牌保留要求及适用例外；历史许可还见 `LICENSE_HISTORY`。实际复用文件应核对[该版本许可证原文](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/LICENSE)，本文不作许可适用结论。
> 定位：本文只覆盖 OpenChat 新增要求中的 **UI/UX、多端同步、真实流式拓扑**，为自研 Go 内核提供参考。模型供应商/工具层以 Pi AI、Hermes、OpenCode 等专篇为准，本文不展开。
> 标记约定：**源码证据** = 在该固定 commit 源码读到；**官方文档描述** = 项目文档/README 描述；**OpenChat 建议** = 拟采用、尚未实现；**待验证** = 静态阅读无法确认的运行时/部署保证。
> 本文只做源码静态阅读，**未运行 Open WebUI**，未做任何集成或验证。

## 结论速读

- **不存在"浏览器一条 SSE 贯穿"这回事。** 正常聊天路径里，浏览器发 `POST /api/chat/completions`，后端把它变成后台 `asyncio.Task` 并**立即返回 JSON `{status, task_ids, chat_id}`**；token/状态改从 **Socket.IO `events`** 事件推送。SSE 只出现在两处：(a) 后端→模型供应商；(b) 兼容 API / MoA 合并等直连路径。**源码证据**
- 后端按 **`user:{user_id}` 房间**广播事件，允许同一用户的多个活动连接接收；前端再用 `event.chat_id === $chatId` 决定哪条消息要更新。这是多标签同步的主要机制，断线与多实例下的投递保证仍需验证。**源码证据 / 待验证**
- 生成独立于发起它的 HTTP 连接：任务登记在 `tasks.py`，Redis 配置下可跨实例下发取消命令；刷新与重连主要依靠 response_streams 快照、任务状态和重载。当前检查路径未确认按持久事件序号重放的机制，不能把快照恢复等同于 durable replay。**源码证据 / 待验证**
- 存在流中增量保存路径：`get_event_emitter` 在 `save_to_chat` 成立时对 `message` 事件追加内容；具体时机随事件与配置分支变化，不能由此推断每个上游 token 都已持久化。**源码证据**
- 本地 OpenChat 已有 ChatPlaceholder 式落地页、侧栏折叠/移动抽屉、50ms 切换淡入、滚动跟随+上滚暂停和首 token 前等待点；尚无跨 tab 推送、run id、重连/回放、独立取消端点、队列与持久草稿，且正文结束时才整体落库。见下文矩阵。**源码证据（本仓）**

## 证据基线

| 项 | 值 |
| --- | --- |
| 仓库 | `open-webui/open-webui` |
| commit | `0a7c15832fb30b1903753e83f81dc7d27e5b0944`（2026-09-04） |
| 研究日期 | 2026-09-13 |
| 前端版本 | `0.11.3`（`package.json`） |
| 许可证 | 自定义 Open WebUI License（非 MIT；品牌条款 + `LICENSE_HISTORY`） |
| 前端栈 | Svelte 5、`socket.io-client ^4.8.3`、`eventsource-parser ^1.1.2` |
| 后端栈 | FastAPI 0.136、`python-socketio 5.16`、SQLAlchemy/Redis 8、aiohttp 3.13 |
| 研究方法 | 按固定 commit 用 `gh` 读取源码静态阅读；**未执行**任何组件 |

## 真实流式拓扑

### 三条链路，各自的职责

```
① 发起（HTTP，非流式）
  浏览器 --POST /api/chat/completions (JSON: session_id/chat_id/id/message_ids/...)-->
  Open WebUI 后端
       ├─ 新 chat：先建 chat 行 + 所有 assistant 占位（done=false）
       ├─ create_task(process_chat)  ← 后台 asyncio.Task，按 chat_id 登记
       └─ 立即返回 JSON {status, task_ids, chat_id}

② 后端 --HTTP 流式--> 供应商
  process_chat → process_chat_payload → chat_completion_handler
    → routers/openai.generate_chat_completion
    → aiohttp session.request(POST .../chat/completions, stream=true)
    ← 供应商 SSE（text/event-stream）封装成 StreamingResponse(stream_wrapper)

③ 后端 --Socket.IO 'events'--> 浏览器
  middleware.process_chat_response 逐块解析 ② 的上游 SSE
    → event_emitter({'type': 'message'|'chat:completion'|'status'|..., 'data': ...})
    → sio.emit('events', {chat_id, message_id, data}, room=f'user:{user_id}')
    → 各 tab 的 socket.on('events', chatEventHandler)
```

- ① 的返回值被前端 `generateOpenAIChatCompletion` 用 **`res.json()`** 消化，**不是**读 SSE。只有 `session_id` 与 `chat_id` 同时存在才走后台任务；否则走"legacy/direct 同步"分支（见下）。**源码证据**：`main.py:chat_completion`（后台分支 `main.py:1776-1858`）、`src/lib/apis/openai/index.ts:generateOpenAIChatCompletion`。
- ② 的传输确实是 SSE：后端用 aiohttp 读供应商流，`routers-openai.py:generate_chat_completion` 在 `Content-Type: text/event-stream` 时返回 `StreamingResponse(stream_wrapper(r), ...)`。**源码证据**
- ③ 的事件体是 `{chat_id, message_id, internal?, data: {type, data}}`，经 Socket.IO（底层 WebSocket/轮询）下发。**源码证据**：`socket/main.py:get_event_emitter`（`room=f'user:{user_id}'`）、`src/lib/components/chat/Chat.svelte:chatEventHandler`。

### "legacy/direct" 与 MoA 才是浏览器直读 SSE

- 当请求缺少 `session_id`/`chat_id`（OpenAI 兼容调用或直连），`main.py` 直接 `await process_chat(...)`，此时 `process_chat_response` 返回的 `StreamingResponse` **就是回给调用方的 HTTP SSE**。**源码证据**：`main.py:1855-1858`、`middleware.py:process_chat_response`。
- 前端 `createOpenAITextStream`（读 `res.body` 的 `EventSourceParserStream`）**只用于 `mergeResponses`（MoA 合并）**，主聊天路径不用。**源码证据**：`Chat.svelte:3893`。
- 前端还有一个"平滑"处理：`streaming/index.ts:streamLargeDeltasAsRandomChunks` 把大于 5 字符的 delta 再切成 1–3 字符、每块 `sleep(5ms)`，以模拟更均匀的吐字；`document.visibilityState === 'hidden'` 时跳过 sleep。**源码证据**（这是 UI 观感，不是协议）。

## UI / UX 细节

### ChatPlaceholder vs 默认 Placeholder

- `src/lib/components/chat/Placeholder.svelte`：默认落地模式，居中显示模型头像、模型名（或 `Hello, {{name}}`）、描述，内嵌一个 `MessageInput`，下方是 Suggestions。**源码证据**
- `src/lib/components/chat/ChatPlaceholder.svelte`：`landingPageMode='chat'` 时可用于新对话首页，由 Messages 的空历史分支渲染；头像、模型名、大灰问候和双列建议左对齐，输入框由外层停靠底部。头像、标题和建议还有各自的淡入，不能与父层 50ms 会话转场混为一谈。**源码证据**
- 两者是表现模式选择，不应简单解释为两个 URL；已有消息、folder、embedded 等还有各自分支。**源码证据**
- 本仓 OpenChat 的 `apps/web/src/components/next/Welcome.tsx` 注释即写明参考 `ChatPlaceholder at 0a7c15832f`，结构（头像、模型名+灰字问候、两列建议）与之对应。**源码证据（本仓）**

### 侧栏状态

- `showSidebar` 持久化到 `localStorage.sidebar`；宽度持久化到 `localStorage.sidebarWidth`；移动端使用上游自己的 `MobileSwipePanel` 滑动面板。OpenChat 的移动端实现采用 Radix Dialog，二者技术实现不同。**源码证据**：`layout/Sidebar.svelte`、本仓 `NextShell.tsx`。
- 本仓 OpenChat 对应 `stores/ui-store.ts`（persist 到 `localStorage('openchat.ui')`）与 `NextShell.tsx` 的移动抽屉。**源码证据（本仓）**

### 切换会话的 50ms fade

- `Chat.svelte:4251` 在 `{#if !loading}` 内用一个 `in:fade={{ duration: 50 }}` 包裹整块聊天内容：`loading` 由 true 变 false 时整体淡入。这是"切换/加载完成"的过渡，不是逐消息动画。**源码证据**
- 本仓 OpenChat 的 `chat-layout.css:.n-chat-fade` 即 `animation: n-chat-fade-in 50ms linear both`，并有 `prefers-reduced-motion` 关闭。**源码证据（本仓）**

### loading / empty / error / stop 中间态

- **streaming 光标**：上游 ResponseMessage 在生成时使用脉冲竖线；本仓 MessageItem 的 `n-caret` 是仅在首 token 前、内容为空时出现的等待圆点。正文末尾持续光标尚未对齐。**源码证据（两仓）**
- **error**：后端发 `chat:message:error`（`data.error`），前端写入 `message.error`，`ResponseMessage` 用 `<Error>` 组件展示，且"有内容也照常显示错误"。本仓 OpenChat 目前是把 `stream.error` 作为一条内联文本 banner，无独立错误结构。**源码证据（两仓）**
- **stop**：前端 `stopResponse()` 先调 `stopTasksByChatId(token, chatId)`（或逐个 `stopTask`），再本地把所有 response 兄弟消息 `done = true`，最后 `processNextInQueue`。输入框在有文本/文件时显示发送、否则在生成中显示停止按钮；`Escape` 也触发 `stopResponse()`。**源码证据**：`Chat.svelte:3729-3778`、`MessageInput.svelte` 的 stop 按钮与 `keydown`。
- **empty**：见上节两个 Placeholder；`Messages.svelte` 里 `Object.keys(history.messages).length == 0` 是判定。

### 滚动跟随与上滚暂停

- `Messages.svelte` 的相关判断为 `scrollHeight - scrollTop <= clientHeight + 50`，等价于剩余距底约 50px 内，而不是距底小于一整个视口高度。它结合 `autoScroll` / `triggerScroll` 决定跟随；回底后还用 rAF 校正 `content-visibility` 引起的重排。浏览器相关分支需分别核验。**源码证据**
- 本仓 OpenChat 的 `NextChat.tsx` 用 `parked` ref + `BOTTOM_SLACK=24` + `n-scroll-bottom` 跳转按钮实现了同类语义。**源码证据（本仓）**

### 草稿与队列

- **草稿**：`Chat.svelte` 的 `saveDraft/restoreChatInput` 把 `{prompt, files, selectedToolIds, selectedSkillIds, selectedFilterIds, imageGenerationEnabled, webSearchEnabled, codeInterpreterEnabled, toolApprovalMode}` 存到 **`sessionStorage`**，key 为 `chat-input`（首页）或 `chat-input-<chatId>`；无草稿写入时 debounce 500ms，`MAX_DRAFT_LENGTH=5000`。**源码证据**：`Chat.svelte:740-763,4003-4041`。
- **队列**：`chatRequestQueues` 是按 chat 分键的 store。生成中再次提交（`enableMessageQueue`）时入队而不是打断；`processNextInQueue` 把队列里的 prompt 用 `'\n\n'` 合并、合并文件后一次提交；支持 `sendQueuedMessageNow`（先 stop 再发）、`editQueuedMessage`、`deleteQueuedMessage`。`QueuedMessageItem` 负责展示。**源码证据**：`Chat.svelte:2457-2531,3136-3175`、`stores/index.ts:121`。
- 本仓 OpenChat **没有**持久草稿与队列，只有一个内存态 `pendingPrompt`，用于"首页输入→创建会话→路由跳转后补发"这一段。**源码证据（本仓）**

### 跨 tab 显示

- 事件按 `user:{id}` 房间广播，所以所有 tab 都更新；前端以 `event.chat_id === $chatId` 过滤，只改当前会话的消息树；侧栏另有 `chat:active` / `chat:list` 监听，用 `setChatActive` 给对应 chat 行打上 `active`，在列表项左侧显示 spinner。**源码证据**：`Chat.svelte:1197`、`layout/Sidebar.svelte:762-810`、`stores/chatList.ts:111-124`、`Sidebar/ChatItem.svelte`。

## 多标签 / 多端同步机制

- **连接入房**：Socket.IO `connect`/`user-join` 时用 token 校验用户，随后 `sio.enter_room(sid, f'user:{user.id}')`。一个用户可以有多个 sid（每个 tab/设备一个）。**源码证据**：`socket/main.py:406-461`。
- **广播 vs 定向**：`get_event_emitter` 用 `sio.emit(..., room=f'user:{user_id}')` **广播给该用户所有会话**；`get_event_call` 用 `sio.call(..., to=session_id)` **定向到单个 tab**（交互式工具确认等需要"只问发起者"的场景），并校验该 sid 属于请求用户。**源码证据**：`socket/main.py:1057-1210`。
- **id 关联**：`chat_id`、`message_id`（assistant 占位 id，由前端生成并随请求上送）、`task_id`（后端 `create_task` 生成，`main.py:1792`）三者分开。`message_ids` 支持多模型并列，每个带 `model_id/message_id/modelIdx`。**源码证据**
- **生成独立于 HTTP**：任务在 `tasks.py`，`create_task` 返回后即脱离请求生命周期；Redis 下的取消走 pub/sub，`redis_task_command_listener` 在所有实例上匹配 `task_id` 再 `task.cancel()`。**源码证据**：`tasks.py:28-66,131-151`。
- **切换当前 chat 的过滤**：`if (event.chat_id === $chatId)`；晚到事件若属于别的 chat，不影响当前消息树，但会被侧栏的 `chat:list`/`chat:active` 逻辑用于刷新列表。**源码证据**
- **后台完成时列表更新**：`chatCompletedHandler` 只 `refreshChatList`；侧栏也监听 `chat:list` / `chat:active`。`emit_chat_list_event` 在新建/更新 chat 时广播。**源码证据**：`Chat.svelte:2533-2539`、`main.py:282-289`。

## 刷新与"恢复"的边界（重点）

- **增量落库**：`get_event_emitter` 在 `save_to_chat` 为真时，对 `message` 事件把 delta **append 到 DB**，对 `replace`/`status`/`embeds`/`files`/`source` 等分别落库。**源码证据**：`socket/main.py:1062-1180`。
- **快照缓存**：`middleware.py` 在流式循环里调用 `save_response_stream(redis, task_id, chat_id, message_id, content, output)`，把"当前拼接内容"写入 Redis hash（默认 `REDIS_RESPONSE_STREAM_TTL=3600` 秒）。**源码证据**：`middleware.py:4706`、`tasks.py:172-229`、`env.py:380-382`。
- **刷新恢复**：`GET /api/chats/{id}` 返回前调用 `overlay_response_streams(data, get_response_streams_by_chat_id(...))`，把 Redis 快照里的 `content/output` 盖回对应 message，并置 `done=false`。**这是快照覆盖，不是事件回放。** **源码证据**：`routers/chats.py:62-85,1324-1344`。
- **重连恢复**：socket `connect` 触发 `handleSocketConnect`：若当前 chat 还有未完成的 assistant 叶子，查 `GET /api/tasks/chat/{chat_id}`；返回 `task_ids` 为空则 `loadChat()` 重载快照，否则继续等广播。**源码证据**：`Chat.svelte:1513-1530`。
- **未确认 durable replay**：在本次检查的主路径中未见 `Last-Event-ID` 或按持久序号重放；所观察到的恢复主要靠快照/重载与任务状态查询。不能据此承诺断线期间的全部 delta 会补发；多实例和部署扩展的投递保证仍需运行验证。**源码证据 / 待验证**

另一个重要时序：已读的 `get_event_emitter` 是先 `sio.emit`，再按事件类型更新 DB。
因此客户端收到某条推送不代表对应写库已提交成功。OpenChat 若提供持久恢复游标，
必须自行定义落库、快照与广播的顺序，不能直接沿用这个假设。

## 本地 OpenChat 对照矩阵

> 上游列来自上述固定 commit；本仓列来自 `apps/web/src`、`apps/server/internal/api/messages.go`。**不要把上游行为反推成"本仓已具备"。**

| 能力 | Open WebUI（固定 commit） | 本仓 OpenChat 现状 |
| --- | --- | --- |
| 落地页 | `Placeholder`（含输入框）+ `ChatPlaceholder`（会话内空态，两列建议） | 已实现 `next/Welcome.tsx`（对应 ChatPlaceholder 视觉） |
| 侧栏折叠/移动抽屉 | `showSidebar` + `localStorage.sidebar` + 宽度持久化 | 已实现 `ui-store` persist + `NextShell` 抽屉 |
| 切换淡入 | `in:fade 50ms` | 已实现 `n-chat-fade` 50ms |
| 滚动跟随/上滚暂停 | `autoScroll` + 距底阈值 + rAF 校正 | 已实现 `parked` + `BOTTOM_SLACK` + 跳转按钮 |
| 生成指示 | 生成中的脉冲竖线 | 首 token 前等待圆点；正文尾部光标尚未对齐 |
| 空/loading/error | Placeholder / Loader / `Error` 组件 | 有空态/loading/内联错误文本；无结构化 `message.error` |
| Stop | 调取消端点停止后台任务 | **AbortController 断 HTTP → `r.Context` 取消**，无取消端点 |
| 传输 | HTTP 发起 + Socket.IO 事件推送 | **单条 `POST .../messages` SSE**（`start→delta…→done|error`） |
| 跨 tab 同步 | `user:{id}` 房间广播 + `chat_id` 过滤 | **无**（各 tab 各自一条 SSE，无共享） |
| 生成独立性 | 后台 Task，独立于请求连接 | 绑定 HTTP 连接；连接断则生成停 |
| 持久化时机 | 流中增量写库 + Redis 快照 | **正文结束才整体落库**（assistant 行先建，status=streaming） |
| 恢复 | Redis 快照 overlay；重连查 `task_ids` | 无 runId/快照/重连；刷新只能重拉历史 |
| 队列/草稿 | `chatRequestQueues` + `sessionStorage` 草稿 | 无队列；仅内存 `pendingPrompt` |
| 事件去重/乱序 | `response.*` delta 用 item/output/content index 归并 | 无 `seq`/去重/乱序处理 |

本仓现状的边界条件值得明确：Stop 是"客户端断开"，后端在**首 token 前**已写入 assistant 行；`messages.go` 用 `context.WithoutCancel` + 5s 超时把已收到的部分在取消时存为 `complete`，只有 provider 真错才存 `failed`；`ctx.Err() != nil` 与 provider error 被区分对待。**缺 runId、缺 replay、缺 cancel endpoint 是明确缺口，不是待完善的小项。** **源码证据（本仓）**

## OpenChat 自研建议

以"Go/Postgres 是权威、run 与订阅连接分离"为目标，第一版保持模块化单体，不要照搬 Python/FastAPI/Socket.IO/Redis 整栈。

1. **权威数据仍归 Go + Postgres**：run 状态、消息 part、seq 全部落库；Redis 若引入仅作加速，不能成为恢复的唯一来源。**OpenChat 建议**
2. **异步执行接口**：`StartRun(ctx, ...) -> runId` 只负责登记与启动，返回后与 HTTP 连接解耦；执行体在服务进程内 goroutine/worker，不随请求 context 取消。**OpenChat 建议**
3. **订阅与执行分离**：用独立 GET 事件订阅读取 Run（路径为示意）；断开订阅不停止运行。同用户其它设备还需要用户级更新通知或重取活动 Run 的发现机制，不能假定它们已经知道新 `runId`。**OpenChat 建议**
4. **稳定 id 与序号**：run/message/part 标识稳定；Run 内事件以 `(run_id, seq)` 去重并顺序应用。发现缺口要补齐或回到快照，有限乱序缓冲不等于任意乱序自动恢复。**OpenChat 建议**
5. **snapshot + delta**：首次连接返回消息 + part 的 snapshot 与 `last_seq`；后续只发 `seq > last_seq` 的 delta。断线重连带 `last_seq`（或 cursor）续订；服务端保留有限窗口的 delta，超出则重发 snapshot。**OpenChat 建议**（比 OWUI 的 Redis 快照更明确，吸收其"快照 overlay"思想但补上序号）
6. **并发发送与服务端取消**：首版由服务端限制同一 conversation 的并发 Run，提供显式取消入口。新的 Run 终态应为 `cancelled`，不是成功的 `completed`；部分消息可以保留，不能把现有 message.status 的 `complete` 约定直接套给 Run。**OpenChat 建议**
7. **agent 只读工具**：run 执行体只拿受限的只读 Reader/工具（详见 Pi/Hermes 专篇），写路径永远不经过 agent；本文不重复其边界。**OpenChat 建议**
8. **不要照搬**：Socket.IO 房间 + Redis pub/sub + 多实例任务清理、`session_id` 反向寻址、`response.*` 与 `chat:completion` 双协议并存，都是 OWUI 历史演进与多进程部署的产物；OpenChat 模块化单体初期不需要。**OpenChat 建议**

## 测试清单（尚未执行，不声称已通过）

- 同一 conversation 开两个 tab：A 发送，B 是否收到同 run 的 delta/终态；B 停止是否停掉 A 看到的 run。
- 手机后台/锁屏后返回，或桌面切后台：SSE/轮询被节流时，重连是否按 `last_seq` 续上、是否重复渲染。
- 在 A 流式期间切到 conversation B，A 继续流：切回 A 时消息树与列表排序是否一致。
- 断线中段重连：故意断网数秒，验证补发/snapshot 回退与去重。
- Stop 并发：快速多次 Stop/Cancel，验证幂等、终态只落一次、队列不被卡死。
- event 乱序/重复：注入重复/乱序 delta，验证按 `seq` 归并、无重复文本。
- provider 错误与进程重启：provider 中途报错落 `failed`；重启后未完成 run 的恢复或标记策略。
- 以上均为建议用例，尚未执行。当前四类 SSE 帧不能直接表达新 Run/parts/恢复语义；需要修改 OpenAPI 源契约并同步生成两端类型。

## 固定 commit 源码导航

链接前缀：`https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/`

- 入口与后台任务/返回值：`backend/open_webui/main.py`（`chat_completion`；后台分支与 `task_ids` 返回）
- 事件发号：`backend/open_webui/socket/main.py`（`get_event_emitter`、`get_event_call`、`connect`/`user-join` 入房、`events:chat`）
- 任务生命周期与响应快照：`backend/open_webui/tasks.py`（`create_task`、`stop_task`、`save_response_stream`、`get_response_streams_by_chat_id`）
- 上游 SSE 解析与增量落库：`backend/open_webui/utils/middleware.py`（`process_chat_response`、`build_chat_response_context`、delta flush）
- 供应商请求：`backend/open_webui/routers/openai.py:generate_chat_completion`
- 快照 overlay：`backend/open_webui/routers/chats.py`（`overlay_response_streams`、`get_chat_by_id`）
- 取消/任务查询端点：`backend/open_webui/main.py`（`/api/tasks/stop/{task_id}`、`/api/tasks/chat/{chat_id:path}`、`/api/tasks/chat/{chat_id:path}/stop`）
- 前端聊天主控：`src/lib/components/chat/Chat.svelte`（`sendMessage`/`sendMessageSocket`/`chatEventHandler`/`stopResponse`/草稿/队列/重连）
- 前端落地页：`src/lib/components/chat/ChatPlaceholder.svelte`、`src/lib/components/chat/Placeholder.svelte`
- 前端消息与状态：`src/lib/components/chat/Messages.svelte`、`src/lib/components/chat/Messages/ResponseMessage.svelte`
- 前端输入/队列：`src/lib/components/chat/MessageInput.svelte`、`MessageInput/QueuedMessageItem.svelte`
- 前端 API：`src/lib/apis/openai/index.ts`（`generateOpenAIChatCompletion`）、`src/lib/apis/streaming/index.ts`（SSE 解析）
- 状态：`src/lib/stores/index.ts`、`src/lib/stores/chatList.ts`、`src/lib/components/layout/Sidebar.svelte`
- 配置默认值：`backend/open_webui/env.py`（`REDIS_RESPONSE_STREAM_TTL`、`WEBSOCKET_*`、`CHAT_RESPONSE_STREAM_DELTA_CHUNK_SIZE`）

常用入口：[聊天入口](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/backend/open_webui/main.py)、[事件广播](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/backend/open_webui/socket/main.py)、[任务与快照](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/backend/open_webui/tasks.py)、[响应处理](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/backend/open_webui/utils/middleware.py)、[前端主控](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/src/lib/components/chat/Chat.svelte)、[消息滚动](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/src/lib/components/chat/Messages.svelte)、[侧栏](https://github.com/open-webui/open-webui/blob/0a7c15832fb30b1903753e83f81dc7d27e5b0944/src/lib/components/layout/Sidebar.svelte)。

### 待验证项

- 多实例 + Redis 下 Socket.IO 的投递/去重保证、房间并集行为，静态阅读无法确认。**待验证**
- Redis 不可用（单进程内存 `response_streams`）时刷新恢复是否仍成立，以及进程重启后的恢复语义。**待验证**
- `response.*` 新协议与 `chat:completion` 旧协议在各供应商下的实际触发组合。**待验证**
- 许可证：本仓如要复用任何 OWUI 代码，须逐文件核对 `LICENSE_HISTORY` 与品牌条款；本文未做合规结论。**待验证**

## 相关文档

- [研究索引](README.md)
- [Pi AI 模块化模型层](pi-ai.md)
- [OpenCode 会话执行](opencode.md)

---

固定基线：`open-webui/open-webui` @ `0a7c15832fb30b1903753e83f81dc7d27e5b0944`（自定义 Open WebUI License，非 MIT；2026-09-04 提交，2026-09-13 研究）。本文为静态源码研究，未做运行验证；源码证据、官方描述、OpenChat 建议与待验证项已分别标注。
