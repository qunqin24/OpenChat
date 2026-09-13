# AGENTS.md

本文件为在此仓库中工作的编码助手提供指引。（`CLAUDE.md` 只是指向这里的一个引用。）

## 命令

`make help` 列出全部命令。常用的几个：

```bash
make setup          # 安装依赖，创建 .env
make up             # 通过 docker compose 起 Postgres + MinIO
make dev-server     # Go API 跑在 :8080，启动时自动迁移
make dev-web        # Vite 跑在 :5173，把 /api 代理到 :8080
make check          # lint + test + build —— CI 跑的就是这个
make generate       # 重新生成 sqlc 和 API 契约（两端都生成）
```

跑单个 Go 测试：

```bash
cd apps/server && go test ./internal/auth/ -run TestHashAndVerify -v
```

前端没有测试运行器。前端的验证手段是 `pnpm --filter @openchat/web exec tsc --noEmit`、`eslint .` 和 `vite build`。

没有 Docker 也能跑：把 `DATABASE_URL` 指向任意一个 Postgres 18 即可，只有附件功能需要 MinIO。

## 契约是唯一事实来源

`api/openapi.yaml` 为**两端**生成代码：

- `apps/server/internal/api/openapi.gen.go` —— Chi 处理器与模型（oapi-codegen）
- `packages/api-client/src/schema.d.ts` —— TypeScript 类型（openapi-typescript）

改了契约却没跑 `make generate-api`，两端就会不同步。生成的代码没有提交，CI 会失败。

标了 `DO NOT EDIT` 的文件**一律不要手改**：`internal/db/sqlc/*`、`internal/api/openapi.gen.go`、`apps/web/src/routeTree.gen.ts`、`packages/api-client/src/schema.d.ts`。要改就改源头（`.sql` 查询、`openapi.yaml`、`src/routes/` 下的文件），然后重新生成。

TypeScript 全仓库锁定在 5.9.3。`openapi-typescript` 需要 JS 版编译器 API，而 TS 7（原生移植版）不提供。

## 两套设计系统，有意为之

**这是本仓库最大的 bug 来源。** 聊天界面和后台面板**不**共用一套视觉语言：

| | 聊天 / 登录 | 后台面板 |
| --- | --- | --- |
| 令牌 | `:root` 上的 `--n-*` | shadcn 的 `--background`、`--border` 等 |
| 作用域类 | `.prism-next`、`.n-auth` | `.shadcn-scope` |
| 样式文件 | `next-theme.css`（由 prism 的 Less 编译而来，保持原样以便重新同步）、`auth.css`、`menu.css` | `shadcn-theme.css`、`shadcn-base.css` |

靠三条规则把它们隔开：

1. **在 Tailwind 4 里，`border` 只设置宽度，颜色来自别处。** 没有默认值时它回退到 `currentColor`，也就是文字颜色，于是画出一道黑框。两套系统各自在自己的作用域内提供这个默认值。
2. **Radix 的 portal 渲染在 `<body>` 下，位于所有作用域类之外。** 在那里通过 `[data-slot]`（shadcn 组件都带这个属性）**以及** `[data-slot] *`（用于嵌在组件内部的普通元素）才能选中它们。漏掉后代选择器，就是下拉菜单内部出现黑框的原因。
3. **`shadcn-base.css` 必须待在 `@layer base` 里。** 无层级的规则优先级高于所有层级，所以一条无层级的默认边框色会盖过 Tailwind 自己的 `border-*` 工具类——那些用 `border-transparent` 预留空间的组件，就会把边框实实在在画出来。

`focus.css` 里我们的 `:focus-visible` 处理是**刻意**只作用于 prism 界面的：shadcn 组件自带焦点环，一旦放开作用域，两者会叠加。

