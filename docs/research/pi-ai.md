# Pi AI：模块化模型层研究

> 研究日期：2026-09-13。主要参考：`earendil-works/pi` 的 `packages/ai`。
> 固定版本：`v0.85.1`，commit `d981de1229ef899957bbe968bc8dcda02a21f477`，MIT。
> 方法：静态源码与官方 README 对照，未运行真实模型请求。
> 定位：OpenChat 自研内核的主要设计参考。下文的 Go 模块和接口均为建议，尚未实现。

## 1. 结论速读

- Pi AI 负责统一模型通信；agent 循环属于另一个包 `packages/agent`。
- 值得参考的是类型、协议实现、供应商工厂、模型集合和凭据接口之间的边界。
- 供应商身份、通信协议、模型能力是三个维度；同一协议可以服务多个供应商。
- 一条 assistant 消息可以含交错的文本、思考和工具调用，不能压成一个字符串。
- 流式增量和最终消息是两个出口：前者服务实时展示，后者确认内容、用量和结束原因。
- 帧编码器处理可变 `partial` 的聚合问题，**不提供跨投递去重或数据库 exactly-once**。
- OpenChat 采用这些设计思想，用 Go 显式装配；产品存储和只读工具权限由自身服务层负责。

## 2. 基线、范围与证据

**上游事实**：本版本包名是 `@earendil-works/pi-ai`。旧仓库地址
`badlogic/pi-mono` 已指向 `earendil-works/pi`，旧教程中的包名、导出 API 不能混用。

已核对的核心范围见文末固定 commit 导航：

- 统一类型、核心导出、`models.ts` 的 `createModels` / `createProvider`。
- OpenAI Chat Completions 协议适配、DeepSeek 工厂、历史转换。
- 事件队列、增量 JSON、消息帧 encoder/reducer、凭据解析接口。
- 官方 README 的事件语义、测试 provider 和迁移说明。

本文用 **上游事实**、**官方文档**、**OpenChat 建议**、**待验证** 区分证据等级。
供应商线上行为、吞吐和取消延迟都没有经过本次实测。

## 3. 模块地图：依赖方向比目录数量重要

下面是主要依赖的概念图，箭头表示「使用」，不是完整 import 图：

```text
应用装配
  ├── 核心入口 index → types / models / auth / 通用工具
  └── providers/all → providers/<id>
                         ├── models.createProvider
                         ├── model catalog 数据
                         ├── auth 策略
                         └── api/*.lazy → api/* 协议实现 → types / stream utilities

compat → legacy 全局注册表（兼容入口，独立于新的按需装配方式）
```

### 3.1 各模块的职责

| 模块 | 输入与输出 | 边界与注意点 |
| --- | --- | --- |
| [`types.ts`][types] | Message、Context、Model、Tool、Usage、流事件 | 内部共享契约；不需要业务数据库或 HTTP handler |
| [`api/*`][openai] | 模型、上下文、选项 → 统一事件流和最终结果 | 负责 wire 转换及供应商 SDK 调用；不是 agent 工具执行器 |
| [`api/lazy.ts`][lazy] | 延迟加载函数 → ProviderStreams | 把重 SDK 的加载推迟到第一次调用 |
| [`providers/<id>`][deepseek] | 身份、端点、认证、模型数据、协议实现 → Provider | 组合具体供应商，不承担会话业务 |
| [`models.ts`][models] | Provider 集合、模型选择、调用参数 → 选定实现 | 编排模型查询、认证解析和按 `model.api` 分发 |
| [`auth/*`][auth] | CredentialStore、供应商认证策略 → 请求凭据 | 隔离 API key/OAuth 解析；不能等同于 OpenChat 用户认证 |
| [`event-stream.ts`][stream] | 生产者事件 → 异步迭代与最终 result | 通用事件队列，不是持久化队列 |
| [`assistant-message-frame.ts`][frames] | 原始模型事件 → 紧凑帧 → 重建消息 | 解析与聚合工具，不负责可靠投递 |

