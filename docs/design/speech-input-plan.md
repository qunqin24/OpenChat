# 语音输入：方案

> 状态：第一期实现中（2026-11-13 定）。相关：[image-generation-plan.md](image-generation-plan.md)（同一套「专用模型 + 后台一页 + 按量计价」的做法）、[usage-and-limits.md](usage-and-limits.md)（账本、预留、容量上限）、[moderation.md](moderation.md)。

## 0. 结论先行

- **做什么**：输入框里一个麦克风按钮。点一下开始录，再点确认（或取消），录音发给服务端转成文字，文字插进输入框光标处，**不自动发送**——用户看一眼、改一改再发。ChatGPT 的「听写」、Open WebUI 的 `VoiceRecording` 都是这个形态。语音对话（边说边答）不在第一期。
- **用谁转**：管理员在后台「语音输入」页选一个**语音转文字模型**（新的模型种类 `transcription`），第一期适配 OpenAI 形状的 `POST /audio/transcriptions`，先接 `gpt-transcribe`（`whisper-1`、`gpt-4o-transcribe`、各家兼容接口照样能用）。模型挂在渠道上，和聊天、图片模型一样按 `servingOrder` 选渠道、失败换渠道、受容量上限约束。
- **怎么计费**：`gpt-transcribe` 按音频分钟计价（0.0045 美元/分钟），models.dev 没收这个价。我们在自己维护的目录补充（`catalog/supplement.json`，和 `chat-latest` 一样）里内置 OpenAI 的 `gpt-transcribe`（0.0045）和 `whisper-1`（0.006），价格写在 `cost.audio_minute`，开箱就按时长计费。管理员在模型设置里填的「每分钟价格」（`model_overrides.audio_price`）盖过内置价；两者都没有而服务商报了 token 用量（`gpt-4o-transcribe`）就按目录的 token 价计。新用量种类 `transcription`。
- **审核**：录音不审，转出来的文字也不审：它只回到用户自己的输入框，用户发送时照常整条送审。
- **存储**：录音不落盘、不进库，转完即丢。

## 1. 别人怎么做

| 产品 | 做法 | 取什么 |
|---|---|---|
| Open WebUI（`MessageInput/VoiceRecording.svelte`、`routers/audio.py`） | 输入框麦克风 → 录音条（音量波形、计时、取消 ✕、确认 ✓）→ 上传 `/audio/transcriptions` → 文字插进输入框；后台「音频」页选 STT 引擎（OpenAI 兼容、本地 Whisper、Azure、Deepgram、浏览器自带）、模型、另配的 base URL 和 key；个人设置有「转写后自动发送」，默认关 | 录音条形态、确认后插入不发送、MediaRecorder 的类型优先级（`audio/webm;codecs=opus` → `audio/webm` → `audio/mp4`）。不取：另配 key（我们复用渠道）、浏览器自带识别（各浏览器质量和隐私不一）、自动发送 |
| ChatGPT | 输入框麦克风「听写」：波形、计时、✓；文字进输入框不发送；另有语音对话模式 | 同上 |

## 2. 模型种类与价格

- `catalog.KindTranscription = "transcription"`。`DefaultKind`：目录条目 `type` 是 `transcription`（models.dev 的写法），或 id 含 `transcribe`、`whisper`、`-asr` 的，判为语音转文字。这类模型不进聊天列表（和图片模型一样只放行 `text`），管理员也可以在模型页手动改种类。
- 每分钟价：管理员填的 `model_overrides.audio_price` 优先，否则取目录的 `cost.audio_minute`（只有我们的补充会写这个字段；同一模型多个服务商时先取渠道自己的）。`UsagePricing` 合成这一个价，计费、预留、后台「没有价格」的提示都读它；模型页显示内置价（`CatalogModel.audioCost`），设置里的输入框以它为占位。
- 计费（`usage.Meter.EndTranscription`）：
  - 秒数取服务商报的 `usage.seconds`（`type: duration`）；没报就用客户端报的录音时长，标「估算」。
  - 有每分钟价（填的或内置的）：费用 = 秒数 ÷ 60 × 每分钟价，`usage_logs.audio_price` 记下当时的价。
  - 否则服务商报了 token（`type: tokens`）就按目录 token 价算；都没有则费用未知（`cost` 为空）。
  - `usage_logs.audio_seconds` 记秒数（numeric）。
