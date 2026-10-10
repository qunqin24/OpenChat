# Cherry Studio：模型兼容、内容转换与附件处理

> 研究日期：2026-09-13。仓库：`CherryHQ/cherry-studio`，AGPL-3.0。
> 固定 commit：`3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6`。
> 方法：源码与同版本官方 reference docs 对照，未运行 Cherry Studio 或真实模型请求。
> 定位：补充兼容案例；OpenChat 内核的主参考仍是 [Pi AI](pi-ai.md)。

本文讨论设计，不引入 Cherry 的实现。实际复用源码时，应另外核对对应文件及依赖许可。
所有 OpenChat 模块、接口、测试和阶段安排均是建议，不代表已实现能力。

## 1. 结论速读

- 供应商身份、端点协议、适配器家族分开，有利于表达自定义代理和同一供应商的多个协议。
- Feature 贡献链能组织请求级能力，但其失败降级机制不能直接用于权限与参数校验。
- 内容转换保留原始结构，避免把 reasoning、工具和附件拍成纯文本后无法重放。
- reasoning 需要能力与 wire 参数映射，单个统一开关不足以覆盖所有供应商。
- 附件应在请求构建时明确选择原生输入或文本抽取，并表达失败与截断。
- 流的聚合、终态、通知和重试是独立职责，桌面 IPC 实现不等于服务端多端同步方案。

## 2. 基线与证据范围

相关目录：

| 路径 | 关注点 |
| --- | --- |
| `packages/aiCore` | Provider 扩展、插件引擎、内部内容转换 |
| `packages/provider-registry` | 模型能力和 reasoning profiles |
| `packages/ai-sdk-provider` | 供应商请求转换 |
| `src/main/ai` | Feature、附件路由、运行时和流管理 |

**直接核对的源码范围**：Provider 初始化/扩展机制、两个内容 adapter、中间件与
PluginEngine、Feature 定义与收集、能力/reasoning profiles、附件路由与原生支持判定、
流管理的结构与 pipe loop、compact replay、retry wrapper 和错误序列化。

**官方文档为主要依据的部分**：provider 三轴解析的完整决策表、模型目录覆盖合并、
AiStreamManager 完整 attach/终态派发流程。其部分函数或类型已读取，但没有逐行核验
全部调用路径。各节据此标为 **上游事实**、**官方文档**、**OpenChat 建议** 或 **待验证**。

## 3. Provider 三轴解析

**官方文档与已读初始化代码**：[`provider-resolution.md`][resolution] 将以下维度分开。

| 维度 | 作用 | 示例 |
| --- | --- | --- |
| `provider.id` | 供应商配置身份 | 内置供应商或自定义 relay |
| `endpointType` | 通信端点协议 | Chat Completions、Responses、Anthropic Messages |
| `adapterFamily` | 实际适配器家族 / 方言 | openai-compatible、anthropic |

文档描述 `resolveAiSdkProviderId` / `resolveProviderVariant` 根据端点配置和已注册变体
选择实现；配置构建使用有序 match/build 表，未匹配时进入通用兼容路径。
具体函数见 [endpoint.ts][endpoint]、[config.ts][config]。

**OpenChat 建议**：

- 用显式配置分离 ProviderID、WireAPI 与 compat，而不是让模型名字符串承担所有含义。
- 路由/能力解析做纯函数，协议适配器负责实际网络 I/O，均不直接写业务数据库。
- 用户的自定义 base URL 与 headers 必须按配置解释；默认方言推断不能随意改写代理。
- 首版复用少数协议实现，不必预建全部 vendor variant。

拟议配置形状，仅用于说明职责：

```text
ProviderConfig
  id
  credentialRef
  endpoints[]
    wireAPI
    adapterFamily
    baseURL
  modelCapabilities / compatibilityOverrides
```

## 4. Feature 与中间件贡献链

**上游事实**：[`RequestFeature`][feature] 通过 applies、model adapters、hooks 和停止条件
声明请求级能力。[`collectFromFeatures`][collect] 会捕获某个 Feature 方法的异常，
跳过对应贡献。随后 [`PluginEngine`][plugins] 在配置上下文、解析模型、转换参数等阶段
执行插件，并组合模型中间件。

这使 reasoning 提取、缓存标记等能力可以拆出主请求函数，但也增加了执行顺序与降级策略。

**OpenChat 建议**：先采用少量显式 middleware/hook 插槽，分清两类能力：

这里的 middleware/hook 只指应用内部的代码组合点，执行顺序由入口显式装配。
OpenChat 不建设插件系统，也不提供用户脚本、第三方插件安装或热加载 API。

| 类别 | 失败策略 |
| --- | --- |
| 身份、参数校验、资源归属、工具白名单、必需内容转换 | 阻止相关操作并返回明确错误 |
| 明确可选的观察/装饰性能力 | 可受控降级，记录原因，并保持授权和结果语义 |

