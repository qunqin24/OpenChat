# 前端手册

面向改动 `apps/web/` 和 `packages/api-client/` 的 agent。前置阅读：
[architecture.md](architecture.md)。

## 目录地图

```
apps/web/
  index.html                  内联 #boot 启动屏（JS 起来前就画的 splash）
  vite.config.ts              tanstackRouter 插件（生成 routeTree）+ @ 别名 + /api 代理
  src/
    routeTree.gen.ts          生成物，DO NOT EDIT
    routes/                   文件路由（TanStack Router）
      __root.tsx              根 layout：QueryClientProvider、字体/主题 boot
      _authed.tsx             无路径 layout：会话守卫，component 是聊天 shell
      _authed.index.tsx       / 空态（新建会话输入框）
      _authed.chat.$conversationId.tsx  会话页
      login.tsx register.tsx  公开页
      admin.tsx               无路径 layout：会话 + admin 角色守卫，shadcn shell
      admin.*.tsx             管理面板页面
      dev.loading.tsx         开发用预览页
    lib/
      api.ts                  api client 单例 + unwrap()（openapi-fetch 不抛错）
      query.ts                QueryClient 单例（retry/staleTime 全局默认）
      query-keys.ts           聊天侧 query key 注册表
      i18n.ts                 i18next 初始化（en/zh，localStorage 探测）
      boot.ts                 #boot splash 的撤下/重建
      utils.ts cookies.ts clipboard.ts grouping.ts initials.ts keys.ts
      show-submitted-data.tsx
    stores/
      chat-store.ts           流式回复的 live state（高频写）
      ui-store.ts             theme/sidebar，persist 到 localStorage('openchat.ui')
    hooks/
      use-conversations.ts    list(infinite)/detail/create/rename/delete
      use-messages.ts         消息列表
      use-send-message.ts     发消息 + 排干 SSE → chat-store → invalidate
      use-table-url-state.ts  admin 表格状态 ↔ URL search
      use-dialog-state.tsx use-mobile.tsx
    features/
      auth/                   AuthApi 接口接缝（types/http/use-auth/index）
      admin/                  管理面板（dashboard/settings/users + context providers）
      dev/
    components/
      next/                   聊天 UI（prism 移植）：NextShell/Sidebar/Composer/MessageItem/...
      auth/                   登录/注册页 + validation.ts
      shadcn/                 vendored satnaing/shadcn-admin（见下）
      ui/                     聊天侧用的少量 Radix 包装（alert-dialog、dropdown-menu）
      GlobalProgress.tsx
    styles/                   见「两套设计系统」
    config/fonts.ts           字体选择器的可选项（@fontsource-variable/*）
    locales/{en,zh}/common.json  单一 namespace；types/i18next.d.ts 给 t() 类型
packages/api-client/
  src/schema.d.ts             生成物，DO NOT EDIT
  src/index.ts                createApiClient（openapi-fetch 薄封装）
  src/errors.ts               OpenChatError（status/code/field/details）
  src/stream.ts               streamMessage() 手写 SSE 解析器
  src/types.ts                从 schema re-export 的常用类型
```

## API 调用

- 一律走 `lib/api.ts` 的 `api` 单例 + `unwrap()`。openapi-fetch 返回
  `{data, error, response}` 不抛错；`unwrap` 把 error 转成
  `throw new OpenChatError(status, body)`，TanStack Query 需要抛。
- `credentials: 'include'` 已配好——这是 HttpOnly cookie 能被带上的
  唯一原因，新建 client 时别丢。
- baseUrl：`import.meta.env.VITE_API_BASE_URL ?? '/api/v1'`。dev 下
  `/api` 由 Vite 代理到 `VITE_PROXY_TARGET ?? http://localhost:8080`，
  浏览器始终同源——cookie 和 SSE 不需要 CORS。
- **SSE 不走 openapi-fetch**（它建不了流式响应模型），用
  `streamMessage()`（`packages/api-client/src/stream.ts`）手写的
  `data:` 帧解析器，产出 `StreamEvent` 联合类型。
