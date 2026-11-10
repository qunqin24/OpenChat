# 图片生成：方案

> 状态：**第 0 期已实现**（2026-11-05：`catalog.DefaultKind` 先读 models.dev 的 `type`，`ListServedModels`、`ModelProfile`、`ListCatalogModelsForChannel` 带出 `model_type`）。**第一期后端已实现**（2026-11-05：迁移、`llm.ImageClient` 与两个适配器、`usage.Meter.EndImages` 与按张计价和预留、`image_generate` 工具、存图与认领、`DeleteAnswerTo` 收 key、提示词审核、三道张数上限与个人配额、后台接口 `/admin/image-generation`、`/files/{id}/view`、契约；与方案的出入见 §17）。**第一期前端已实现**（回复里的图与「拿来改」、后台「图片生成」页），随 v1.5.0 发布。§16 的十一个问题都已由用户拍板（2026-11-05）并落到正文，没有待定的问题。相关：[usage-and-limits.md](usage-and-limits.md)（账本、预留、容量上限）、[storage-and-files.md](storage-and-files.md)（存储、配额、收据、清理、库）、[moderation.md](moderation.md)、[plugins-plan.md](plugins-plan.md)（`tool_images`、工具进度）、[conversations.md](conversations.md)（改写/重新生成删行）。

## 0. 结论先行

- **入口：第一期只做工具。** 给聊天模型一个 `image_generate` 工具（照 Open WebUI 原生函数调用模式的 `generate_image`/`edit_image`、ChatGPT 的做法），模型自己写提示词、决定比例，用户用自然语言「再亮一点」「把猫换成狗」反复改。任何能调工具的聊天模型都能用，出图的是管理员在后台指定的**图片模型**（`model_overrides.kind = 'image'`）。后台开着就在所有对话里提供、所有人都能用，由模型自己判断什么时候出图，没有「+」菜单开关；限制由管理员在后台设。「在选择器里直接选图片模型对话」放第二期（§3）。
- **上游第一期两种**：OpenAI 形状的 `POST /images/generations` 和 `/images/edits`（OpenAI gpt-image、xAI、火山方舟 Seedream、智谱、硅基流动、各家中转都说这个），以及 Gemini 原生 `generateContent` + `responseModalities: ["IMAGE"]`（Nano Banana 系列）。Responses 的 `image_generation` 托管工具、OpenRouter 的 `modalities`、DashScope 原生异步接口都不在第一期（§4）。
- **生成的图是用户的文件**：存成 `attachments` 行（挂在工具结果那条消息上），进存储驱动、算存储配额（`roomFor`）、进「库」、能下载；改写、重新生成、删对话、临时对话过期、删账号时随消息走现成的「先收集 `storage_key` 再删文件」（§6）。不用 `tool_images`（那是 bytea、不算配额、不进库）。
- **审核**：模型写的提示词送审（新来源 `image_prompt`，**不算违规**，和 `task_run` 一样）；用户图片作为编辑原图时只认发送时的收据；生成结果不审，也不设开关（§7）。
- **计费**：新 kind `image`；服务商报了 token 用量且目录有价就按 token（gpt-image、Gemini 都报），否则按「每张价格」（模型设置里新增，管理员填；填了就以它为准）；发请求前按「张数 × 单张估价」预留，容量上限照常生效；张数限制（每次、每 run、每人每天）由管理员设（§8）。
- **范围**：主对话、临时对话、定时任务给；子 Agent、深度研究不给（§9）。
- **前端**：工具步骤显示进度，图片以大图直接画在回复里（不是折叠在工具步骤下），点开用现有的 `ImageViewer`，带下载和「拿来改」（§10）。视觉稿先出草图。

## 1. 别人怎么做

| 产品 | 做法 | 我们取什么 |
|---|---|---|
| Open WebUI（v0.13.0，读过 `routers/images.py`、`utils/middleware.py`、`tools/builtin.py`） | 后台「图片」一页：一个全局引擎（OpenAI 形状 / Gemini / ComfyUI / A1111），**自己的** base URL 和 key（不复用聊天连接）、一个模型、默认尺寸、附加参数 JSON；编辑另配一套；按用户组的 `features.image_generation` 权限。聊天里两条路：①输入框的「图片」按钮：发送前直接拿最后一条用户消息（可选让任务模型改写成提示词）出图，图片作为消息文件推给页面，再往系统消息里塞一段 `<context>图已展示…</context>` 让聊天模型接着说；最近两条消息里有图就改走编辑。②原生函数调用时不走①，模型拿到内置工具 `generate_image(prompt, size)` / `edit_image(prompt, image_urls, size)`，结果告诉模型「图已经显示给用户了」。图片存成文件、挂在对话消息上 | 工具形态与「已显示，别再贴」的结果写法；后台一个图片模型 + 默认尺寸 + 附加参数；生成与编辑合在一处配置。不取：另配 key（我们复用渠道）、按钮强制出图 |
| ChatGPT | 图片生成是聊天模型的内置能力，用户说「画…」就出图，在同一对话里接着改；图片进「库」 | 用自然语言迭代、进库 |
| LobeChat、Cherry Studio（公开文档，未读源码） | 单独的「绘画 / AI 图片」页面：选模型、比例、张数，按话题保存历史；聊天里另有出图插件 | 第二期「直接选图片模型」的参考：比例、张数这些参数放在输入框上 |
| LibreChat（公开文档） | Agent 工具 `image_gen_oai` / `image_edit_oai` 等，按服务商各一个工具 | 不按服务商拆工具，一个 `image_generate` 由后台决定用谁 |

## 2. 现状：能直接复用什么

