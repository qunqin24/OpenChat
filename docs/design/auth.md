# 认证

> 规则摘要见 [AGENTS.md](../../AGENTS.md)；本文是完整设计说明。文中「后端约定」「前端约定」等指 AGENTS.md 的章节，细节按主题分在本目录各文件，索引见 [README.md](README.md)。

HttpOnly Cookie 会话，库里只存 token 的 SHA-256；前端从不接触 token，靠 `/auth/session` 判断登录状态，API 客户端必须 `credentials: 'include'`。用户名不能含 `@`（数据库约束保证，登录框同时接受用户名和邮箱）。第一个注册的账号成为管理员（见下「注册」）；最后一个启用的管理员不能被降级或停用：凡是可能让管理员减少的改动（`PATCH /admin/users/{id}` 和 `POST /admin/users/bulk` 的改角色、停用）都在**一个事务里先拿 `LockAdminMembership`（事务级 advisory lock）**，再读目标、更新、数启用的管理员（0 个就回滚，409 `last_admin`），不能「先数后写」分成两步，否则两个管理员同时互相降级会都读到「还有两个」而都成功、一个不剩；批准（`approved`）不会减少管理员，不排队。新增任何能降级或停用账号的路径都要走这把锁（测试在 `admin_last_admin_pg_test.go`，用自己的库，因为数的是全库的管理员）。改密码会清掉其会话，已开着的页面下一次请求得到 401 就被带去登录页（见前端约定）。自助接口（`PATCH /auth/me`、`POST /auth/password`）刻意不带角色和停用字段。

## App 的会话（bearer token）

浏览器之外的客户端（Android App）拿不到也用不了 Cookie，所以同一张 `sessions` 表另有一种交付方式：

- **怎么拿到 token**：`POST /auth/login`、`/auth/register`、`/auth/passkey` 带请求头 `X-OpenChat-Client: app` 时，`issueSession` 照常写会话行但**不设 Cookie**，响应体是 `SignInResult`（`SessionUser` 加 `token`、`expiresAt`）。不带这个头时和以前完全一样（浏览器拿到的 `SignInResult` 没有这两个字段）。
- **不让网页拿到 token**：带 `Origin` 的请求（浏览器的页面发出的 POST 一定带）用 app 模式一律 400 `app_client_from_browser`；`X-OpenChat-Client` 也不在 CORS 允许的请求头里。原生 App 的 HTTP 栈不发 `Origin`。这样 HttpOnly Cookie 防的「页面脚本读到会话」不会从这条路漏出去。
- **怎么用**：`Authorization: Bearer <token>`。`auth.TokenFromRequest` 先读它、再读 Cookie，之后都是同一条路（`GetSessionUser` 挡过期、停用、待审批，`GetDisabledSessionUser` 给停用账号 403 `account_disabled`），所以所有规则对两种会话一样成立。来自 bearer 的请求被拒时不清 Cookie。登出 `POST /auth/logout` 按 token 删掉那一行，仍在公开分组里（停用账号的 App 也能退出）。
- **续期**：Cookie 自带 30 天有效期，不续；App 的会话在 `GET /auth/session`（App 每次打开都会调）且剩余不到 15 天时延长到 30 天（`RenewSession`，同时写 `last_used_at`），所以一个月内打开过就不会被登出，每个会话一个月最多写一两次。改密码照旧删掉其他会话、保留当前这个。
- 测试：`auth/session_test.go`（读 token 的顺序）、`api/auth_app_pg_test.go`（拿 token、bearer 访问、被拒的情形、登出、续期）。

## 停用账号

