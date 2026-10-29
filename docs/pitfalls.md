# 已知坑（agent 易错清单）

按「踩过的坑」和「容易犯的错」组织，每条都对应代码里的真实机制。必须遵守的规则在 [AGENTS.md](../AGENTS.md)，这里偏向「为什么会出错、出了什么症状」。

## 生成物

- **手改生成文件是返工的开始**：`internal/db/sqlc/*`、`internal/api/openapi.gen.go`、`apps/web/src/routeTree.gen.ts`、`packages/api-client/src/schema.d.ts` 全部 `DO NOT EDIT`。改源头（`queries/*.sql`、`api/openapi.yaml`、`routes/*`）再生成。
- 改了 `openapi.yaml` 或 `queries/*.sql` 不跑 `make generate`：CI 的 `git diff --exit-code` 会红。
- TypeScript 被 pin 在 5.9.3（根 `package.json`）。升 TS 会弄死 `openapi-typescript`：它要 JS 编译器 API，TS 7 的原生移植版没有。

## 后端

- **新端点忘挂 `router.go`**：生成代码只提供方法，挂载是手工的。忘挂 = 404；挂错组 = 权限漏洞。没有兜底。
- **越权读要 404，不要 403**：`GetConversation` 把 `user_id` 写进查询；后台接口对非管理员回 404。回 403 等于告诉对方资源存在。
- `pgx.ErrNoRows` 不过 `dbErr` 会变成 500 而不是 404。
- 新唯一索引不进 `conflictingField`：唯一冲突报 500，表单拿不到字段错误。
- **事件流已经切成 SSE 之后，`httpx.Fail` / `http.Error` 无效**（状态行已发出）。这只适用于 `GET /conversations/{id}/events`（`streamConversationEvents`）：它在 `NewSSEWriter` 之后出错只能结束这条流，客户端从新的快照重连。创建消息的 `POST /conversations/{id}/messages` 不是流，返回 202 加 JSON，照常 `httpx.Fail`。
- 别给 `http.Server` 加 `WriteTimeout`，也别加全局 middleware 超时：会把事件流切断。
- 回答是服务器上的 run，不属于发起它的请求：别让它的提交依赖请求的 context（run 的提交用自己的带超时的 context，`agent/execution.go`）。handler 里提交后的收尾（删文件、停 run）要用 `context.WithoutCancel(ctx)`，否则客户端一断开就半途而废。
- `email`/`name` 的唯一性在 `lower()` 索引上：别加按原文的 `UNIQUE`。
- 登录失败路径必须等时：`DummyVerify` 不是装饰，删掉会泄露邮箱是否注册。

## 前端

- **openapi-fetch 不抛错**：必须过 `unwrap()`，否则 4xx 静默通过。
- 新建 api client 丢了 `credentials: 'include'`：cookie 不发送，所有请求都表现为未登录。
- **key 带过滤条件的 query 没写 `placeholderData: (previous) => previous`**：改条件的瞬间界面清空。这是硬性约定，不是优化。
- 把流式文本塞进 TanStack Query：每秒多次 `setQueryData` 会冲垮缓存。流式中间态属于 `stores/chat-store.ts`。
- 直接渲染服务端 `error.message`：没本地化。auth 错误走 `SERVER_ERRORS` 的 code 映射，其他错误用 `OpenChatError.code` 选文案。
- `form.watch()`：React compiler 没法围着它做 memo，用 `useWatch`。
- zod schema 放模块顶层：`t()` 不可用，规则也可能需要组件状态；在组件里构建。
- 对消息键 `invalidateQueries`：消息列表从尾部分页，整键失效会把读者翻开的旧页全部重新下载，用 `refreshNewestMessages`（见 AGENTS.md）。

## 样式（本仓最高发的 bug 源）

- Tailwind 4 的 `border` 只有宽度没有颜色：出黑框的第一嫌疑人。检查元素是否在正确的作用域类里。
- Radix portal 在 `<body>` 下、作用域之外：给 shadcn 写覆盖，`[data-slot]` 和 `[data-slot] *` 两个选择器都要写。
- 把 `shadcn-base.css` 的规则挪出 `@layer base`：无 layer 的规则胜过所有 cascade layer，`border-transparent` 会被画成实线。
- 把 `focus.css` 的 `:focus-visible` 全局化：shadcn 组件叠出双层 ring。
- 同一个属性在 `chat-theme.css` 和后面导入的聊天样式表里各写一遍：后者按导入顺序赢，前面那份就是死代码，改它没有效果。属性只放一处。
- 在模块顶层调 `t()`：i18n 实例未必就绪，也脱离 React 生命周期；做成 hook。

## 安全

- 任何日志或错误信息里都不能出现密码、token 原文（库里只存哈希是有意的）。
- `RequireAdmin` 必须挂在 `Require` 之内：它假设上下文里已有用户。
- 给自助端点加参数前先想：`UpdateOwnProfile` 刻意没有 role / disabled 字段，那是权限边界，不是遗漏。
- `.env` 不入库；新的密钥只以占位形式进 `.env.example`。
