# 沙箱：调研与规划（草案）

> 状态：**规划，未实现**。调研时间 2026-11-02，版本和产品状态以该日为准。用户拍板「待决定的问题」后再转成正式设计文档，写进 [README.md](README.md) 索引和 AGENTS.md。
>
> 相关方案：
> - [skills-plan.md](skills-plan.md)：技能机制，先做；
> - [deep-research-plan.md](deep-research-plan.md)：深度研究，不需要沙箱。
>
> 本文只管沙箱：第 1 期给办公文件用的轻量沙箱，后几期扩展成能联网、能修 GitHub issue 的「Agent 电脑」。

## 0. 结论先行

- **第 1 期要解决的场景**：
  - 做 PPT、Word 文档、Excel 表格；
  - **保留原格式**修改用户上传的 docx、xlsx、pptx、pdf：改措辞、按模板填、加修订，改公式和图表，合并、拆分 PDF，填表单，加水印，转换格式。
  - 第二类做不到「只拿 `doctext` 抽出的文字」，必须在沙箱里用 Python 库直接操作原件，claude.ai 的 docx、xlsx、pptx、pdf 技能就是这么做的。所以第 1 期就上沙箱。
  - 深度研究照 [deep-research-plan.md](deep-research-plan.md) 做，不需要沙箱。
- **形态**：claude.ai 的「短命容器」模式（Pattern 1）。
  - 用 gVisor（`runsc`，不要 KVM），**完全断网**。
  - **只有正在工作的 run 才有容器**：第一次用到时才起，run 结束就回收。
  - 对话的工作区打包成一个文件，存进存储后端（本地磁盘或 MinIO/S3），**计入用户的存储配额**，下一个 run 再恢复。
  - 闲置的用户只占存储，不占 CPU 和内存。**不给任何人常驻的机器。**
- **Agent 循环留在 Go 里**（Anthropic Managed Agents 的「大脑、手、会话」分离）。给模型一套 Coding Agent 式的工具：`bash`、`read`、`write`、`edit`（定点替换，`find` 必须恰好出现一次，同写作块）、`glob`、`grep`，都经 `sandboxd` 的 `execute()` 在容器里执行。OpenChat 现有的约定全部沿用：并发、收据、配额、用量、审核、子 Agent 规则；渠道 key 也不进沙箱。
- **文件**：
  - 原件经 `attachmentResolver.one` 核对 SHA-256 收据后，放进 `/workspace/inputs/`；
  - 模型写进 `/workspace/outputs/` 的文件在 run 结束时收回，成为这次回答的附件；
  - Office 文件顺手用 LibreOffice 转一份 PDF 预览。
- **技能**：照 [skills-plan.md](skills-plan.md)：
  - 格式按 agentskills.io 规范，内置技能用 `go:embed` 编进程序；
  - 技能清单进对话快照，模型用 `skill` 工具（view、read、search）读；
  - 沙箱接上之后，技能里的 `scripts/`、`assets/` 才用得上：只读挂到容器的 `/skills/<name>/`。

  docx、xlsx、pptx、pdf 四个内置技能自己写：Anthropic 那几份不是开源许可。
- **成本**：
  - **并发上限就是成本上限**，满了就排队。
  - 每个容器默认 1 核、2 GiB 内存上限。实测：LibreOffice 转换普通文件峰值 100–150 MB，极端的 195 MB 大图 PPT 约 1 GB。
  - 16 GB 的机器同时约 6 个，32 GB 的约 12 个。
  - 不够时先加第二台跑 `sandboxd` 的机器，再考虑远程后端（E2B、Modal 等）。
- **在应用里改**：沙箱产出的是文件，不能在应用里编辑；在线改文字仍由写作块负责。另做「表格块」「幻灯片块」，列为待决定的问题。

## 1. 调研

### 1.1 「执行代码」类工具

