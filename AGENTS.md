# AGENTS.md

编码助手的仓库指引（`CLAUDE.md` 只是指向这里）。Go API 在 `apps/server`，React 前端在 `apps/web`，契约在 `api/openapi.yaml`。

## 命令

```bash
make setup      # 安装依赖，创建 .env
make up         # docker compose 起 Postgres + MinIO（可选：DATABASE_URL 指向任意 Postgres 18 即可）
make ser        # API :8080，启动时自动迁移
make web        # Vite :5173，/api 代理到 :8080
make check      # lint + test + build，即 CI
make generate   # 重新生成 sqlc、API 契约、GenUI 目录
```

单个 Go 测试：`cd apps/server && go test ./internal/auth/ -run TestHashAndVerify -v`。前端没有测试运行器，靠 `tsc --noEmit`、`eslint .`、`vite build` 验证。

## 生成的代码

`api/openapi.yaml` 同时生成 `internal/api/openapi.gen.go` 和 `packages/api-client/src/schema.d.ts`，改完跑 `make generate-api`。标了 `DO NOT EDIT` 的文件（另有 `internal/db/sqlc/*`、`routeTree.gen.ts`）不要手改，改源头再生成。TypeScript 锁定 5.9.3（openapi-typescript 需要 JS 版编译器）。

## 两套设计系统，不要混用

聊天/登录界面用 `--n-*` 令牌、`n-` 类名、`.prism-next`；后台面板用 shadcn（`components/shadcn/**`，`.shadcn-scope`）。两边共用的东西放 `lib/`、`hooks/`，或者各写一份。以下两条命令应当无输出：

```bash
grep -rn "components/shadcn" apps/web/src/components/next apps/web/src/components/auth
grep -rn -- "--n-\|prism-next" apps/web/src/features/admin apps/web/src/components/shadcn
```

- Tailwind 4 的 `border` 只设宽度，没有默认颜色时画成黑框；两套系统各自在作用域内提供默认边框色。
- Radix portal 渲染在 `<body>` 下、作用域之外，用 `[data-slot]` 和 `[data-slot] *` 选中。
- `shadcn-base.css` 必须在 `@layer base` 里，否则会盖过 Tailwind 工具类。
- shadcn 组件搬自 satnaing/shadcn-admin，偏离上游处要写注释说明原因。

## 后端约定

- 路由在 `internal/api/router.go` 手工挂载，每个新端点都要有意识地放进某个保护分组。
- 归属写进查询（如 `GetConversation` 接收 `user_id`），别人的数据读出来就是不存在；后台接口对非管理员返回 404 而不是 403。
- 迁移嵌进二进制、启动时执行；`make migration name=...` 新建。
- 附件存储在 后台 → 文件存储 里配置（`/admin/storage`，存于 `app_settings`，运行时生效，不用重启，也没有任何存储相关的环境变量）。驱动是本地磁盘或 S3 兼容存储；`storage.Switch` 把新上传交给当前驱动，读取则先看本地磁盘有没有、再找 S3，所以切换驱动不会让旧文件失联。本地驱动由我们自己的 API 签发 HMAC 签名的读写地址 `/api/v1/files/content/{key}`，签名密钥在首次启动时自动生成并存进 `app_settings`（`storage_signing_key`，不经任何 API 暴露）；S3 密钥只写不读。库里的设置应用失败时回退到本地 `./data`，不拒绝启动。
- 对话是一棵树：每条用户消息（input）有 `messages.parent_input_id`（首条为空），`conversations.current_input_id` 指向当前叶子；看到的对话记录和发给模型的上下文都是「根 → 当前叶子」这条路径，加上路径上每个 input 未被 `superseded` 的那次回答。读取一律走 SQL 函数 `conversation_path` / `conversation_transcript`（`ListMessages`、`ListRecentMessages` 都用它，游标沿路径回退），不要在新查询里另写一份。改写任何一条提问 = 在同一个 parent 下加一个兄弟（`edit_of` 指向第一个版本）并成为新叶子，旧分支只是离开路径、不删除；`POST .../messages/{id}/switch` 切到兄弟版本，落在其子树里 `shown_at` 最新的叶子，返回新路径的最新一页。`superseded` 只管同一个 input 的多次回答（重新生成），重新生成和 `ChooseAnswer` 只在路径最后一轮可用。移动叶子的请求（发送、改写、切换、回答 ask_user）都先 `LockConversation` 再读叶子。
- 发消息返回 SSE；助手消息在第一个 token 前就写库，取消算 `complete`，provider 报错才算 `failed`。
- 服务端自带前端：`make build` 先建 web（vite 同时预压缩出 `.br`/`.gz`），再拷进 `internal/webui/dist` 嵌入二进制，一个进程、一个源。`/assets/*` 带 `immutable` 长缓存，`index.html` 是 `no-cache`；缺失的 `/assets/x.js` 是真 404，不回落到 index.html。开发仍走 `make web`。
- 没跑过 web 构建时 `webui/dist` 里只有 `.gitkeep`，服务端只提供 API，`/` 返回一行纯文本说明；该目录除 `.gitkeep` 外都被 gitignore，不要提交。
- 个人记忆（`internal/memory`、`tools/memory.go`）：对话在第一次运行时把用户的自定义指令和记忆存成 `conversations.memory_snapshot`，此后每次请求的系统提示词都从这份快照渲染，同一对话内字节不变（提示缓存和思考块校验依赖它）；对话中改动的记忆从下一个对话起生效。模型用短 id（`m1`…）引用快照里的记忆，id 只在本对话的快照里有意义；工具执行时的用户取自 `agent.ScopeFrom(ctx)`，永远不信参数。记忆上限 100 条、每条 200 字，满了拒绝而不淘汰。只有模型的改动写 `memory_events`（撤销和"最近更改"用），用户自己删改记忆时会连带清掉对应日志。记忆接口全在 `/me/*`，没有任何管理员接口能读到别人的记忆或自定义指令；管理员只有 `memory` 总开关。
- 临时对话（`conversations.temporary`）：不进列表、不起标题、没有记忆和 memory 工具（自定义指令照常），创建 24 小时后由 `Server.StartCleanup` 里的单条 `DELETE` 删除（多实例安全）；用量日志的会话外键是 `SET NULL`，记录保留。