| 已有 | 现在是什么 | 怎么用 |
|---|---|---|
| `model_overrides.kind = 'image'`、`catalog.KindImage` | 只用来把模型挡在聊天列表外（`model_resolver.go`、`model_settings.go` 只放行 `text`） | 第一期就是「图片模型」的身份：后台选图片模型只列这类，`pickChannel` 要加一个按 kind 的变体（§5） |
| `catalog.DefaultKind` | 输出模态**只有** image 才算图片模型 | **要修**：models.dev 的 `gpt-image-1.5`、`gpt-image-1-mini`、`chatgpt-image-latest` 输出写的是 `{text,image}`，现在被当成文本模型列进聊天，选了就报错。models.dev 条目里有 `"type": "image"`（`raw`），改成优先读它。Gemini 的 `*-image` 也是 `type: image`，第一期随之变成图片模型（它们现在作为聊天模型只会回文字，图片被 `gemini/decode.go` 丢掉） |
| 目录里出图的模型 | 本地快照：openai 4 个（gpt-image-2 有 token 价 5/30）、google 6 个（gemini-3.1-flash-image 0.5/60 等）、xai `grok-imagine-image`（没价格）、阿里 `qwen-image-2.0`/`wan2.7-image`（没价格）、各中转若干 | 有 token 价的按 token 计；没价格的靠新增的「每张价格」（§8）；models.dev 漏收的照例补进 `internal/catalog/supplement.json` |
| `/conversations/{id}/images` | 代取**回复里界面组件引用的外部图片**（只认本对话工具结果里出现过的地址），不是生成的图 | 不复用，别混名字：生成的图走附件的签名地址 |
| `tool_images` | 插件/浏览器工具返回的图片，bytea 存库、随 run 级联删、不算配额、不进库 | 不复用（理由见 §6）；只借它的「按字节认类型，只收 PNG/JPEG/WebP」和「结果里只放引用」 |
| `attachments` + `attachmentResolver` + 收据 | 用户上传：签名读写地址、配额、库、清理、SHA-256 收据 | 生成的图就是附件行；编辑时把用户图片发给图片模型必须走 `attachmentResolver.one` |
| `ImageViewer`（`components/chat/ImageViewer.tsx`、`stores/image-viewer-store.ts`） | 附件、库、GenUI 的看大图 | 直接用 |
| `agent.ReportProgress`、`tool_progress` | 工具运行中给页面报进度 | 「正在生成…」和已用时间；第二期放部分预览 |
| 渠道、`servingOrder`、`failover` | 聊天请求按优先级/权重选渠道、没输出就换渠道 | 图片请求同样按 `servingOrder` 走、同样的换渠道规则（§5） |

## 3. 入口与交互

**第一期：`image_generate` 工具**（2026-11-05 定）。后台开了图片生成、指定了图片模型时，所有人的主对话、临时对话和定时任务的 run 都多一个 `image_generate`，由模型判断什么时候用（ChatGPT 的做法）；不做「+」菜单开关，也不设「只给管理员」。管理员要限制普通用户，用张数上限（§8）；要整个只留给管理员，把图片模型的「向普通用户开放」关掉即可（`pickImageChannel` 会照常拒绝，工具对普通用户就不提供）。是否提供在 run 第一次请求时定下、记在 `Scope.State`，到 run 结束不变（和 `browser` 一样：中途变了工具，思考块签名会失效）。

为什么先做工具，不先做「选图片模型直接对话」：

- 工具路线对所有聊天模型、所有上游都一样，适配器只是「给提示词和原图，拿回图片字节」，第一期就能覆盖 §4 的两类上游；
- 迭代修改靠聊天模型理解上下文（「刚才那张，背景换成夜景」），它自己把指代翻译成完整提示词和原图 id；纯图片模型做不到这个；
- 计费、审核、配额都集中在一个工具的执行里，好管。

**第二期：在选择器里直接选图片模型。** 模型选择器多一组「图片」，选了以后这个对话的每条消息直接出图（Open WebUI 按钮那条路、LobeChat 的绘画页）：

- 只出图的模型（gpt-image-2、Seedream、grok-imagine）：起一种「图片 run」（`runs.mode = 'image'` 一类的标记），不调聊天模型，用户这条消息的文字就是提示词，带图就是编辑；照常经 `chooseRun` → 审核 → `lockLive` → `requireRevised` → `prepareSnapshot` → `createRun`，助手消息里放生成的图。输入框上加比例、张数。
- 能聊也能出图的（Gemini Nano Banana、OpenRouter 的 `modalities`）：当作聊天模型，适配器要能解出**助手输出里的图片**（新 `llm` 零件），多轮时把之前的图发回去（Gemini 3 的图片零件带思考签名）。工作量明显更大，单列。

## 4. 上游与适配器

新接口 `llm.ImageClient`（`internal/llm/image.go`），和 `ModelClient` 并列，不塞进 `Stream`：

```go
type ImageRequest struct {
	Model   string
	Prompt  string
	Images  []AttachmentPart // 编辑的原图，已由 attachmentResolver 填好 Data
	Count   int
	Aspect  Aspect           // square / landscape / portrait（按模型映射成尺寸）
	User    string           // 和聊天请求同一个 end-user 标识
	Extra   map[string]any   // 模型设置里的附加参数（quality、output_format…）
}
type ImageResponse struct {
	Images      []GeneratedImage // 字节 + 类型；URL 的由适配器下载
	Usage       *Usage           // 服务商报了才有
	ServedModel string
	Revised     string           // 服务商改写过的提示词（dall-e-3），可空
}
```