- `OpenChatError.field` 存在 ⇒ 表单内联错误；否则 banner/toast。
  **auth 调用例外**：走 `features/auth/`，服务端 code 经 `SERVER_ERRORS`
  映射成 `AuthErrorKey`，server 的 `message` 永不直接渲染（没本地化）。

## 状态归谁管

| 状态 | 归属 | 原因 |
| --- | --- | --- |
| 会话、会话列表、消息、admin 数据 | TanStack Query | 服务端权威 |
| 流式回复、AbortController、pendingPrompt | `stores/chat-store.ts` | 每秒多次变更，会打爆 query cache |
| theme、sidebar | `stores/ui-store.ts`（persist） | 纯客户端偏好 |

规则：key 里带过滤条件（page/search/days/cursor）的 query 必须有
`placeholderData: (previous) => previous`，否则一切换就清空闪 loading。
key 统一在 `lib/query-keys.ts`（聊天侧）和 `features/admin/use-admin.ts`
的 `adminKeys`（`['admin', ...]`）。变更后 invalidate 对应的 key；
`useSendMessage` 在 `start` 帧和流结束后两次 invalidate 消息列表。

QueryClient 全局默认（`lib/query.ts`）：`staleTime: 30s`，4xx 不重试，
5xx 最多重试 2 次，mutation 不重试，`refetchOnWindowFocus: false`。

## 路由与守卫

- 文件路由，`routeTree.gen.ts` 由 vite 插件自动生成（dev/build 时），
  不要手改。
- `_authed.tsx`：`beforeLoad` 里 `ensureQueryData(sessionQueryOptions)`
  拿会话，没有 → `redirect('/login?redirect=<href>')`。`admin.tsx` 同样
  守卫再加 `role !== 'admin'` → `redirect('/')`（不给"无权限"页，与服务端
  回 404 同理）。守卫在 `beforeLoad`，未登录看不到 shell 闪烁；
  `ensureQueryData` 复用缓存，受保护路由间跳转零额外请求。
- 登录/注册是 `/login`、`/register`，在 `_authed` 之外。

## 发消息的完整链路（前端）

`useSendMessage(conversationId)`：

1. `chatStore.start(id)` → 新 AbortController（并 abort 上一个流）。
2. `streamMessage()` 逐帧产出：`start`（存 messageId，立刻 invalidate
   消息列表把 user 消息拉进来）→ `delta`（`appendDelta`）→
   `done`（`finish`）/ `error`（`fail`）。
3. `finally`：invalidate 消息列表 + 会话列表（让 updated_at 排序生效）。
4. abort（用户按 Stop）不算失败：`signal.aborted` → `finish()`。

新增一种 `StreamEvent` 类型时三处要同步：`api/openapi.yaml`、
`stream.ts` 的解析、`use-send-message.ts` 的 switch。

## 表单

- auth 页：`components/auth/validation.ts` 手写校验，validator 返回
  `AuthErrorKey` locale key。
- admin 面板：react-hook-form + zod（vendored `ui/form.tsx`）。
  - **zod schema 在组件内构建**——message 要 `t()`，且
    `users-action-dialog` 的密码规则依赖"新建还是编辑"。
  - 监听字段值用 `useWatch`，不用 `form.watch()`（React compiler 无法
    对其做 memo）。
  - 服务端 field error → `form.setError(field, ...)`；非 field 错误 →
    toast（sonner）。

## 表格

admin 用户表是 manual 模式（`manualPagination` + `manualFiltering`）：
分页/搜索都在服务端（offset 分页）。要点：

- `getRowId` 返回账户 id——选中状态靠 id 存活过 re-fetch。
- 列的译名同时放 header renderer 和 `meta.title`（列显隐菜单读不到
  header，会 fallback 到裸列 id）。
- 表格状态同步 URL search：`hooks/use-table-url-state.ts`。

## 两套设计系统（动手前必读）

聊天/auth 与 admin 面板**不共享**视觉语言：

