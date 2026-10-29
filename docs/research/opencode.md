# OpenCode：会话运行、消息 Part 与取消持久化

> 研究日期：2026-09-13。仓库：`anomalyco/opencode`，MIT。
> 固定快照：`dev` 分支 commit `95daf90670b7c039c436c85537da5fbfe2205b41`。
> 旧地址 `sst/opencode` 已重定向到上述仓库。
> 方法：静态源码阅读，未运行 OpenCode；本文的 OpenChat 模块与测试均为拟议。

OpenChat 以 [Pi AI](pi-ai.md) 为模型层的主要参考。本篇补充一次回答如何运行、
工具状态如何表达、取消如何收尾，以及哪些事件何时持久化。

## 1. 结论速读

1. **上游事实**：单次 assistant 响应处理与整个运行循环是两层职责。
2. **上游事实**：工具失败本身不等于整个运行结束；外层还要判断停止原因、未完成调用和错误。
3. **上游事实**：取消会固化已有部分内容，并把未完成工具标为中断状态。
4. **上游事实**：文本 delta 广播与 part 快照持久化分离，不是每 token 都写数据库。
5. **OpenChat 建议**：借鉴生命周期和 part 模型，使用现有 Go/Postgres；所有权和只读策略自行实现。
6. **重要纠正**：`edit: deny` 不足以保证只读；供应商单轮结束也不等于整个 Run 完成。

## 2. 研究范围与证据等级

主要核对 `packages/opencode/src/session/` 的 processor、prompt、run-state、retry、
session、tools，permission 与工具定义，以及 `packages/core` 的事件表和 projector。
所有链接固定到同一 commit，见文末导航。

本文的 **上游事实** 来自已读源码；**OpenChat 建议** 是改写策略；**待验证** 表示
未通读的分支、部署层性质或尚未运行的行为。没有以官网概述替代代码证据。

## 3. 两层运行模型

主要调用链的概念图：

```text
SessionPrompt.loop(sessionID)
  → SessionRunState.ensureRunning
    → runLoop(sessionID)
      ├── 读取已保存的消息与当前 user/assistant
      ├── 判定是否仍有工作
      ├── 创建 assistant message
      ├── SessionTools.resolve
      └── SessionProcessor.process(streamInput)
            ├── LLM.stream / AI SDK
            ├── handleEvent：更新文本、思考、工具 part
            └── 返回 compact / stop / continue
```

### 3.1 Processor：一次模型响应

**上游事实**：[`SessionProcessor.process`][processor] 消费一条 LLM 事件流，维护
当前 assistant message 及其 parts，返回 `compact | stop | continue`。

它处理：

- `text-start/delta/end`。
- `reasoning-start/delta/end`。
- `tool-input-*`、`tool-call`、`tool-result`、`tool-error`。
- `step-start`、`step-finish` 与相应 token/cost 信息。

返回值主要取决于需要压缩、被阻断及 assistant error 等状态。
`step-finish` 的 provider reason 写入 assistant 的 `finish`，留给外层判断。

### 3.2 runLoop：整个运行

**上游事实**：[`runLoop`][prompt] 每轮取最新消息，检查 assistant 与当前 user 的
关系、`finish` 和未完成工具调用，决定继续创建下一条 assistant，还是结束。

典型区别：

| 观察到的结果 | 含义 |
| --- | --- |
| 一条 assistant 文本块结束 | 该块完成，其他块或工具仍可能继续 |
| `finish = tool-calls` | 模型本轮请求工具，不应当作用户任务完成 |
| Processor 返回 `continue` | 交给外层继续判定，不是无条件再请求模型 |
| 终止 finish 且无待回填工具 | 才可能结束整个 Run |

**OpenChat 建议**：明确 `ModelResponse`、`StepResult`、`RunStatus` 三种语义。
它们可以共享少量结束原因类型，但不能只用一个 `done` 帧表达全部生命周期。

## 4. 工具 Part 的状态机

**上游事实**：processor 的核心状态变化如下。

| 状态 | 触发 | 核心函数 |
| --- | --- | --- |
| `pending` | 建立调用占位、输入仍在形成 | `ensureToolCall` |
| `running` | 完整工具调用进入处理 | `tool-call` 分支 |
| `completed` | 工具返回结果 | `completeToolCall` |
| `error` | 工具异常或中断 | `failToolCall` / cleanup |

普通 `tool-error` 会更新 part，而不是直接提升为 provider 网络错误。
错误结果可以进入后续模型上下文，让模型选择恢复办法。

**上游事实**：部分人工权限/问题拒绝（例如 `RejectedError`）会设置 blocked，默认
使运行收束；`continue_loop_on_deny` 会影响该策略。规则匹配的 `DeniedError` 与
用户拒绝的 `RejectedError` 不是同一类型，不能把所有拒绝路径概括成相同处理。