**上游事实**：[`index.ts`][index] 明确以 side-effect-free 核心入口为目标，
不自动导入供应商工厂和生成目录。`compat.ts` 则保留旧的全局 API registry。

**重要区分**：`Model` 和能力配置可以是可序列化数据；`Provider` 的组合对象中
包含 `api`、`auth` 等行为接口，不能把整个运行时工厂当作可序列化纯数据。

### 3.2 新增供应商为什么不必改循环

**上游事实**：`createProvider` 接收 `id/baseUrl/auth/models/api`。
`api` 可为单个 `ProviderStreams`，也可为按 `model.api` 选择的实现表。
已有协议的新供应商通常只增加工厂、模型数据及汇总登记。

新增 wire 协议则需要新的协议实现及相关类型/导出登记；不能把「加供应商」与
「实现全新协议」的成本混为一谈。

**OpenChat 建议**：组合根放在 `cmd/openchat/main.go`。Registry 接收构造好的实例，
避免包级 `init()` 隐式注册、用户凭据全局单例和运行时扫描任意插件目录。
**不做插件系统**：这里的注册是应用内部装配；新增能力以仓库内模块和测试一起发布，
不提供第三方插件安装或热加载。

## 4. 三轴模型：身份、协议、能力

| 轴 | Pi 的载体 | DeepSeek 示例 |
| --- | --- | --- |
| 供应商身份 | `Provider.id` / `Model.provider` | `deepseek` |
| 通信协议 | `Model.api` / ProviderStreams | 此版本工厂使用 `openai-completions` |
| 模型能力与兼容性 | catalog、`model.compat`、协议内兼容解析 | reasoning、工具支持、DeepSeek 参数与历史回显规则 |

**上游事实**：[`deepseek.ts`][deepseek] 把身份、端点、认证和 OpenAI Completions
实现组合起来。协议中的 `detectCompat()` 仍会识别供应商 id / URL，设置
`thinkingFormat: "deepseek"` 等差异。这说明 Pi 也不是完全没有供应商特判。

**OpenChat 建议**：把协议选择和兼容配置显式化。通用 Runner 不识别 DeepSeek、
Anthropic 等名字；适配器读取兼容配置。URL 启发式只作为有边界的默认解析手段，
不能任意改写用户配置的代理端点。

## 5. 消息内容块与流生命周期

### 5.1 统一类型

**上游事实**：主要消息角色为 user、assistant、toolResult；共享类型包括文本、
thinking、图片和 ToolCall。assistant 还携带 provider/api/model、responseId、
usage、stopReason、原始停止原因等信息。详情见 [`types.ts`][types]。

**OpenChat 建议**：未来业务消息采用结构化 parts；给 UI 的投影与送给模型的上下文
分别构造。原始供应商状态不能只藏在 Markdown 文本里，也不能未经筛选全部发给浏览器。

### 5.2 成功、失败和最终结果

**官方文档**的正常顺序是：

```text
start
  text_start / text_delta / text_end
  thinking_start / thinking_delta / thinking_end
  toolcall_start / toolcall_delta / toolcall_end
done(final assistant message)
```

上面各块事件可能交错，必须按 `contentIndex` 聚合。
生成开始后的错误走 `start → updates → error`；请求建立失败也可能直接只有 `error`。
不能要求每个错误前面都有一个 `start`，或假设每个网络 chunk 对应一个内容块。

**上游事实**：`EventStream<T, R>` 同时提供事件迭代与 `result()`。
`AssistantMessageEventStream` 在终态给出最终 assistant message；错误结果也可能
包含已生成部分。这个结果不等于 Go 数据库已经提交成功。

### 5.3 `partial` 不是历史快照

**官方文档与源码**：`partial` 是共享的、可变的截至目前消息。事件入队以后，生产者
还可能继续更新它。直接把这个对象保存到历史事件列表，会让旧事件看起来包含未来内容。