| | 聊天 / auth | admin |
| --- | --- | --- |
| token | `:root` 的 `--n-*` | shadcn 的 `--background`/`--border`/... |
| scope class | `.prism-next`、`.n-auth` | `.shadcn-scope` |
| 样式文件 | `next-theme.css`（prism Less 编译产物，**逐字保留**以便 re-sync）、`auth.css`、`menu.css`、`sidebar.css`、`chat-*.css` | `shadcn-theme.css`、`shadcn-base.css` |

三条铁律（违反任意一条都会渲染出黑框）：

1. Tailwind 4 的 `border` 只设宽度，颜色回落 `currentColor`。默认边框色
   由各系统在**自己的 scope 内**提供——跨 scope 用 `border` 就是黑框。
2. Radix portal 挂到 `<body>`，在所有 scope class 之外。shadcn 组件在
   portal 里靠 `[data-slot]` **和** `[data-slot] *` 命中（后代选择器
   不能省）。
3. `shadcn-base.css` 必须留在 `@layer base`：无 layer 的规则胜过所有
   cascade layer，会压过 `border-*` 工具类，`border-transparent` 预留
   的空间就被画成可见的线。

其他：`focus.css` 的 `:focus-visible` 只作用于 prism 表面（shadcn 自带
ring，全局化会叠双环）。`components/shadcn/**` 是 vendored 上游代码，
与上游的每处偏差都要注释原因（已有偏差：theme provider 代理到本仓
store、灰色零色度、bulk-actions 栏本地化且不缩放、destructive 菜单项
红字非红行）。

## i18n

- `t()` 覆盖一切用户可见字符串，包括 vendored shadcn 组件。
- 单一 namespace `common`，`locales/{en,zh}/common.json` 顶层分区：
  `app / nav / chat / state / next / auth / admin`。
- `types/i18next.d.ts` 把 key 类型化——重命名 key 会 break build；
  新增 key 两个 locale 文件都要加。
- `t` 只在组件/hook 内可用：模块级常量（如 `sidebar-data.ts`）必须做成
  hook（`useSidebarData()`）。
- 语言探测：localStorage(`openchat.lang`) → navigator，fallback `en`。

## 字体与主题

- 字体自托管 `@fontsource-variable/{inter,manrope}`，`config/fonts.ts`
  是选择器的清单。应用范围只在 `.shadcn-scope`（`body` 自己设了
  `font-family`，html 上的类继承压不过它）。
- 主题：`ui-store` 的 `theme`（light/dark/system）→ `applyTheme` 切换
  `<html>` 上的 `.dark` class，`--n-*` 与 shadcn token 都挂这个钩子。

## 动效

- `globals.css` 全局 `prefers-reduced-motion` 关闭动画。
- `data-motion="essential"` 的元素（加载指示器）自己处理：用透明度
  脉冲替代位移——完全静止的进度条和挂死的页面无法区分。

## boot splash

`index.html` 内联了 `#boot` splash 的 markup 和样式；`lib/boot.ts` 在
router idle 后淡出移除（`transitionend` + 600ms 兜底超时——reduced
motion 或后台标签页不触发 transitionend）。改 splash 只改 index.html，
`boot.ts` 从活元素读 markup，没有第二份拷贝。

## 验证

```bash
pnpm --filter @openchat/web exec tsc --noEmit   # 类型
pnpm --filter @openchat/web lint                # eslint
pnpm --filter @openchat/web build               # 产物
pnpm -r typecheck                               # 含 api-client
```

没有前端测试框架——验证 = tsc + eslint + vite build。
CI 额外检查 `packages/api-client/src/schema.d.ts` 与 `openapi.yaml` 同步。

## 改前端时的自检清单

- [ ] 新用户可见字符串进了两个 locale 文件
- [ ] 带过滤条件的 query 有 `placeholderData: (previous) => previous`
- [ ] 新 query key 登记进 `query-keys.ts` / `adminKeys`
- [ ] shadcn 组件的新字符串已翻译；portal 内样式覆盖同时写了
  `[data-slot]` 和 `[data-slot] *`
- [ ] 没有把 server 的 `error.message` 直接渲染到 UI（走 code → locale）
- [ ] 改动属于哪套设计系统？样式写在对应 scope 的 css 里
- [ ] `tsc --noEmit` + `eslint` 过
