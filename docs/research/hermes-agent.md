# Hermes Agent 模块边界研究

> 研究对象：[NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent)（官方描述 "The agent that grows with you"），由 NousResearch 开发，内置对 DeepSeek 的 provider profile 支持。
> **它不是 DeepSeek 官方框架。** 在 `deepseek-ai` 组织下未找到官方 Hermes 仓库；搜索到的 "Hermes + DeepSeek" 几乎都是第三方集成或桌面汉化，不应据此认为 Hermes 属于 DeepSeek。
> 固定版本：commit `b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a`，研究日期 2026-09-13，License：**MIT**（仓库根 `LICENSE`）。
> 定位：OpenChat 自研 Go 只读 Chatbot 内核。**Pi AI 是主要设计参考**，本文只补工具组织、Transport 协议边界、上下文与辅助调用边界，**不整体嵌入 Hermes**。
> 标记约定：**上游事实** = 在固定版本源码中直接核对；**官方文档描述** = 仓库文档的说明；**OpenChat 建议** = 拟采用、尚未实现；**推测** = 依据不足的推断；**待验证** = 静态阅读无法确认、留待首版实测的决策。
> 本文只做源码静态阅读，**未运行 Hermes**，也未在 OpenChat 中做任何构建、集成或验证。

## 结论速读

- 本次最值得借鉴的三个边界是 `ProviderProfile`（供应商描述）、`ProviderTransport`（协议转换）、`ToolRegistry`（工具注册）；这是对下文源码事实的研究判断。
- 顶层 agent 循环**没有**与 provider 解耦干净：`run_agent.py` 仍散落 `_is_azure_openai_url`、`_is_github_copilot_url`、`_is_ollama_glm_backend`、`_model_requires_responses_api` 等特判。**上游事实**
- 大量目录来自平台适配、CLI 子命令和工具，不能单独证明模块边界清晰。长文件适合作为审查入口；真正需要关注的是跨层供应商特判、隐式转发和共享状态，而非仅凭行数评价质量。
- 对 OpenChat 最有价值的是：声明式 provider profile + 独立 Transport 接口 + 带可用性门控的具名 toolset；其余（SQLite SessionDB、动态插件、写 memory/自建 skills/terminal）不适合照搬。
- **关键纠正**：把工具从模型 schema 隐藏 ≠ 权限隔离。Hermes 的 `check_fn` 只决定工具是否出现在请求里，真正执行仍走 dispatch 授权/护栏路径。OpenChat 必须在 dispatch 层再做白名单 + 参数 + 资源归属校验。**OpenChat 建议**

## 证据基线

| 项 | 值 |
| --- | --- |
| 仓库 | `NousResearch/hermes-agent` |
| commit | `b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a` |
| 日期 | 2026-09-13 |
| License | MIT |
| 实现语言 | Python 为主（单一仓库，另含 TS/JS 前端、gateway、TUI） |
| 与 DeepSeek 关系 | 支持，非官方；`plugins/model-providers/deepseek/` 只是普通供应商插件 |
| 研究方法 | `git clone --depth 1` 固定 commit 后静态阅读；**未执行**任何模块 |

官方仓库描述取自 README 与 `AGENTS.md`。凡"官方文档描述"会单独标注，其余为源码阅读结论。本文不声称做过运行时验证。

## 模块地图与依赖方向

**上游事实**（基于目录与 import 关系，非运行时验证）：

```
调用方 (cli.py / gateway / tui_gateway)
   │
   ├── run_agent.py: AIAgent ──► agent/conversation_loop.py: run_conversation
   │        │                       └── turn_* / tool_executor / context_compressor
   │        ├── agent/transports/*  (协议转换，按 api_mode 解析)
   │        ├── providers/*         (ProviderProfile 注册表)
   │        └── tools/registry.py   (工具注册表) ←─ tools/*.py 自注册
   │
   ├── model_tools.py: get_tool_definitions (toolset + registry → 模型 schema)
   └── hermes_cli/plugins.py: PluginContext (插件注册 tool/provider/hook/middleware)
```