| 方案 | 执行位置与隔离 | 模型怎么调用 | 文件进出 | 会话与超时 | 网络 | 自托管 |
| --- | --- | --- | --- | --- | --- | --- |
| **LobeChat / LobeHub 云端沙箱** | 开源仓库里没有运行时：经 `SandboxProvider` 调外部服务。默认是闭源的 Marketplace SDK，跑在 AWS Bedrock AgentCore（Firecracker）上；自托管只能接第三方的 Onlyboxes | 内置工具 `lobe-cloud-sandbox`，13 个 API（`executeCode`、`runCommand`、读写编辑搜索文件、`exportFile`）；执行和写文件都要用户批准 | 沙箱自己 curl 预签名地址，把附件下载到 `/mnt/data`；导出时沙箱自己 PUT 到预签名地址。自托管时出过 bug（#13660、#17586） | 每个话题一个沙箱，过期重建并告诉模型；命令默认 120 秒 | 可配，默认策略未确认 | 要登录 LobeHub Marketplace，或部署 Onlyboxes；旧的 Pyodide 已移除 |
| **Open WebUI** | Pyodide（浏览器里的 WASM）或你自己提供的 Jupyter（本身不是沙箱）；两者都已标为 legacy，官方推荐 Open Terminal | `execute_code(code)` | 输出的 base64 图片由服务端改存为文件 | Jupyter 每次执行新建内核；超时 60 秒 | 视容器 | 能 |
| **LibreChat Code Interpreter** | 开源后端 [code-interpreter](https://github.com/LibreChat-AI/code-interpreter)：NsJail，或 libkrun microVM 里再套 NsJail（要 KVM） | `POST /exec {code, lang, files}` | 每次运行前恢复对话的文件，写出的文件随消息返回 | 默认无状态；超时 15 秒 | 经出站网关 | 能但重：五个服务加 Redis、MongoDB、S3；托管版已停止新订阅 |
| **OpenAI `code_interpreter`** | 托管 VM | 内置的 python 工具 | `container_file_citation` | 闲置 20 分钟过期 | 默认关 | 不能 |
| **Anthropic code execution / claude.ai** | 托管容器，gVisor（[How we contain Claude](https://www.anthropic.com/engineering/how-we-contain-claude)） | `bash_code_execution` 和 `text_editor_code_execution` 两个子工具 | 上传在 `/mnt/user-data/uploads`，产出在 `/mnt/user-data/outputs`；技能只读挂在 `/mnt/skills/{public,private,examples}`；**做 PPT 靠 pptx 技能（说明加脚本），不是专门的工具** | 复用容器，有 REPL 状态 | 完全断网 | 不能 |

来源：
- LobeHub：[provider factory](https://github.com/lobehub/lobehub/blob/canary/apps/server/src/services/sandbox/factory.ts)、[manifest](https://github.com/lobehub/lobehub/blob/canary/packages/builtin-tool-cloud-sandbox/src/manifest.ts)、[自托管文档](https://github.com/lobehub/lobehub/blob/canary/docs/self-hosting/environment-variables/cloud-sandbox.mdx)、维护者在 [#13915](https://github.com/lobehub/lobehub/issues/13915) 的说明
- Open WebUI：[文档](https://docs.openwebui.com/features/chat-conversations/chat-features/code-execution/python)
- LibreChat：[文档](https://www.librechat.ai/docs/features/code_interpreter)、[托管版停止订阅](https://code.librechat.ai/pricing)
- OpenAI：[指南](https://developers.openai.com/api/docs/guides/tools-code-interpreter)
- Anthropic：[文档](https://platform.claude.com/docs/en/agents-and-tools/tool-use/code-execution-tool)、[Bishop Fox 实测](https://bishopfox.com/blog/journey-into-claudes-sandbox)

### 1.2 「Agent 电脑」类产品（后几期的参照）

| 产品 | 环境 | 持久化 | 网络与凭据 | 对外写 |
| --- | --- | --- | --- | --- |
| [Hermes Agent](https://github.com/NousResearch/hermes-agent)（Nous，MIT） | 终端后端可选 local、docker、ssh、modal、daytona、vercel_sandbox；Docker 后端是一个常驻容器，所有工具经 `docker exec` 执行，没有 gVisor；单用户设计 | `/workspace` 绑定挂载，保留文件、不保留进程 | 不转发宿主的环境变量；iron-proxy 用占位符换真密钥；有 SSRF 防护 | 容器里不审批命令，只保留硬性黑名单 |
| ChatGPT Work（[发布](https://openai.com/index/chatgpt-for-your-most-ambitious-work/)、[安全](https://learn.chatgpt.com/docs/enterprise/chatgpt-work-cloud-security.md)、[拆解](https://latent.space/p/unpacking-chatgpt-work)） | 托管 microVM，跑在 Codex 的 harness 上；浏览器是另外托管的服务 | 工作区同步进存储，需要时恢复到新的 VM 上 | 每项能力单独开关；浏览器登录时凭据不经模型 | 「重要操作」前确认 |
| Codex 云端（[环境](https://learn.chatgpt.com/docs/environments/cloud-environments.md)、[联网](https://learn.chatgpt.com/docs/cloud/internet-access.md)） | 发布好的环境镜像，每个任务一个工作区 | 7 天 | 白名单，可只允许读的方法；network secrets：程序只拿到占位符，代理换真值 | 用户自己点开 PR |
| Claude Code 网页版（[文档](https://code.claude.com/docs/en/claude-code-on-the-web.md)、[沙箱设计](https://www.anthropic.com/engineering/claude-code-sandboxing)） | 每个会话一台 VM | 闲置暂停，被回收后是新 VM | 三档（None、Trusted、自定义）；git 代理，真 token 从不进 VM | 推 `claude/` 前缀的分支，用户点 Create PR |
| Managed Agents（[原文](https://www.anthropic.com/engineering/managed-agents)） | 会话、harness、沙箱分开；沙箱只是 `execute(name, input) → string`，第一次需要时才开；拆开后 TTFT p50 降约 60% | 不依赖容器存活 | 只能走带每会话 JWT 的代理，没有 DNS，私网不通；凭据在 vault，由代理注入 | — |
| Manus（[沙箱](https://www.manus.im/blog/manus-sandbox)、[E2B 案例](https://e2b.dev/customers/how-manus-uses-e2b-to-provide-agents-with-virtual-computers)） | 每个任务一台云 VM（E2B Firecracker）；常开的 Cloud Computer 另外收费，$30–50/月 | 休眠 7 或 21 天后回收，回收后只恢复产出和项目文件 | 用户「接管」浏览器来登录 | — |
| OpenHands（[Docker 执行](https://docs.openhands.dev/openhands/usage/agent-canvas/backend-setup/docker-execution.md)） | 每个对话一个 agent-server 容器，要挂 docker.sock；开源版是单用户 | 绑定挂载，闲置 20 分钟停 | 文档没写出站限制；secrets 作为环境变量给 Agent，它读得到 | 确认策略加 LLM 风险分级 |

共识：
- 保留文件，不保留进程；
- 凭据不进沙箱，由出站代理注入；
- 对外写由用户确认；
- 沙箱里的操作不逐条审批（Claude Code 的用户批准了约 93% 的权限请求，存在审批疲劳）。

能自托管的现成方案都不合适：
- OpenHands 开源版是单用户；
- Open WebUI 的 Terminals 编排器不是开源许可；
- OpenSandbox 的多租户只在 K8s 上有，它的出站控制[官方写明在 gVisor 上不可用](https://github.com/opensandbox-group/OpenSandbox/blob/main/docs/architecture/network/egress.md)；
- E2B 要 KVM。

### 1.3 隔离技术

| 技术 | 隔离 | 要 KVM | 冷启动 | 判断 |
| --- | --- | --- | --- | --- |
| **gVisor**（runsc，systrap） | 用户态内核 | **不要** | 进程启动，没有内核引导（未在 runsc 下实测，见 §6） | **选它**；Modal、ChatGPT 代码解释器（[逆向](https://pncnmnp.github.io/blogs/chatgpt-containers.html)）、claude.ai 都用它（[平台文档](https://gvisor.dev/docs/architecture_guide/platforms/)） |
| 普通 Docker 加固 | 共享宿主内核；默认 seccomp 只挡约 44 个系统调用 | 不要 | 本机实测 runc 0.32 秒 | 内核漏洞即可逃逸，不用于多用户（[文档](https://docs.docker.com/engine/security/seccomp/)） |
| Firecracker、Kata、microsandbox、[E2B runtime](https://github.com/e2b-dev/runtime) | microVM | 要 | 约 100–150 ms（厂商数据） | 有 KVM 的部署以后可以作为可选后端；E2B 单机版要 12 GiB 内存 |
| Pyodide / WASM | WASM | 不要 | 快 | 只有 Python，不能操作 LibreOffice；而且 run 在服务端执行（定时任务、后台 run 时没有浏览器） |
| Daytona | — | — | — | 仓库 2026-06 起[归档](https://github.com/daytonaio/daytona) |

gVisor 的两个限制：
- 容器内跑不了 iptables/nftables（[gvisor#9917](https://github.com/google/gvisor/issues/9917)），所以出站控制要在沙箱外面做（后几期用得上）；
- 文件 I/O 比 runc 慢。

## 2. 第 1 期两种做法的比较

| | A. 结构化块（不要沙箱） | **B. 轻量沙箱（推荐）** |
| --- | --- | --- |
| 做法 | 写作块加导出；新做表格块、幻灯片块，模型写结构化内容，面板里预览，服务端导出 xlsx（excelize）、pptx | 模型在断网的 gVisor 容器里用 python-docx、openpyxl、python-pptx、pypdf、LibreOffice 读写文件，照技能里的说明和脚本做 |
| **改用户上传的原件、保留格式** | **做不到**：只拿得到 `doctext` 抽出的文字，样式、修订、公式、图表、版式都没了 | 能：直接改原件 |
| 质量上限 | 受限于我们定的主题和版式 | 高，取决于模型和技能（claude.ai 就是这样） |
| 在应用里编辑 | 能，块就是可编辑的 | 不能：产出是文件；在线改文字仍靠写作块 |
| 资源成本 | 几乎为零 | 每个工作中的 run 一个容器（§5），按需起、用完回收 |
| 实现量 | 中：两种新块、编辑器、导出器，每种格式各写一遍 | 中：`sandboxd`、镜像、六个工具、文件进出、技能；格式能力交给现成的库 |
| 和后几期的关系 | 只服务办公文件 | 就是「Agent 电脑」的第一块：同一套 `sandboxd`、工具、工作区 |

**推荐 B**：用户上传自己的 Word、PDF 要求照原样改，这是最常见的办公请求之一，A 根本接不住。A 的「能在应用里改」由写作块继续承担，以后需要时再加表格块、幻灯片块（待决定 3）。

考虑过、不采用的做法：把 OpenCode 这类开源 Coding Agent 整个跑在沙箱里，由主 Agent 交给它。不采用的原因：
- 「大脑」进了容器，任务没做完容器就不能回收，容器一挂状态就丢；
- 沙箱必须能连模型接口；
- 会出现两套审核、压缩、记忆和界面。

我们只借它的**工具形态**。

## 3. 轻量沙箱的设计

### 3.1 架构

```
openchat（run 循环、工具、存储、审核）
   │ 内部网络 + 令牌
   ▼
sandboxd ──docker.sock──▶ Docker（runtime=runsc，--network=none）
   └─ 每个工作中的 run 一个容器，run 结束即删
```

- **`sandboxd`**：同一仓库的另一个 Go 入口，同一个镜像。
  - 它是唯一挂 `docker.sock` 的服务，`openchat` 自己不挂。
  - 只开放窄接口：`Acquire(conversation)`、`Execute(name, input)`、`PutFiles`、`Collect`、`Snapshot`、`Release`、`Health`。
  - 建容器的参数全写死在 `sandboxd` 里，`openchat` 只能选限额档位。
  - 内部有 `Backend` 接口，第 1 期只有 docker+runsc。
- **`openchat` 侧**：只有工具（`agent.Tool`）和一个 `Sandbox` 客户端。它不知道容器在哪，只知道「给这个对话借一台、执行、还回去」。
- **容器参数**：

  ```
  --runtime=runsc --network=none --read-only --user 1000:1000
  --cap-drop=ALL --security-opt=no-new-privileges
  --cpus=1 --memory=2g --memory-swap=2g --pids-limit=256
  --tmpfs /tmp:size=512m --tmpfs /workspace:size=2g
  -v skills:/skills:ro
  ```

### 3.2 生命周期：只给工作中的 run

```
run 第一次调沙箱工具 ──▶ 借容器（排队）──▶ 从存储恢复工作区、同步 inputs
       ...同一 run 的后续调用复用这台容器...
run 结束（完成、失败、停止、awaiting_input）──▶ 收 outputs、生成预览 ──▶ 打包工作区存进存储 ──▶ 删容器
```

- **不留宽限期**：下一次 run（用户追问）重新借一台，冷启动约 1–2 秒（§5）。`awaiting_input`（等用户回答 `ask_user`）同样归还，不让一台容器空等用户。
- **工作区快照**：`/workspace` 下除 `inputs/` 之外的内容打成 `tar.zst`，存在 `workspaces/<对话>/` 下，**计入用户的存储配额**，过 `LockUserStorage` + `roomFor`。
  - 超额时快照不存，下次是空工作区，并告诉模型和用户；
  - 单个快照有上限（默认 500 MB），超了只保留 `outputs/` 和最近写过的文件；
  - 快照和对话同生死：对话删除（软删除到期硬删）、账号删除时按「附件行消失」的规矩删（先收 key、`DELETE … RETURNING`、提交后删文件），避开 OpenAI 那种「删了聊天、快照还在」的问题。
- **进程重启**：`openchat` 重启时，执行中的调用按 `Replayable() = false` 记为 `interrupted`；`sandboxd` 启动时清掉所有带 `openchat.sandbox=1` 标签的容器；工作区以上一次存下的快照为准。

### 3.3 工具（Coding Agent 式）

| 工具 | 参数 | 说明 |
| --- | --- | --- |
| `bash` | `command`、`timeout_seconds`（默认 60，最多 300） | 输出保留开头和结尾，合计不超过 32 KB；断网 |
| `read` | `path`、`offset`、`limit` | 带行号；二进制文件报类型和大小，提示用对应的库或技能脚本处理 |
| `write` | `path`、`content` | 只能写 `/workspace` 下 |
| `edit` | `path`、`find`、`replace` | `find` 必须恰好出现一次，否则不改并说明原因（同写作块的 `update`） |
| `glob` | `pattern` | 列文件 |
| `grep` | `pattern`、`path`、`glob` | ripgrep，结果有条数上限 |

- 都是 `Replayable() = false`。同一轮的调用会并发执行：同一 run 的容器在 `openchat` 侧单飞地借，`edit`、`write` 按路径串行。
- **命名**：和现有的 `file_read`/`file_search`（读附件文字，没有沙箱时用）分开。有沙箱时附件就在 `inputs/` 里，提示词让模型用沙箱工具。
- **提示词**：`agent_setup.go` 加一段英文的 `sandboxGuide`，写明：
  - 什么时候用：用户要文件、要保留原格式改文件、要精确计算；
  - 什么时候不用：在线写文字用写作块；
  - 原件在 `inputs/`、交付写进 `outputs/`；
  - 没有网络，不能装包，预装了哪些库；
  - 先看对应的技能（`skill view docx` 等）。

  模型用错时改这段提示词或工具描述，不在服务端改写它的输出。
- **子 Agent**：默认不给（AGENTS.md：新工具默认不给子 Agent）。
- **临时对话**：可以用，快照随对话 24 小时清掉。
- **定时任务**：照常可用。

### 3.4 文件进出

- **进**：
  - 范围同 `file_read`：本对话里用户自己的、`status <> 'blocked'` 的消息里的附件；
  - 每个文件都经 `attachmentResolver.one` 读、核对收据，对不上的不放，结果里注明；
  - 放原件（Word、Excel、PDF 不转文字），文件名清理后放进 `inputs/`，按附件 id 和摘要增量同步；
  - 发送时已经审过，不再审。
- **出**：
  - run 结束时，`outputs/` 里在这次 run 中新增或改动的文件（按哈希）逐个取回，存成这次回答的附件：`attachments` 加 `run_id`（`ON DELETE CASCADE`）和 `origin = 'sandbox'`，`message_id` 指助手消息；
  - 过 `roomFor`，单个上限默认 50 MB，单次 20 个；
  - 重新生成、改写时随 run 消失。今天的 `DeleteAnswerTo` 是 `:exec`、不交回 `storage_key`，要改成 `DELETE … RETURNING`，再在提交后删文件。
- **预览**：docx、pptx、xlsx 在容器归还前用 LibreOffice 转一份 PDF（实测一般 0.6–1.3 秒），pptx 另用 `pdftoppm` 出前几页缩略图。预览也存成附件，前端用现有的 PDF 查看方式打开，原件可以下载。
- **安全**：
  - 产出是模型写的，不审，同模型写的文档；
  - 下载走签名地址，带 `nosniff` 和 `CSP: default-src 'none'; sandbox`，HTML、SVG 一律当文件下载；
  - S3 的签名读取地址要同样强制 `Content-Disposition`，实现时专门测试。

### 3.5 技能（引用 [skills-plan.md](skills-plan.md)）

- 机制按 skills-plan：
  - 格式按 agentskills.io 规范；
  - 内置技能在 `internal/skills/builtin/<name>/`，用 `go:embed` 编进程序；
  - 清单进对话快照；
  - 模型用 `skill` 工具（view、read、search）读说明和参考文件。
- 沙箱接上后（skills-plan 第 3 期的「沙箱挂载与脚本」）：`sandboxd` 借出容器时，把这个对话快照里的技能版本写进只读的 `/skills/<name>/`，模型照 `SKILL.md` 调用 `scripts/` 里的脚本、用 `assets/` 里的模板。
- 第 1 期的四个内置技能 `docx`、`xlsx`、`pptx`、`pdf` 自己写。Anthropic 的同名技能写着「© Anthropic, All rights reserved」，使用受其服务条款约束，不能照搬。内容包括：
  - 常见任务的做法：保留格式改文字、加修订、按模板填、改公式、合并和拆分、填表单、转格式；
  - 做完自检：转 PDF 看页数、用 `pdftoppm` 看图；
  - 中文字体。

## 4. 安全边界与上限

| 项 | 默认（后台可调） |
| --- | --- |
| 网络 | **完全断网**（`--network=none`），没有例外 |
| 单次 `bash` | 60 秒，最多 300 秒；到点杀掉整个进程组；run 被停止时立即杀 |
| 单个容器 | 1 核、2 GiB 内存（不能用 swap）、256 个进程、`/workspace` 2 GiB、`/tmp` 512 MB |
| 单个 run 的沙箱时长 | 30 分钟（只算借着容器的时间） |
| 同时借出 | 全局按机器设（§5），每人 1 个 |
| 排队 | 借不到就等，界面显示「排队中」；超过 5 分钟仍借不到，工具返回 `IsError`「sandbox is busy」，模型据此告诉用户 |
| 每人每天 | 沙箱总时长 2 小时（防滥用，可关） |
| 快照 | 计入存储配额；单个 500 MB |

- **用量记录**：新增 `sandbox_sessions`（run、用户、借出和归还时间、CPU 秒、峰值内存、退出原因），后台仪表板显示次数和时长。
- **花费**：沙箱不花模型的钱，不进 `usage_logs` 的金额；但 run 的 token 照常计入容量上限。
- **审核**：用户能影响沙箱的只有附件（已审）；不开任何让用户直接把文字送进沙箱的接口。

## 5. 成本与单机容量

**实测**（2026-11-02，本机 Docker Desktop arm64、**runc**，不是 gVisor；runsc 下的数字第 0 期在 VPS 上补测）：

| 项 | 结果 |
| --- | --- |
| 容器起停（`docker run --rm … true`） | 0.32 秒 |
| `import pandas, matplotlib, docx, openpyxl, pptx, pypdf, pdfplumber, reportlab` | 首次 0.90 秒，第二次 0.49 秒 |
| LibreOffice 转 PDF：30 页带图的 pptx | 冷 1.28 秒，峰值 129 MB |
| 同上：约 50 页的中文 docx | 0.68 秒，98 MB |
| 同上：1 万行带公式的 xlsx | 0.73 秒，152 MB |
| 同上：60 页、195 MB 的大图 pptx（极端情况） | 10.8 秒，峰值约 1 GB |

峰值内存取自容器 cgroup 的 `memory.peak`，含读文件的页缓存。

**镜像大小**（apt 元数据加实测）：

| 部分 | 大小 |
| --- | --- |
| Debian slim 基础镜像 | 约 75 MB |
| Python 库（pandas、matplotlib、python-docx、openpyxl、python-pptx、pypdf、pdfplumber、reportlab 及依赖，实测 site-packages） | 337 MB |
| LibreOffice writer、calc、impress 的 nogui 版 | 444 MB |
| Noto CJK 字体 | 89 MB |
| poppler 等 | 约 50 MB |

合计：解压后约 **1.0 GB**，压缩后下载估计 350–450 MB；不带 LibreOffice 约 0.55 GB。作为对照，Gotenberg（LibreOffice 加 Chromium）的镜像是 2.48 GB。

**冷启动估计**（一次 run 第一次用沙箱）：容器起来约 0.3–0.8 秒（gVisor 待测）+ 恢复工作区（小于 50 MB 时不到 1 秒）+ 同步 inputs，**合计 1–2 秒**；之后同一 run 里的调用直接执行。

**容量**：内存是硬约束（上限是 cgroup 硬限），CPU 可以超卖：一个 run 大部分时间在等模型输出，容器空闲。

| 机器 | 给系统、Postgres、MinIO、openchat 留 | 可分给沙箱 | 每个上限 2 GiB 时同时 | 说明 |
| --- | --- | --- | --- | --- |
| 16 GB（常见 4 vCPU） | 约 4 GB | 约 12 GB | **约 6 个** | 大多数操作只用 100–500 MB；改成每个 1 GiB 能到约 12 个，但大文件会被 OOM |
| 32 GB（常见 8 vCPU） | 约 4–6 GB | 约 26 GB | **约 12 个** | — |

数的是**同时在用沙箱的 run**，不是用户：
- 一个 run 只在第一次调沙箱工具到 run 结束这段时间占容器；
- 闲置用户只占存储里的快照。

**扩容**：
1. 第二台机器只跑 `sandboxd`：它本来就是经网络调用的服务，`openchat` 按权重分配，像渠道那样。
2. 远程后端：E2B（托管，或在有 KVM 的机器上自托管 runtime）、Modal Sandboxes（gVisor）、Vercel Sandbox 等实现 `Backend` 接口。
   - 工作区快照用我们自己的 `tar.zst` 格式，换后端照样能恢复；
   - 远程后端按秒计费，费用记进 `sandbox_sessions`；
   - 同样不给网络、不给凭据。

## 6. 部署

- `deploy/docker-compose.yml` 加一个可选的 profile：

  ```bash
  docker compose -f deploy/docker-compose.yml --env-file deploy/.env --profile sandbox up -d
  ```

  启用后起一个 `sandboxd` 服务，挂 `docker.sock`，只在内部网络。
- 宿主机先装 gVisor：apt 源加上 `runsc install`，再重启 Docker。`sandboxd` 启动时自检 Docker、`runsc`、镜像、能否起一个空容器，结果报给后台。**没有 `runsc` 就不开**，不退回 runc（待决定 5）。
- **后台 → 系统「沙箱」**（设置存在 `app_settings`）：
  - 开关（默认关）、开放给谁、`sandboxd` 地址和令牌（只写不读）
  - 第 4 节的各项上限、全局并发数
  - 「测试连接」按钮
- `ChatFeatures` 加 `sandbox`。是否给输入框加开关见待决定 4。

## 7. 之后：从轻量沙箱到「Agent 电脑」

第 1 期的 `sandboxd`、工具和工作区快照原样沿用，逐步加上：

- **网络（经代理）**：
  - 沙箱只接一个 `internal` 的 Docker 网络，唯一的出口是 `sandboxd` 里的 Go 出站代理（gVisor 里设不了防火墙，只能靠拓扑）；
  - 代理在拨号时检查解析出的地址，复用 `tools.PublicAddress`，挡住内网、`169.254.169.254` 和 DNS 重绑定；
  - 只开 80、443，有限速、每日流量上限和审计日志；
  - 默认是「常用开发站点」白名单（GitHub、PyPI、npm 等）；
  - 永远不放行 OpenChat 自己的域名和模型渠道的地址：白名单里的每一个域名都是授出去的能力（Anthropic 官方事故：放行了自家 API，被借来外传数据）。
- **GitHub**：
  - 用户的 fine-grained PAT 加密存放，只在代理里，沙箱里是占位符；
  - 代理只对 GitHub 的几个域名解开 TLS、注入 token，并检查仓库和分支：只能推 `openchat/` 前缀的分支，不能删、不能强推；
  - 开 PR、评论走服务端的 `github` 工具，run 停在 `awaiting_input` 等用户确认（沿用 `ask_user` 的机制）。
- **长任务**：
  - Agent 模式下更高的轮数上限和 run 时长；
  - 允许后台进程（只在借出期间存活）；
  - 「电脑」面板显示命令时间线和文件树；
  - 完成通知。
- **浏览器**：独立的浏览器容器，截图加 CDP 操作，可以接管来登录；凭据不经模型。
- **子 Agent**：共用主对话的工作区。

## 8. 分期

| 期 | 交付 | 完成的标准 |
| --- | --- | --- |
| **0 验证**（3–5 天） | 在无嵌套虚拟化的 x86_64、arm64 VPS 上装 gVisor systrap，重测 §5：容器起停、Python 导入、LibreOffice 转换的时间和峰值、工作区打包和恢复、tmpfs 计量；确认 LibreOffice 能在 runsc 下运行 | 一页实测记录，修订 §4、§5 的默认值 |
| **先做** | [skills-plan.md](skills-plan.md) 第 1–2 期（机制和内置技能）；[deep-research-plan.md](deep-research-plan.md) 的第 1 期 | 见各自文档 |
| **1 轻量沙箱** | `sandboxd`（docker+runsc 后端，断网）和镜像；按 run 借还、排队、工作区快照（计入配额）；六个工具和 `sandboxGuide`；inputs 同步（收据）、outputs 收回、PDF 预览；四个内置技能（docx、xlsx、pptx、pdf）和 `/skills` 挂载；`DeleteAnswerTo` 等路径交回 key；后台设置和自检；回复里的文件卡片和预览；compose profile 和部署文档；设计文档转正，AGENTS.md 加规则 | 用 Haiku 实测：①上传一份 Word 合同，改三处措辞并加修订，格式不变；②上传 Excel，改公式、加一张图表；③从一段需求做一份 10 页的 PPT；④合并两个 PDF 并加水印。带库测试覆盖：收据不符的文件不进沙箱、配额满时快照不存、重新生成后产出文件和行都删掉、对话删除后快照没了、并发满时排队 |
| **2 体验** | 模型能看自己生成的预览图（工具结果带图片）；导出写作块为 docx/PDF（用沙箱里的 pandoc、LibreOffice，和深度研究的导出共用）；深度研究报告里的数据图表；第二台 `sandboxd` 机器；后台统计 | 画错的图模型能自己发现并重画 |
| **3 Agent 电脑** | §7：经代理联网、GitHub 连接和确认、长任务和「电脑」面板、子 Agent 共用工作区、远程后端（E2B、Modal 等） | 修 issue 得到一个经用户确认才开出的 PR；电脑里拿不到真 token |
| **4** | 浏览器容器、GitHub App、通用的网络密钥 | — |

## 9. 待用户决定的问题

1. **第 1 期就上轻量沙箱（B），对吗？** 理由是要保留原格式改用户上传的文件。
2. **开放给谁？** 建议先只给管理员，再按用户开放：它占真实的 CPU 和内存，并发上限就是成本上限。
3. **表格块、幻灯片块（「在应用里改」）要不要做？** 建议先不做：在线改文字有写作块；表格和 PPT 先以文件交付，看用户反馈再说。
4. **要不要输入框开关？** 还是有沙箱时就默认提供、由模型判断何时用。建议不加开关，靠提示词让模型只在需要文件时用。
5. **没有 gVisor 时一律不开？** 建议是，不提供 runc 退路。
6. **默认资源**：每个容器 1 核、2 GiB；每人同时 1 个；排队等 5 分钟；单个 run 沙箱时长 30 分钟；每人每天 2 小时。是否合适？
7. **工作区快照计入存储配额**，超额就不存（下次是空工作区）：可以吗？
8. **run 结束就回收、不留宽限期**：每次追问多 1–2 秒冷启动，换来闲置时零占用。可以吗？
9. **镜像**：约 1 GB（压缩后约 400 MB），只在启用沙箱时才拉取。要不要拆成不带 LibreOffice 的精简版？不建议：预览和格式转换都靠它。
10. **四个内置技能自己写**（Anthropic 的不是开源许可），第 1 期就做全四个，还是先做 docx 和 pptx？