| 上游 | 端点 | 第一期 | 说明 |
|---|---|---|---|
| OpenAI 形状 | `POST {base}/images/generations`（JSON）、`/images/edits`（multipart，`image[]`） | 做 | `providers/openai/images.go`。gpt-image 总是回 `b64_json`；别家可能回 `url`（智谱、xAI 可选）或 `images[].url`（硅基流动）：解码器都认。`url` 的下载：与渠道 base 同源的带渠道认证，其余经 `tools.GuardedClient`（只连公网），限 20 MB、按字节认类型。`user` 字段只在渠道「发送用户标识」开着时发。服务商安全拒绝（OpenAI 400 `moderation_blocked`）映射成工具错误，不当故障 |
| Gemini 原生 | `POST .../models/{m}:generateContent`，`generationConfig.responseModalities: ["IMAGE"]`、`imageConfig.aspectRatio` | 做 | `providers/gemini/images.go`。原图作为 `inlineData` 放进同一条用户消息；回来的 `inlineData` 就是图；`usageMetadata` 报 token |
| OpenAI Responses `image_generation` 托管工具 | 聊天请求里 `tools: [{type: "image_generation"}]` | 不做 | 图在聊天模型那次请求里生成，出几张、花多少由 OpenAI 决定，预留、每天张数、提示词审核都插不进去；只有官方渠道有。§3 的工具已覆盖同样的能力，以后真要做再单独评估 |
| OpenRouter `modalities: ["image","text"]` | Chat Completions，回 `message.images[]` | 第二期 | 属于「能聊也能出图」 |
| DashScope 原生（qwen-image、wan） | 异步任务接口 | 以后 | 先靠说 OpenAI 形状的中转 |
| Anthropic | 无 | — | |

- **哪些渠道能出图**：按渠道类型的协议（`WireApi`）选适配器：OpenAI 协议的类型走 `/images/*`，`gemini` 走原生；`anthropic` 类型的渠道上的图片模型在后台标「这个渠道不能出图」。渠道类型表（`GET /admin/channel-types`）加一列 `images`，模型设置里提示。
- **比例与分辨率 → 各家的参数**（2026-11-05 用户要求能定分辨率后加）：工具给模型两样：`aspect`（`auto` 或 1:1、3:2、2:3、4:3、3:4、16:9、9:16、21:9，也认 square/landscape/portrait）和可选的 `size`（精确像素 `WIDTHxHEIGHT`，每边 256–4096，给了就盖过 aspect）。适配器按家族换算（`providers/openai/images.go` 的 `imageFields`、`providers/gemini/images.go` 的 `imageConfig`）：gpt-image 给了 `size` 就原样传，否则按形状取 1024×1024 / 1536×1024 / 1024×1536 或 `auto`；dall-e-3 取它三种里最接近的；Grok 只收比例（`aspect_ratio`，精确尺寸换成最接近的比例）；Seedream 原样传尺寸，否则按约 2048² 的面积算；Gemini 传 `aspectRatio`，给了尺寸再按长边加 `imageSize` 1K/2K/4K（2.5 的图片模型不认档位会报错，模型照实转告）；其余 OpenAI 形状的照传。gpt-image 一族的精确尺寸先按它的限制收进范围（每边取 16 的倍数、单边 ≤ 3840、长边 ≤ 短边 3 倍、总像素 655,360–8,294,400，`gptImageSize`），比如 1920×1080 发成 1920×1088。出来的实际尺寸和要的不一样时，结果里告诉模型。
- **画质与透明背景**（2026-11-05 加）：工具可选 `quality`（low / medium / high / xhigh / max）和 `background: transparent`。没给画质时用后台的「默认画质」，任何画质都压到「最高画质」以内（`imageConfig.DefaultQuality`/`MaxQuality`，空为服务商默认/不限），被压了会在结果里说。管理员也可以「固定画质」（`QualityFixed`，2026-11-05 用户要求）：所有图片都按默认画质出，工具定义里不出现 `quality`；是否提供这一项在 run 第一次请求时随工具一起定下（`Scope.State` 里存的是那一份 `imageTool`），中途改设置只影响下一个 run，执行时若设置已改成固定也照固定的出。
- **复杂度**（2026-11-05 加，GPT Image 2.6 的 `complexity`）：low / medium / high，出图前规划多少；不填时服务商按 high，不另收费，越高越慢、可能改动超出要求。工具可选 `complexity`（提示模型：局部改动用 low，文字多、布局复杂用 high），后台「默认复杂度」和「固定复杂度」与画质同一套（`DefaultComplexity`/`ComplexityFixed`，固定时工具里没有这一项）。只发给 gpt-image 2.6 及以后（`takesComplexity` 按 id 里的版本号判断，没有版本号的如 `chatgpt-image-latest` 不发），别的模型记在 `Ignored`。
- **用户标识**：gpt-image 一族改发 `safety_identifier`（Images API 的 `user` 已弃用），其他 OpenAI 形状的服务仍发 `user`；都只在渠道开着「发送用户标识」时发。SVG、GIF 输出（2.6 新增）暂不规划。只有 gpt-image 一族照传画质，透明背景同时定成 PNG；dall-e-3 把高、更高、最高当 `hd`；别的模型不认就记在 `ImageResponse.Ignored`，结果里告诉模型没生效。画质越高 token 越多，按 token 计费的自然算进去，估算和预留按画质查表（`PictureTokens`）。
- **超时**：图片请求单独的超时（默认 3 分钟，gpt-image 高质量常要 30–90 秒），不套用聊天的首 token 超时。

## 5. 给模型的工具 `image_generate`