依赖大体单向：`tools/*` → `tools/registry.py`；`model_tools.py` → registry + toolsets；`agent/transports` → `providers`（只读 profile）；上层 `run_agent`/`cli` 负责装配。`tools/registry.py` 的注释自称 "imports nothing from model_tools or tool files"，有意做成依赖叶子以避免循环。**OpenChat 建议**：保留"注册表是依赖叶子"这条规则。

## ProviderProfile 与 DeepSeek profile 注册流程

**上游事实**

- `providers/base.py` 定义 `@dataclass ProviderProfile`，字段含 `name / api_mode / aliases / env_vars / base_url / auth_type / fallback_models / default_aux_model` 等；可覆写钩子有 `prepare_messages`、`build_extra_body`、`build_api_kwargs_extras`、`create_client`、`fetch_models`、`resolve_aux_model`、`supported_reasoning_efforts`。
- `base.py` 的 docstring 将 client 构造、凭证轮换和流式生命周期留给 AIAgent；但同一文件仍提供 `create_client()` 扩展钩子，可返回特殊客户端。因此 profile 是组合描述与行为钩子，不是完全可序列化的纯配置。
- `providers/README.md`（**官方文档描述**）说明 profile 本体放在 `plugins/model-providers/<name>/`，由 `providers/__init__.py._discover_providers()` 惰性发现；用户目录 `$HERMES_HOME/plugins/model-providers/` 覆盖内置，last-writer-wins。
- `plugins/model-providers/deepseek/__init__.py`：定义 `DeepSeekProfile(ProviderProfile)`，唯一重写 `build_api_kwargs_extras`，处理 V4 "thinking 默认开、后续回合必须回显 `reasoning_content`，否则 HTTP 400"的陷阱，并把 effort 映射到 `reasoning_effort`；随后 `register_provider(deepseek)`。同目录 `plugin.yaml` 声明 `kind: model-provider`。
- 注册后自动被下游读取（**官方文档列举，未逐条运行验证**）：`hermes_cli/auth.py`（环境变量）、`hermes_cli/models.py`（模型目录）、`hermes_cli/doctor.py`（健康检查）、`agent/model_metadata.py`（hostname 反查）、`agent/auxiliary_client.py`（aux 模型）、`agent/transports/chat_completions.py`（每次请求调用 `prepare_messages` / `build_extra_body`）。

**OpenChat 建议**：采用结构化供应商描述与入口显式注册。DeepSeek 的实现细节（thinking 回显）放在 profile/适配层，不进入 agent 循环；不采用上游的目录发现和插件加载。**推测**：`plugin.yaml` 的 `kind` 对内置 profile 更多是元数据，实际注册由 `__init__.py` 的 `register_provider` 完成。

## ProviderTransport 职责边界

**上游事实**

- `agent/transports/base.py` 的 `ProviderTransport(ABC)` 有四个抽象方法：`convert_messages`、`convert_tools`、`build_kwargs`、`normalize_response`；辅助钩子 `validate_response`、`extract_cache_stats`、`map_finish_reason`。
- 文件 docstring 原话：transport 拥有"一个 api_mode 的数据通路"，但**不拥有** "client construction, streaming, credentials, caching, interrupts or retries — those stay on AIAgent"。
- `agent/transports/types.py` 定义统一的 `NormalizedResponse` / `ToolCall` / `Usage`。协议私有状态（Codex 的 `call_id`、Gemini 的 `thought_signature`）放进 `provider_data`，共享类型不被污染——这是很干净的一层。
- `agent/transports/__init__.py:get_transport(api_mode)` 按字符串解析并惰性发现；`chat_completions.py` 末尾 `register_transport("chat_completions", ...)` 自注册。

仍留在 `AIAgent`（`run_agent.py`）的工作：**上游事实**

- client 构造与 credential pool、流式与中断、重试与 fallback、超时/陈旧检测、prompt caching，以及大量 provider 名字特判（`_is_openrouter_url`、`_is_copilot_url`、`_is_ollama_glm_backend` 等）。
- 过渡痕迹：`get_transport` 找不到时返回 `None`，调用方"回落到 legacy flag 路径"（`transports/__init__.py` 注释）。