**OpenChat 建议**：

- 普通只读工具失败返回有类型的失败结果，允许在步数预算内处理。
- 未授权工具、资源越权、必需参数校验失败必须阻止对应操作，不能靠整轮重试绕过。
- 每种错误是否允许模型再尝试，由 Runner 的显式策略决定。
- provider 执行的原生工具与 Go 本地工具区分来源，避免再次执行已由供应商完成的操作。

## 5. 取消与部分结果

**上游事实**：[`SessionRunState`][runstate] 管理运行中的 Runner，取消通过 Effect
中断运行任务。processor 的 interrupt/cleanup 路径会：

1. 为当前文本与 reasoning part 补结束时间并保存快照。
2. 将仍运行的工具改为中断错误，带 `interrupted` 元信息。
3. 给 assistant message 补完成信息。
4. 通过运行状态层最终回到 idle。

这些行为说明「停止生成」是一组状态收尾操作，不是只让前端停止画字。

**OpenChat 建议**：由服务端按 `runId` 取消上下文，将取消传入模型和工具调用，
保存部分结果，并明确收束所有进行中的 parts。部分文字可以继续展示；下一次模型
上下文是否包含它，交给 Context Builder 的策略。

多端场景下，用户主动 Stop 与某一条 SSE 连接断开是两种事件。
这一点要与 [OpenWebUI 同步研究](openwebui-experience-streaming.md) 结合。

## 6. 重试边界

**上游事实**：[`SessionRetry.policy`][retry] 参与 processor 的外层重试，处理可重试
错误与指数退避，读取 `Retry-After` / `retry-after-ms`。AI SDK 内部重试被设置为 0，
以免多层重试相乘。运行状态会显示 retry/attempt/next 等信息。

**OpenChat 建议**：统一由一层控制重试，明确 attempt 与已产生的输出。

- 尚未向用户发布内容、尚未执行工具时，可对允许的瞬时错误做有上限重试。
- 已输出文本、已经执行工具或已经发布终态后，不透明地重放整个 stream 会造成重复。
- 工具失败不应伪装成模型网络失败，否则会错误地重跑模型或工具。
- 只读调用也可能消耗额度、时间和外部请求次数，不能因此宣称重复执行无成本。

具体恢复策略必须有 fixture 验证，不能只根据 `isRetryable` 一个布尔值决定。

## 7. 消息 ID、Part 与持久化边界

### 7.1 ID

**上游事实**：[`id.ts`][ids] 采用带前缀的可排序 ID，组合时间戳与计数器；
message、part、event 等具有不同前缀。

**OpenChat 建议**：借鉴稳定的实体标识，不必更换现有 UUID 体系。
消息与 part 身份、Run 内事件顺序是不同概念，顺序使用明确的 `seq` 表达。

### 7.2 delta 与快照不是同一种写入

**上游事实**：[`Session.updatePartDelta`][session] 主要发布 delta 事件，不写 part 快照。
`updatePart` 发布完整 part 更新，常见于文本结束、工具状态变化和 step-finish。

[`SessionProjector`][projector] 消费消息/part 事件并 upsert 对应读模型。
[`event/sql.ts`][eventsql] 存在按 aggregate 的 sequence 与事件表结构。

**边界**：不能由此推导每个 token 已持久化、每个客户端可重放任意历史 delta，
或事件追加与投影天然是同一事务。这些部署与事务性质需要单独核验。

**OpenChat 建议**：借鉴增量展示与权威快照分离，但持久化策略服从我们的多端需求。
可以合并短时间内的 delta 再保存/广播；对外提供的恢复游标必须对应可恢复的记录。
超出事件保留窗口时走快照恢复。无需复制整套通用事件溯源框架。

## 8. 工具权限：哪些机制不能直接当作只读保证

**上游事实**：[`Tool.Def`][tool] 包含 id、参数与 execute。具体工具调用 `ctx.ask`：
读操作使用 `read` 权限，写入常使用 `edit`，Shell 的权限 key 则是 `bash`。
[`SessionTools.resolve`][tools] 负责将会话和 agent 权限接入工具上下文。

[`permission/index.ts`][permission] 的规则具有 allow/deny/ask 三态，匹配顺序和
用户批准结果共同决定执行。ask 可以等待外部 reply。

重要纠正：

- `edit: deny` 不会自动禁止 Bash、自定义工具、MCP 或子 agent 间接写入。
- `disabled()/visibleTools()` 有工具到权限 key 的映射，但本次没有确认它在所有
  生产工具注册路径都参与 schema 裁剪，不能声称 deny 必定隐藏全部相关工具。
- 即便 schema 已隐藏工具，执行侧仍需拒绝模型返回的未知或未授权调用。

