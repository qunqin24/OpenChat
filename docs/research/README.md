# Chatbot 内核研究索引

研究日期：2026-09-13。

这里记录 OpenChat 自研只读 Chatbot 内核的参考资料。**Pi AI 是主要设计参考**；
DeepSeek Harness 补充官方 harness 的模块接口与装配边界，Hermes 补充工具和上下文边界，
OpenCode 补充运行生命周期，Cherry Studio 补充模型兼容与附件处理，
**OpenWebUI 补充 UI/UX、多端同步和流式体验**。

本目录是源码研究与设计草案，**不代表新内核与同步能力已在 OpenChat 中实现**。
部分 UI 对齐已经落地，具体差异见 OpenWebUI 专篇的本地对照矩阵。
当前系统见 [AGENTS.md](../../AGENTS.md)、[功能设计](../design/README.md) 和 [代码约定](../conventions.md)。

## 阅读顺序

1. [Pi AI 模块化模型层](pi-ai.md)：统一类型、协议适配、供应商注册、流式事件与历史转换。
2. [Hermes 模块边界](hermes-agent.md)：ProviderProfile、Transport、工具集、上下文与辅助调用。
3. [OpenCode 会话执行](opencode.md)：单轮处理和运行循环、消息 part、取消、重试与持久化边界。
4. [Cherry Studio 模型接入](cherry-studio.md)：供应商解析、能力映射、内容转换、附件与兼容案例。
5. [OpenWebUI 体验与流式同步](openwebui-experience-streaming.md)：交互中间态、HTTP/Socket.IO/SSE 分工、多端、快照恢复与停止。
6. [DeepSeek Harness](deepseek-harness.md)：DeepSeek 官方 harness 的模块化、可替换接口和执行流程对照。
7. [models.dev 能力目录](models-dev.md)：公开数据字段、reasoning_options、渠道映射、本地覆盖与后台刷新。

关注多端或前端实时体验时，先读第 5 篇。

## 研究基线

| 项目 | 参考定位 | 固定版本 / commit | 许可证 |
| --- | --- | --- | --- |
| [Pi AI](https://github.com/earendil-works/pi/tree/d981de1229ef899957bbe968bc8dcda02a21f477/packages/ai) | 主要参考 | `v0.85.1` / `d981de1229ef899957bbe968bc8dcda02a21f477` | MIT |
| [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness/tree/c291e7961a515f6d7af9304e7fd1d257929aef26) | 模块接口与执行边界 | `c291e7961a515f6d7af9304e7fd1d257929aef26` | MIT |
| [Hermes Agent](https://github.com/NousResearch/hermes-agent/tree/b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a) | 工具与上下文 | `b6b53c69a6ed49cb099cf1bfe76b5e6edd718e5a` | MIT |
| [OpenCode](https://github.com/anomalyco/opencode/tree/95daf90670b7c039c436c85537da5fbfe2205b41) | 会话与运行生命周期 | `95daf90670b7c039c436c85537da5fbfe2205b41` | MIT |
| [Cherry Studio](https://github.com/CherryHQ/cherry-studio/tree/3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6) | 兼容与附件案例 | `3e9b7d2d08ac6ac3c907e4a36ba3e086489993b6` | AGPL-3.0 |
| [OpenWebUI](https://github.com/open-webui/open-webui/tree/0a7c15832fb30b1903753e83f81dc7d27e5b0944) | UI/UX 与多端流式体验 | `0a7c15832fb30b1903753e83f81dc7d27e5b0944` | 自定义 Open WebUI License，见专篇 |
| [models.dev](https://github.com/anomalyco/models.dev/tree/2671cfe1743e05e420da45c143b0ec96ae61a8f1) | 模型能力目录数据源 | 源码 `2671cfe1743e05e420da45c143b0ec96ae61a8f1`；在线 JSON 独立更新 | MIT |

Hermes 指 NousResearch 的 Hermes Agent，它支持 DeepSeek。Pi 的旧仓库地址
`badlogic/pi-mono` 已指向 `earendil-works/pi`；不同版本的包名和 API 不能混用。
DeepSeek 官方的 `deepseek-ai/deepseek-harness` 是另一个独立参考项目，不与 Hermes 混称。

## 已明确的 OpenChat 方向

- 自研 Go 内核，采用模块化单体，复用现有 API、Postgres 和 React 前端。
- **不做插件系统**：扩展通过仓库内新增实现、稳定接口和入口显式装配完成。上游插件相关内容仅用于研究模块边界，不是我们的插件开发路线。
- 产品以 Chatbot 为核心；模型只能调用明确授权的只读业务工具。
- Pi AI 提供主要参考：协议层、供应商、模型能力、消息内容块与事件流的边界。
- models.dev 作为模型能力目录的默认数据来源；目录元数据、实际协议适配和产品权限分别处理。
- Go 产品后端负责认证、资源归属、聊天记录和运行状态持久化。
- 多端目标是 Run 与订阅连接分离，以稳定标识、事件序号和快照恢复表达一致性；这些属于拟议的新协议。
- 研究源码与引入代码是不同工作；实际复用代码时核对对应文件及依赖的许可证。

## 如何使用和维护这些文档

每篇研究应包含版本、结论、模块地图、关键流程、源码导航、OpenChat 映射、局限和验证建议。
阅读时注意以下标记：

- **上游事实**：在固定版本源码中直接核对的行为。
- **官方文档描述**：来自固定版本 README 或文档的说明，未必已逐条核对运行路径。
- **OpenChat 建议**：拟采用的设计，尚需实现与验收。
- **待验证**：静态阅读不能确认的运行时行为，或有意留待首版验证的决策。

源码链接尽量固定到 commit，而不是浮动的 `main` / `dev`。更新研究基线时，
同时更新本索引与对应专篇，并复核跨项目结论。文中的示意接口和测试清单不是已发布 API 或已通过的测试结果。