- **参数**：`prompt`（必填，完整描述，不是「改成蓝色」这种增量——提示词里写明）、`aspect`（比例，见 §4）、`size`（可选，精确像素）、`count`（1 到后台的每次上限，默认 1）、`images`（可选，原图 id 列表，有就是编辑）。描述写英文（AGENTS.md「写给模型的文字一律英文」）。
- **原图 id**：生成的图在结果里给 id；用户发的图片在上下文里也要有 id，`agent.BuildContext` 在每张用户图片前加一行 `[Image img_ab12cd34: cat.png]`（**不论这个 run 有没有图片工具都加**，这样同一对话的历史字节稳定；上线后各对话的提示缓存失效一次）。id 是附件 uuid 的前缀，只在本对话里解析：本对话非 `blocked` 的用户消息里的图片、本对话生成的图片，读字节一律经 `attachmentResolver.one`（所有者 + SHA-256 收据），对不上就报错给模型。
- **执行顺序**（`api/image_tool.go`，一次调用）：
  1. 读设置、取 `Scope`；子 Agent 不会拿到这个工具（§9），兜底再拒一次；定时任务的 run（`runs.unattended`）照常执行——这个工具不需要用户确认；
  2. 每人每天张数：`LockImageQuota`（按用户的 advisory xact lock）下数今天的 `image_requests` 行、插一行 `pending`（§8）；
  3. 存储：`roomFor(count × 单张预留字节)` 只检查不写（钱花出去之前先确认存得下）；
  4. 提示词审核（§7）；
  5. `pickImageChannel(ctx, imageModelID, asker)`：和 `pickChannel` 同一套（`admin_only`、容量、`servingOrder`），只是要求 `kind = 'image'`；`asker` 按 run 的用户构造（`staff`/`admin` 取这个人是不是管理员），不是 `askerFrom(ctx)`——工具跑在后台 run 里，没有请求上下文；
  6. 构造 `llm.ImageRequest`（带 `User`）→ `Begin(Call{Kind: image, InputEstimate, Images: count})` → `Reserve`（含输出预留，§8）→ `Generate` → `End`；没拿到任何图就失败的，换下一个渠道重试（`servingOrder` 的下一个，和聊天的 `failover` 同一规则），换渠道每次重新 `Begin/Reserve`；
  7. 存图：每张按字节认类型（只收 PNG/JPEG/WebP，解码一次拿宽高）、一个事务里 `LockUserStorage` → `roomFor(实际大小)` → 插 `attachments` 行（`message_id` 为空、`origin = 'generated'`、`run_id`、记 SHA-256）→ 提交 → `Local.PutNew`/S3 写；写失败删行；
  8. `image_requests` 行改成 `done`（记实际张数）或 `failed`（不占当天名额）；
  9. 返回 `ToolResult`：`Content` 写「生成了 N 张，id 是…，已经显示在你的回复里，不要用 Markdown 再贴、不要逐张描述；要改哪张就带上它的 id 再调用」；新字段 `Generated []llm.GeneratedRef`（附件 id、类型、宽高）给页面。
- **认领**：agent 写这条工具结果行时，在同一个 commit 里把 `Generated` 里的附件行的 `message_id` 设成这一行（`ClaimGeneratedImages`，按 `run_id` + `user_id` 限定）。进程在第 7 步和认领之间死掉，留下的是没被认领的行，24 小时后由 `DeleteStaleUploads` 连文件一起清掉，不用另写清理。
- **模型看不看生成的图**：第一期不看（每张图再按输入收一次钱，而迭代靠 id 和提示词已经够）。要「画完自己检查」再加开关，`Generated` 可以像 `ToolResultPart.Images` 一样被 `BuildContext` 放进下一条消息。
- **并发与重放**：可以和别的工具同时跑（同一轮几张图并行出），不实现 `SequentialTool`；`Replayable() = false`（重跑会再花一次钱），进程重启时是 `interrupted`。
- **进度**：开始时 `ReportProgress`「正在生成 N 张图片」，页面自己数已用时间；第二期 OpenAI 的 `partial_images` 预览也走这里（只给页面，不进上下文）。
- **系统提示词**：提供工具时加一小节（英文）：只在用户要图时调用；写完整的提示词；用户说「改一下」时带上原图 id；出图后简短说明、不重复贴图；被服务商拒绝时照实告诉用户。

## 6. 存储与清理

- **为什么是 `attachments` 而不是 `tool_images`**：生成的图是用户要留着的东西——要进「库」、要下载、要算配额；几 MB 一张放进 Postgres 的 bytea 会把库撑大；而 `attachments` 的删除路径早就都会收集 `storage_key`：`DeleteTranscriptFrom`（改写）、`DeleteExpiredTemporaryConversations`、`PurgeDeletedConversations`、`DeleteAccount`、`DELETE /files/{id}` 都是按 `message_id` 或 `user_id` 连带找文件，挂在工具结果行上的图自动跟着走。
- **要补的一处**：`DeleteAnswerTo`（重新生成）现在不返回 `storage_key`（注释说「文件只挂在用户消息上」）。要改成 `RETURNING` 被删行的附件 key、提交后 `removeFiles`，并改注释。这是本功能唯一需要改的现有删除路径，测试要覆盖「重新生成后旧回答的图被删」。
- **迁移**（`make migration name=image_generation`，排在所有已有迁移之后）：`attachments` 加 `origin text`（NULL = 用户上传，`'generated'`）、`run_id uuid REFERENCES runs ON DELETE SET NULL`、`sha256 text`、`width`/`height`；`image_requests` 表（§8）；`usage_logs` 加 `images integer NOT NULL DEFAULT 0`、`image_price numeric`，`usage_logs_kind` 约束加 `image`；`model_overrides` 加 `image_price numeric`；`users` 加 `image_daily integer`（可空，`CHECK (image_daily >= 0)`）；`moderation_events` 的 `source` 约束加 `image_prompt`。
- **存储 key**：`uploads/<用户>/<uuid>.<ext>`，在孤儿清扫的范围内（行和文件对得上就不删），和用户上传同一个前缀，`ListKnownStorageKeys` 照常认。
- **配额**：生成的图算进这个人的存储（`GET /me/storage` 的「图片」类）。满了：工具在出图**之前**就报「存储已满」（第 3 步），模型照实转告，页面在工具步骤里显示「空间已满」。
- **上传的「认领」接口不能认领生成的图**：`admitAttachments` 要排除 `origin = 'generated'`，用户想拿生成的图去改，走「拿来改」（§10）引用它的 id，而不是当成新上传。
- **库**：`ListLibraryFiles` 不用改就能列出（它不限角色）；页面在图片卡上标「生成」，筛选多一项「生成的图片」。

