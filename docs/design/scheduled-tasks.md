# 定时任务

到点以用户的名义把一段指令发进任务专属的对话、由模型照常回答（参考 ChatGPT 的 Tasks）。规则摘要见 [AGENTS.md](../../AGENTS.md)。

## 数据与范围

- 表 `scheduled_tasks`（迁移 `20261027013302_scheduled_tasks.sql`）：标题、指令 `prompt`、周期（`kind` = once/daily/weekly/monthly，`run_at` / `minute_of_day` / `weekdays`（0 = 周日）/ `month_day`，`time_zone`）、`enabled`、`next_run_at`、上次运行 `last_run_at` / `last_run_id` / `last_error`。
- 状态由列推出：`enabled = false` 是暂停；`enabled` 且 `next_run_at IS NULL` 是一次性任务已跑完（completed）；其余是 active。
- 每个任务一个专属对话（创建任务时新建，标题同任务名，模型取请求里的或默认模型）。对话被软删除，任务就从列表消失；硬删除时级联删任务。删任务不删对话。
- 每人最多 `maxEnabledTasks`（10）个「还会运行」的任务；新建和恢复时在一个事务里先 `LockTaskOwner`（advisory lock）再数，不会两个请求同时钻空子。
- 后台 → 系统「定时任务」开关（`scheduled_tasks`，默认开）。关闭时 `/tasks` 全部 404、`ChatFeatures.scheduledTasks = false`（侧栏入口消失）、不给模型工具、调度器不跑；任务保留。
- 路由只在登录分组，工具对临时对话不提供。

## 周期计算（`internal/schedule`）

纯函数：`Spec.Next(after)` 返回严格晚于 `after` 的下一次。按任务时区的墙上时间算（跨夏令时保持 08:30 就是 08:30）；每月 31 号在小月落在月底。二进制内嵌 `time/tzdata`，镜像里没有时区库也能用。

## 调度器（`api/task_scheduler.go`）

- 每 30 秒一轮：`ClaimDueTasks` 用 `FOR UPDATE SKIP LOCKED` 取到期任务，同一事务里 `AdvanceTask` 把 `next_run_at` 推到「从现在起」的下一次（停机期间错过多次只补一次），提交后再逐个起 run。多实例安全；提交与起 run 之间崩溃只会丢这一次，不会重复。
- 重复任务迟到超过 1 小时（服务停过、开关关过）不补跑，记 `last_error = missed`；一次性任务无论多晚都跑。
- 起 run 走 `startTaskRun`，就是 `CreateMessage` 的路径：`requireRevised` → `chooseRun` → 审核 → `prepareSnapshot` → 事务内 `lockLive` + `appendInput` + `createRun`（并发上限照样生效）→ `startRun`。上下文里放 `auth.WithUser(owner)`，挑模型按本人身份（管理员专用模型、容量上限照常）。
- 起不了时把 API 错误码记进 `last_error`（`conversation_busy`、`model_capacity_reached`、`too_many_runs`、`account_unavailable` …）；被内容审核拒绝的任务自动暂停（每次都会被拒）。

## 审核

指令不是此刻敲进来的，按自定义指令的套路审（`moderation_text.go`）：

- 页面上保存时审，来源 `task`，算一次违规（strike）；通过则把 `memory.Hash(prompt)` 存进 `prompt_checked`。
- 运行前若 `prompt_checked` 与当前指令的哈希不符（审核当时关着、服务不可用放行等）再审一次，来源 `task_run`，**不算**违规（`CountUserStrikes` 排除 `snapshot`、`task_run`）。
- 模型通过工具写的指令也用 `task_run`：是模型的话，不罚用户。

## 模型工具 `scheduled_tasks`（`api/task_tool.go`）

- 放在 `api` 包而不是 `tools` 包：它要用服务器的审核、挑模型和上限。运行器在服务器之前创建，所以用 `runner.AddTools(server.ServerTools)` 事后挂上（`main.go`；`ServerTools` 在 `document_tool.go`，把定时任务、写作块、子智能体的工具合在一起，子智能体的 run 只拿到只读的写作块）。`main.go` 在 `runner.Resume` 之前先装好 `AddTools`、`SetTurnNote`、`SetSubagents`、`AfterRun`，所以进程重启后恢复的 run 一开始就有这些工具和钩子。
- 操作：list / create / update / pause / resume / delete。用户永远取自 `agent.ScopeFrom(ctx)`，别人的任务 id 读出来就是不存在。
- 时间：模型只知道日期，所以工具按用户的墙上时间读写，时区取 `users.time_zone`（前端在 `_authed` 的 `beforeLoad` 里发现与浏览器不同就 `PATCH /auth/me {timeZone}`，`lib/time-zone.ts`），没有就 UTC；每个结果都写明用户时区和当地当前时间。
- 系统提示词只在提供了这个工具时多一段说明（不影响其他请求的提示缓存）。
- 前端：成功的 create/update/pause/resume/delete 在回复里画成一行 `TaskChip`（像记忆的那行），点进 `/tasks`；list 和失败的调用是活动行里的普通步骤。

## 前端

- `/tasks`（`TasksPage`）：空状态是几张「点子」卡片（预填表单），列表分「运行中」和「已暂停 / 已完成」，每行可编辑、立即运行、暂停/恢复、打开对话、删除；`TaskDialog` 新建/编辑。查询键 `queryKeys.tasks.list()`，每分钟刷新一次（调度器会推进时间），不落本地缓存。
- 表单的时间是浏览器的墙上时间，保存时带浏览器时区（`lib/tasks.ts`）；编辑别的时区建的任务会改成当前浏览器时区。
