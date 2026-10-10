# DeepSeek Harness (`dsh`) 研究：官方 agent harness 的模块与协议边界

> 状态：**静态源码研究，未安装、未运行、未调用其工具**。研究日期：2026-09-13。
> 固定版本：`c291e7961a515f6d7af9304e7fd1d257929aef26`（`master`，2026-09-10T14:17:09Z，
> 合并 PR #3977 "release-0.1.5-sync-master"）。许可证 **MIT**（根 `LICENSE`；根 package
> 名 `@deepseek-ai/dsh-root`，版本 `0.1.5-rc.2`，Node `^22.19.0 || >=24.0.0`）。
>
> 阅读约定：**上游事实**＝在固定 commit 已直接核对的源码；**官方 doc 描述**＝仓库内文档自述；
> **OpenChat 建议**＝尚未实现的拟议；**待验证**＝静态阅读不能确认的行为。链接均固定到
> 上述 commit。本文**不声称 OpenChat 已实现**其中任何能力。
>
> **OpenChat 不做插件系统。** 本篇保留上游 plugin/Cordis 术语以准确记录来源，
> 只采纳其中的小接口、单向依赖、可替换实现和显式装配思想；后续扩展统一为仓库内模块。
> 除第 11 节列出的直接读取源码，生命周期/重连/并发等论述主要依据同版本官方文档和包 README，
> 不代表已经逐路径核实运行时保证。

## 0. 研究对象澄清（重要）