**OpenChat 建议**：借鉴纯转换/归一化的边界。Hermes 的 Transport 定义较窄，不能直接当成我们完整的 ModelClient。OpenChat 的 provider adapter 可以负责网络与流解析，内部再拆纯转换函数；业务存储、运行循环和重试预算留给上层。

## ToolRegistry / toolset / availability 与调用流程

**上游事实**

- `tools/registry.py`：`ToolEntry` 记录 `name / toolset / schema / handler / check_fn / requires_env / is_async / max_result_size_chars / dynamic_schema_overrides`。`register(...)` 在工具文件 import 时调用；跨 toolset 同名会被拒绝，插件覆盖需 `override=True` 且操作者 `allow_tool_override` 显式授权。
- `discover_builtin_tools()` 扫描 `tools/*.py`，用 AST 判断模块体是否调用 `registry.register(...)`，结果按 `(mtime, size)` 缓存到磁盘（约 100 个文件）。
- `toolsets.py`：声明式 `TOOLSETS` 字典，条目含 `tools` / `includes` / `posture`。`hermes-webhook` 只给 4 个只读工具（`_HERMES_WEBHOOK_SAFE_TOOLS`），`_HERMES_CORE_TOOLS` 被各平台 bundle 共享；`resolve_toolset()` 展开 `includes`。
- `model_tools.py:get_tool_definitions(enabled_toolsets, disabled_toolsets)`：先对 enabled 取并集，再对 disabled **做减法**，最后按 `check_fn` 过滤，返回模型可见的 schema 列表（带 LRU 缓存）。
- 调用流程：模型返回 tool_calls → `agent/tool_executor.py`（并发/串行两路）→ 授权闸门（`_ConcurrentToolAuthorizationGate`）→ `_pre_tool_block` / `_dispatch_authorized_once` → 执行 handler → 结果回写 messages。

**OpenChat 建议**：把只读工具组织为显式具名 toolset（如 `readonly`），写工具"根本不在集合里"；`Available(ctx)` 表达凭证/后端是否就绪。静态注册优于动态发现——工具少时不需要 AST 扫描 + 磁盘缓存那套。

## Context engine / 压缩 / 辅助模型路由

**上游事实**

- `agent/context_engine.py:ContextEngine(ABC)` 定义压缩接口：`should_compress`、`compress`、`prune_tool_results_only`、`select_context`、`on_turn_complete`、`get_tool_schemas` 等。内置实现是 `agent/context_compressor.py`；插件只能注册**一个** context engine（`hermes_cli/plugins.py:register_context_engine`）。
- `agent/conversation_compression.py` 的 `CompressionCommitFence`（第 366 行）是压缩提交栅栏：压缩在独立 daemon 线程对消息做私有深拷贝，只有被 admit 的 commit 才把结果发布回主对话，宿主超时后仍在跑的工作被丢弃。文件头部写明了线程安全契约。
- `agent/auxiliary_client.py` 是"副任务路由"：压缩、vision、搜索等走独立 client 与 fallback 链，`get_text_auxiliary_client(task, main_runtime)` 负责解析；`default_aux_model` / `resolve_aux_model` 让便宜模型跟随供应商。`aux_probe_mode()` 让可用性探测不构造真实 SDK。
- 规模警示：`auxiliary_client.py` 7607 行、`context_compressor.py` 4949 行、`conversation_compression.py` 4043 行。

**OpenChat 建议**：学"辅助 LLM 调用独立于主聊天路径"的边界——把标题生成、长会话摘要收敛到一个 `AuxClient` 接口，与流式主路径隔离，不阻塞回复。不要照搬其多级 fallback 矩阵和供应商特判密度；首版可以只有"主模型 + 可选一个便宜摘要模型"。

## 真实耦合与大文件问题

不能用目录多当模块化论据。**上游事实**：

