# 部署文件

这个目录放生产部署用的文件：`docker-compose.yml`（Postgres、应用、Caddy）、`Caddyfile`、`docker-compose.build.yml`（用当前代码自己构建镜像）、`docker-compose.trial.yml`（没有域名时的纯 HTTP 试用），以及配置模板 `.env.example`。

在仓库根目录，复制 `deploy/.env.example` 为 `deploy/.env` 并填好 `DOMAIN` 和 `POSTGRES_PASSWORD`，然后：

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

配置变量、更新、备份与恢复、时区、限流与代理、发布新版本、排错，都在部署指南里：

- 简体中文：[docs/deployment.md](../docs/deployment.md)
- English: [docs/deployment.en.md](../docs/deployment.en.md)