**停用不清会话、不让人退出登录**（`api/account_disabled.go`）：停用时记下原因 `users.disabled_reason`（`admin` 或 `moderation`）和管理员写给对方的 `disabled_note`（可选，≤500 字，`PATCH /admin/users/{id}` 和批量接口的 `disabledNote`；启用时清空）。`auth.Middleware.Require` 在 `GetSessionUser` 查不到时再用 `GetDisabledSessionUser` 看是不是已停用账号的有效会话，是就交给 `OnDisabled`（`NewRouter` 里设成 `failDisabled`）：所有需要登录的接口都回 403 `account_disabled`，`details` 是 `AccountDisabled`（原因、停用时间、管理员的话；审核封禁另有窗口内被拦的条数、小时数和类别，`ModerationBanSummary`；以及要联系的管理员：停用它的那位管理员（`users.disabled_by`，他还是启用的管理员时），否则同其他被拒账号一样是 `GetContactAdmin`），别的什么都做不了。`/auth/logout` 挪出了登录分组，已停用的会话也能退出；重新启用后同一个会话直接能用。前端：任何请求收到它，`lib/account-disabled.ts` 记下详情并跳到 `/disabled`（`_authed` 的守卫也会跳），页面说明原因，「退出登录」才真的退出，「已经恢复了？重新检查」问一次 `/auth/session`，恢复了就回首页。用停用或待审批的账号登录（密码对了服务端才说 `account_disabled` / `pending_approval`，前者带同样的原因）时，登录页不显示一行红字，而是盖上全屏的毛玻璃覆盖层（`components/auth/AccountOverlay.tsx`，照搬 OpenWebUI 的 `AccountPending.svelte`）：停用的原因，或「账号等待审批」加「再次检查」（用刚才的账号密码再登录一次，批准了就直接进去）。「返回」或 Esc 回到清空的表单。两种拒绝（和审批模式注册的 202）都带上要联系的管理员（`details.admin` / `RegisterPending.admin`：最早创建、仍启用的管理员的名字和邮箱，`GetContactAdmin`），覆盖层照 OpenWebUI 显示「管理员：名字（邮箱）」；只有证明了密码（或刚注册）的人才拿得到，密码错时什么都不给。

## 注册与审批

后台 → 系统 有两个开关（`app_settings`：`allow_registration`「允许注册」、`registration_approval`「新用户需审批」，都默认**开**，所以升级后的实例默认是审批模式），组合出三种模式：

- **关闭**：`POST /auth/register` 返回 403 `registration_closed`，只有管理员能在 后台 → 用户 新建账号。
- **开放**（允许注册、不审批）：新账号马上能用，和以前一样。
- **审批**（允许注册、需审批）：新账号 `users.pending = true`，注册返回 **202** `{status: "pending_approval"}`，不发会话；登录时密码对了才返回 403 `pending_approval`（密码错依旧是通用的 401 `invalid_credentials`，不泄露账号是否存在），也不发会话。「需审批」只在允许注册时有意义，界面在关闭注册时把它调暗禁用。

几条不变量：

- **第一个账号**永远是管理员、永远不 pending、永远不被开关拒绝，否则全新安装会把自己锁在外面。这个判断在 `RegisterUser` 的 INSERT 里做，注册先拿 `LockRegistration` 的 advisory lock，所以两个人同时抢第一个也不会都成为管理员。公开的 `/auth/options` 在还没有账号时报告 `registration: true, approval: false`。
- 管理员新建的账号（`POST /admin/users`）永远不 pending，关闭注册时它就是唯一的加人方式。
- `pending` 默认 false，迁移后所有已有账号都算已批准（`migration_signup_pg_test.go` 测）。
- **pending 的账号什么都碰不到**：登录不发会话，`GetSessionUser` 还多一道 `NOT users.pending` 作为第二把锁，所以 `auth.Middleware.Require`（唯一的登录闸）也会拒绝它——就算有人哪天写出一条给它发会话的路径（`GetDisabledSessionUser` 同样排除 pending）。新增任何「凭 token 认人」的路径都要走 `GetSessionUser`，别另写。
- 批准只有一个方向：`PATCH /admin/users/{id}` 带 `approved: true`，或 `POST /admin/users/bulk` 带 `approved: true`（和 `role`、`disabled` 三选一）；`approved: false` 是 400 `invalid_approval`，没有「退回待审批」。批准和启用/停用互相独立。非管理员调这些接口得到 404。
- **拒绝 = 停用**（不是删除）：待审批的行上「拒绝」就是 `disabled: true`，之后它是个普通的已停用账号（登录说 `account_disabled`），再「启用」会让它回到待审批名单。用户列表的 `status` 过滤（`pending`/`enabled`/`disabled`，可重复）里 disabled 优先于 pending，所以「待审批」名单只含还在等决定的人。
- 登录页和注册页靠公开的 `GET /auth/options`（`registration`、`approval`）决定显示什么：注册关闭时隐藏「注册」链接、`/register` 显示「注册已关闭」；审批模式下注册成功后注册页盖上同一个「账号等待审批」覆盖层（「再次检查」用注册时的邮箱和密码登录）而不是跳进聊天。读不到选项时按开放处理（服务端才是裁判）。改了开关要让 `queryKeys.auth.options` 失效。