- `cli.py` 4650 行、`run_agent.py` 1556 行，且 `run_agent.py` 用 `_forward()` 把 27 处方法转发到 `agent/*` 辅助模块，形成隐式接口。
- 固定快照中 `agent/` 顶层 219 个文件、`tools/` 顶层 259 个文件，另有子目录；其它目录也包含大量平台、命令与工具实现。文件数量本身不是可组合边界的证据。
- `SessionDB`（`hermes_state.py:377`）由 12 个 mixin 拼成，`hermes_state_*.py` 合计 15924 行，覆盖 WAL、FTS5、readpool、repair、portability、migration。这是 SQLite 单机多进程存储的产物，与 OpenChat 的 Postgres + 后端持久化方向冲突。
- provider 特判泄漏进 agent 循环；`get_transport` 的 legacy 回落说明迁移尚未完成。**推测**：这些是长期演进中的历史包袱，不代表推荐架构。

## Go 改写映射（OpenChat 建议）

模块化单体、静态显式注册、依赖单向。下表是概念对应：

| Hermes 概念 | OpenChat Go 对应 | 说明 |
| --- | --- | --- |
| `ProviderProfile` | `internal/provider.Profile` 结构 + `Register` | 数据 + 少量钩子，不含 client |
| `ProviderTransport` | `internal/provider.Transport` 接口 | 只做转换/归一化；OpenAI 兼容可一个实现 |
| `ToolRegistry` / `ToolEntry` | `internal/tool.Registry` + `Tool` 接口 | 显式注册；`Available(ctx) bool` |
| toolset（`TOOLSETS`） | 静态 `readonly` 工具集声明 | 写工具不在集合 |
| `AIAgent` 循环 | `internal/agent`（不 import 具体 provider） | 只依赖接口 |
| `auxiliary_client` | `internal/aux`（首版可极简） | 摘要/标题副任务隔离 |
| `SessionDB` | 现有 Postgres store/service | agent 只产出事件，service 落库 |

组合根放 `cmd/openchat/main.go`，手动装配具体实现并注入接口，与仓库现有"手挂路由、保护级别一处可见"的风格一致。**待验证**：首版是否需要独立 Transport 抽象，还是 `Profile` + 一个 OpenAI 兼容 client 就够。

## 只读权限边界（重点纠正）

**OpenChat 建议**：

1. **从模型 schema 隐藏工具 ≠ 权限隔离。** Hermes 的 `check_fn` 只在 `get_tool_definitions` 阶段决定工具是否出现在请求里；真正的屏障在 dispatch——执行路径含授权闸门与 `_pre_tool_block`（`agent/tool_executor.py`），`agent/tool_guardrails.py` 还用 `IDEMPOTENT_TOOL_NAMES` / `MUTATING_TOOL_NAMES` 做静态分类。**上游事实**
2. OpenChat 的 dispatch 必须独立做三层校验：**白名单**（工具名属于只读集合）、**参数校验**（schema + 业务约束）、**资源归属校验**（附件/知识库/会话必须属于当前用户，查询带 `user_id`，读不到即"不存在"）。
3. **只读工具不能拿到完整可写 DB / storage client。** 应注入受限的查询接口（如 `KnowledgeReader`、`AttachmentReader`），而不是 `*sql.DB` 或 MinIO 客户端；让权限在接口形状上就不可写，比文档约定更可靠。
4. 不引入 Hermes 的写类工具：`terminal`、`execute_code`、`write_file`/`patch`、`memory`、`skill_manage`（自建/改 skills）、`delegate_task`、`cronjob_manage`，以及动态插件加载与 `override` 工具。`_HERMES_CORE_TOOLS` 默认全开这些是它的产品定位，不是只读 chatbot 该有的默认。

## 首版 / 后置（OpenChat 建议）

**首版**

- Provider：声明式 profile + 首个 OpenAI 兼容实现。当前 `emit(string)` 接口只适合文本，接 thinking/tool 时需升级为统一内容块与事件契约，不能声称保持原接口就完成 agent 接入。
- Tools：静态注册的只读工具集（检索 / 读附件 / 读知识库 / 读网页），`Available` 表达依赖是否就绪。
- Dispatch：白名单 + 参数 + 归属校验；只读 Reader 接口注入。
- 持久化：继续由 Go/Postgres 负责，但需按多端要求扩展 Run、parts 与恢复游标；不把当前结束时保存文本的流程当作可靠同步方案。
- 上下文：先实现预算限制和完整轮次裁剪，不能截断一半工具调用/结果；辅助模型仅用于可选摘要。

