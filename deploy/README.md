# 部署

一台装了 Docker 的服务器，一个解析到它的域名。镜像把前端打进了二进制，一个容器同时提供网页和 API；Postgres 存数据，Caddy 负责 HTTPS。

## 第一次上线

```bash
git clone <仓库> openchat && cd openchat
cp deploy/.env.example deploy/.env
# 编辑 deploy/.env：DOMAIN 填域名，POSTGRES_PASSWORD 填一串随机字符（openssl rand -hex 24）
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build
```

- 域名的 A/AAAA 记录要先指到这台服务器，80 和 443 端口要开着，Caddy 第一次启动时会自动申请证书。
- 打开 `https://<域名>`，**第一个注册的账号就是管理员**，所以上线后立刻自己先注册。
- 进 后台 → 渠道 添加模型渠道；需要的话在 后台 → 系统 打开游客模式。

## 更新

```bash
git pull
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build
```

数据库迁移在服务启动时自动执行。

## 数据在哪

| 卷 | 内容 |
|---|---|
| `openchat_postgres-data` | 数据库 |
| `openchat_openchat-data` | 附件（本地存储驱动）、头像 |
| `openchat_caddy-data` | 证书 |

备份数据库：

```bash
docker compose -f deploy/docker-compose.yml exec postgres pg_dump -U openchat openchat > backup.sql
```

附件也可以之后在 后台 → 文件存储 切到 S3 兼容存储，不用改部署文件。

## 注意

- `APP_ENV=production` 时登录 Cookie 只走 HTTPS。没有域名、只想用 `http://IP:8080` 先试一下的话：把 `openchat` 服务的 `expose` 换成 `ports: ["8080:8080"]`，`APP_ENV` 设为 `development`，不启动 caddy（`up -d postgres openchat`）。
- 游客模式按 IP 限流读的是 `X-Forwarded-For`。Caddy 会用访客的真实地址覆盖它；如果前面还有别的代理或 CDN，要让它们也正确传递，否则限流按代理的地址算。
- 看日志：`docker compose -f deploy/docker-compose.yml logs -f openchat`。
