# 用量、计费与各类上限

用量账本（含估算）、模型容量上限与在飞预留、并发回答上限、模型对普通用户的开放开关、使用日志分页，以及容量上限的前端。规则摘要见 [AGENTS.md](../../AGENTS.md)；下文为完整设计说明（原 AGENTS.md 相应条目，未删减）。

> 文中「后端约定」「前端约定」「游客模式」「内容审核」等指原 AGENTS.md 的章节，现按主题分在本目录各文件，索引见 [README.md](README.md)。

## 向普通用户开放（`admin_only`）

- 模型设置里的「向普通用户开放」（`model_overrides.admin_only`）：关掉后非管理员的模型列表里没有它，按 id 发也被拒（`pickChannel` 的 `staff` 参数，用户请求传 `isStaff(ctx)`）；管理员照常用，服务端自己的任务（标题等 `taskModel`、校验任务模型）传 `true`，不受影响。新增挑模型的地方要想清楚传哪个。

## 没有服务商用量的请求也要计费

- 没有服务商用量的请求也要计费（`usage.Meter.settle`，`internal/llm/estimate.go`）：用户按停止、流中途出错、或服务商根本不报用量（OpenAI 兼容服务只在最后一个 chunk 带 usage，Anthropic 在 `message_start` 只给输入、`message_delta` 才给输出），账本以前写的是 0 token、`cost` NULL，等于这些请求不花钱——有人可以反复发大上下文再停掉。现在只要请求**被服务商接手**，行里就是估算值并标 `usage_logs.estimated = true`（界面在费用旁显示「估算」）：输入取 `Call.InputEstimate`（`llm.EstimateInput(request)`，压缩用的同一套 CJK 一字一 token、其余 4 字符一 token，乘 1.25），输出取已流出的文本、推理、工具参数的 `llm.EstimateOutput`，价格走目录价，所以计入上限。**服务商报了的部分保持原样，只补缺的**：只有输入则输出补估算，没有输入才补输入估算，完整上报（即使被停止）一个字都不动、不标估算。「被接手」的判断：被取消一律算（请求已发出，服务商读了上下文就计费）；其余错误要看流有没有开始（`Meter.Wrap` 见过任何事件，或响应里已有内容）——状态码错误、连不上这类没开始的不算，行里仍是 0。写账本用 `WithoutCancel` 的上下文，取消路径照样写。新增的模型请求路径要：先构造 `llm.Request`，`Begin(Call{InputEstimate: llm.EstimateInput(req)})`，`Reserve`，再 `Stream`，再 `End`。对话请求的 `partialStream`（节流 goroutine）要在 `Reserve` 通过之后才创建——先创建的话，被拒的请求会把它留在后面；它的 goroutine 另外随请求的 ctx 退出，漏掉一条 `close()` 的路径也不会泄漏（`TestRefusedReservationLeavesNoGoroutine`）。进程关闭（`errShutdown`）时的在飞请求不写账本，下一个进程会重做这次请求，这是已知缺口。

## 同时进行的回答数上限

- 同时进行的回答数有上限（`internal/api/run_limits.go`）：一个账号跨所有对话最多同时有 N 个 `running` 的 run（`app_settings.max_concurrent_runs`，默认 3，0 为不限，上限 1000；后台 → 系统 → 对话）；管理员不受限。游客按网络地址算（`guest_max_concurrent_runs`，默认 2，后台 → 系统 → 访问，和每小时限额同一个地址，`runs.client_ip`）。检查在 `createRun` 里，发送、重新生成、改写、回答 `ask_user`、游客运行都经过它，没有别的路：在建 run 的**同一个事务里**先取 `pg_advisory_xact_lock`（键是账号 id 或 `ip:<地址>`）再数再插，锁持有到提交，所以两个请求抢最后一个名额只有一个过，多实例也成立；超了返回 429 `too_many_runs`，事务回滚，什么都没写（重新生成/改写删的行也回来了）。`awaiting_input` 的 run 不占名额。游客的 `GuestRun` 另在存文件之前无锁预检一次（`guestRunSlotFree`），只为尽早拒绝；游客的 run 只在请求连接期间存在、重启后不会恢复，所以超过 20 分钟没有任何提交的游客 `running` 行不占名额（进程崩溃留下的残余，`sweepGuests` 之后才会删）。新增会起 run 的路径必须走 `createRun`。

## 模型容量上限

