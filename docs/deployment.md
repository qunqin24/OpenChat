# 部署指南

**简体中文** · [English](deployment.en.md) · [返回 README](../README.md)

这份指南带你把 OpenChat 部署到一台自己的服务器上：Docker Compose 起 Postgres、应用和 Caddy，Caddy 自动签发 HTTPS 证书。

**目录**

- [它是怎么跑起来的](#它是怎么跑起来的)
- [环境要求](#环境要求)
- [配置](#配置)
- [第一次启动](#第一次启动)
- [上线之后做什么](#上线之后做什么)
- [更新](#更新)
- [备份与恢复](#备份与恢复)
- [把附件切到 S3](#把附件切到-s3)
- [放在别的反向代理或 CDN 后面](#放在别的反向代理或-cdn-后面)
- [不用 Docker 运行](#不用-docker-运行)
- [发布新版本](#发布新版本)
- [排错](#排错)

## 它是怎么跑起来的

`deploy/docker-compose.yml` 定义三个服务：

| 服务 | 镜像 | 作用 |
| --- | --- | --- |
| `postgres` | `postgres:18-alpine` | 存全部业务数据 |
| `openchat` | `ghcr.io/qunqin24/openchat` | 应用本体。一个 Go 进程同时提供网页和 API，前端已嵌在二进制里，启动时自动执行数据库迁移 |
| `caddy` | `caddy:2-alpine` | 对外的入口：80/443 端口、自动 HTTPS、HTTP/2 和 HTTP/3，反向代理到应用 |

应用容器只对 Caddy 开放 8080 端口，不直接暴露到公网。

## 环境要求

- 一台 Linux 服务器，装有 Docker 和 Docker Compose 插件（`docker compose version` 能运行）。amd64 和 arm64 都有镜像。
- 一个域名，A（和 AAAA，如果你有 IPv6）记录**已经**指向这台服务器。Caddy 第一次启动时就要用它申请证书。
- 防火墙和云厂商的安全组放行 **80** 和 **443**（TCP），建议也放行 443 的 UDP，HTTP/3 用它。

## 配置

```bash
git clone https://github.com/qunqin24/OpenChat.git openchat && cd openchat
cp deploy/.env.example deploy/.env
```

编辑 `deploy/.env`（这个文件包含密码，不要提交到 Git，仓库已经忽略它）：

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `DOMAIN` | 是 | `localhost` | 站点的域名，不带 `https://`。Caddy 用它签证书，应用用它设置 `CORS_ORIGINS` |
| `POSTGRES_PASSWORD` | 是 | 无 | 数据库密码，用一串随机字符，例如 `openssl rand -hex 24`。留空时 Compose 会拒绝启动 |
| `APP_ENV` | 否 | `production` | `production` 时登录 Cookie 带 `Secure`，必须走 HTTPS。仅在无域名试用时才改成 `development` |
| `OPENCHAT_IMAGE` | 否 | `ghcr.io/qunqin24/openchat:latest` | 应用镜像。想固定版本就写成 `ghcr.io/qunqin24/openchat:1.0.0` |
| `TZ` | 否 | `Asia/Shanghai` | 「一天」「一个月」按哪个时区算：模型每日、每月花费上限的清零时间，以及仪表盘的「今天」。IANA 名称，如 `America/New_York` |

> **关于 `POSTGRES_PASSWORD`**：数据库第一次初始化时就把密码写进了数据卷。之后再改 `.env` 里的值，数据库里的密码不会跟着变，应用会连不上。要改密码请在数据库里执行 `ALTER USER`，再同步 `.env`。

### 固定镜像版本

默认的 `:latest` 在每次发布稳定版时移动。想自己决定什么时候升级，就在 `deploy/.env` 里固定版本：

```bash
OPENCHAT_IMAGE=ghcr.io/qunqin24/openchat:1.0.0
```

镜像标签是版本号去掉 `v`（Git 标签 `v1.0.0` 对应镜像 `1.0.0`）。预发布版本（例如 `1.1.0-rc.1`）只有自己的标签，不会动 `latest`。

### 其他应用变量

用 Compose 部署时下面这些已经设好，不需要动。自己直接运行二进制时才需要关心（见[不用 Docker 运行](#不用-docker-运行)）：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `DATABASE_URL` | 本机开发库 | Postgres 连接串。Compose 里指向 `postgres` 服务 |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `CORS_ORIGINS` | `http://localhost:5173` | 允许的跨域来源，逗号分隔。网页和 API 同源时用不到；Compose 里设为 `https://<DOMAIN>` |
| `SHUTDOWN_TIMEOUT` | `15s` | 收到停止信号后等待请求结束的时间 |

附件存储**没有**环境变量，在后台里设置（见[把附件切到 S3](#把附件切到-s3)）。

## 第一次启动

在仓库根目录执行：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

镜像会从 GitHub Container Registry 拉取。想用当前检出的代码自己构建，就在命令末尾加 `--build`。

看一眼状态：

```bash
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml logs -f openchat
```

三个服务都是 running 之后，打开 `https://你的域名`。第一次访问时 Caddy 要先拿到证书，可能需要几十秒。

### 成为管理员

**第一个注册的账号自动成为管理员**，不受注册开关的限制，也不需要审批。所以上线后请立刻自己去注册，不要先把地址发给别人。

之后的注册规则由管理员在 后台 → 系统 里控制：

| 模式 | “允许注册” | “新用户需审批” | 效果 |
| --- | --- | --- | --- |
| 审批（默认） | 开 | 开 | 任何人可以注册，但账号要管理员在 后台 → 用户 批准后才能登录 |
| 开放 | 开 | 关 | 注册后立即可用 |
| 关闭 | 关 | （不适用） | 不接受自助注册，只有管理员能在 后台 → 用户 新建账号 |

登录和注册的尝试次数按 IP 限流，防止暴力猜密码和批量注册。

## 上线之后做什么

### 添加模型渠道

进入 后台 → 渠道，点击添加：

1. 选一个渠道类型（Anthropic、OpenAI、Gemini、DeepSeek、OpenRouter 等，或者“OpenAI 兼容 / 中转”填自己的地址）。有国内站和国际站的厂商，要选你的密钥所属的那个，两边的密钥不通用。
2. 填 API 密钥。
3. 拉取模型列表，勾选要启用的模型。

之后在 后台 → 模型市场 里可以逐个调整：是否支持推理和工具、开放哪些思考档位、Token 上限，或者关掉“向普通用户开放”，让某个模型只有管理员能用。

要让对话自动生成标题，到 后台 → 系统 指定一个用于后台任务的模型，选便宜又快的；不选就用每个对话自己的模型。

### 联网搜索

后台 → 联网搜索 里打开开关。搜索服务有两种：

- **免费入口**：不用密钥，Exa、Parallel、Firecrawl、Keenable 轮流处理，某家限流就换下一家。有频率限制，适合轻量使用；搜索词会发送给这些服务。
- **Tavily**：需要保存 Tavily 的密钥。

网页读取不依赖搜索服务，始终可用。

### 打开游客模式

游客模式默认关闭。到 后台 → 系统 打开后，登录页会多出“以游客身份使用”。游客不需要账号，对话、记忆和自定义指令只保存在他们自己的浏览器里，服务器不留。

游客的请求不属于任何账号，所以可以限额：每个 IP 每小时最多多少次、全体游客每天最多多少次，填 0 表示不限。**这些限额按访客 IP 计算**，请先读[放在别的反向代理或 CDN 后面](#放在别的反向代理或-cdn-后面)。

### 其他系统开关

后台 → 系统 里还有：自动压缩上下文、记忆、生成式界面、向用户提问、允许上传头像、调试模式。后台 → 系统 的“数据保留”设置已删除对话、未发送上传、孤立文件各保留多久。

## 更新

更新前先[备份](#备份与恢复)。数据库迁移在应用启动时自动执行，不需要单独的步骤。

**用 `latest`（没有固定版本）：**

```bash
git pull
docker compose -f deploy/docker-compose.yml --env-file deploy/.env pull openchat
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

`git pull` 是为了拿到新的部署文件；只有镜像变了，可以省略。

**固定了版本：** 先把 `deploy/.env` 里的 `OPENCHAT_IMAGE`改成新版本，再执行上面的后两条命令。

确认新版本起来了：

```bash
docker compose -f deploy/docker-compose.yml logs --tail 50 openchat
curl -sS --max-time 20 https://你的域名/healthz
```

要回退时，把镜像版本改回去，并且**恢复升级前的数据库备份**。旧版本不认得新版本改过的库，而且它启动时不会报错（它没有要执行的迁移），只会在用到新改动的地方出问题，所以一定要连库一起退回。`docker compose run --rm openchat rollback` 能撤销最近一个迁移，但只适合排查，不能代替备份。

> 只支持单实例运行：一个数据库只配一个应用容器。两个实例会重复执行同一个回答，迁移也可能同时执行。

## 备份与恢复

需要备份两样东西，而且要成对备份：

| 数据 | 位置 |
| --- | --- |
| 数据库 | `openchat_postgres-data` 卷（Postgres） |
| 附件（本地存储驱动）和头像 | `openchat_openchat-data` 卷，容器里是 `/app/data` |

Caddy 的证书在 `openchat_caddy-data` 卷里，丢了会重新申请，不算必须备份。

### 备份

```bash
# 数据库
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_dump -U openchat openchat > openchat-$(date +%F).sql
chmod 600 openchat-$(date +%F).sql

# 附件和头像（把卷打成 tar.gz，放在当前目录）
docker run --rm -v openchat_openchat-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/openchat-data-$(date +%F).tar.gz -C /data .
```

> **备份里有密钥。** 数据库备份包含各渠道的 API Key、联网搜索的密钥、S3 密钥和文件签名密钥，都是明文。像对待密码一样保管它：只放在自己能控制的地方，不要传到公开的网盘或仓库。

如果附件已经切到 S3，新上传的文件在桶里，要在存储服务那一侧做好桶的备份；切换之前留在卷里的旧附件仍然需要备份。

> 数据目录必须和数据库配套。应用会清理 `uploads/` 下没有数据库记录、且超过保留时间的文件，所以不要把一个数据目录指向别的数据库，也不要只恢复其中一边。

### 恢复

在一台全新的部署上，先只起数据库：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d postgres

# 恢复数据库
docker compose -f deploy/docker-compose.yml exec -T postgres \
  psql -U openchat openchat < openchat-2026-01-01.sql

# 恢复附件卷（卷不存在时 docker 会自动创建）
docker run --rm -v openchat_openchat-data:/data -v "$PWD":/backup alpine \
  sh -c 'tar xzf /backup/openchat-data-2026-01-01.tar.gz -C /data && chown -R 65532:65532 /data'
```

（`65532` 是应用在容器里运行的用户，恢复出来的文件要归它所有，否则应用写不了上传和日志。）

再启动其余服务：`docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d`。

> 请恢复到一个应用还没有启动过的新数据库上（如上，先只启动 `postgres`）。应用一旦启动就会建表，往已经建过表的库里再导入会报“已存在”。

## 把附件切到 S3

默认附件存在服务器本地磁盘（`openchat-data` 卷）。想改用 S3 兼容存储（AWS S3、Cloudflare R2、MinIO 等），不需要改部署文件：

1. 在存储服务上建一个桶，并**给桶配置 CORS**。浏览器会直接把文件 `PUT` 到桶里，所以桶必须允许你的站点来源、`PUT` 方法和 `Content-Type` 请求头，否则上传会被浏览器拦下。S3 风格的例子：

   ```json
   [
     {
       "AllowedOrigins": ["https://chat.example.com"],
       "AllowedMethods": ["PUT", "GET"],
       "AllowedHeaders": ["Content-Type"],
       "MaxAgeSeconds": 3000
     }
   ]
   ```

2. 进入 后台 → 文件存储，存储方式选“S3 兼容存储”，填 Endpoint（用 AWS S3 时留空）、Bucket、Region、Access Key ID 和 Secret Access Key。MinIO 一类需要打开“路径风格”。
3. 点“测试连接”，它会实际写入一个小文件、读回再删除，不会保存设置。
4. 通过后保存。**立即生效，不用重启。**

密钥只写不读，保存后只显示末尾四位。留空 Access Key 时，服务端使用自己环境里的 AWS 默认凭证。

切换不会让旧附件失联：读取时先看本地磁盘有没有，再去 S3 找，所以旧文件留在 `openchat-data` 卷里照常可读，新上传的才进桶。这个卷仍然要继续备份。

## 放在别的反向代理或 CDN 后面

默认的 Caddy 已经把这些事做好了。如果你想用自己的 nginx、Traefik，或者前面还有 Cloudflare 一类的 CDN，需要注意下面几件事。

### 把应用交给你自己的代理

去掉 `caddy` 服务（或者不启动它：`up -d postgres openchat`），让应用只监听本机，把 `openchat` 服务的 `expose` 换成：

```yaml
    ports:
      - "127.0.0.1:8080:8080"
```

然后让你的代理把整个站点反向代理到 `127.0.0.1:8080`，并由它终结 HTTPS。nginx 的写法见 [deploy-caddy.md](deploy-caddy.md)。

代理需要做到：

- **不要缓冲事件流**。发消息和对话事件是 SSE（`POST /api/v1/conversations/{id}/messages`、`GET .../events`），要关掉缓冲，并且读超时要足够长，否则长回答会被掐断。
- **使用 HTTPS**。生产模式下登录 Cookie 带 `Secure`，浏览器只在 HTTPS 下发送它。

### 按 IP 限流依赖正确的转发头

游客限额、登录和注册的失败次数限制，都**按访客 IP 计算**。应用拿到的 IP 来自请求头：优先 `True-Client-IP`，其次 `X-Real-IP`，最后才是 `X-Forwarded-For`（取最左边的值），都没有时用连接对端的地址。所以：

- 你最外层的代理必须把这些头**覆盖**成真实访客地址，并丢掉访客自己带来的值。否则任何人都可以随便填一个地址，绕开限额。
- 如果前面还有别的代理或 CDN，要让地址一层层正确传到应用。传不对的话，应用看到的永远是代理的地址，所有访客共用同一份额度：游客限额很快被用完，登录失败次数也会被全站共享，几个人输错密码就可能把所有人挡在外面。
- 在 CDN（例如 Cloudflare）后面使用 Caddy 时，Caddy 看到的对端是 CDN 的地址。需要让 Caddy 信任 CDN 的地址段（Caddy 的 `trusted_proxies` 选项）再取访客地址，具体写法见 Caddy 文档。

仓库里的 `deploy/Caddyfile` 在没有 CDN 的情况下就是对的：它用连接地址覆盖转发头。改它之前先理解上面这几点。

## 不用 Docker 运行

应用就是一个二进制，前端嵌在里面。从源码构建需要 Go 1.26、Node.js 22 和 pnpm：

```bash
make build        # 先构建前端，再编译 bin/openchat
```

然后准备一个 Postgres 18，设好环境变量，运行二进制：

```bash
export APP_ENV=production
export DATABASE_URL='postgres://用户:密码@主机:5432/库名?sslmode=disable'
export CORS_ORIGINS='https://你的域名'
./bin/openchat    # 监听 :8080，启动时自动迁移
```

它同时提供网页和 `/api/v1`。前面仍然要放一个终结 HTTPS 的反向代理，要求同上。附件的本地目录是进程工作目录下的 `./data`，请让它放在持久化的磁盘上。

二进制还有两个子命令：`./bin/openchat migrate` 只执行迁移，`./bin/openchat rollback` 回退最近一次迁移。

## 发布新版本

这一节是给仓库维护者的。发布由 Git 标签触发：

```bash
git tag v1.0.0 && git push origin v1.0.0
```

推送 `v*` 标签后，GitHub Actions 的 Release 工作流会：

1. 构建 `linux/amd64` 和 `linux/arm64` 的镜像，推到 `ghcr.io/qunqin24/openchat`，标签是 `1.0.0` 和 `latest`；
2. 创建同名的 GitHub Release，附上根据提交生成的发布说明和 `docker pull` 命令。

带后缀的预发布标签（例如 `v1.1.0-rc.1`）只推 `1.1.0-rc.1`，不动 `latest`，Release 也标为预发布。

打标签前，先让 `main` 上的 CI 全绿（`make check` 在本地跑的就是同一套）。

> **第一次发布之后检查镜像的可见性。** 新建的 GitHub 容器包默认可能是私有的。如果别人 `docker pull` 时被拒绝，到仓库页面右侧的 Packages 里打开 `openchat`，在 Package settings 里把可见性改为 Public。

## 忘记管理员密码

镜像里带了一个管理工具 `adminctl`，直接改数据库里的账号（会创建不存在的账号，已存在的就改密码和角色）：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm --entrypoint /app/adminctl openchat \
  -email you@example.com -password '新的密码' -role admin
```

它用的是应用同一个 `DATABASE_URL`，改完立刻生效，该账号原来的登录会话会失效。

## 排错

### 登录之后马上又回到登录页

最常见的原因：用 `http://` 访问，但 `APP_ENV` 是 `production`。生产模式下登录 Cookie 带 `Secure`，浏览器不会通过 HTTP 发送它，所以登录“成功”了却立刻掉线。解决办法是用 HTTPS 访问。只是临时试用的话，见 README 的[“没有域名，先在本机试一下”](../README.md#没有域名先在本机试一下)。

其次检查：浏览器是否禁用了 Cookie，以及你的代理是否把 `Set-Cookie` 去掉了。

### HTTPS 证书没有签下来

先看 Caddy 的日志：

```bash
docker compose -f deploy/docker-compose.yml logs caddy
```

然后逐项排查：

- 域名的 A/AAAA 记录是否指向这台服务器。多余的 AAAA 记录指向别处，是常见的坑。
- 80 和 443 是否对外开放：服务器防火墙、云厂商安全组都要看。
- 80/443 是否被别的程序占用（`ss -ltnp | grep -E ':(80|443)\b'`）。
- 域名前面是否有 CDN 的代理模式，它会挡住验证请求。先关闭代理，签到证书后再打开。
- 短时间内反复失败会触发证书机构的频率限制，等一段时间再试。

### 拉取镜像被拒绝（denied / unauthorized）

镜像包是私有的。让维护者把 `ghcr.io/qunqin24/openchat` 包设为 Public（见[发布新版本](#发布新版本)），或者先 `docker login ghcr.io`。

### 应用一直重启，或者 Caddy 返回 502

看应用日志：

```bash
docker compose -f deploy/docker-compose.yml logs --tail 100 openchat
```

常见原因：数据库连不上（`POSTGRES_PASSWORD` 在数据库初始化之后又改过，见[配置](#配置)里的说明）；应用还在启动或迁移，等几秒再试。

### 看日志、打开更详细的日志

```bash
docker compose -f deploy/docker-compose.yml logs -f openchat   # 应用
docker compose -f deploy/docker-compose.yml logs -f caddy      # 代理和证书
docker compose -f deploy/docker-compose.yml logs -f postgres   # 数据库
```

后台 → 系统 里有“调试模式”开关，打开后按 debug 级别输出，立即生效，不用重启。日志量很大，也可能包含请求细节，排查完请关掉。

**日志存在哪、保留多久**

- 每个容器的输出最多保留 5 个 20 MB（`deploy/docker-compose.yml` 里的 `x-logging`），不会写满硬盘。但容器被替换（更新镜像）时，`docker compose logs` 里之前的内容就没了。
- 应用另外把同样的日志写一份到数据卷里的 `logs/openchat.log`，按 20 MB 轮转、保留 5 个旧文件（`openchat.log.1` … `.5`），**更新之后也还在**。环境变量 `LOG_FILE`、`LOG_FILE_MAX_MB`、`LOG_FILE_KEEP` 可以改位置和大小。

```bash
# 看数据卷里的日志（包括更新前的）
docker run --rm -v openchat_openchat-data:/data alpine ls -l /data/logs
docker run --rm -v openchat_openchat-data:/data alpine tail -n 200 /data/logs/openchat.log
```

**按用户反馈的错误编号查**

每一行日志都是 JSON，带上能串起来的编号：

| 字段 | 是什么 |
|---|---|
| `request_id` | 一次请求的编号。发消息等操作失败时，提示里显示的「错误编号」就是它 |
| `run_id` | 一次回答的编号。回答失败时消息下面显示的「错误编号」是它的前 8 位 |
| `user_id` / `conversation_id` | 是谁、在哪个对话；游客的请求带 `guest: true` |
| `model` / `channel_id` | 回答用的模型和渠道（回答相关的行） |

```bash
# 用户报来「错误编号 3f9a1c2e」：找出这次回答的所有日志
docker run --rm -v openchat_openchat-data:/data alpine grep -h '3f9a1c2e' /data/logs/openchat.log /data/logs/openchat.log.1
# 某个用户最近的请求
docker compose -f deploy/docker-compose.yml logs openchat | grep '"user_id":"<用户 id>"'
```

模型服务返回的原始报错只写在服务端日志里（`model request failed`、`run failed` 这几行），界面上只显示翻译过的提示。也可以先去 后台 → 使用日志，按「失败」筛选，看每次失败请求的模型、渠道、错误码和简短原因。

### 附件上传失败（用了 S3）

多半是桶没有配 CORS，见[把附件切到 S3](#把附件切到-s3)。先用后台里的“测试连接”确认密钥和桶本身是通的。

### 想看服务是否活着

```bash
curl -sS --max-time 20 https://你的域名/healthz
```