**OpenChat 建议**：网络事件采用明确的不可变 delta 与标识；需要快照时在确定边界生成。
避免把 Go 可变消息指针跨 goroutine 直接当作历史事件，也避免每 token 深拷贝整篇回复。

## 6. 工具参数：展示解析与执行校验分开

**上游事实**：OpenAI Completions 的工具参数可能按 index 分片，id/name 也可能后到。
适配器先组装块，再用 [`parseStreamingJson`][json] 做 best-effort 解析。
该函数尝试完整解析、修复部分转义问题，再降级到 partial JSON，最终可返回空对象。

**官方文档**：`toolcall_end` 表示参数传输完成，仍需 schema 校验。

**OpenChat 建议**：

1. 增量解析只用于显示「正在准备哪个工具、参数到哪里了」。
2. 参数结束后严格解析、校验 schema、检查白名单和资源归属，再执行。
3. 不把 best-effort 的 `{}` 当成合法默认参数，不边收 JSON 边执行。
4. 工具参数和结果都有明确的大小上限，防止无限累积。

## 7. 原生 metadata 与上下文迁移

**上游事实**：类型和适配代码中存在以下信息：

- Anthropic thinking signature 与 redacted thinking 标记。
- Google ToolCall 的 `thoughtSignature`。
- OpenAI Responses 等协议的 response id、工具 namespace；部分网关的 reasoning details。
- 原始 stop reason、实际响应模型、缓存 token 和 reasoning 用量。

签名、redacted 内容和加密 reasoning 不是同一种东西，不能用一个普通文本字段混装后
假定可以跨供应商重放。

**上游事实**：[`transformMessages`][transform] 根据是否同模型，处理 thinking 降级、
签名剥离、工具 id 归一及缺失 tool result；该路径会过滤 error/aborted assistant。

**待验证**：官方 README 对中止后继续、跨模型 thinking 的概述与具体转换路径有差异，
必须结合调用该转换函数的入口确认，不能单凭一句说明推断全部恢复行为。

**OpenChat 建议**：

- 原始会话记录保留取消的部分回复；模型上下文可按明确策略选择是否纳入。
- 转换生成派生上下文，不修改已保存的原始记录。
- 缺失工具结果表达为未完成、取消或失败，不能伪造成功结果。
- 同源恢复保留必要私有元数据，跨协议迁移按适配器能力处理。

## 8. 错误、取消、用量

**上游事实**：OpenAI Completions 适配器会处理 `finish_reason` 缺失的兼容情况。
只有兼容配置明确允许时才推断 stop/toolUse；否则流结束却没有有效停止原因属于错误。
取消通过 AbortSignal 传播，终态区分 `aborted` 与 `error`。

用量字段可能位于最后一个 chunk，并存在多种缓存统计字段。
Pi 在相关路径中将 input 视为扣除缓存读写的输入量，再单列缓存 token。

**OpenChat 建议**：自己的契约要明确 input 是否含缓存、reasoning 是否为 output 子集，
不能盲目相加造成重复计费。保留供应商原始 usage，缺失时标未知而不是伪装成零成本。
重试边界还需结合 [OpenCode](opencode.md) 和 [Cherry Studio](cherry-studio.md) 分析。

## 9. 帧编码器的责任：聚合不等于可靠投递

**上游事实**：[`AssistantMessageFrameEncoder`][frames] 使用已覆盖字符计数等状态，
处理「消费 start 时 partial 已被后续 delta 推进」的问题。工具参数也有 checkpoint。
它会拒绝某些非法顺序，例如重复 start、类型不符、结束后继续 delta。

它没有稳定 frame id、跨连接 sequence number 或数据库去重键。
`reduceAssistantMessageFrames()` 是顺序聚合；重复输入同一个文本 delta 会重复追加。

**OpenChat 建议**：由宿主设计 `runId + seq`、唯一约束、订阅游标、快照和去重。
Pi 的 encoder 只能作为流解析与重建的参考，不能当成断线恢复或 exactly-once 的保证。
多端与重连的总体策略见 [OpenWebUI 体验与流式同步](openwebui-experience-streaming.md)。
另可对照 [DeepSeek Harness](deepseek-harness.md) 对 live stream、canonical 结算和持久提交的区分。