- 预留（`SpendReserver`）：有每分钟价时按客户端报的时长 × 每分钟价预留，否则不预留（同没有价格的模型）。

## 3. 后台「语音输入」页

`/admin/speech-input`，侧栏「通用」组里，在「图片生成」之后。设置存 `app_settings` 的 `speech_input`：

| 字段 | 默认 | 说明 |
|---|---|---|
| `enabled` | 关 | 开着且选了模型，输入框才有麦克风 |
| `model` | 空 | 只列种类为 `transcription`、有渠道在服务的模型 |
| `maxSeconds` | 300 | 一段录音最长多少秒，10 到 1800；到了自动停止并转写 |

页面另显示 `modelPriced`：选的模型有没有每分钟价或 token 价，没有就提示去模型页填，否则用量日志里没有费用。

谁能用：开着、有模型、模型对这个人开放（「向普通用户开放」关着时只有管理员）。和图片生成一样没有单独的「只给管理员」开关。

## 4. 接口

- `GET /features` 多一个 `speechInput: { maxSeconds }`（不能用时没有这个字段），输入框据此显示麦克风。
- `POST /transcriptions`：请求体就是录音本身（`Content-Type: audio/webm`、`audio/mp4`、`audio/ogg`、`audio/wav`、`audio/mpeg`、`audio/x-m4a`，可带 `;codecs=…`），查询参数 `seconds`（客户端量的时长，整数，必填）。回 `{ text }`。
  - 不能用：409 `speech_input_off`。
  - 超过 25 MB（OpenAI 的上限）：413 `audio_too_large`；`seconds` 超过 `maxSeconds` + 5 秒余量：413 `audio_too_long`；类型不收：415 `unsupported_audio`。
  - 模型满额：409 `model_capacity_reached`（同聊天）；上游失败：502 `transcription_failed`，限流 429 `model_rate_limited`。
  - 在登录分组里，不经内容审核（理由见 §0）。用户标识照常（`llm.Request.User` 的同一个 end-user 标识，渠道开了才发）。
  - 每次请求记一行用量（kind `transcription`，没有对话和 run）。
- `GET/PATCH /admin/speech-input`：读写 §3 的设置，PATCH 校验范围和模型（400 `invalid_speech_input_settings`）。
- 模型设置：`ModelKind` 加 `transcription`，`ModelOverride.audioPrice`（每分钟美元，最多六位小数）。
- 用量日志：`UsageKind` 加 `transcription`，`UsageLog.audioSeconds`、`audioPrice`。

## 5. 适配器

`llm.TranscriptionClient`（`internal/llm/transcription.go`）：

```go
type TranscriptionRequest struct {
	Model     string
	Audio     []byte
	MediaType string // audio/webm …
	User      string
}
type TranscriptionResponse struct {
	Text        string
	Seconds     float64 // 服务商报的时长，没报为 0
	Usage       *Usage  // 服务商报 token 时
	ServedModel string
}
```

OpenAI 形状（`providers/openai/transcriptions.go`）：multipart `file`（文件名按类型给扩展名，服务商按扩展名认格式）、`model`、`response_format=json`；`user` 只在渠道「发送用户标识」开着时发。Anthropic、Gemini 渠道没有这个接口，选到它们的渠道报「这个渠道不能转写」，换下一个。语言提示（`languages[]`）、提示词、流式返回第一期不做。超时 2 分钟。

## 6. 输入框

- 麦克风按钮在发送按钮旁，`speechInput` 可用且浏览器有 `MediaRecorder` 时显示。点开先请求麦克风权限，被拒给出提示。
- 录音中：输入框变成录音条（音量波形、计时、取消、确认）；到 `maxSeconds` 自动确认。确认后显示「正在转写」，回来的文字插在光标处（前后补空格的规则同粘贴），焦点回到输入框；取消则丢弃。
- 录音格式按浏览器支持依次取 `audio/webm;codecs=opus`、`audio/webm`、`audio/mp4`（Safari）。
- 视觉稿先出草图再做。

## 7. 不做的（第一期）

- 语音对话、朗读回答（TTS）。
- 浏览器自带识别（Web Speech API）：质量和隐私因浏览器而异，且不经我们的渠道和账本。
- 每人每天的分钟数上限：先靠模型的容量上限管花费，确有需要再加。
- App：系统输入法自带听写；App 里的麦克风以后单做。