**后置**

- 独立 Transport 多协议（Anthropic / Gemini）。
- 压缩提交栅栏、独立 aux fallback 链。

**不采纳的扩展机制**：插件安装、动态模块扫描、热加载与第三方代码加载不在 OpenChat
实现目标中；后续扩展也采用仓库内 Go 模块、小接口和显式装配。

## 离线测试建议

- Provider 注册表：断言注册、覆盖、别名解析；未知 provider 应明确拒绝或走用户显式配置的回退，不能隐式切到其它供应商。DeepSeek 的 extra_body / reasoning 映射用表驱动测试。
- Transport：输入 OpenAI 格式到输出 kwargs / 归一化响应的快照测试；不做网络调用。
- 工具集：断言 `readonly` 集合精确等于白名单、写工具永远不在其中；`Available=false` 的排除。
- **权限不变量（最重要）**：对每个只读工具构造"他人资源 id"输入，断言返回不存在/拒绝；断言工具构造函数无法接受可写 store；调用未注册工具名必须拒绝。这些应是纯单元测试，不需要模型。
- 测试只验证 OpenChat 自身，不依赖 Hermes 运行时。

## 固定 commit 源码导航

链接前缀：`https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/`

- 供应商档案基类：`providers/base.py`
- 档案注册与发现：`providers/__init__.py`、`providers/README.md`
- DeepSeek profile：`plugins/model-providers/deepseek/__init__.py`、`plugins/model-providers/deepseek/plugin.yaml`
- Transport 基类/类型/解析：`agent/transports/base.py`、`agent/transports/types.py`、`agent/transports/__init__.py`、`agent/transports/chat_completions.py`
- 工具注册表：`tools/registry.py`（`ToolEntry`、`register`、`discover_builtin_tools`）
- 工具集声明：`toolsets.py`（`TOOLSETS`、`resolve_toolset`）
- 暴露给模型的 schema：`model_tools.py:get_tool_definitions`
- 工具执行/授权：`agent/tool_executor.py`、`agent/tool_guardrails.py`
- 主循环：`agent/conversation_loop.py:run_conversation`
- 上下文引擎：`agent/context_engine.py`
- 压缩与提交栅栏：`agent/conversation_compression.py:CompressionCommitFence`
- 辅助模型路由：`agent/auxiliary_client.py`
- 插件上下文：`hermes_cli/plugins.py`、`hermes_cli/middleware.py`
- 会话存储：`hermes_state.py:SessionDB`、`hermes_state_sessions.py`

常用入口可直接打开：[ProviderProfile](https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/providers/base.py)、[DeepSeek profile](https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/plugins/model-providers/deepseek/__init__.py)、[Transport](https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/agent/transports/base.py)、[ToolRegistry](https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/tools/registry.py)、[toolsets](https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/toolsets.py)、[context engine](https://github.com/NousResearch/hermes-agent/blob/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a/agent/context_engine.py)。

### 待验证项

- Hermes 的 dispatch 权限模型是否足以覆盖"资源归属校验"——本文只读到工具级授权/护栏，未逐工具核对业务归属。**待验证**
- `check_fn` 被绕过时执行路径的实际拒绝行为，未运行确认。**待验证**
- 各平台 toolset 的默认开启组合是否真如注释所述，未逐平台运行。**待验证**
- OpenChat 首版是否值得独立 Transport 抽象，属设计决策。**待验证**

## 相关文档

- [研究索引](README.md)
- [Pi AI 模块化模型层](pi-ai.md)
- [DeepSeek 官方 Harness](deepseek-harness.md)

---

固定基线：`NousResearch/hermes-agent` @ `b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a`（MIT，2026-09-13）。本文为静态源码研究，未做运行验证；源码事实、官方描述、OpenChat 建议与推测已分别标注。