不能照搬「所有 Feature 失败后继续」或在 Go 中统一 recover 后丢弃贡献。
审计或计量若被定义为必需能力，也不能归入可选观察项。

## 5. 内容 IR 与原生信息保留

**上游事实**：[`adapter.ts`][adapter] 与 [`modelMessageAdapter.ts`][modeladapter]
在 AI SDK 消息与内部表示之间转换。

主要手法：

- 原始 user/assistant/tool content 放入透传字段。
- 保留原文本，判断内部表示是否真的被修改。
- 未修改时尽量原样回写，避免丢失文件、reasoning、工具或其他结构信息。
- 被修改或由摘要合成的消息，再从内部结构重建。
- 在适配边界检查历史中工具调用与结果的配对关系。

**OpenChat 建议**：把权威消息与模型请求上下文分开。上下文裁剪或协议转换产生派生值，
不能因为转换需要而覆盖用户在 UI 中看到的原始历史。

### 5.1 原生工具与本地工具必须区分

**上游事实**：某些 provider 原生工具的调用和结果会一起出现在 assistant 内容中。
转换逻辑用已内联回答的调用 id 排除重复的 open tool-call，避免再次补结果。

**OpenChat 建议**：

- 原生工具由供应商执行；我们投影结果，不能再作为本地工具执行一次。
- 原生检索仍可能有供应商费用，需要按实际 usage/费用语义计量。
- 供应商支持某工具，不等于该请求自动获得使用权限；请求中仍只声明授权能力。
- 本地工具由 Go 的只读工具执行器运行。缺失结果表示未完成、中止或失败，不能伪造成功。
- 原生代码执行、写入型工具同样不属于当前只读 Chatbot 的工具集合。

## 6. reasoning 与能力映射

**上游事实**：[`capabilities.ts`][capabilities] 根据模型、供应商和请求设置解析实际能力。
[`reasoningProfiles.ts`][profiles] 将 off/auto/effort 等界面选择映射到不同 wire 参数，
涉及 token budget、thinking 类型、reasoning effort 等不同表达。

[`reasoningExtraction.ts`][extraction] 为部分兼容端点提供内联思考标签提取；
原生 reasoning 通道则使用原生事件。请求转换还处理 `max_tokens` 与
`max_completion_tokens` 等参数差异。

**OpenChat 建议**：

- 参数映射以模型能力和所选协议为依据，避免一个全局 reasoning 布尔值。
- `<think>` 等标签提取只针对明确的兼容配置；不能无条件扫描并移走普通正文或代码中的标签。
- 给前端统一的 reasoning parts，保留供应商恢复所需的私有数据。
- capabilities 决定请求能否使用能力，工具权限仍是独立校验。

## 7. 附件：原生输入或文本抽取

**源码与官方文档**：[`attachmentRouting.ts`][attachments]、[`nativeFileSupport.ts`][nativefiles]
以及 [附件说明][attachmentdoc] 描述按单个文件和目标模型判断路由。

| 情况 | 上游处理思想 |
| --- | --- |
| 模型及 converter 能处理原生类型 | 保留 file/image 等原生内容并物化输入 |
| 文本或可抽取文档 | 抽取、内联，并对过长文本截断 |
| 图片不能原生输入 | 尝试 OCR 等既有路径；无法得到内容时明确失败 |
| 截断后仍需阅读全文 | 借助分页读取能力，而不是假定模型已看到全部文件 |

**重要边界**：具体失败降级随文件类型和处理分支变化，不能泛化为所有失败都静默降级。
本次没有完整验证 OCR 解析库、缓存与部署依赖。

**OpenChat 建议**：

1. 先根据当前用户校验附件归属，解析对象存储内容。
2. 结合模型和协议能力决定原生输入还是文本抽取。
3. 把处理失败、截断和分页句柄明确表达给用户和上下文。
4. 工具用受控的 fileId/读取句柄，避免暴露任意服务器路径。

这能补上当前 OpenChat「附件已绑定消息，但未进入模型请求」的缺口。

## 8. 流生命周期、聚合和终态

**上游事实**：流管理代码和类型以 topic 标识活动流，存在 pending、streaming、done、
error、aborted 等状态，以及独立的取消控制器。

[`pipeStreamLoop.ts`][pipe] 将 UIMessageChunk 流分别用于广播和最终消息累积，
收集流内 error 与广播路径异常；终态可带已累积的部分消息。

**官方文档及已读类型/片段**：[`stream-manager.md`][streamdoc] 描述 attach、多播、
缓存重放以及 persistence → notification → cleanup 的终态阶段。
本次未逐行核验 AiStreamManager 的全部方法和事务保证。

**OpenChat 建议**：明确部分内容、持久化完成、客户端已收到、Run 已结束四个事实。
它们不是同一个时刻。最终消息保存失败也不能向客户端报告一个虚假的成功终态。

## 9. 重试和错误序列化