## 7. 审核

- **提示词送审**（`moderate`，来源 `image_prompt`）：提示词是模型写的，不是用户新发的输入，按现在的规则可以不审；但它会以运营者的账号发给图片服务商，而图片服务商对违规提示词比聊天更敏感（OpenAI 会因反复的 `moderation_blocked` 封号）。所以审，但和 `task_run`、`snapshot` 一样**不算违规**、不处罚用户（2026-11-05 定：模型写的不算；`CountUserStrikes` 排除 `image_prompt`）；被拒时工具返回错误「这个提示词没有通过内容审核」，模型照实转告。审核关着就不审。审核服务出错按 `moderation_on_error`；提示词太长按 `errTooLargeToCheck`（工具参数上限 4000 字，一般到不了）。
- **原图**：用户发的图片在发送时审过、记了收据，编辑时只认收据（`attachmentResolver.one`），不再审；生成的图本来就是我们写的，记了 SHA-256。
- **生成结果不审**（2026-11-05 定），后台也不设开关：上游自带安全过滤，每张图再调一次多模态审核要钱、要 5–12 秒。所以没有 `image_result` 来源。
- **服务商的安全拒绝**不是我们的审核：不写 `moderation_events`，工具错误里说「图片服务拒绝了这个请求」，账本照常记一行 `error`。

## 8. 计费与上限

- **账本**：每次图片请求一行 `usage_logs`，`kind = 'image'`，`model_id` 是图片模型的请求 id（容量上限按它算），`images` 是存下的张数。
- **价格**：
  1. 服务商报了 token 用量、目录有 token 价 → 按 token 算（gpt-image 回 `usage.input_tokens/output_tokens`，Gemini 回 `usageMetadata`），和聊天一样；
  2. 否则模型设置里有「每张价格」（`model_overrides.image_price`，美元，numeric）→ `images × image_price`，行里记下当时的单价；
  3. 都没有 → `cost` NULL，模型设置面板提示「没有价格，容量上限不会触发」，和现在没价格的模型一样。
  - **管理员填了单价时以单价为准**（2026-11-05 定：第 2 条优先于第 1 条，即使服务商报了 token），行为可预期。也是因为 gpt-image 的输入图片 token 价（编辑时）比文字贵，目录只有一个输入价，按 token 算编辑请求会少算一点，要准就填单价盖过去。
- **估算**（`usage.Meter.settle` 的扩展）：请求被服务商接手但没拿到用量（取消、中途断）时，按请求的张数估：有单价用单价，否则按模型家族的每张 token 估计（gpt-image 按尺寸和质量查表，Gemini 每张 1290 token）× 输出价，标 `estimated`。用户在出图途中停掉，服务商多半照样收钱，所以照常记。
- **预留**（`spend_reserve.go`）：现在只预留「输入估算 × 输入价」，对图片几乎是零。`usage.Call` 加 `Images int`，`Reserve` 对 `kind = image` 再预留「张数 × 单张估价」（同上一条的估价），`ModelWouldReach` 一并比较。没价格的模型不预留，和现在一样。
- **限制由管理员设**（2026-11-05 定：所有人都能用，管理员可设限制）。三道张数上限都在后台「图片生成」页（§11），管理员自己不受每天张数限制。
- **每人每天张数**：全站默认 `app_settings.image_daily`（**默认 0 = 不限**，2026-11-05 定），后台「图片生成」页改；某个人单独的配额是 `users.image_daily`（NULL = 用全站默认，0 = 不限，N = 每天 N 张），后台「用户」编辑对话框里改，`UpdateUserRequest.imageDaily = -1` 恢复默认——和存储配额（`storage_quota` / `users.storage_quota_bytes`）同一个套路。生效的数 = 个人的（不为 NULL 时）否则全站的；管理员始终不限，对话框里管理员这一项不可编辑。后台用户列表不加列（第一期），用户详情显示「今天已用 / 配额」。计数方式：`image_requests(id, user_id, run_id ON DELETE SET NULL, count, status, created_at)`，在 `LockImageQuota` 下数「今天（服务器时区零点起）不是 `failed` 的行的 `count` 之和」再插，超了工具返回「今天的图片额度用完了」，页面工具步骤里显示同一句。run 被删不退名额（和 `research_starts` 一样），失败的退。
- **每次张数上限**（`image_max_per_call`，默认 4）、**每个 run 的张数上限**（`image_max_per_run`，默认 8，在 `Scope.State` 里数）：防止模型在一个回复里循环出图；这两道对所有人（含管理员）生效，因为防的是模型而不是人。
- **定时任务**：和对话里一样计费、一样受这三道上限和容量上限管；每天张数用完时这次任务的回答里写明，不暂停任务。
- **并发**：图片调用跑在 run 里，已经受 `max_concurrent_runs` 管；同一 run 里同时的图片调用受 `ToolConcurrency` 管，不另设。
- **使用日志页**：类型筛选多「图片生成」，列表里图片行显示张数。

