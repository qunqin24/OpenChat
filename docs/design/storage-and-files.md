# 文件存储、上传与运维

附件与头像的存储驱动、上传写一次与审核收据、附件清理、版本检查、日志、服务端自带前端。规则摘要见 [AGENTS.md](../../AGENTS.md)；下文为完整设计说明（原 AGENTS.md「后端约定」中的相应条目，未删减）。

> 文中「后端约定」「前端约定」「游客模式」「内容审核」等指原 AGENTS.md 的章节，现按主题分在本目录各文件，索引见 [README.md](README.md)。

## 存储驱动与签名地址

- 附件存储在 后台 → 文件存储 里配置（`/admin/storage`，存于 `app_settings`，运行时生效，不用重启，也没有任何存储相关的环境变量）。驱动是本地磁盘或 S3 兼容存储；`storage.Switch` 把新上传交给当前驱动，读取则先看本地磁盘有没有、再找 S3，所以切换驱动不会让旧文件失联。本地驱动由我们自己的 API 签发 HMAC 签名的读写地址 `/api/v1/files/content/{key}`，签名密钥在首次启动时自动生成并存进 `app_settings`（`storage_signing_key`，不经任何 API 暴露）；S3 密钥只写不读。库里的设置应用失败时回退到本地 `./data`，不拒绝启动。

## 上传的文件写一次

- **上传的文件写一次**：审核读的图片和模型读的图片必须是同一份字节，而上传是「PUT 到签名地址 → 消息按 id 认领」两步，审核读文件发生在中间。两个驱动各用一招，缺一不可：①本地驱动的 `PUT /files/content/{key}` 用 `Local.PutNew`（硬链接提交，名字被占就失败，竞争的写入者只有一个赢），对象一旦写成，再 PUT 一律 409 `already_uploaded`（认领之后仍是 409 `already_attached`）；要换文件就重新申请一个上传。②S3 的预签名 PUT 拒绝不了第二次写入，认领之后 15 分钟内仍然有效，所以**审核留下收据**：`CreateMessage` 里 `checkedImages` 把审核实际读到并交给审核服务的那几份字节算 SHA-256，写进用户消息的 `AttachmentPart.SHA256`（`messages.parts` 的 JSON，不用迁移；不是读两次，所以「审了」和「记了」之间没有缝）。`agent.attachmentResolver` 每次把文件读进请求前都比对，对不上就不发这个文件，换成一行「[Attachment … was not included: the file changed after it was checked.]」，运行照常。改写问题会把旧提问的附件引用连同摘要一起带到新问题上。没有读过图片的情况（审核关着、`model` 模式只审文字、管理员豁免、PDF）不记摘要，也就没有可比的东西——收据只担保「审过的就是发的」。访客不受影响：文件字节就在请求里，服务端自己 `PutObject`，审核看的和存的是同一个 `[]byte`，没有上传地址可以回头写。新增读用户上传文件的地方，如果会把它发给模型，必须走这条解析（`attachmentResolver`），别自己 `Read` 了就发。

## 附件行消失时的清理

- 附件行消失的地方都要先收集 `storage_key`、删行、再尽力删文件（`internal/api/cleanup.go`，查询在 `queries/cleanup.sql`，用 `DELETE ... RETURNING`，多实例安全）：临时对话 24 小时后、软删除的对话 30 天（`deletedRetention`）后连同消息和附件硬删，从未被消息认领的上传 24 小时后删；改写删掉的提问的附件在提交后立即删（见上）。本地磁盘另有孤儿清扫：`uploads/` 下没有附件行且超过 24 小时的文件会被删，所以数据目录必须和数据库配套，不要拿别的库去指同一个 `./data`。`storage.Backend` 有 `Delete`，`Switch` 会同时从磁盘和 S3 删。

## 头像

- 头像：上传由 后台 → 系统 的「允许上传头像」控制（`app_settings.avatar_uploads`，默认关；关时 `PUT /me/avatar` 返回 403 `avatar_uploads_off`，前端从 `ChatFeatures.avatarUploads` 读，按钮显示「暂不可用」；移除头像始终可以）。用户上传图片本身（`PUT /me/avatar`，png/jpeg/webp，≤1 MiB），服务端解码、取中心正方形、缩到 256 px、重新编码成 PNG 再存，所以文件里夹带的东西不会被保留；对象在 `avatars/<用户>/<uuid>.png`，库里只存 key（`users.avatar_key`），读取用的是附件同一套签名读取地址，`avatarURL` 对同一个 key 在有效期过半前复用同一个地址。`files_local.go` 对 `avatars/` 前缀的 key 查 `users` 而不是 `attachments`，且只允许读；被替换或移除的图片文件立即删，行不再指向它后旧地址也就不再被服务。孤儿清扫只看 `uploads/`，不要把头像放进去。

## 版本与检查更新