`components/shadcn/**` 搬运自 [satnaing/shadcn-admin](https://github.com/satnaing/shadcn-admin)（MIT）。**凡是偏离上游的地方都要写注释说明原因**，这样将来重新同步时才分得清哪些是有意为之、哪些是漂移。已知的偏离：主题 provider 代理我们自己的 store；灰阶彩度清零；批量操作栏做了国际化并去掉了悬浮放大；危险菜单项是红字而非红底。

## 状态：服务端 vs 客户端

- **TanStack Query** 负责一切以服务端为准的数据——对话、消息、会话、后台数据。查询键集中在 `lib/query-keys.ts` 和 `features/admin/use-admin.ts`。
- **Zustand** 负责不在服务端的东西：流式回复的中间态（`stores/chat-store.ts`，它每秒变化很多次，放进查询缓存会把缓存冲垮），以及主题和侧边栏状态（`stores/ui-store.ts`）。

一次流式响应结束时，`use-send-message.ts` 会让消息列表失效，由 Query 重新拉取权威版本。这个交接方式就是应该遵循的范式。

**任何查询键里含有筛选条件（页码、搜索词、时间窗）的查询，都需要 `placeholderData: (previous) => previous`**，否则每次变更界面都会先清空。

## 认证

会话是 **HttpOnly Cookie**，背后由 `sessions` 表支撑。Cookie 携带 32 字节熵，库里只存它的 SHA-256。

由此决定的几件事：

- 前端从不接收、存储或读取任何 token。「我登录了吗」是靠调用 `/auth/session` 来回答的——这也是会话属于 Query 而非 Zustand 状态的原因。
- API 客户端设置了 `credentials: 'include'`（`lib/api.ts`）。少了它，浏览器会静默丢掉 Cookie。
- `features/auth/` 是那道缝：一个 `AuthApi` 接口，在 `index.ts` 里一行代码选定实现。
- **第一个注册的账号成为管理员**，这个判断写在 INSERT 内部，所以并发注册不会两个都抢到。管理员不能撤销自己的权限，最后一个启用状态的管理员不能被降级或停用。
- 管理员也能创建账号（`POST /admin/users`），与注册的区别在于角色由调用方指定、且不下发会话。停用账号或替换其密码会清掉它的所有会话——凭证换了而它签发的会话还活着，等于没换，管理员改自己的密码同样如此。
- 自助接口（`PATCH /auth/me`、`POST /auth/password`）**刻意**与管理员接口分开，而且更窄：`UpdateOwnProfile` 根本没有 `role` 和 `disabled` 参数，所以任何输入都无法提升调用者自己的权限。改自己的密码需要提供当前密码，并保留发起请求的那个会话、只吊销其余的；管理员改别人的密码两者都不需要。

## 后端约定

**路由是手工挂载的**，在 `internal/api/router.go` 里，而不是用 `HandlerFromMux`，这样每个端点的保护级别在一处就能看清。新端点必须被**有意识地**放进某个分组——它不可能因为被遗忘而默认变成公开的。

**归属是查询的一部分，而不是查完再检查。** `GetConversation` 接收 `user_id`；别人的行读出来就是「不存在」。后台 API 返回 404 而不是 403 也是同一个道理——403 等于向不该知道的人确认了后台的存在。

**迁移是嵌进二进制的**（`go:embed`）并在启动时执行，所以部署不需要额外步骤。Goose 把每个迁移放在事务里跑，失败可以干净回滚。用 `make migration name=add_something` 创建新迁移。

聊天 provider 是一个两方法的接口（`internal/chat`）。默认装的是 `Echo`，这样没有任何模型密钥也能把整套跑起来；要替换在 `cmd/openchat/main.go` 里改。

`POST /conversations/{id}/messages` 返回 SSE。助手消息那一行在**第一个 token 之前**就写库，所以流到一半崩溃回复也还在。上下文被取消（点了停止、关了标签页）会把已生成的部分存为 `complete`；只有 provider 报错才算 `failed`。

## 前端约定

**路由**是 `src/routes/` 下的文件式路由。守卫挂在无路径布局上：`_authed.tsx` 要求已登录，`admin.tsx` 额外要求管理员角色。两者都跑在 `beforeLoad`，所以未登录的访客不会先闪一下应用外壳。守卫用的是 `context.queryClient.ensureQueryData`，因此在受保护路由之间跳转不会产生额外请求。

**表格**跑在手动模式（`manualPagination`、`manualFiltering`）——分页和搜索是服务端的事。`getRowId` 必须是账号 id，这样重新拉取后选中状态才不会丢。列的译名除了写在 header 渲染函数里，还要放一份到 `meta.title` 上：列显隐菜单没有 header 可读，否则会退回显示原始列名。

**表单**跟着它所在的那半边应用走。登录注册页是手写校验（`components/auth/validation.ts`，校验函数返回本地化键），因为它们早于后台面板存在、而且每个表单只有两个字段。后台面板和上游一样，用 react-hook-form + zod，经由搬运来的 `ui/form.tsx`。**zod schema 要建在组件内部**——提示文案需要翻译，而且在 `users-action-dialog.tsx` 里密码规则还取决于当前是新建还是编辑。用 `useWatch` 而不是 `form.watch()`，React 编译器拒绝对后者做记忆化。

字段级的 API 错误通过 `OpenChatError.field` 抵达表单，契约只在确实能归咎于某一个输入时才设置它（邮箱重复、密码过短、用户名被占用）。其余情况用横幅或 toast。认证相关的调用是例外：它们走 `features/auth/`，把服务端错误码映射成 `AuthErrorKey` 本地化键——**服务端自己的 `message` 从不直接渲染**，因为它没有本地化。

**字体**是自托管的（`@fontsource-variable/*`），所以设置页里的字体选择离线可用，也不会向第三方发请求。这个选择只作用于 `.shadcn-scope` 之内：`body` 设了自己的 `font-family`，`<html>` 上继承下来的类压不过它；而且聊天界面的排版属于搬运过来的 prism 设计，不该由用户替换。

**国际化**：每一条面向用户的字符串都要走 `t()`，搬运来的组件也不例外——语言包是 `zh` 和 `en`。键是按语言文件做类型约束的（`types/i18next.d.ts`），所以改错键名会直接编译失败。搬运一个新的 shadcn 组件，意味着**用它之前先把它的字符串翻掉**；上游发的是硬编码英文，`sidebar-data.ts` 之所以必须改成 `useSidebarData()` hook，就是因为 `t` 只在组件内部可用。

**动效**：`prefers-reduced-motion` 在 `globals.css` 里全局生效，但标了 `data-motion="essential"` 的元素会退出全局规则、自行处理。加载指示器用的就是这条——一个冻住的进度条和一个卡死的页面无法区分。它们把位移换成透明度脉动，而不是干脆不动。

## 目前明确不做的

**不做账号删除**——停用是可逆的等价操作，而删除用户会级联删掉他的所有对话。**也没有邀请和邮箱验证**：管理员建号时直接设定密码，通过别的渠道告知对方。设置页只搬了上游五个分区中的三个；通知分区没有任何东西可通知，显示分区和配置抽屉重复。**没有模型供应商目录**（搬来的 UI 是按有这个东西设计的：各家的密钥、模型列表、推理强度、联网开关）。**没有附件界面、工具调用和思维链展示。**