## 9. 和现有功能的关系

| 功能 | 给不给 | 说明 |
|---|---|---|
| 主对话 | 给 | 后台开着就给所有人（图片模型对普通用户关闭时除外） |
| 临时对话 | 给 | 图挂在消息上，24 小时后随对话删（现成的清理会收 key）；不进库 |
| 子 Agent | 不给 | 子 Agent 的输出用户看不到，图没地方显示；按「新工具默认不给」 |
| 深度研究（计划/研究/研究员） | 不给 | 报告是写作块文字 |
| 定时任务 | 给（2026-11-05 定） | run 经 `startTaskRun` 起、是 `unattended`，工具不需要确认所以照常可用；图落在任务专属对话里（进库）；照样受三道张数上限和容量上限管（§8）。任务的提示词说「每天画一张」时每次都花钱，任务页不另加提示（第一期） |
| 手机 App | 第一期要能显示 | App 用同一份 `chat-core` 的转录，工具结果里多了 `generatedImages`；App 画缩略图、点开全屏、长按保存。没做完之前 App 上显示「图片 · 在网页版查看」占位，不能什么都不显示 |
| 上下文压缩 | 不受影响 | 摘要里只有工具结果的文字（id 和提示词），压缩后 id 仍能解析（附件行还在） |
| 改写 / 重新生成 | 图随行删 | §6；重新生成那条要补 `RETURNING` |
| 导出 | 只有文字 | 导出现在只含文本和附件文件名；生成的图记成「[生成的图片 文件名]」 |
| 搜索、引用其他对话 | 不变 | 引用时模型读到的是转录文字，生成的图写成一行说明 |
| 写作块 | 不嵌图 | 第一期不支持把图插进文档 |
| 请求轨迹 | 记录 | 图片请求也写 `request_traces`（只存引用，不存字节），轨迹视图里是一种新颜色的段 |

## 10. 前端

> 视觉部分先给 2–3 个草图（放 scratchpad），定了再写；只做草图里定下的东西。

- **显示位置**：图画在**回复正文里**、工具调用所在的位置，不藏在可折叠的工具步骤下（工具步骤照常一行「生成图片 · 12 秒」，可展开看提示词）。一张时按原比例、最大宽度跟正文；多张两列网格。成图用草图 `image-mockups/chat-images.html` 的 **A「大图直出」**（2026-11-05 定）：无边框、圆角，下载和「拿来改」是悬停时右上角的两个小按钮（手机上长按）。新组件 `GeneratedImages`（`components/chat/`，`n-` 类名、`--n-*` 令牌）。
- **生成中的占位：「显影」**（2026-11-05 用户从三版草图里选定，草图是 scratchpad 的 `image-mockups/generating.html`）：卡片按要的比例先画出来（不跳布局），里面是一个 WebGL 片元着色器画的低饱和流动色场（域扭曲的 fbm，浅色/深色各一套调色），起初朦胧发灰，整幅画面一起「对焦」：细节一层层出来、颜色同时变饱满、画面略微收拢（缩放 1.06 → 1），没有方向、没有扫描的亮边（2026-11-05 用户否掉了从上往下扫的柔光，从「对焦 / 斑块显影 / 先色后形」里选了对焦，草图 `image-mockups/develop.html` 的第一个）；约 9 秒一轮（变清晰 → 停 → 退回朦胧）；**每一轮对焦出来是另一套五色调色**（日落、深海、森林、晨雾粉、琥珀、暮色，依次轮换，在完全朦胧时换，用户选定的是草图 `image-mockups/focus.html` 的第三个「每轮换一套」：清晰时颜色要比最初那版丰富），朦胧时都是同一种浅灰（换调色时要保证朦胧态的亮度不跳）；表面一层很轻的胶片颗粒。卡片下方左边「正在生成图片」（文字上一道光扫过），不显示尺寸和秒数（2026-11-05 用户要求去掉；卡片仍按要的尺寸或比例画，auto 时画成方形）。提示词不显示（是模型写给图片模型的英文，2026-11-05 用户要求去掉）。实现要点：一张卡一个 canvas、一个着色器程序，按 `devicePixelRatio`（至多 2）渲染，卡片离开视口或标签页隐藏时停帧（`IntersectionObserver` + `visibilitychange`），同屏多张共用一个 WebGL 上下文或限制个数；拿不到 WebGL 时退回静态的模糊渐变。它是加载指示器，标 `data-motion="essential"`；`prefers-reduced-motion` 时色场不流动、只保留缓慢的对焦。出图时不换卡片（按位置做 key，同一块色场一直在流动）：图片加载完才从模糊、偏淡、略大「对焦」到原样，盖在色场上（约 1.1 秒），之后撤掉色场，「正在生成图片」那一行淡出并收起（约 0.5 秒）；按计时走，不靠 `transitionend`（实测有时晚到好几秒）。减少动效时只做 0.25 秒的淡入。从历史记录打开的对话直接显示图片，不播这段。
- **交互**：点开用现有 `ImageViewer`；悬停（手机上长按）有「下载」（`GET /files/{id}/download`，原图）、「拿来改」（把这张图作为引用放进输入框：输入框里显示一个小缩略图芯片，发送时在消息开头加 `<image-ref id="img_…"/>` 一行，和技能行一样在显示、编辑、复制前拆掉，系统提示词说明它）。被从库里删掉的图显示「图片已删除」。
- **数据**：`ToolResultPart` 的 API 形状加 `generatedImages: [{id, mediaType, width, height, url}]`，`url` 由服务端和附件同一套签名地址生成（`attachmentsFor` 的同类逻辑）；进度走现有的 `tool_progress`。所有字符串走 `t()`（`zh`、`en`）。
- **工具步骤的错误**：额度用完、存储满、审核拒绝、服务商拒绝，各一句人话，不弹 toast（这是回复的一部分，模型也会说）。
- **第二期**：选择器里的「图片」组、输入框上的比例/张数、部分预览。