- 版本与检查更新（`api/version.go`，后台 → 系统 →「常规」第一行）：当前版本在编译时写进二进制（`config.buildVersion`，Dockerfile 用 `-ldflags -X` 从构建参数 `APP_VERSION` 写入，发版流程从 tag 传；本地构建是 `dev`，`dev` 不和发布版比较）。**不要再让环境变量决定版本**：以前是镜像里的 `ENV APP_VERSION`，而 1Panel、Portainer 这类面板「升级容器」时会把旧容器的环境变量原样带到新容器，旧镜像的 `APP_VERSION` 盖住新镜像的，升级后的服务一直报旧版本号（v1.1.3 及之前）。最新版本由**服务端**问 GitHub 的 `releases/latest`（不含草稿和预发布），结果在进程里缓存：打开页面时超过 6 小时才再问（`GET /admin/version`），「检查更新」按钮立即问（`POST /admin/version/check`），两次真正的请求至少隔 1 分钟；问失败不报错，带 `checkError` 返回已知的结果。没有管理员打开这一页就从不对外请求。仓库地址是 `releasesLatestURL` 常量，fork 了要改它。

## 日志与请求编号

- 日志：`logging.WithFields`/`Annotate` 把请求编号、用户等放进上下文，所有带 ctx 的 `slog.*Context` 行都会自动带上（`auth.Require` 标 `user_id`，游客路由标 `guest`，请求日志补 `conversation_id`）；后台运行没有请求上下文，用 `ex.logger()`，它带 run、对话、用户、模型、渠道。响应头 `X-Request-Id` 是请求编号（`httpx.RequestID` 生成：12 位小写 base32 随机串，不含主机名，也不采用客户端送来的 `X-Request-Id`——chi 自带的两样都做，所以没用它），前端在发送失败的提示里显示它，回答失败的提示显示 run id 前 8 位（`ErrorId`），用户反馈时报这个编号。`LOG_FILE` 设了就把同样的 JSON 日志写一份到文件、按大小轮转（镜像默认 `/app/data/logs/openchat.log`，在数据卷上，换容器也不丢）。新写的日志行尽量带 ctx，别丢掉这些字段。

## 服务端自带前端

- 服务端自带前端：`make build` 先建 web（vite 同时预压缩出 `.br`/`.gz`），再拷进 `internal/webui/dist` 嵌入二进制，一个进程、一个源。`/assets/*` 带 `immutable` 长缓存，`index.html` 是 `no-cache`；缺失的 `/assets/x.js` 是真 404，不回落到 index.html。开发仍走 `make web`。
- 没跑过 web 构建时 `webui/dist` 里只有 `.gitkeep`，服务端只提供 API，`/` 返回一行纯文本说明；该目录除 `.gitkeep` 外都被 gitignore，不要提交。

## 升级后提示刷新（`webui.BuildID`、`X-OpenChat-Build`、`lib/build-check.ts`）

服务端在每个响应里带 `X-OpenChat-Build`：嵌进二进制的前端 `index.html` 的 SHA-256 前 12 位。`index.html` 列出了所有带哈希的资源名，所以只有前端真的变了它才变——重启、只改服务端都不变；没有嵌前端（开发时 Vite 提供页面）就不带。前端所有请求都经过 `appFetch`（账号）或游客的 `network`，两处都调 `noteBuild`：记住第一次看到的值，之后看到不同的值就通过 `onBuildChange` 弹一次不会自动消失的提示类通知（`toast.info`，和其他通知同一套带图标的彩色样式）「服务端已更新，请刷新页面」，带「刷新」按钮，按钮在 `toast.css` 里画成跟随通知颜色的描边小按钮（`main.tsx`）。镜像升级、服务重启后，事件流的自动重连通常是第一个发现的请求。和 Open WebUI 不同：它在 WebSocket 重连后比对版本号和部署 ID，不同就直接强制刷新页面；这里只提示，免得丢掉输入框里写了一半的消息。新增不经 `appFetch` 的网络请求时，也要调 `noteBuild`。


## 模型目录（models.dev）的更新

模型的名称、上下文长度、价格、能力等元数据来自 models.dev（`internal/catalog`），存成本地快照，市场卡片和模型设置只读快照，不实时请求。快照超过一天（`catalog.StaleAfter`）才更新：启动时查一次，之后每小时查一次（`StartCatalogRefresh`，`catalogCheckEvery`），过期才去拉，请求带 ETag，没变时 models.dev 只回 304。以前只在启动时查，服务不重启快照就一直不更新。拉取失败不影响服务，快照保持原样，原因记在 `catalog_state.last_error`，下一次检查重试。

模型市场标题下的状态行（`catalog-status-line.tsx`）显示快照的模型数、更新时间、是否过期和失败原因，旁边的「刷新目录」按钮立即拉一次（`POST /admin/catalog`，不看是否过期）。这两样在 `478c312`（模型覆盖设置）里被一起删掉过，接口留着、前端没有入口，之后恢复。

模型设置的「恢复默认」删掉这个模型的整行覆盖（`DELETE /admin/models/{id}/settings` → `DeleteModelOverride`）：类型、推理、工具、输入模态、上下文/输入/输出长度回到快照里的值（models.dev 没有这个模型时按 id 推断：含 `rerank`/`embed` 的是重排/嵌入，其余是文本，工具开），**同时**清掉 models.dev 里没有的管理员设置——「向普通用户开放」回到开、容量上限和统计周期清空、管理员豁免关闭。已花费的统计不受影响（`usage_logs` 不动）。
