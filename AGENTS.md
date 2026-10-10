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
- 发消息返回 SSE；助手消息在第一个 token 前就写库，取消算 `complete`，provider 报错才算 `failed`。
- 服务端自带前端：`make build` 先建 web（vite 同时预压缩出 `.br`/`.gz`），再拷进 `internal/webui/dist` 嵌入二进制，一个进程、一个源。`/assets/*` 带 `immutable` 长缓存，`index.html` 是 `no-cache`；缺失的 `/assets/x.js` 是真 404，不回落到 index.html。开发仍走 `make web`。
- 没跑过 web 构建时 `webui/dist` 里只有 `.gitkeep`，服务端只提供 API，`/` 返回一行纯文本说明；该目录除 `.gitkeep` 外都被 gitignore，不要提交。

## 前端约定

- 服务端数据用 TanStack Query（键在 `lib/query-keys.ts`、`features/admin/use-admin.ts`）；流式中间态和 UI 状态用 Zustand。流结束后让查询失效，重新拉取。查询键含筛选条件时加 `placeholderData: (previous) => previous`。
- 路由守卫挂在无路径布局的 `beforeLoad`：`_authed.tsx` 要求登录，`admin.tsx` 要求管理员。
- 表格是服务端分页/筛选；`getRowId` 用记录 id；列译名同时放进 `meta.title`。
- 后台表单用 react-hook-form + zod，schema 建在组件内部（文案要翻译）；用 `useWatch` 而不是 `form.watch()`。登录注册页是手写校验。
- 所有面向用户的字符串都走 `t()`（`zh`、`en`，键有类型约束）；服务端 `message` 不直接渲染。
- 动效：全局遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`，用透明度脉动代替位移。

## 认证

HttpOnly Cookie 会话，库里只存 token 的 SHA-256；前端从不接触 token，靠 `/auth/session` 判断登录状态，API 客户端必须 `credentials: 'include'`。用户名不能含 `@`（数据库约束保证，登录框同时接受用户名和邮箱）。第一个注册的账号成为管理员；最后一个启用的管理员不能被降级或停用。停用账号或改密码会清掉其会话。自助接口（`PATCH /auth/me`、`POST /auth/password`）刻意不带角色和停用字段。不做账号删除，停用即可。
