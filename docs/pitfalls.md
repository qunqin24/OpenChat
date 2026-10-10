# 已知坑（agent 易错清单）

按"踩过的坑"和"容易犯的错"组织。每条都对应代码里的真实机制。

## 生成物

- **手改生成文件是返工的开始**：`internal/db/sqlc/*`、
  `internal/api/openapi.gen.go`、`apps/web/src/routeTree.gen.ts`、
  `packages/api-client/src/schema.d.ts` 全部 `DO NOT EDIT`。改源头
  （`queries/*.sql`、`api/openapi.yaml`、`routes/*`）再生成。
- 改了 `openapi.yaml` 或 `queries/*.sql` 不跑 `make generate`：
  CI 的 `git diff --exit-code` 会红。
- TypeScript 被 pin 在 5.9.3（根 `package.json` 的 pnpm override）。
  升 TS 会弄死 `openapi-typescript`——它要 JS 编译器 API，TS 7 没有。

## 后端

- **新端点忘挂 `router.go`**：生成代码只提供方法，挂载是手工的。
  忘挂 = 404；挂错组 = 权限漏洞。没有兜底。
- **越权读要 404 不要 403**：`GetConversation` 把 `user_id` 写进查询；
  admin API 对非 admin 回 404。返回 403 等于告诉对方资源存在。
- `pgx.ErrNoRows` 不过 `dbErr` 会变成 500 而不是 404。
- 新唯一索引不进 `conflictingField`：唯一冲突报 500，表单拿不到
  field error。
- SSE 已切换后 `httpx.Fail`/`http.Error` 无效（状态行已提交）——
  用 `sse.Event(StreamError{...})`。
- 别给 `http.Server` 加 `WriteTimeout` 或全局 middleware timeout，
  会把长 SSE 切断。
- 落库最后一步必须用 `context.WithoutCancel` 的新 context，否则客户端
  断开时部分回复丢失。
- `email`/`name` 唯一性在 `lower()` 索引上——别加按原文的 UNIQUE。
- 登录失败路径必须等时：`DummyVerify` 不是装饰，删掉会泄露邮箱是否
  注册。

## 前端

- **openapi-fetch 不抛错**：必须过 `unwrap()`，否则 4xx 静默通过。
- 新建 api client 丢 `credentials: 'include'`：cookie 不发送，全部请求
  表现为未登录。
- **key 带过滤条件的 query 没写 `placeholderData: (previous) =>
  previous`**：改条件瞬间 UI 清空。这是硬性约定不是优化。
- 把流式文本塞进 TanStack Query：每秒多次 setQueryData 会 thrash。
  live 流属于 `chat-store`。
- 直接渲染服务端 `error.message`：没本地化。auth 错误走
  `SERVER_ERRORS` code 映射，其他错误用 `OpenChatError.code` 选文案。
- `form.watch()`：React compiler 不做 memo，用 `useWatch`。
- zod schema 放模块顶层：`t()` 不可用且规则可能需要组件态，在组件里
  构建。

## 样式（本仓最高发 bug 源）

- Tailwind 4 `border` 只有宽度没有颜色——出黑框的第一嫌疑人。检查元素
  是否在正确的 scope class 里。
- Radix portal 在 `<body>` 下、scope 之外：给 shadcn 写覆盖必须
  `[data-slot]` + `[data-slot] *` 两个选择器都写。
- 把 `shadcn-base.css` 的规则挪出 `@layer base`：无 layer 规则胜过
  cascade layer，`border-transparent` 会被画成实线。
- 把 `focus.css` 的 `:focus-visible` 全局化：shadcn 组件叠双 ring。
- 改 `next-theme.css`：它是 prism Less 的编译产物，改了会失去 re-sync
  能力；本地适配写到 `sidebar.css`/`chat-*.css` 等覆盖层。
- 在模块顶层调 `t()`：i18n 实例未必就绪且脱离 react 生命周期——
  做成 hook。

## 安全

- 任何 log/错误里不出现密码、token 原文（库只存哈希是有意的）。
- `RequireAdmin` 必须挂在 `Require` 之内——它假设 context 里已有 user。
- 给自助端点加参数前先想：`UpdateOwnProfile` 刻意没有 role/disabled
  字段，那是权限边界不是遗漏。
- `.env` 不入库；新 secret 只进 `.env.example` 的占位形式。