**OpenChat 建议**：静态装配少量只读业务工具，执行时再次检查名称、完整参数和资源
归属。工具接收只读能力接口，不接收整个数据库连接池、文件系统或可写存储客户端。

## 9. 多用户边界属于 OpenChat

本研究没有验证 OpenCode 的 session/message/part 路径提供与 OpenChat 等价的
`user_id` 归属约束。事件表中的 owner/aggregate 字段也不能直接解释为用户 ACL。

**OpenChat 建议**：继续由 Go 从会话 Cookie 得到可信用户身份，归属写进查询；
Run、消息、part、事件订阅都必须关联到授权会话。模型参数不能指定其自身的权限身份。

## 10. Go 改写：借鉴职责，不耦合协议与工具执行

以下仅是拟议流程，不是已发布 API：

```text
Chat Service：创建运行、装配用户上下文、控制持久化与订阅
  Runner：有上限的循环
    Model Client：产生一轮模型事件与最终 ModelResponse
    Processor：聚合文本/thinking/tool-call parts
    Tool Registry：完整校验后执行授权的只读调用
    Context Builder：将工具成功或失败结果组织为下一轮上下文
  Runner：根据停止原因、预算、取消状态收束
```

供应商适配器负责协议，不直接执行我们的本地工具，也不直接写数据库。
在 OpenCode 中同一事件流里出现 tool result，不代表 Go 的 Provider 层也必须承担执行职责。

## 11. 分期建议与不直接迁移的部分

- 首先明确单轮结果与 Run 结果、parts 状态、取消和错误，再接最小只读工具循环。
- 将 Effect/Context/Layer 的框架机制改为 Go 小接口与显式构造。
- 文件快照、patch、LSP、Git、Shell、子 agent 调度等不属于当前只读 Chatbot 目标。
- 长期记忆、复杂压缩与多供应商自动 fallback 可以分期评估；插件系统不在 OpenChat 的实现目标中，扩展统一走仓库内模块与显式装配。
- 数据库事件/part 快照与多端恢复一起设计；不把 delta-only 广播当作可靠同步方案。

## 12. 确定性验证清单（拟议）

1. 一轮 `tool-call` 输出后 Runner 正确执行工具并进入下一轮。
2. 普通工具失败进入模型上下文；权限失败阻止操作，不能重试绕过。
3. provider finish 为 toolUse 与 Run completed 明确分开。
4. 文本中途取消保留 partial，running tools 进入中断终态。
5. 多个 parts 的事件交错不会串内容；停止后不再追加新的有效 delta。
6. 只有首输出前允许透明重试，重复调用受到预算和状态约束。
7. 每个 conversation 的并发运行规则由服务端保证。
8. 持久化/广播顺序、重复事件和恢复游标要有专门测试，不能仅检查 UI 字符串。

这些是将来应建立的测试，本文没有执行它们。

## 13. 固定 commit 源码导航

| 主题 | 链接 | 主要入口 |
| --- | --- | --- |
| 单响应处理 | [processor.ts][processor] | create、process、handleEvent、cleanup |
| 外层运行 | [prompt.ts][prompt] | runLoop、loop |
| 运行取消 | [run-state.ts][runstate] | ensureRunning、cancel |
| 重试 | [retry.ts][retry] | policy、delay |
| 消息与 part 更新 | [session.ts][session] | updateMessage、updatePart、updatePartDelta |
| ID | [id.ts][ids] | ascending |
| 权限 | [permission/index.ts][permission] | evaluate、ask、reply、disabled |
| 工具装配 | [session/tools.ts][tools] | resolve |
| 工具接口 | [tool/tool.ts][tool] | Tool.Def |
| 投影 | [projector.ts][projector] | PartUpdated / MessageUpdated handlers |
| 事件表 | [event/sql.ts][eventsql] | event、event_sequence |

**待验证**：native runtime、v2 工具注册路径、事件与投影的事务边界、部署层 ACL、
传输层是否批量合并 delta，以及动态扩展对权限和重试的影响。

相关：[研究索引](README.md) · [Pi AI](pi-ai.md) ·
[OpenWebUI 同步研究](openwebui-experience-streaming.md) · [当前设计](../design/README.md)。

[processor]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/session/processor.ts
[prompt]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/session/prompt.ts
[runstate]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/session/run-state.ts
[retry]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/session/retry.ts
[session]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/session/session.ts
[ids]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/id/id.ts
[permission]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/permission/index.ts
[tools]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/session/tools.ts
[tool]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/opencode/src/tool/tool.ts
[projector]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/core/src/session/projector.ts
[eventsql]: https://github.com/anomalyco/opencode/blob/95daf90670b7c039c436c85537da5fbfe2205b41/packages/core/src/event/sql.ts
