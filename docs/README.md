# docs/

给改这个仓库的 agent 和人看的文档。必须遵守的规则和常用命令在仓库根目录的 [AGENTS.md](../AGENTS.md)（`CLAUDE.md` 只是指向它），这里不重复。

| 目录或文件 | 内容 |
| --- | --- |
| [design/](design/README.md) | 各功能的完整设计：对话与转录、用量与上限、存储与文件、聊天页、认证、写作块、定时任务、子智能体、联网、内容审核、品牌。改某一块之前先读对应那份 |
| [conventions.md](conventions.md) | 代码约定与设计取舍：错误处理、sqlc、分页、会话与密码的实现理由、前端 API 调用、表单与表格、i18n、字体与主题、启动画面 |
| [tasks.md](tasks.md) | 常见任务的步骤：加端点、查询、迁移、对话事件、页面、环境变量等，以及验证命令 |
| [pitfalls.md](pitfalls.md) | 已知的坑：症状和原因 |
| [deployment.md](deployment.md)、[deployment.en.md](deployment.en.md) | 部署指南（中文、英文） |
| [releases/](releases/) | 发布说明，每个版本一份 `vX.Y.Z.md`，同时用作 GitHub release 的正文 |
| [research/](research/README.md) | 固定版本的开源项目源码研究与设计建议，不是对我们 API 的说明；当前系统行为以 AGENTS.md、`design/` 和源码为准 |