本仓库是 **DeepSeek 官方** 的 `deepseek-ai/deepseek-harness`，命令行名 `dsh`，口号
"Everything is a Plugin"，构建在 [Cordis](https://github.com/cordiverse/cordis) 微内核插件
框架之上。它**不是** NousResearch 的 `hermes-agent`（Python、MIT、"The agent that grows
with you" 的独立项目）；两者组织、语言、架构都不同，引用时不能混用结论。

OpenChat 已研究的 Pi AI（`earendil-works/pi`）在这里不是外部竞品，而是 dsh 的**被依赖项**：
dsh 的多供应商适配器 `@deepseek-ai/dsh-llm-pi-ai` 就包在 pi-ai 之上。这佐证了 OpenChat
「Pi AI 作模型层主参考」的选型，也说明 dsh 在 Pi 之外还叠加了一整套自研运行时。

## 1. 研究基线（来源在各项注明）

| 项 | 值 |
| --- | --- |
| 根版本 / 许可证 | `0.1.5-rc.2` / MIT；Node `^22.19.0 \|\| >=24.0.0`，pnpm `11.7.0` |
| Session 格式 | 固定快照中常量 `SESSION_FORMAT_VERSION = 3`；与其它发布标签的兼容性仍需核验 |
| 开发状态（官方 doc 描述） | README 明示 **developer preview**，"THERE WILL BE COMPATIBILITY-BREAKING CHANGES" |

仓库有多套发布面：Web、headless 一次性运行、SDK JSON-RPC、ACP、Electron 桌面、Python SDK。
`SAFETY.md` 明确警告：未做安全审计，可执行模型生成的代码、加载第三方插件、访问网络/进程/
凭据/文件，沙箱与审批**不保证隔离**。

## 2. 模块地图与依赖方向

dsh 将模型适配器、工具注册表、会话日志、agent loop 等能力组织为插件，
由启动时的 **profile → bundles → patch 层**组合成 Cordis 插件树
（`docs/architecture.md`，官方 doc 描述）。

核心包（官方 doc 描述，来自 `docs/architecture.md` 与各包 README；包路径已核对）：

| 包 | 职责 | `ctx` key |
| --- | --- | --- |
| `packages/core/session` | 只追加 `SessionEvent` 日志 + 内存 store，**唯一事实源** | `ctx.sessions` |
| `packages/core/system-prompt` | prompt section 与工具 schema 装配 | `ctx.systemPrompt` |
| `packages/core/tools` | 有作用域的工具注册表与受保护执行管线 | `ctx.tools` |
| `packages/core/agent` | `Agent` 接口、live registry、`agent/*` 事件 | `ctx.agents` |
| `packages/core/agent-loop` | 默认驱动器，实现上述接口 | `ctx.agentLoop` |
| `packages/llm/llm` | Message/ContentBlock/StreamChunk 词汇 + 适配器缝 | `ctx.llm` |
| `packages/client/connection` | 浏览器↔Host 的 RPC/流/reconnect 载体 | Client |

依赖方向（可整理为概念调用图）：

```text
dsh CLI → app-boot：profile + bundles + patch 组合成插件树
  └─ dsh-base：模型适配器、工具、持久化、sandbox/approval、settings、credentials
       ├─ ctx.agents (AgentHandle: create/resume) ── agent-loop driver
       │   turn/start → claim inbox → assemble prompt+tools → agent/pre-step
       │   step/start → agent/request → prepareCall → 绑定 adapter registration
       │   deriveMessages(log) 冻结请求 → llm/stream → adapter.stream() → StreamChunk*
       │   agent/assistant-stream start/chunk/end（live）→ assistant/message|attempt（持久）
       │   tool/call → pre-execute → guards → execute → post-execute → tool/result
       │   step/end → 下一 step 或 turn-stopping → turn/end
       ├─ ctx.sessionPersistence（SessionHandle: create/open/read/append/flush/close）
       └─ packages/api/*-controller → Client connection → 浏览器 Session/Conversation/UI
```

官方文档要求扩展面向 `packages/core/agent` 接口而不是默认 `agent-loop`，使 loop 可替换；
本次未穷举所有包的 import 以证明没有例外。一个 adapter call 对应一次 provider 尝试，库级重试禁用；
工具只通过受限 seam（`ctx.fs`/`ctx.shell`/`ctx.subprocess`）触达系统；适配器与 runner 可在
无数据库、无 HTTP server 下测试。

## 3. 一次模型响应 vs 整个 run 的边界

dsh 用 **step**（一次模型请求 + 它请求的工具执行）与 **turn**（零或多个 step，在首个输入
被 claim 前打开、在不再欠工作时关闭）两个尺度（`docs/agent-lifecycle.md`）。

持久事件：`turn/start`、`turn/end`、`step/start`、`step/end`、`system/message`、
`user/message`、`assistant/message`、`assistant/attempt`、`tool/call`、`tool/result`、
`request/header`。**不持久**的是 `agent/assistant-stream` 的 start/chunk/end 帧与大部分
`agent/*` 控制事件（inbox/status/request/validation）。

一次响应是原始 `StreamChunk`（token 级 delta）流，由共享 `BlockAssembler` 折叠回
`ContentBlock`、usage、finish reason 与 replay state。`assistant/message` **内嵌产生它的
精确紧凑 stream**；`assistant/attempt` 保留失败/重试/取消/流错误尝试但不产生模型可见历史；
**进程在结算前硬崩溃不会留下持久 attempt stream**。边界要点：**模型可见即已记录**（进入
请求者须能从日志重建并有 invariant 校验）；失败尝试不产生 surface message、不提交工具
副作用；`max-tokens` 截断会**丢弃全部工具调用**（截断调用不可安全执行），replay envelope
对应 per-block 条目同步裁掉。

## 4. 工具调用与结果回灌

工具注册表执行一条明确管线（`docs/subsystems/tools.md`，源码
`packages/core/tools/src/index.ts`）：

```text
tools/pre-execute (可重排 allow/deny/ask waterfall) → registered monotonic guards
  → tools/execute (around-dispatch，可替换 signal) → tools/post-execute (检查/替换结果)
  → definition.finalizeContent (同步最后一公里裁剪，必须 total、不得 throw) → tools/result
```

`ToolDefinition` = 模型可见的 `ToolSchema`（name/description/parameters）+ 强制 `output`
规范声明（raw JSON Schema + `render`/`presentationMeta`）+ `execute` + host-only 元数据
（`timeoutMs`、`isConcurrencySafe`、`presentCall/presentResult`）。注册表 `schemas()` 用
**显式白名单**构造对模型可见的 schema，上述执行/展示元数据**永不泄漏**到请求。

回灌链路：`tool/call` 记录**原始未解析** `arguments` 字符串 → 结果经管线规范化与
schema 校验 → `tool/result` 记录 `ToolResultMessage`、可选 `error{name,code}`、可选工具私有
`meta`（JSON 可序列化，用于回放同一张 UI 卡片）。模型参数先做**完整解析与校验**才执行；
未知工具不执行。并行仅对**显式** `isConcurrencySafe() === true` 的调用开放，其余作为排他
屏障；结果顺序保持模型序（`maxParallelToolCalls` 默认 10，1 为全串行）。

接口输入输出（**拟议示意，非 dsh 的 API**）：

```text
LlmAdapter.Stream(ctx, GenerateOptions) → AsyncIterable<StreamChunk>
  → BlockAssembler → Message{content, source} + TokenUsage + FinishReason
ToolRuntime.Execute(ToolExecutionInput{callId, name, arguments, agent?, signal})
  → ToolExecutionResult{content: ContentBlock[], isError?, meta?}
SessionHandle.Append(events[]) / Flush() ; Session.DeriveMessages() → Message[]
```

## 5. 终止、取消、重试、预算

- **取消**：`Agent.cancel` 协作式 abort；被取消流把**已交付用户的文本/推理前缀**结算为
  `assistant/message {interrupted:true}`，未派发工具调用不出现；首因优先，完成/取消竞争不改终态。
- **重试**：`dsh-llm-retry` 在 `agent/request-error` waterfall 执行，原则 **durable before
  wait, open-step boundaries**：先写 log-only `llm/retry`（retry id/policy/failure/delay），退避
  后写 `llm/retry-started`，再在**同一未关闭 step** 重跑；`ctx.llm.stream()` 直调者保持单次
  尝试。默认 normal 模式 5 次，仅重试 `EMPTY_RESPONSE`/`RATE_LIMIT`/`SERVER`/`TIMEOUT`/`TRANSPORT`。
- **空响应**：终止 `stop` 但零内容块 → `EMPTY_RESPONSE`，默认可重试。
- **超时**：远程适配器有 `streamIdleTimeoutMs`（默认 5 分钟）；工具级 `timeoutMs` 由
  `guard/timeout-policy` wrapper 实施，声明超时即断言工具转发 `exec.signal`。
- **预算**：`dsh-agent-loop` **没有内建 turn 预算**（官方明写），工具/steering 会一直延续
  turn，只能从 `agent/turn-stopping` 等扩展点 cancel；真正起约束的只有 `maxParallelToolCalls`、
  工具/流超时、token meter/compaction。**这是缺口，不是可抄的设计。**

## 6. DeepSeek 特有协议与 thinking/tool 流的规范化

**通用抽象**（`packages/llm/llm`，与供应商无关）：`Message`、merge-extensible `ContentBlockMap`
（`text`/`reasoning`/`image`/`file`/`tool-call`/`tool-result`）、`StreamChunk`、`BlockAssembler`、
`FinishReasonMap`、`TokenUsage`（cache 字段 disjoint）、`LlmFailure`、`ResolvedRetryPolicy`、`LlmAdapter`。

**只服务 DeepSeek 的**（`llm-deepseek`、`deepseek-llm-api-extensions`、`session-log-deepseek`、
`plugin-package-inventory-deepseek`；pi-ai 路径**不**实现）：

- **HTTP 头**：每请求 `user-agent`；授权 chat 带 `x-deepseek-harness-user-id`（匿名 UUID）；
  带 sessionId 带 `x-deepseek-harness-session-id`；compaction 带 `x-deepseek-harness-compact:1`。
- **body 扩展**（`dsh_` 前缀，独立版本，位于 messages/system/tools **之外**，不加模型 token）：
  默认开的 `dsh_plugin_packages`（active 插件清单）与默认关的 `dsh_session_log`（**把 canonical
  Session 日志连续后缀上传到 endpoint**，用 `afterSeq`/`throughSeq` + `delivery-accepted` 水位做
  at-least-once）。
- **wire 字段**：顶层 `thinking{type}`、`reasoning_effort: low|high|max`；assistant 的
  `reasoning_content` 在 thinking 模式 tool-call turn 上**必需回传**。
- **规范化**（`translate.ts`）：`reasoning_content`→`reasoning` 块（**首个空串不开块**）；
  `content`→`text`；`tool_calls`→`tool-call` 块（`id`/`name` 是身份，continuation 重发空值
  时保留既有值）。usage/finish 全推迟到 `[DONE]`（usage 在 finish 前、finish 后无 chunk）。
  DeepSeek `prompt_tokens` **含** cache，`mapUsage` 减回 disjoint。图片走 Files API，失败整
  请求回退 inline base64（不混用 file id 与 inline）。
- **选项限制**：不映射 `tool_choice`；pi-ai 适配器拒绝 `GenerateOptions.stop`。

对 OpenChat：这些都是**供应商 wire 细节**，应像 `LlmFailure` 一样规约成中性 code 交给上层
（如 `CONTEXT_WINDOW_EXCEEDED`、`EMPTY_RESPONSE`），上层按 code 路由而**不读 provider 文案**。
但 `dsh_session_log` 是**把会话内容外发 provider endpoint** 的远程副作用，与 OpenChat 只读边界
和隐私取向冲突，**不建议采纳**。

## 7. 权限、工具可见性、运行时执行检查

dsh 用**三个互相独立的旋钮**（`docs/subsystems/permission-presets.md`）：

- **工具可见性**：`ToolRestriction`（`allow`/`deny`）作用于 scope 继承的工具（全局 + 祖先），
  多个求交，scope 自身注册豁免。schema 可见**不等于**执行权限。
- **执行**：registry 完整解析参数后再检查工具名/参数；`tools/pre-execute` 的 allow/deny/ask
  waterfall 可重排；`ctx.approval` 的 `ApprovalOutcome` 闭合且 fail-closed——只有
  `allowed-once` 放行，`rejected`/`cancelled`/`unavailable` 一律拒绝，答案缺失/不拥有/抛错/
  不合规也归 `unavailable`；每 session `ask`（默认）或 `never`（确定性 reject）。
- **沙箱**：`ctx.sandbox` 按 session 解析文件效果模式，默认 `read-only`，显式 opt-in
  `workspace-write`；切换是 log-only `sandbox/mode` 事件、重放恢复；文件名由「模式 + 文件
  系统 seam」执行，不由模型参数决定。

**但不适配 OpenChat 的部分**：`dsh-base` 默认装的是**完整 coding 工具集**——`read`/`write`/
`edit` 文件编辑、bash/PowerShell shell、subprocess/terminal/PTY、web search、公开 HTTP(S)
fetch、subagents、todo/goal，以及 LSP、MCP、code-runtime、e2b 远程沙箱等可选 seam。权限预设
的默认表甚至包含 `danger-full-access` + `never`。所以 **OpenChat 绝不能把 dsh 的默认工具集
当作自身能力清单**；可借鉴的是三层授权结构与「schema 可见 ≠ 可执行」「fail-closed」「工具
只拿受限 Reader 接口」，而不是它的 shell/write/远程执行。

## 8. 流式事件、UI 消费、恢复/同步/持久化的真实边界

- **持久化**：`SessionPersistence` seam 抽象 `create/open/stat/list/export`，返回 per-session
  `SessionHandle`（`read/append/flush/close`）单写者。`append` 只保证「已接受、有序、本实例
  可见」，**只有 `flush` 承诺跨崩溃持久**。默认 JSONL backend，生成代不可改名/替换/删除，
  相邻迁移各负责一步 `vN→vN+1`。**崩溃不截断**：日志可停在打开的 `turn/start`，由 agent 层
  在 resume 时补写 `interruptedTurnClosers`（缺失工具错误、开放 `step/end`、合成
  `turn/end{reason:{kind:'interrupted'}}`），且只在持有写所有权时写入。
- **传输**：Host 用 Typert `@Remote` 声明**一元**方法；会话事件流/分页/增量/projection
  **不属于** Remote 描述符，必须走独立数据协议。Client `follow()` 首帧含 header、tail page、
  cursor 与完整 projection baseline，之后按 `seq` 连续 `replace/prepend/append/
  settle-assistant`，缺口用 tail page 修复；控制流每代以完整基线开场、重连直接替换。
- **UI 消费**：客户端只保留逻辑事件窗口 `SessionEventLikeEntry`（durable event 或 Client-only
  `assistant/live-chunk`），折叠为 Chat/Trajectory 快照。**live chunk 瞬态且仅 update**，
  durable 结算内嵌完整紧凑 stream，故重连与翻页可重放同一 Assistant 状态，无需 durable token 行。
- **重连**：物理 WS 与逻辑流恢复分离；carrier 失败可重试，业务错误/坏首帧/协议违规终态。
  普通通知不重放，有状态域必须自带 baseline/cursor/显式查询；**不存在**通用 `resync()`。

必须澄清：这**不是**多租户隔离。Web 端 loopback-only、单浏览器会话、启动 token → 签名
cookie，身份是 Harness home 的匿名 UUID，无登录/多用户账号体系。也**不要神化 durable
replay**：live 帧瞬态，进程在结算前崩溃则无 attempt stream，控制基线是进程内状态、Host
重启后无法重建 jobs。

## 9. 对 OpenChat 可采纳的机制（OpenChat 建议）

1. **权威记录与派生历史分开**：参考 `deriveMessages(log)` 的可重建思路，保证 Run/Part/事件与快照一致。
   OpenChat 可以维护消息读模型，不要求所有业务状态采用完整事件溯源；关键是避免两套可独立修改的事实源。
2. **区分 live、canonical 结算与持久提交**：上游把 live stream 与 assistant message/attempt 结算分开。
   结合第 8 节，append 接受记录不等于 flush 的跨崩溃承诺；不能只凭 committed end 的名字断言磁盘已持久。
   OpenChat 的成功终态应在必要数据库提交之后发布。
3. **明确 adapter 终态与 attempt**：参考 usage/finish 的有序规则和统一失败类型，一次模型调用对应一次 attempt。
   可保留工具原始 JSON 用于诊断，但执行前必须严格解析与校验。记录重试计划后再退避；
   已公开输出或已执行工具后不做透明整轮重放，不能认为处于 step 边界就天然安全。
4. **固定工具执行管线**：完整参数校验、授权、执行、结果验证各有接口；权限检查是不可跳过的业务步骤。
   OpenChat 不采用可由插件重排的授权 waterfall，也不建设插件宿主；工具只收受限 Reader，越权阻止操作。
5. **多端订阅**：GET 流首帧给 snapshot + cursor/lastSeq，按 seq 应用、重复忽略、缺口补
   snapshot/tail page；控制/发现状态每代完整基线替换，瞬时通知不重放——即「不重发 prompt 修断线」。

**明确不建议采纳**：`dsh_session_log` 外发、`danger-full-access` 预设、coding 工具集
（shell/write/terminal/code-runtime/MCP/LSP/subprocess/e2b）、动态插件 HMR 与 Typert 代码
生成、Electron/Python SDK 多发布面。

## 10. Go 重写边界、测试方法、过度复杂点、与 Pi AI 的分工

**最值得按边界重写的模块**（Go，模块化单体，沿用现有 Postgres/OpenAPI/React）：

- `internal/llm`：Message/ContentBlock/StreamChunk/FinishReason/TokenUsage/LlmFailure +
  小接口 + 聚合器；参考语义，用 Go 自己定义类型并做性质测试，不整体复制 TypeScript 实现。
- `internal/providers/deepseek`：serialize/translate/sse 三件套，fixture 驱动；把
  `reasoning_content` 开块规则、tool-call 身份合并、usage 推迟到 `[DONE]`、cache 减法写成
  表驱动测试。
- 运行、存储、工具与上下文分别按小接口组织。
  Inbox、复杂重试与历史投影按实际需求分期，首版只装配内置实现，不增加第三方插件机制。
  Step/tool/time/usage 预算由我们的 Runner 显式执行。
- **测试手段**：离线 SSE 分帧 fixture；fake adapter（Echo）+ 脚本化 provider；fake
  persistence；崩溃恢复测试（构造以 `turn/start` 结尾的日志，断言补写 closers）；并行工具池
  的屏障/顺序测试；request 可从日志重建的 invariant 测试。这些正是 dsh 的可迁移资产。

**过度复杂 / 不适合**：Cordis 微内核 + 插件 HMR + profile/bundle/patch 活更新；Typert 的
两套 `ts.Program`（Host/Client）代码生成；declaration-merging 的 `SessionEventMap`/
`ContentBlockMap` 与大量 branded 类型（Go 应改为显式接口 + 生成代码）；deep-frozen request
+ 全量 dev invariant 的审计面；约 1.03 万文件的 monorepo 与多发布面。

**与 Pi AI 的分工建议**：Pi AI 继续负责**协议词汇表与供应商注册**（OpenChat 已选的主参考）；
dsh 提供**运行生命周期与持久化边界**的样板（turn/step、durable settlement、step 边界重试、
工具管线、事件溯源日志、重连语义）；OpenChat 自身负责 Go 服务端的认证/归属、Postgres
持久化、单用户作用域只读工具与 SSE 传输。dsh 的 `llm-pi-ai` 恰好证明「中性 seam + 供应商
专属 adapter 并存」可行，但不要把 dsh 的运行时当成 Pi 的替代。

## 11. 局限与待验证

- 本报告为**静态阅读**：依据仓库官方文档 + 少量直读源码（`llm-deepseek/src/types.ts`、
  `translate.ts`、`core/session/src/types.ts`），未运行。
- `SESSION_FORMAT_VERSION = 3` 已直读确认；checkout 与「已发布格式」是否一致，**待验证**。
- `dsh_session_log` 默认关闭见 wire 文档，但 shipped web profile 是否在某 patch 中开启，
  **待 CLI `--dump-config` 实测**；重连/流协议来自 README，未抓真实 WS 帧，时序**待运行验证**。
- `always` 模式下游策略若不响应取消且不 settle，会阻塞回退/静止/卸载（dsh 自认的活性风险）；
  OpenChat 采用有上限重试和运行预算。**本次未验证等价的多租户/账号隔离**：所研究的 dsh 路径是单
  Harness-home、loopback 的本地工具，不可据此推断多用户部署能力。

## 12. 参考与源码导航（固定 commit）

- 仓库主页：<https://github.com/deepseek-ai/deepseek-harness/tree/c291e7961a515f6d7af9304e7fd1d257929aef26>
- 文档：`docs/architecture.md`、`docs/agent-lifecycle.md`、`docs/module-graph.md`、
  `docs/subsystems/{llm-streaming,tools,session,persistence}.md`、`docs/deepseek-llm-api-wire-extensions.md`
- 源码：`packages/core/{session/src/types,tools/src/index,agent-loop/src/tool-calls}.ts`、
  `packages/llm/llm/src/{types,assembler}.ts`、`packages/llm/llm-deepseek/src/translate.ts`、
  `packages/llm/llm-retry/src/index.ts`、`packages/session/session-persistence/src/index.ts`、
  `packages/interaction/user-approval/src/index.ts`、`packages/sandbox/sandbox-policy/src/index.ts`

常用入口：[架构](https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/docs/architecture.md)、[生命周期](https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/docs/agent-lifecycle.md)、[LLM 类型](https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/llm/llm/src/types.ts)、[块聚合器](https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/llm/llm/src/assembler.ts)、[工具注册表](https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/core/tools/src/index.ts)、[DeepSeek 响应转换](https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/llm/llm-deepseek/src/translate.ts)。

相关研究：

- [研究索引](README.md)
- [Pi AI](pi-ai.md)
- [Hermes Agent](hermes-agent.md)
- [OpenCode 会话运行](opencode.md)
- [Cherry Studio 模型兼容](cherry-studio.md)
- [OpenWebUI 体验与流式同步](openwebui-experience-streaming.md)