## 11. 后台

- 后台 → 系统 → 新页签「图片生成」（shadcn，react-hook-form + zod；版式用草图 `image-mockups/admin-page.html` 的**方案 1**，2026-11-05 定：和「浏览器」页一样的分节——开关行、图片模型、限制、测试，不加顶部概况）：总开关（`image_generation`，默认关；开着就给所有人，不设「谁能用」）；图片模型（下拉，只列 `kind = 'image'` 且有渠道能出图的；保存时用 `asker{staff, admin, ignoreCap}` 校验能答；页面提示「要只给管理员用，关掉这个模型的『向普通用户开放』」）；每次张数上限、每个 run 张数上限、每人每天张数（全站默认，0 = 不限）；测试按钮（用最便宜的设置出一张、不进任何对话、记一行用量，`kind = image`、没有对话）。
- 用户编辑对话框：「每天图片配额」（留空 = 用全站默认，0 = 不限），和「存储上限」放在一起；用户详情显示今天已用张数。
- 模型设置（图片模型）：「每张价格」、附加参数（JSON）、渠道能不能出图的提示；「恢复默认」一并清掉每张价格。
- 使用日志：类型筛选「图片生成」、张数列。
- 前端 `ChatFeatures.imageGeneration`（这个人能不能用）：只影响「拿来改」等入口显示；工具给不给由服务端决定。

## 12. 要改的文件

**第一期**

- 契约：`api/openapi.yaml`（`ToolResultPart.generatedImages`、后台图片设置的 GET/PATCH 与测试、`ModelSettings.imagePrice`、`UpdateUserRequest.imageDaily` 与用户详情的今日用量、`UsageLog.images`、`UsageKind` 加 `image`、`ChatFeatures.imageGeneration`、新错误码说明），`make generate-api`。
- 迁移：`make migration name=image_generation`（§6 列的列和表、三个 CHECK 约束）。
- 目录：`internal/catalog/settings.go`（`DefaultKind` 读 `raw.type`）、`store.go`；测试。
- `internal/llm/image.go`（`ImageClient`、`ImageRequest/Response`、比例映射、每张 token 估计）。
- `internal/providers/openai/images.go`、`internal/providers/gemini/images.go` + 测试（`testdata` 里放录下的响应）。
- `internal/usage/usage.go`（`Call.Images`、`settle` 的图片估算、按张计价）、`internal/api/spend_reserve.go`（输出预留）。
- `internal/api/image_tool.go`（工具）、`image_settings.go`（后台设置、测试按钮）、`model_resolver.go`（`pickImageChannel`）、`model_settings.go`（每张价格）、`admin.go` 的 `UpdateUser`（个人每天配额）、`document_tool.go` 的 `ServerTools`（挂上 `ImageTools`，子 Agent 分支不加）、`agent_setup.go`（系统提示词一节）、`mapping.go`（`generatedImages`）、`messages.go` 的 `attachmentsFor` 同类签名、`admitAttachments`（排除生成的图）、`usage_logs.go`（筛选）。
- `internal/agent`：`tools.go`（`ToolResult.Generated`）、`execution.go`（写工具结果行时认领）、`context.go`（用户图片前的 id 行）、`attachments.go`（按短 id 解析）。
- 查询：`queries/attachments.sql`（插入生成的图、认领、按短 id 找）、`queries/messages.sql`（`DeleteAnswerTo` 返回 key）、`queries/image_requests.sql`、`queries/usage_logs.sql`。
- 前端：`packages/chat-core`（转录类型、`image-ref` 行的拆解与测试、语言文件）、`apps/web/src/components/chat/GeneratedImages.tsx`、`ToolActivity.tsx`、`MessageItem.tsx`、`Composer.tsx`（「拿来改」芯片）、`LibraryPage.tsx`（生成标记与筛选）、`features/admin/`（图片生成页、模型设置、用户编辑里的每天配额、使用日志）；`apps/mobile`（显示与占位）。
- 文档：本文改成实现记录；`AGENTS.md` 加「图片生成」一节；`docs/design/README.md` 索引。

**第二期**：`runs` 的图片 run 标记与执行器、选择器分组、输入框参数；`llm` 的助手输出图片零件、Gemini/OpenRouter 解码；OpenAI `partial_images` 预览。

## 13. 要过的约定（清单）

