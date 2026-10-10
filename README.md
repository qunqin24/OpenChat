<div align="center">

<img src="design/logo-tangent.svg" width="112" height="112" alt="OpenChat">

# OpenChat

**自托管的 AI 聊天应用：接入你自己的模型渠道，一个二进制、一个 Docker 镜像就能上线。**

**简体中文** · [English](README.en.md)

[![License: MIT](https://img.shields.io/github/license/qunqin24/OpenChat)](LICENSE)
[![Release](https://img.shields.io/github/v/release/qunqin24/OpenChat?include_prereleases&sort=semver)](https://github.com/qunqin24/OpenChat/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/qunqin24/OpenChat/ci.yml?branch=main&label=CI)](https://github.com/qunqin24/OpenChat/actions/workflows/ci.yml)
[![Docker 镜像](https://img.shields.io/badge/ghcr.io-qunqin24%2Fopenchat-2496ED?logo=docker&logoColor=white)](https://github.com/qunqin24/OpenChat/pkgs/container/openchat)

[快速开始](#-快速开始) · [部署指南](docs/deployment.md) · [功能](#-功能) · [开发](#-开发) · [贡献约定](AGENTS.md)

</div>

<!--
截图占位：有了截图再放开下面几行，并把图片放进 docs/images/。
<p align="center">
  <img src="docs/images/chat.png" width="860" alt="聊天界面">
</p>
-->

## 这是什么

OpenChat 是一个可以自己部署的 AI 聊天网站。你在后台填入模型厂商或中转服务的密钥（渠道），用户就能在网页上和这些模型对话。它适合：

- 想给自己、家人、团队或小社区提供一个统一聊天入口，又不想把对话交给第三方产品的人；
- 手里有多个模型厂商的 API，想在同一个界面里切换、统一计费统计的人；
- 想要一个结构清晰、能自己改的全栈项目（Go + React）的开发者。

服务端是一个 Go 程序，前端打包后嵌在二进制里，数据放在 PostgreSQL。没有 Redis，没有消息队列，也没有单独的前端站点。

## ✨ 功能

### 模型

- **多渠道接入**：内置二十多种渠道类型，包括 Anthropic、OpenAI、Google Gemini、DeepSeek、月之暗面、智谱、阿里云百炼、火山方舟、硅基流动、阶跃星辰、MiniMax、OpenRouter、OpenCode Go 等，也支持 Ollama、LM Studio 和任意 OpenAI 兼容的中转地址。部分厂商同时提供国内站和国际站两个入口，添加渠道时选择密钥所属的那一个。
- **模型市场**：模型能力、上下文长度和价格元数据来自 [models.dev](https://models.dev) 目录，你可以从各渠道拉取模型列表，挑选要启用的模型。
- **逐模型设置**：覆盖模型类型、是否支持推理与工具、输入模态、开放哪些思考档位、最大输入/输出 Token；也可以把某个模型设为**仅管理员可用**。
- **思考过程与强度**：支持推理的模型会显示思考过程，用户可以在输入框里选择思考强度。

### 对话

- **流式回答**，随时可以停止，已生成的部分会保留。
- **联网搜索与网页读取**：模型可以调用搜索和读取网页。搜索服务可选免密钥的公共入口（Exa、Parallel、Firecrawl、Keenable 轮流使用，轻量使用）或 Tavily，并可设置站点黑名单。
- **生成式界面**：模型可以用卡片、指标、表格、步骤等组件组织回答。后台开关，默认关闭。
- **向用户提问**：遇到只有用户才能决定的事，模型会弹出选择题并等待回答。后台开关，默认开启。
- **附件**：图片（PNG、JPEG、WebP、GIF，单个不超过 5 MB）和 PDF（不超过 10 MB），模型需要支持相应的输入类型。
- **编辑提问与重新生成**：改写一条提问，或让模型重答最后一轮，会用新内容**替换**其后的内容，被替换的消息会被删除。
- **上下文窗口指示**：显示当前对话已用了模型上下文窗口的多少。
- **自动压缩上下文**：对话接近模型的上下文窗口时，较早的历史由该对话自己的模型概括，最新的提问原文保留。原始消息仍然显示在对话里，中间有分隔线，可以撤销。默认开启，后台有开关。
- **搜索对话**：按标题和消息内容搜索自己的全部对话。
- **临时对话**：不进列表、不起标题、不使用记忆，24 小时后自动删除。

### 个性化与隐私

- **自定义指令与个人记忆**：用户可以写下关于自己的说明和回答风格，模型可以记住长期信息供之后的对话使用。用户随时可以查看、修改、删除，并撤销模型最近的改动。管理员只有一个总开关，**看不到任何人的记忆和自定义指令**。
- **游客模式**：管理员打开后，登录页出现“以游客身份使用”。游客不需要账号，对话、记忆和自定义指令只保存在他们自己的浏览器里，服务器不留。按 IP 和总量限额，默认关闭。
- **我的数据**：导出自己的全部对话（JSON），或一键删除全部对话。
- 界面支持**简体中文和英文**，外观可选跟随系统、浅色、深色。

### 账号与注册

- **注册控制**：后台有“允许注册”和“新用户需审批”两个开关（默认都开），组合出**关闭**、**开放**、**审批**三种模式。**第一个注册的账号成为管理员**，不受开关限制。
- 登录和注册的尝试次数按 IP 限流。
- 会话用 HttpOnly Cookie，数据库里只存令牌的哈希。

### 管理后台

- **仪表盘**：请求数、Token、花费、缓存命中率与缓存节省的费用、失败和延迟、各渠道健康、用户花费排行。
- **使用日志**：逐条记录每次模型请求，可筛选，游标分页。
- **渠道、模型、用户**：增删渠道，管理模型，批量启用或停用账号、审批新用户。
- **文件存储**：附件放本地磁盘或任意 S3 兼容存储（AWS S3、MinIO、Cloudflare R2 等），在后台切换，立即生效，不需要重启，旧文件仍然可读。
- **数据保留**：设置已删除对话、未发送上传和孤立文件各保留多久。
- **系统开关**：游客模式与限额、记忆、生成式界面、向用户提问、头像上传、后台任务（如生成标题）使用的模型、调试日志等。

### 部署

- **单个二进制，前端内嵌**：一个进程、一个源，不需要单独的静态站点。静态资源构建时预压缩（brotli、gzip）。
- **多架构 Docker 镜像**：`ghcr.io/qunqin24/openchat`，支持 `linux/amd64` 和 `linux/arm64`。
- 数据库迁移嵌在二进制里，启动时自动执行。

## 🚀 快速开始

需要一台装了 Docker（含 Compose）的 Linux 服务器，一个解析到它的域名，并放行 80 和 443 端口。

```bash
git clone https://github.com/qunqin24/OpenChat.git openchat && cd openchat
cp deploy/.env.example deploy/.env
# 编辑 deploy/.env：DOMAIN 填你的域名，POSTGRES_PASSWORD 填一串随机字符（openssl rand -hex 24）
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

然后打开 `https://你的域名`：

1. **立刻注册第一个账号**，它就是管理员。
2. 进入 后台 → 渠道，添加模型渠道，拉取并启用模型。
3. 回到聊天页，开始使用。

Caddy 会自动申请并续期 HTTPS 证书，第一次启动需要几十秒。证书没有签下来、登录不生效等问题见[部署指南的排错一节](docs/deployment.md#排错)。

### 没有域名，先在本机试一下

生产模式下登录 Cookie 带 `Secure` 标记，浏览器只会在 HTTPS 下发送它，所以用纯 HTTP 访问会“登录了又掉线”。只想试用的话，改用开发模式、直接暴露应用端口：

`deploy/docker-compose.trial.yml` 已经写好了这些改动，叠加上去、不启动 Caddy 即可：

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.trial.yml --env-file deploy/.env up -d postgres openchat
```

打开 `http://服务器地址:8080`。**这只是试用**，正式使用请走上面的 HTTPS 部署。

## 📦 部署指南

完整的部署指南在 **[docs/deployment.md](docs/deployment.md)**，涵盖：

- 环境要求与配置变量，固定镜像版本
- 第一次启动、成为管理员、添加渠道、打开游客模式
- 更新、备份与恢复
- 把附件切到 S3
- 放在其他反向代理或 CDN 后面（以及按 IP 限流为什么依赖转发头）
- 发布新版本
- 排错

`deploy/` 目录下的简短说明见 [deploy/README.md](deploy/README.md)；用 nginx 代替 Caddy 的写法见 [docs/deploy-caddy.md](docs/deploy-caddy.md)。

## 🛠 开发

需要 Go 1.26、Node.js 22 和 pnpm（版本由根 `package.json` 的 `packageManager` 指定），以及一个 PostgreSQL 18：可以用仓库里的 Docker Compose 起一个，也可以把 `DATABASE_URL` 指向任何现成的 Postgres 18。

```bash
make setup   # 安装依赖，创建 .env
make up      # docker compose 起 Postgres + MinIO（有自己的 Postgres 可以跳过）
make ser     # API，:8080，启动时自动迁移
make web     # Vite，:5173，/api 代理到 :8080
make check   # lint + test + build，也就是 CI 跑的那一套
```

打开 `http://localhost:5173`。`make help` 列出全部命令。

- 想得到和线上一样的单个二进制：`make build`，产物是 `bin/openchat`。
- 契约在 `api/openapi.yaml`，改完运行 `make generate`，会重新生成 Go 服务端接口和 TypeScript 客户端；标了 `DO NOT EDIT` 的文件不要手改。
- 带数据库的 Go 测试读取 `TEST_DATABASE_URL`，没有就跳过；`make test` 会基于 `.env` 里的数据库自动设置。
- 目录：`api/` 契约，`apps/server/` Go API，`apps/web/` React 前端，`packages/api-client/` 生成的类型化客户端，`deploy/` 生产部署文件，`design/` 标志。
- 架构约定、两套设计系统的边界、各项功能的行为细节都写在 **[AGENTS.md](AGENTS.md)**，改代码前请先读它。

## 🧱 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go、chi、pgx、sqlc、goose（迁移嵌入二进制）、oapi-codegen |
| 数据库 | PostgreSQL 18 |
| 契约 | OpenAPI 3.1，同时生成 Go 服务端接口和 TypeScript 客户端 |
| 前端 | React 19、TypeScript、Vite、TanStack Router / Query、Zustand、Tailwind CSS 4 |
| UI | 聊天界面自成一套设计系统；管理后台使用 shadcn/ui |
| 国际化 | i18next（简体中文、英文） |
| 文件存储 | 本地磁盘，或 S3 兼容存储 |
| 部署 | 单个二进制（前端内嵌），Docker 镜像基于 distroless，Caddy 提供 HTTPS |

## 📄 许可证

[MIT](LICENSE)