## 登录与注册的限流

失败次数存在 `auth_attempts(kind, ip, subject, created_at)`，Postgres 里，所以多实例共享；`CleanupNow` 里 `purgeAuthAttempts` 删一小时前的行。IP 用 `clientIP`（`internal/api/auth_limits.go`，`RealIP` 处理过的 `RemoteAddr`，IPv6 按 /64），所以同样**必须**部署在会正确设置转发头的代理后面——注意 `RealIP` 优先读 `True-Client-IP`、`X-Real-IP`，再读 `X-Forwarded-For`，`deploy/Caddyfile` 因此会丢掉前者、用连接地址覆盖后者；换别的代理要做同样的事。

- 登录：每个 IP 每 15 分钟 10 次失败，每个（IP，标识）5 次；超过就 429 `login_rate_limited`，带 `Retry-After`（秒，同时在 `details.retryAfter`），且**在检查密码之前**就拒绝，所以限流期间密码对了也进不去。**绝不按标识单独锁账号**，否则任何人都能从别处把管理员锁在外面；也因此换一个 IP 或另一个标识不受影响。标识存的是小写去空格后的 SHA-256，不存原文。
- 先占位后验证：登录先在 advisory lock 下「数 + 记」一行，密码对了再把这行删掉（所以只有失败才占额度，成功的登录不会耗尽它，也不会清掉别人的失败）。这样一批并发的猜测在 argon2 慢慢算的时候就已经被数进去，而不是算完才记。被拒的请求不记录，免得延长等待。密码对但账号待审批/已停用不算失败。
- 注册：每个 IP 每小时 5 次，**每次尝试都算**（包括邮箱已被占用的——那是探测邮箱的办法），在 argon2 之前计数；超过是 429 `register_rate_limited`。关闭注册时的拒绝发生在计数之前，不记录。
- 新增一类需要限流的公开端点，用 `takeAttempt(ctx, attemptLimit{...}, ip, subject)`，别另造一张表。

## 删除账号

- 后台 → 用户，行菜单「删除账号」，要输入账号名确认（`DELETE /admin/users/{id}`，`api/admin_delete.go`）。不能删自己（409 `self_delete`），不存在的账号 404。
- 一个事务里：`LockAdminMembership` → 读目标 → `CancelUserRuns`（标记停止）→ `LabelDeletedUserUsage`（用量行写上 `deleted_user` = 当时的名字）→ `DeleteAccount`（删用户，会话、对话、消息、run、附件、记忆、定时任务、轨迹都级联删，同一语句交回附件和头像的 storage key）→ 若删的是启用的管理员就数一遍剩下的（0 个则 409 `last_admin` 回滚）。提交后 `runner.Cancel` 停掉那些 run、尽力删文件。
- 用量行保留（金额和容量上限从账本算），`user_id` 由外键置 NULL；使用日志和仪表盘里显示「已删除的账号：名字」；没有账号也没有删除标记的行（旧版本留下的请求）显示「无账号」。审核日志的 `user_id` 同样置 NULL。

## 通行密钥（`api/passkeys.go`，go-webauthn；前端 `@simplewebauthn/browser`）

