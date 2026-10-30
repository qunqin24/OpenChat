# 写作块（文档）

模型帮用户写或改一篇文字（文章、邮件、文案，或用户贴来要改的文字）时，放进「写作块」：回复里是一张卡片，点开在聊天旁边的面板里所见即所得地编辑。规则摘要见 [AGENTS.md](../../AGENTS.md)。

## 数据

- `documents`（属于创建它的 run：`run_id ON DELETE CASCADE`）+ `document_versions`（每次修改一个版本，`source` = `model`/`user`；模型的版本也挂在自己的 run 上）。文档的内容就是最新版本。
- 改写问题、重新生成会删掉回答的 run，写作块和模型那次的版本随之消失，和回答的行一致；用户自己保存的版本不属于任何 run，只随文档一起删。对话删除时级联删除。
- 内容存 Markdown；附件之类没有。

## 模型的工具 `document`（`api/document_tool.go`）

- `create`(title, content)、`update`(id, edits[{find, replace}] 或 content 整篇重写、可改 title)、`read`(id)、`list`。只能动本对话的文档。
- `update` 的每个 `find` 必须在当前文本里恰好出现一次，否则整次调用不改任何东西并说明原因（没找到 / 出现多次）。写入前在事务里锁文档、确认没有新版本插进来（用户刚保存过就报错让模型先读）。
- 文档最新版本是用户写的时，`read`/`update` 的结果会提示「用户改过」。
- 用户自己改过、没告诉模型就让它「接着写」（`api/document_edits.go`）：
  - **轮次说明**：每个 run 第一次请求前问一次 `DocumentNote`（`agent.TurnNote`，`runner.SetTurnNote`），对话里最新版本是用户写的文档（最多 5 篇），按段落列出从「模型知道的那版」到最新版的改动（类 diff：`-` 删、`+` 增、两个空格是上下文段，标题变了另起一行），包在 `<document-edits>` 里，接在这个 run 的输入消息末尾；同一 run 的每次请求都一样（共用前缀缓存），**从不入库**。下一轮还没被模型写过新版就再说一次——每个 run 的说明都不在转录里，只能每轮重给。改动超过 6000 字只说「改动太长，先 read」；用户文本先过 `documentText` 审核，被拒/审核不可用就不给原文。
  - **整篇重写的保护**：`update` 带 `content` 时，最新版是用户的、且这个 run 没看过它，就拒绝并附上改动，记为已看过，模型下一次就能在用户的版本上重写。定点 `edits` 不受限——`find` 对不上自然会失败。
  - 「看过」记在 run 自己的 `agent.RunState`（`Scope.State`，随执行存在，不落库）：建、读、写、轮次说明里给过 diff 的版本。进程重启后清空，最坏多拒一次重写。基准版本是看过的那版，没有就是模型最后写的那版（`LatestModelDocumentVersion`）。
- 系统提示词只在提供了这个工具时多一段说明：长篇写作、改用户的文字时用写作块；回复里不要重复正文；修改用定点编辑。
- 临时对话可以用。

## 审核

模型写的版本直接记为已检查（`checked` = 标题+正文的哈希）。用户的版本保存时不审（否则每次停顿打字都会审），等模型要读它时（`read`/`update` 经 `documentText`）再审，来源 `document`，算一次违规——因为这时它确实要发给模型。被拒时工具返回错误，让模型请用户修改。

## 前端

- 回复里：每次 `create`/`update` 都画成卡片，显示这次调用写出的那个版本（不是最新版）：`create` 是全文；`update` 只显示和上一版相比新出现的段落（`changedPassages`），没变的部分用「⋯」省掉，改动超过六成或只删不增时显示全文。卡片超出一屏才渐隐，可复制、在旁边打开；文档已在面板里打开时缩成贴合文字宽度的小胶囊（`n-doc-pill`：「已修改」+ 标题 + `v<版本>`），点哪个面板就打开到哪个版本，面板正在显示的那版是浅蓝底（`viewing` 因此放在 `document-panel-store`，离开最新版前先保存未存的输入）。正在写时（`WritingCard`）是一张「正在写作…/正在修改…」卡片：标题、字数，和模型正在写的字——`create` 是 `content`，`update` 是各段 `replace`，从流式参数里边到边读（`streamingText`，切在转义中间的半个转义丢掉）；新字在一个 196px 窗口的底部，旧的从顶部淡出；纯文本段落（`shownText`，只排末尾 1200 字），不按 Markdown 渲染，也不给每个字建元素；第一个字之前是三条闪光占位条。`read`/`list` 是活动行里的普通步骤。卡片按文档 id 读版本（`useDocument`），工具结果里的版本号比缓存新就重取。
- 面板（`DocumentPanel`，Tiptap + `@tiptap/markdown`）：打开时聊天栏让出右半边（`.n-main.has-doc`），手机上占满；可全屏。停止打字 1.2 秒后保存为新版本（`PATCH /documents/{id}`，带 `baseVersion`，冲突 409 时载入最新版本）。撤销/重做按钮在版本间前后查看，旧版本只读，可「恢复这个版本」。复制、下载 `.md`。
- 切到只读、按版本载入内容都不能算作编辑：Tiptap 的 `setEditable` 默认会触发 update 事件，必须传 `false`，`onUpdate` 也只在可编辑时记为改动（曾因此把旧版本的文字存成了新版本）。
- 回头找文档：顶栏右侧有「写作」按钮（`DocumentsMenu`，只在对话里有文档时出现），一篇时点了直接打开，多篇时列出全部、按最近修改排序（`GET /conversations/{id}/documents`，`ListConversationDocuments` 带 `user_id`，非本人对话 404），不用滑回写作那一条回复。列表在文档被写出/修改（`DocumentCard`）和用户保存后失效重取。面板与聊天之间的分隔条可拖动（`PanelResizer`，宽度存 localStorage，双击还原）。
- 选中文字出现浮条（`DocumentBubble`，Tiptap `BubbleMenu`）：「提出修改请求」、链接、粗体、斜体、段落样式。样式列表画在浮条里面，不用 Radix 菜单：菜单的 portal 定位在浮条之外，曾跑到页面左上角；浮条的 `mousedown` 都 `preventDefault`，否则编辑器失焦、选区和浮条一起消失。
- 「提出修改请求」把选区序列化成 Markdown（`passageFor` 找原文里能逐字对上的那段），放进输入框上方的引用卡（`document-quote-store`），手机上随即关掉面板。发出去的消息正文是 `<document-selection id="..." title="...">\n原文\n</document-selection>\n\n请求`（`chat-core/src/document-selection.ts`）；用户消息里解析出来画成引用卡，编辑/复制只碰请求那几个字。系统提示词让模型只改这段（`op=update`，以原文作 `find`）。
- 修改过程：`update` 的参数还在流式输出时（`pendingEditOf` 从半截 JSON 里取 id 和 `find`），回复里是「正在修改…」，面板自动打开一次，被改的段落（`blocksTouched`；给了 `content` 就是全部）盖上骨架屏闪光（`document-marks.ts` 的 ProseMirror decoration，`n-doc-rewriting`，`data-motion="essential"`），编辑器只读；新版本到了换上内容，新出现的段落淡淡高亮一下（`n-doc-fresh`）。
- 进行中的修改按卡片实例登记（`document-activity-store`，键是 `useId`），不按工具调用 id：流式回复和存好的消息会同时画同一个调用，按调用 id 时先卸载的那张会把另一张登记的删掉，闪光提前一秒消失。查看旧版本的 effect 只在「看哪个版本」真变了时才重设内容，否则每次重取都会把标记连同内容冲掉。