- 模型请求：先构造 `llm.ImageRequest`（带 `User`）→ `Begin(Call{InputEstimate: …, Images: n})` → `Reserve` → `Generate` → `End`；`usage_logs_kind` 加 `image`。
- 挑模型：`pickImageChannel` 是 `pickChannel` 的同族，容量检查在里面；`asker` 按 run 的用户构造；按 `servingOrder` 换渠道，别只取第一个。
- 起 run：第一期不新增起 run 的路径（工具跑在已有 run 里）。第二期的图片 run 必须 `lockLive` → `requireRevised` → `prepareSnapshot` → `createRun`，审核在 `chooseRun` 之后、事务之前，从 App 发起的照样记 `from_app`。
- 工具：能并发；子 Agent 默认不给（`ServerTools` 的子 Agent 分支不加）；是否提供在 run 第一次请求时定下（`Scope.State`）；写给模型的文字英文；模型用错了改提示词和工具描述，不在服务端改它的参数。
- 存储：新增存文件的路径在同一事务里过 `roomFor`（`LockUserStorage` 后再数）；附件行消失的地方先收集 `storage_key`（补 `DeleteAnswerTo`）；用户文件发给模型/图片服务必须经 `attachmentResolver`；本地写用 `Local.PutNew`；生成的图放 `uploads/`，不放 `avatars/`。
- 审核：提示词用 `moderate`，来源 `image_prompt` 不算违规；太大包 `errTooLargeToCheck`；判定要完整（`verdictFrom`）。
- 上游下载图片地址经 `GuardedClient`（同源渠道地址除外）。
- 金额 `numeric`、在 SQL 里比较；容量按请求的 `model_id`。
- 前端：字符串走 `t()`；`n-` 设计系统；动效遵守 `prefers-reduced-motion`；不建淡完不回收的逐元素动画；回复零件的 key 仍按位置 `r<n>`。
- 日志带 ctx，后台 run 用 `ex.logger()`；审计不记提示词原文以外的东西——`usage_logs` 不存提示词，提示词只在消息转录（工具调用参数）里。

## 14. 分期

| 期 | 内容 |
|---|---|
| 0 | `DefaultKind` 读 models.dev 的 `type`（独立的小修，先做：现在 gpt-image-1.5 等被错列成聊天模型） |
| 1 | `image_generate` 工具（生成 + 编辑）；OpenAI 形状与 Gemini 原生两个适配器；附件存储、认领、`DeleteAnswerTo` 补 key；提示词审核；`image` 用量、按 token/按张计价、输出预留、每天张数；后台图片生成页与每张价格；网页版显示、下载、「拿来改」、库；App 显示 |
| 2 | 选择器里直接选图片模型（只出图的走图片 run，Gemini/OpenRouter 走聊天 + 输出图片零件）；输入框比例/张数；OpenAI 部分预览 |
| 3 | 局部重绘（遮罩）界面、DashScope 原生、把图插进写作块；视需要评估 Responses `image_generation` |

真实调用测试用最便宜的组合：聊天模型 Claude Haiku 5.5，图片模型挑渠道里单价最低的（如 `gemini-3.1-flash-lite-image` 或 `gpt-image-1-mini` 低质量）。

## 15. 风险

- **花钱快**：一张高质量 gpt-image 约 0.1–0.2 美元，模型自作主张出图或循环出图会很贵——靠提示词「只在用户要图时」、每次/每 run/每天三道张数上限、容量上限兜底。
- **价格不准**：多数非 OpenAI/Gemini 的出图模型在目录里没价格，不填「每张价格」就是免费、上限不触发；后台模型卡片和图片生成页都要醒目提示。
- **上游形状不一**：「OpenAI 兼容」的图片接口各家字段不同（尺寸写法、`url`/`b64_json`/`images[]`），适配器要按录下的真实响应写测试，不认识的形状报清楚的错，不猜。
- **慢**：30–90 秒一张，用户可能以为卡住了——进度行带已用时间，占位按比例先画出来。
- **账号风险**：提示词审核挡住一部分；服务商的安全拒绝照实转告、计入账本，后台能在使用日志里按错误码看到。

## 16. 已定与待定

已定（2026-11-05，用户拍板）：

1. **入口**：第一期只做 `image_generate` 工具，背后的图片模型由管理员在后台设定（§3、§11）。
2. **工具什么时候给**：后台开了就给所有对话，由模型自行决定使用，不做「+」菜单开关（§0、§3、§5）。
3. **谁能用**：所有人都能用，管理员可设置限制（§3、§8、§11）。
4. **每人每天张数**：同 3，由管理员设（§8）。
5. **定时任务**：给（§5 第 1 步、§8、§9）。
6. **生成的图**：不审，不设开关（§7）。
7. **提示词审核**：照审，但模型写的不算违规（§7）。
8. **单价与 token 都有时**：以管理员填的每张价格为准（§8）。
9. **Gemini 的 `*-image` 模型改成图片模型**：可以，第 0 期起不在聊天列表里，第二期再能直接对话（§2、§14）。

10. **每人每天张数的全站默认**：0 = 不限（§8、§11）。
11. **按人设配额**：第一期就做，照存储配额的套路，后台用户编辑里改（§6 迁移、§8、§11、§12）。

目前没有待定的问题。

## 17. 第一期后端实现记录（2026-11-05）

与上文的出入：

- **页面拿图不走附件列表，走 `details` + 查看地址**：事件流里的消息不带附件（`MessageAppended` 只有行），所以工具结果的 `details.images` 给出每张图的附件 id、句柄（`img_…`）、类型和宽高，页面用 `GET /files/{id}/view`（302 到短期签名地址，只认本人、挂在消息上的文件，临时对话的也行）画图。`ToolResultPart` 的契约没有加字段。
- **原图 id 就是附件 uuid 的前 8 位十六进制**（`agent.ImageHandle`），在本对话里按 `ListConversationImages`（本对话非 `blocked`、用户自己发的或本对话生成的图片）解析。
- **运营者的按模型附加参数（`Extra`）还没有后台入口**：适配器支持（`null` 删字段），设置里暂时没有。
- **出图请求的超时**是 4 分钟（`imageRequestTimeout`），适配器的响应头超时 5 分钟。
- 测试：适配器 8 个（httptest），`TestImageTool`（带库：提供条件、出图、存储与收据、按张计价、认领、按句柄编辑、每天配额、`DeleteAnswerTo` 交回 key）。