- **登录不先问账号**：`POST /auth/passkey/options` 发一个可发现凭据（discoverable）的挑战，浏览器让人挑一个本站的通行密钥，`POST /auth/passkey` 带回答案。凭据 id 查到 `passkeys` 行，验证器返回的 user handle 必须是这把密钥所属账号的 id（`users.id` 的 16 字节）。验证通过之后和密码登录从「密码对了」那一步起完全一样：停用 403 `account_disabled`、待审批 403 `pending_approval`、否则 `issueSession`。
- **限流**：答案验证前在 `takeAttempt(passkeyLimit, ip, "")` 占位（每 IP 15 分钟 10 次失败，429 `login_rate_limited`），验证通过才交还；发挑战的 options 端点只建一行挑战，按 IP 每分钟限次（内存）。签名计数倒退（`CloneWarning`）当作克隆的验证器拒绝。
- **挑战一次性**：`webauthn_challenges` 存 go-webauthn 的会话，五分钟有效，`TakeWebAuthnChallenge` 用 `DELETE … RETURNING` 取，好坏都只能用一次；注册的挑战记着发给谁，别的账号拿去答是 400。清理循环删过期的。
- **管理自己的通行密钥**：`/auth/passkeys`（列表、`options` + `POST` 添加、`PATCH` 改名、`DELETE` 删除），归属写进查询（`user_id`），别人的读出来就是不存在。注册时排除这个账号已有的验证器，要求可发现凭据（resident key required）。名字默认取浏览器和系统（`lib/passkey-name.ts`，「Chrome · macOS」）。`synced` 是 `BackupEligible`（存在密码管理器或平台账号里、可跨设备）。凭据整个存 `passkeys.credential`（jsonb），`credential_id` 单独一列唯一索引供登录查找；账号删除级联删除。
- **设置**（后台 → 身份验证 → 通行密钥，`/admin/authentication/passkeys`，`app_settings.passkeys`）：开关默认开；依赖方 ID 和允许的来源都留空时取请求的 `Origin`（没有就 `X-Forwarded-Proto` + `Host`），依赖方 ID 跟着它走：页面只拿得到自己域名的通行密钥，所以这样取是安全的。**固定了依赖方 ID 而来源留空时，绝不信请求自报的来源**：浏览器允许同一父域下的兄弟子域（`evil.example.com`）用父域的依赖方 ID，它可以把真实的答案转交过来；这时只认服务器配置的 `CORS_ORIGINS` 里属于该域名的来源和该域名本身，一个都没有就是 503 `passkey_origins_needed`（测试 `TestPasskeySiblingSubdomainIsRefused`）。正式部署建议固定；用户验证 `preferred`（默认）或 `required`。关掉后已有通行密钥保留但不能用，`AuthOptions.passkeys` 为假，登录页不显示按钮。改依赖方 ID 会让按旧域名建的通行密钥失效。
- **Android App**（`api/android_passkeys.go`）：App 的答案里，来源不是页面而是签名证书：`android:apk-key-hash:<证书 SHA-256 的 base64url>`。后台在同一个 Tab 里登记 App（包名 + 每个签名证书的 SHA-256 指纹，正式版和调试版各一行，存在 `app_settings.passkeys` 的 `androidApps` 里），由它得出两样东西：go-webauthn 的 `RPOpaqueOrigins`（接受这些来源；`RPOrigins` 仍只能是 http(s)），以及公开的 `GET /.well-known/assetlinks.json`（Digital Asset Links，`get_login_creds`，缓存 5 分钟，通行密钥关掉时为空数组）。Android 在依赖方 ID 的域名上读这个文件，确认 App 可以用该域名的通行密钥，所以有 App 时依赖方 ID 应固定为站点域名。App 的请求不带 `Origin`，依赖方 ID 由 `X-Forwarded-Proto` + `Host` 得出，也就是站点自己的域名。测试在 `TestPasskeys`（未登记时拒绝、登记后通过、assetlinks 的内容）和 `android_passkeys_test.go`。
- 身份验证页按「一种方式一个 Tab」组织，以后的 SSO、二次验证等加成新 Tab。