## 10. 对 Go 模块化单体的映射

以下是职责示意。

```text
Chat Service → Context Builder → 统一 LLM Request
             → Runner → ModelClient 接口 → 协议适配器
                      → 只读 ToolRegistry
             → Run Store / 客户端事件推送

统一 Message / Part / Event / Usage 类型是内部基础契约。
协议适配器不导入业务数据库或 HTTP handler。
```

主参考边界：小接口、显式构造、按协议复用、能力配置与行为分开。
Go 无需照搬 TypeScript 的动态 import、legacy 全局 registry 或全量模型代码生成工具链。

## 11. 采纳顺序与离线验证

**建议优先**：统一类型、一个 OpenAI-compatible 适配器、结构化流事件、正确终态与取消、
完整工具参数校验、确定性 fake provider。模型数量扩张、OAuth、动态目录、复杂跨协议迁移
可分期处理；可靠同步的必要契约从首版设计，具体范围见总方案。

拟议测试清单（尚未在 OpenChat 实现）：

| 类别 | 样例与断言 |
| --- | --- |
| 网络分帧 | 一条 SSE 跨多次 read、多事件合并、UTF-8 边界、CRLF、提前 EOF |
| 内容块 | 文本/thinking/tool 参数交错，按 part 标识重建而非全局拼接 |
| 工具参数 | id 后到、多调用交错、截断 JSON、非法转义；未完成参数绝不执行 |
| 终态 | stop、length、toolUse、error、aborted；终态唯一，完成结果与聚合一致 |
| 用量 | 末块 usage、缺失 usage、不同缓存字段、reasoning 不重复统计 |
| 上下文 | 同源签名保留、跨协议转换、取消回复展示与上下文纳入策略 |
| 重建 | encoder/reducer 正常往返；重复 delta 的上层去重另测 |
| 集成 | 无数据库和真实模型密钥的脚本化 provider，多轮只读工具闭环 |

Pi 的 [`faux` provider][faux] 和测试目录可用于参考测试组织。
本研究未证明其调度、估算 token 和真实供应商完全等价。

## 12. 固定版本源码导航与维护

- [统一类型][types]、[核心入口][index]、[Models / Provider 工厂][models]。
- [DeepSeek 工厂][deepseek]、[供应商汇总][all]、[lazy API][lazy]。
- [OpenAI Completions][openai]、[Anthropic Messages][anthropic]、[历史转换][transform]。
- [事件队列][stream]、[增量 JSON][json]、[消息帧][frames]、[凭据解析][auth]。
- [Faux provider][faux]、[兼容入口][compat]、[官方 README][readme]、[测试目录][tests]。

待继续核验：生成模型数据的完整 compat 表、Anthropic/Google 签名重放的线上约束、
不同版本 API 迁移及 README/源码差异。更新版本时先复跑离线 fixture，再补必要的实网测试。

相关：[研究索引](README.md) ·
[Hermes](hermes-agent.md) · [OpenCode](opencode.md) · [Cherry Studio](cherry-studio.md) ·
[当前后端](../backend.md)。

[types]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/types.ts
[index]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/index.ts
[models]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/models.ts
[deepseek]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/providers/deepseek.ts
[all]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/providers/all.ts
[lazy]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/api/lazy.ts
[openai]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/api/openai-completions.ts
[anthropic]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/api/anthropic-messages.ts
[transform]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/api/transform-messages.ts
[stream]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/utils/event-stream.ts
[json]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/utils/json-parse.ts
[frames]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/utils/assistant-message-frame.ts
[auth]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/auth/resolve.ts
[faux]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/providers/faux.ts
[compat]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/src/compat.ts
[readme]: https://github.com/earendil-works/pi/blob/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/README.md
[tests]: https://github.com/earendil-works/pi/tree/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai/test