- 模型容量上限（`internal/api/model_capacity.go`，列在 `model_overrides`：`capacity` / `capacity_period` / `capacity_admin_exempt` / `capacity_since`）：给单个模型设一个美元上限，周期是 `total` / `monthly` / `daily`。花费是 `usage_logs.cost` 里该模型自周期开始的总和，**按请求的 `model_id` 算，不按 `served_model`**（模型的上架、选择、对话里存的、设置里配的都是请求 id；`served_model` 是服务商回显的字符串，可能是带日期的变体或中转站自己的名字）。所有用户、访客、管理员、`chat` 和 `title` 等各种 kind 的请求都计入。没有价格的模型（元数据没有输入或输出价格）`cost` 是 NULL，总和恒为 0，上限永远不会触发——设置面板会提示。服务商没返回用量不再算「没有价格」：账本会估算（见下一条），估算出的花费照常计入。金额全程是 `numeric`：存成整数微美元（至多 6 位小数）、在 SQL 里和总和比较（`ModelSpends`，一次查询算出一批模型），不要经过 float 比较；达到即触发（`花费 >= 上限`）。
  - 周期按服务器本地时区的零点算（日从当天零点、月从 1 日零点，`time.Date` 构造，所以夏令时的 23/25 小时日也对）。时区来自环境变量 `TZ`（部署默认 `Asia/Shanghai`），`cmd/openchat` 引入 `time/tzdata`，因为生产镜像是 distroless、没有系统时区库，缺了它 Go 会悄悄把 `TZ=Asia/Shanghai` 当成 UTC。`total` 从 `capacity_since` 起算，NULL 表示整个历史；「重新计算」（`POST /admin/models/{id}/capacity/reset`）把它设为现在，使用日志不动。保存设置不会改 `capacity_since`（`SaveModelOverride` 的 `ON CONFLICT` 不更新它）；清掉上限时周期和管理员豁免会被写回默认值（`total`、开），不留看不见的状态。
  - 「恢复默认」（`DELETE .../settings`）删掉整行，所以上限、周期、豁免和起点一并消失；保存时如果什么都没决定（含没有上限）也删行，起点随之丢失。花费记录仍在使用日志里。
  - 上限对所有人生效，除非该模型的 `capacity_admin_exempt` 开着且请求者是管理员（`asker.admin`；管理员的花费照样计入）。访客不是管理员。后台为某人做的事（标题）按那个人算：管理员的对话在豁免下照常起标题。
  - 执行点：①起一次运行之前，`chooseRun` → `pickChannel(ctx, modelID, asker)` 返回 `errModelCapacity`（409 `model_capacity_reached`），此时什么都还没写，发送、重新生成、改写、回答 ask_user 和访客运行都走这条；"随便哪个模型"（空 id）会跳过已满的模型，全满才拒绝。②运行当中，`agent.Options.Capacity`（`api.NewCapacityCheck`）在 `generate` 里**每次模型请求之前**问一次，满了就以 `failWithRow(CodeModelCapacity)` 结束，留下一条带该错误码的空失败助手消息；账本在请求结束时才写，所以光看账本，超出量是检查通过时已在飞的请求，不是整个工具循环；再加上下面的「在飞请求先预留」，同时发起的请求会互相看见。查询出错按失败处理（运行以 `internal` 结束），不放行。③任务模型：配置的任务模型满了就退回对话自己的模型，也满了就跳过标题（`taskModel` 返回 `errModelCapacity`；按需起标题的接口把它原样返回）。④`GET /models`、`/guest/config` 的 `atCapacity` 按请求者算，满的模型仍然列出来。新增任何会用模型的新路径（比如上下文压缩、内容审核）都要在它的请求前过这几个点之一，用量记一个新 kind 就会自动计入花费。
  - 保存任务模型（`UpdateSystemSettings`）不检查容量（`asker.ignoreCap`），只看它能不能被回答。
  - 索引：`usage_logs_model_idx` 是 `(model_id, created_at DESC) INCLUDE (cost)`，上限的求和是 index-only scan；只有设了上限的模型才查花费，没设的只多一次主键查询。`total` 且从不重新计算的上限要扫该模型全部历史，历史很长就用「重新计算」收窄。
  - 在飞请求先预留（`internal/api/spend_reserve.go`，`usage.Reserver`）：对**设了上限的模型**，每次模型请求（对话请求、运行里的压缩、标题、手动压缩，凡是过 `usage.Meter` 的）在发出前调 `meter.Reserve`，先预留「估算输入 token × 目录输入价」这一笔，要求「已花 + 其他在飞请求的预留 + 本次预留 < 上限」，否则拒绝（对话请求以 `failWithRow(CodeModelCapacity)` 结束，标题跳过，手动压缩 409 `model_capacity_reached`）；请求的账本行写完后 `Meter.End` 才释放，调用方另外 `defer meter.Release()` 兜底。预留只含输入（输出事先不知道，同时有多少个回答在写由下面的并发上限管），缓存命中也按全价输入预留，所以偏保守；没有价格的模型不预留。**只在进程内存里、尽力而为**：不建表、不会留下脏行，单实例下是准的，多实例各看各的预留，超出量最多是其他实例上在飞的请求。预留带 1 小时 TTL 防泄漏。比较在 SQL 里按 numeric 做（`ModelWouldReach`），持有互斥锁跨过这次查询，这样「读到的花费」一定包含了已释放预留对应的行。管理员被豁免时不被拒绝，但预留照记（他们的花费对别人算数）。

## 使用日志的游标分页

- 使用日志用游标分页（`created_at, id`，走 `usage_logs_created_idx`），`nextCursor` 是下一页的 `cursor`，筛选条件在翻页间必须不变；总数是 `summary.requests`，没有页码也没有 `total`。

## 容量上限的前端

- 容量上限的前端：设置面板的「容量」一节（`features/admin/models/capacity.ts` 是纯逻辑，有 vitest）、卡片上的用量条、`ChatModel.atCapacity`。选择器里满的模型**保留在列表里但禁用**，原因写在名字下面（禁用项没有指针事件，tooltip 出不来）；对话自己的模型或用户明确选的模型即使满了也不会被悄悄换掉——新对话的默认模型才会绕开满的（`defaultModelId`）；在满的模型上发送时 `NextChat` 先重取一次模型列表（列表最多旧一分钟、还会本地持久化），仍满就弹提示、什么都不创建，取不到就交给服务器决定。重新生成/改写/回答由服务器拒绝，`useSendError` 弹同一句话并让模型列表失效；运行中途被拦住，错误行按 `chat.error.model_capacity_reached` 显示，`use-conversation-events` 看到 `run_finished` 带这个码也会让模型列表失效。
