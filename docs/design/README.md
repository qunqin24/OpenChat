# docs/design — 功能设计说明

[AGENTS.md](../../AGENTS.md) 只放必须遵守的规则；这里是每个功能的完整设计、理由、错误码和边界情况。改某一块之前先读对应那份。

| 文件 | 内容 |
| --- | --- |
| [conversations.md](conversations.md) | 线性转录、改写/重新生成、删除对话取消 run、被拒绝的 `blocked` 行、上下文压缩与摘要、标题、个人记忆、临时对话、搜索、引用其他对话、导出 |
| [usage-and-limits.md](usage-and-limits.md) | 用量账本与估算、模型容量上限与在飞预留、并发回答上限、「向普通用户开放」、使用日志分页、容量上限的前端 |
| [storage-and-files.md](storage-and-files.md) | 存储驱动、每人的存储上限、上传大小上限与写一次、收据、文本/Word/Excel/PDF 文件的读取、附件清理、头像、版本检查、日志、服务端自带前端 |
| [frontend-chat.md](frontend-chat.md) | 一个聊天页、首条消息流程、流式显现、转录分页与交接、发送与 outbox、事件流、压缩上下文的前端、通用前端约定 |
| [auth.md](auth.md) | 会话、最后一个管理员、停用（403 `account_disabled` 与 `/disabled` 页）、注册与审批、登录/注册限流、删除账号、通行密钥 |
| [writing-blocks.md](writing-blocks.md) | 写作块：文档与版本、模型的 `document` 工具、审核、卡片与编辑面板 |
| [scheduled-tasks.md](scheduled-tasks.md) | 定时任务：表与上限、周期计算、调度器、审核、模型工具 `scheduled_tasks`、页面 |
| [sub-agents.md](sub-agents.md) | 子智能体：隐藏的子对话、`subagent`/`subagent_message`/`subagent_stop`、报告的标记与送达、上限、面板与回复里的行、后台设置 |
| [web-tools.md](web-tools.md) | 联网：`web_search` 的后端与设置、时间范围与限定网站、发布日期、搜索提示词、`web_fetch` 的安全检查、正文抽取成 Markdown、PDF、分段读与查找、短期缓存 |
| [moderation.md](moderation.md) | 内容审核：检查范围、设置、日志与处罚、被拒绝的消息、指令与记忆的审核、用户标识、前端 |
| [brand.md](brand.md) | 品牌：标志（水波）的寓意、登录页标语 |

文中「后端约定」「前端约定」「认证」「内容审核」指 AGENTS.md 的章节，它们的细节分布在以上文件里。