## 前端约定

- 服务端数据用 TanStack Query（键在 `lib/query-keys.ts`、`features/admin/use-admin.ts`）；流式中间态和 UI 状态用 Zustand。流结束后让查询失效，重新拉取。查询键含筛选条件时加 `placeholderData: (previous) => previous`。
- 路由守卫挂在无路径布局的 `beforeLoad`：`_authed.tsx` 要求登录，`admin.tsx` 要求管理员。
- 用户消息带 `branches`（同一 parent 下的兄弟版本，多于一个才有）渲染 ‹ i/n › 翻页器，切换用 `useSwitchVersion`：返回的最新一页用 `spliceNewest` 接进缓存，不要 `invalidateQueries`。改写任意一条提问时，`buildTurns` 把被改写的消息及其后的所有轮次藏起来（`replacing.from`，或进行中的 run 的 `editOf`）。
- 对话记录从尾部分页：`GET .../messages` 首页是最新的一页（页内仍是旧→新），`before` 取更早的页；`hooks/use-messages.ts` 是 `useInfiniteQuery`（`pages[0]` 最新），`NextChat` 只渲染最后 20 个 turn，滚到顶部再多画 20 个，画到已加载的最旧处才请求更早一页。run 结束只用 `refreshNewestMessages` 重取最新一页并拼接，不要对该键 `invalidateQueries`（会按游标重下所有已展开的页）。
- 表格是服务端分页/筛选；`getRowId` 用记录 id；列译名同时放进 `meta.title`。
- 后台表单用 react-hook-form + zod，schema 建在组件内部（文案要翻译）；用 `useWatch` 而不是 `form.watch()`。登录注册页是手写校验。
- 所有面向用户的字符串都走 `t()`（`zh`、`en`，键有类型约束）；服务端 `message` 不直接渲染。
- 动效：全局遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`，用透明度脉动代替位移。
- 设置对话框（通用/个性化/账户/数据）是 `?settings=<tab>` 搜索参数（`lib/settings.ts`），挂在 `_authed` 布局上，渲染进 `.prism-next` 里；样式照搬 Open WebUI 经典设置弹窗，在 `styles/settings.css`。记忆列表是它里面的第二层弹窗（`MemoryManager`）。记忆相关查询键在 `queryKeys.memory`，不写入本地缓存（`query-persist` 只存白名单），临时对话的转录同样不落本地缓存。

## 认证

HttpOnly Cookie 会话，库里只存 token 的 SHA-256；前端从不接触 token，靠 `/auth/session` 判断登录状态，API 客户端必须 `credentials: 'include'`。用户名不能含 `@`（数据库约束保证，登录框同时接受用户名和邮箱）。第一个注册的账号成为管理员；最后一个启用的管理员不能被降级或停用。停用账号或改密码会清掉其会话。自助接口（`PATCH /auth/me`、`POST /auth/password`）刻意不带角色和停用字段。不做账号删除，停用即可。