**上游事实与官方说明**：[`createRetryableWrap.ts`][retry] 组织 API key 切换、同模型
退避和跨模型 fallback；流式重放受到首个内容 chunk 的边界限制。
[`serializeError.ts`][errors] 提取供应商 SDK 的 statusCode、retryability 等信息。

**OpenChat 建议**：

- 首个可见输出前可有限重试；之后保留 partial 并明确失败，避免重复追加。
- 不自动切到用户没有授权的供应商、模型或凭据。
- 内部错误分类可保留诊断信息；发往浏览器时应使用可本地化、脱敏的产品错误。
- 不直接透传完整 provider responseBody、请求 headers 或凭据细节。

## 10. 桌面实现与多端服务的差异

Electron 的 main/renderer IPC、SharedCache 镜像、本地文件物化不能直接搬成 Go 服务。
但**多播、重连和快照恢复的需求仍然存在**，不能因为没有 Electron 就省略。

OpenChat 应使用自己的 Run/消息标识、事件序号、权限检查与订阅接口，
具体协议见 [OpenWebUI 体验与同步研究](openwebui-experience-streaming.md)。

本地 Shell、写文件、Coding Agent 子进程、任意插件安装与交互式审批系统不属于当前
只读 Chatbot 的基础能力。正常消息保存与用户主动上传仍由 Go 产品后端负责。

## 11. 分期与拟议测试

优先借鉴：配置与协议解耦、能力映射、结构化内容、错误与重试边界、受控附件输入。
完整模型目录覆盖合并、大量供应商变体、跨模型自动 fallback、OCR 可按阶段增加。

拟议离线 fixture：

| 类别 | 应验证的情况 |
| --- | --- |
| Provider 解析 | 供应商身份与协议不同、用户代理、已知/未知变体、显式覆盖 |
| Feature | 可选能力失败可观测；权限/校验失败必须阻止操作 |
| 内容转换 | 原始结构往返、修改后重建、签名/文件保留、缺失工具结果 |
| 工具来源 | 原生结果不重复执行；未授权原生能力不进入请求 |
| reasoning | off/auto/effort、内联标签跨 chunk、正文/代码中的标签不误判 |
| 附件 | 原生支持、抽取成功/失败、无 vision、超限截断、跨用户拒绝 |
| Stream | 首 chunk 前重试、中段失败、取消 partial、终态唯一 |
| 同步 | 多订阅、快照/增量一致、重复事件去重、断线恢复 |

这些测试是建议清单，不是已经在 OpenChat 通过的测试结果。

## 12. 固定版本导航与待验证项

- [Provider 三轴说明][resolution]、[endpoint][endpoint]、[config][config]、[初始化][initialization]。
- [Feature 定义][feature]、[贡献收集][collect]、[插件引擎][plugins]。
- [内容 adapter][adapter]、[ModelMessage adapter][modeladapter]。
- [能力解析][capabilities]、[reasoning profiles][profiles]、[reasoning 提取][extraction]。
- [附件路由][attachments]、[原生文件支持][nativefiles]、[附件文档][attachmentdoc]。
- [AiStreamManager][manager]、[pipe loop][pipe]、[流管理文档][streamdoc]、[重试][retry]、[错误][errors]。

待验证：流 attach/终态派发完整调用路径、持久化与通知的事务边界、模型目录用户覆盖语义、
实际 OCR 依赖和线上供应商差异。本文不以静态阅读替代这些验证。

相关：[研究索引](README.md) · [Pi AI](pi-ai.md) · [OpenCode](opencode.md)。

[resolution]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/docs/references/ai/provider-resolution.md
[endpoint]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/provider/endpoint.ts
[config]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/provider/config.ts
[initialization]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/packages/aiCore/src/core/providers/core/initialization.ts
[feature]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/runtime/aiSdk/params/feature.ts
[collect]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/runtime/aiSdk/params/collectFromFeatures.ts
[plugins]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/packages/aiCore/src/core/runtime/pluginEngine.ts
[adapter]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/packages/aiCore/src/core/context/adapter.ts
[modeladapter]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/packages/aiCore/src/core/context/modelMessageAdapter.ts
[capabilities]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/runtime/aiSdk/params/capabilities.ts
[profiles]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/packages/provider-registry/src/reasoningProfiles.ts
[extraction]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/runtime/aiSdk/params/features/reasoningExtraction.ts
[attachments]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/messages/attachmentRouting.ts
[nativefiles]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/runtime/aiSdk/params/nativeFileSupport.ts
[attachmentdoc]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/docs/references/ai/chat-attachments.md
[manager]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/streamManager/AiStreamManager.ts
[pipe]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/streamManager/pipeStreamLoop.ts
[streamdoc]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/docs/references/ai/stream-manager.md
[retry]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/runtime/aiSdk/retry/createRetryableWrap.ts
[errors]: https://github.com/CherryHQ/cherry-studio/blob/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6/src/main/ai/utils/serializeError.ts
