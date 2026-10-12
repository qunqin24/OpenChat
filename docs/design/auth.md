# 认证

> 规则摘要见 [AGENTS.md](../../AGENTS.md)；本文是完整设计说明（原 AGENTS.md 同名章节，未删减）。文中「后端约定」「前端约定」等指原 AGENTS.md 的章节，现按主题分在本目录各文件，索引见 [README.md](README.md)。

HttpOnly Cookie 会话，库里只存 token 的 SHA-256；前端从不接触 token，靠 `/auth/session` 判断登录状态，API 客户端必须 `credentials: 'include'`。用户名不能含 `@`（数据库约束保证，登录框同时接受用户名和邮箱）。第一个注册的账号成为管理员（见下「注册」）；最后一个启用的管理员不能被降级或停用：凡是可能让管理员减少的改动（`PATCH /admin/users/{id}` 和 `POST /admin/users/bulk` 的改角色、停用）都在**一个事务里先拿 `LockAdminMembership`（事务级 advisory lock）**，再读目标、更新、数启用的管理员（0 个就回滚，409 `last_admin`），不能「先数后写」分成两步，否则两个管理员同时互相降级会都读到「还有两个」而都成功、一个不剩；批准（`approved`）不会减少管理员，不排队。新增任何能降级或停用账号的路径都要走这把锁（测试在 `admin_last_admin_pg_test.go`，用自己的库，因为数的是全库的管理员）。停用账号或改密码会清掉其会话，已开着的页面下一次请求得到 401 就被带去登录页（见前端约定）。用停用或待审批的账号登录（密码对了服务端才说 `account_disabled` / `pending_approval`）时，登录页不显示一行红字，而是盖上全屏的毛玻璃覆盖层（`components/auth/AccountOverlay.tsx`，照搬 OpenWebUI 的 `AccountPending.svelte`）：「你已被封禁」，或「账号等待审批」加「再次检查」（用刚才的账号密码再登录一次，批准了就直接进去）。「返回」或 Esc 回到清空的表单。两种拒绝（和审批模式注册的 202）都带上要联系的管理员（`details.admin` / `RegisterPending.admin`：最早创建、仍启用的管理员的名字和邮箱，`GetContactAdmin`），覆盖层照 OpenWebUI 显示「管理员：名字（邮箱）」；只有证明了密码（或刚注册）的人才拿得到，密码错时什么都不给。自助接口（`PATCH /auth/me`、`POST /auth/password`）刻意不带角色和停用字段。不做账号删除，停用即可。

## 注册与审批

后台 → 系统 有两个开关（`app_settings`：`allow_registration`「允许注册」、`registration_approval`「新用户需审批」，都默认**开**，所以升级后的实例默认是审批模式），组合出三种模式：

- **关闭**：`POST /auth/register` 返回 403 `registration_closed`，只有管理员能在 后台 → 用户 新建账号。
- **开放**（允许注册、不审批）：新账号马上能用，和以前一样。
- **审批**（允许注册、需审批）：新账号 `users.pending = true`，注册返回 **202** `{status: "pending_approval"}`，不发会话；登录时密码对了才返回 403 `pending_approval`（密码错依旧是通用的 401 `invalid_credentials`，不泄露账号是否存在），也不发会话。「需审批」只在允许注册时有意义，界面在关闭注册时把它调暗禁用。

几条不变量：

- **第一个账号**（不算 guest）永远是管理员、永远不 pending、永远不被开关拒绝，否则全新安装会把自己锁在外面。这个判断在 `RegisterUser` 的 INSERT 里做，注册先拿 `LockRegistration` 的 advisory lock，所以两个人同时抢第一个也不会都成为管理员。公开的 `/auth/options` 在还没有账号时报告 `registration: true, approval: false`。
- 管理员新建的账号（`POST /admin/users`）永远不 pending，关闭注册时它就是唯一的加人方式。
- `pending` 默认 false，迁移后所有已有账号都算已批准（`migration_signup_pg_test.go` 测）。
- **pending 的账号什么都碰不到**：登录不发会话，`GetSessionUser` 还多一道 `NOT users.pending` 作为第二把锁，所以 `auth.Middleware.Require`（唯一的登录闸）也会拒绝它——就算有人哪天写出一条给它发会话的路径。新增任何「凭 token 认人」的路径都要走 `GetSessionUser`，别另写。
- 批准只有一个方向：`PATCH /admin/users/{id}` 带 `approved: true`，或 `POST /admin/users/bulk` 带 `approved: true`（和 `role`、`disabled` 三选一）；`approved: false` 是 400 `invalid_approval`，没有「退回待审批」。批准和启用/停用互相独立。非管理员调这些接口得到 404。
- **拒绝 = 停用**（账号从不删除）：待审批的行上「拒绝」就是 `disabled: true`，之后它是个普通的已停用账号（登录说 `account_disabled`），再「启用」会让它回到待审批名单。用户列表的 `status` 过滤（`pending`/`enabled`/`disabled`，可重复）里 disabled 优先于 pending，所以「待审批」名单只含还在等决定的人。
- 登录页和注册页靠公开的 `GET /auth/options`（`registration`、`approval`、`guest`）决定显示什么：注册关闭时隐藏「注册」链接、`/register` 显示「注册已关闭」；审批模式下注册成功后注册页盖上同一个「账号等待审批」覆盖层（「再次检查」用注册时的邮箱和密码登录）而不是跳进聊天。读不到选项时按开放处理（服务端才是裁判）。改了开关要让 `queryKeys.auth.options` 失效。

## 登录与注册的限流

失败次数存在 `auth_attempts(kind, ip, subject, created_at)`，Postgres 里，所以多实例共享；`CleanupNow` 里 `purgeAuthAttempts` 删一小时前的行。IP 用和游客限额同一个 `clientIP`（`RealIP` 处理过的 `RemoteAddr`，IPv6 按 /64），所以同样**必须**部署在会正确设置转发头的代理后面——注意 `RealIP` 优先读 `True-Client-IP`、`X-Real-IP`，再读 `X-Forwarded-For`，`deploy/Caddyfile` 因此会丢掉前者、用连接地址覆盖后者；换别的代理要做同样的事。

- 登录：每个 IP 每 15 分钟 10 次失败，每个（IP，标识）5 次；超过就 429 `login_rate_limited`，带 `Retry-After`（秒，同时在 `details.retryAfter`），且**在检查密码之前**就拒绝，所以限流期间密码对了也进不去。**绝不按标识单独锁账号**，否则任何人都能从别处把管理员锁在外面；也因此换一个 IP 或另一个标识不受影响。标识存的是小写去空格后的 SHA-256，不存原文。
- 先占位后验证：登录先在 advisory lock 下「数 + 记」一行，密码对了再把这行删掉（所以只有失败才占额度，成功的登录不会耗尽它，也不会清掉别人的失败）。这样一批并发的猜测在 argon2 慢慢算的时候就已经被数进去，而不是算完才记。被拒的请求不记录，免得延长等待。密码对但账号待审批/已停用不算失败。
- 注册：每个 IP 每小时 5 次，**每次尝试都算**（包括邮箱已被占用的——那是探测邮箱的办法），在 argon2 之前计数；超过是 429 `register_rate_limited`。关闭注册时的拒绝发生在计数之前，不记录。
- 新增一类需要限流的公开端点，用 `takeAttempt(ctx, attemptLimit{...}, ip, subject)`，别另造一张表。
