# 前端：聊天页与数据约定

聊天页（一个页面、一个实例）、首条消息流程、流式显现、转录分页与交接、发送与 outbox、已结束的 run、事件流、会话失效、压缩上下文的前端，以及通用的前端约定。规则摘要见 [AGENTS.md](../../AGENTS.md)；下文为完整设计说明。通用的前端约定（目录、构建、两套设计系统）见 [../conventions.md](../conventions.md)。

> 文中「后端约定」「前端约定」「内容审核」等指 AGENTS.md 的章节，细节按主题分在本目录各文件，索引见 [README.md](README.md)。

## 数据、路由与会话

- 服务端数据用 TanStack Query（键在 `chat-core/src/query-keys.ts`、`features/admin/use-admin.ts`）；流式中间态和 UI 状态用 Zustand。流结束后让查询失效，重新拉取。查询键含筛选条件时加 `placeholderData: (previous) => previous`。
- 路由守卫挂在无路径布局的 `beforeLoad`：`_authed.tsx` 要求登录，`admin.tsx` 要求管理员。会话用 `currentSession(queryClient)`（`features/auth/use-auth.ts`）取：5 分钟内查过的直接用，更老的重新问服务端（别用 `ensureQueryData`，它不管多旧都返回缓存，被停用的账号会一直半死不活）；重新问失败（断网）时退回上次的答案。
- 会话在页面开着时失效靠请求的错误码发现。改了密码或 cookie 过期是 401；账号被停用**不是**：会话保留，每个请求都回 403 `account_disabled`（见 [auth.md](auth.md#停用账号)），下一条分开说。401 这条路：`lib/query.ts` 的 `QueryCache`/`MutationCache` 把每个失败交给 `setErrorListener` 登记的监听者（`main.tsx` 里是 `leaveIfSessionExpired`，`lib/session-expired.ts`），不走缓存的调用（事件流）自己调 `reportError`。401（`invalid_credentials` 除外——那是登录表单自己的错，而且认证接口抛的是 `AuthFieldError`）且页面此前认为已登录，就清缓存、清 chat store、清本地存的缓存，`replace` 到 `/login?redirect=…`；已在 `/login`、`/register` 不动。事件流收到 401 或 403 `account_disabled` 就 `reportError` 一次并停止重连（重连永远是同样的拒绝，而别的查询未必会再请求一次、让页面去 `/disabled`；`chat-core/conversation-follow.ts`）。新写的不经查询缓存的网络调用，遇到 401 也要 `reportError`。
- **被停用的页面**：`main.tsx` 的 `setErrorListener` 同时调 `leaveIfSessionExpired` 和 `showIfAccountDisabled`（`lib/account-disabled.ts`）。后者在错误是 403 `account_disabled` 时把详情记进 `useAccountDisabled` store，并 `replace` 到 `/disabled`（已在 `/disabled`、`/login`、`/register` 不动）；`_authed.tsx` 的守卫重新问会话时遇到同样的 403 也跳过去。`/disabled` 是 `routes/disabled.tsx`，在登录布局之外：`beforeLoad` 里 store 没有详情（刷新页面、直接打开）就问一次 `/auth/session`——仍是 403 就记下详情留在这页，成功了说明已重新启用、回首页，别的错误去 `/login`。页面是 `components/auth/DisabledPage.tsx`（样子是 `AccountOverlay.tsx` 里的 `DisabledScreen`，和登录页的覆盖层同一块）：说明原因；「重新检查」再问一次 `/auth/session`，恢复了就清详情、让查询重取、回首页，还停用着就提示仍被停用；「退出登录」才真的退出（`useSignOut`）。
- 换人要清干净：登录、注册成功和退出都 `queryClient.clear()` + `resetChatStore()` + `forgetQueryCache()`；`adoptQueryCache` 发现缓存属于别人（本页内存里的或上次存的）时，除会话外的查询全部移除（记忆、个性化、后台的也算）并重置 chat store。

## 一个聊天页

- **一个聊天页**（`components/chat/ChatPage.tsx`，规则在 `chat-core/src/chat-view.ts`，有测试）：空白新对话页（`/`）和每个对话（`/chat/$id`）是同一个 `ChatView` 实例，像 Open WebUI 的同一个 `Chat.svelte`。路由是无路径布局 `_authed._chat.tsx`，它渲染 `ChatPage` 而**不渲染 `<Outlet />`**；子路由（`index`、`chat.$conversationId`）没有组件，只是地址，`chat.$conversationId` 的 loader 照旧预取（悬停预加载 `defaultPreload: 'intent'` 没变）。`ChatPage` 持有 `ChatView`（`epoch`、`route`、`shown`），`key={epoch}`：地址变到**页面没在显示的**对话（侧栏、搜索、前进后退、深链、别处删除后回首页）就 `epoch+1`，页面整个重来，和以前的 `key={conversationId}` 一样；地址变到页面**已经显示的**对话（首条消息自己建的那个）不动页面——这是整个重构的目的。首条消息的流程在 `ChatView.handleSend`：先 `post('', item)` 立刻显示气泡，`createConversation`（`onSuccess` 把详情和一页空转录塞进缓存，所以新页面没有 loading 也没有 `temporary` 样式闪烁），`post(id, item)`，`onCreated(id)`（页面从此显示这个对话，地址还在 `/`），`sendMessage.mutateAsync`，**服务器答复之后**才 `navigate({ to: paths.chat, replace: true })`（`replace`：空白页只是草稿，不是回得去的地方，和 Open WebUI 的 `history.replaceState` 一样）。所以失败路径永远在 `/` 上：没写任何东西（`startWroteNothing`）就 `deleteConversation` + `onRemoved(id)`（页面回到空白页，文字和文件原样在输入框里）；被审核拒绝等则 navigate 到对话。`outbox['']` 只在建对话那一小段用，页面显示对话后由 effect 放掉（先放会让空白页多画一帧欢迎页）。页面卸载后（用户在等待中点了别的对话）这次发送在后台完成，`alive` ref 挡住一切对页面的操作，不会把用户拉回去；空对话照样删。新增在首条消息路径上写页面状态的代码要过 `alive`。**等待期间点「新对话」**：地址还是 `/`，而路由器对「去当前地址」什么都不做（同址只重新 load，不留痕），页面无从得知——所以每个「新对话」入口（侧栏的链接和 ⌘⇧O、标志、搜索面板）都带 `state: newChatState`，在 history state 里放一个每次不同的 `newChat` 标记；`ChatPage` 看到**新出现的**标记、且地址是 `/` 时 `startOver`（页面比地址超前就重来成空白页，本来就是空白页则不动，草稿保留），重来后旧页面卸载，`alive` 挡住它的后续跳转，那条消息在后台照常发完。只认「出现」不认「消失」：跳到 `/chat/<id>` 时标记没了，而路由器先更新 location 再更新 params，那一帧里路由还读作 `/`，当成请求会把刚建的对话页重来一遍。新增「新对话」入口要带 `newChatState`。没有 `pendingPrompt`/`returnedDraft`/`handOff`/`is-carried`——它们是旧的「两个页面之间交接」的产物，现在没有交接。`chat-core/src/chat-view.ts` 的 `adopt(view, id)` 是另一回事：空白页为刚发的消息建好对话后，`onCreated(id)` 经它让页面从此显示这个对话（`shown` 设成 `id`；已经显示着某个对话的页面不理会）；建好的对话被拒绝而删掉时，`onRemoved(id)` 经 `release` 把页面放回空白页。

## 改写与重新生成的显示、转录分页与交接

- 没有版本翻页器。改写和重新生成从点击起就把要删的行藏起来（store 的 `replacing`：`{from}` 是被改的提问及其后，`{after}` 是最后一条提问之后；`buildTurns` 用它），服务端接受后用 `truncateTranscript` 把这些行从缓存里去掉，再让 `replacing` 结束；run 的输入已在列表里（重新生成）时 `buildTurns` 也会藏掉它后面不属于这个 run 的行。
- 对话记录从尾部分页：`GET .../messages` 首页是最新的一页（页内仍是旧→新），`before` 取更早的页；`hooks/use-messages.ts` 是 `useInfiniteQuery`（`pages[0]` 最新），`ChatView` 只渲染最后 20 个 turn，滚到顶部再多画 20 个，画到已加载的最旧处才请求更早一页。run 结束只用 `refreshNewestMessages` 重取最新一页并拼接（交接逻辑在 `chat-core/src/hand-over.ts`，有测试：只有刷新**确认成功**——`refreshConfirmed`，兜底的 `refetchQueries` 也要 `throwOnError`——之后才 `settle` 本地的 live 副本；失败时答案留在屏幕上，隔几秒重试，之后任何一次成功的刷新、重连后的 snapshot 都会放手），不要对该键 `invalidateQueries`（会按游标重下所有已展开的页）。

## 发送、outbox 与已结束的 run

- 发送：`outbox` 里的气泡由 `useSendMessage` 的 hook 级 `onError` 撤掉（调用级的 `mutate(…, { onError })` 在组件卸载后不执行，发完立刻切对话就会留下永远等回复的气泡）；新对话的第一条消息也一样：它用 `sendMessage.mutateAsync({ conversationId: 刚建的 id, … })` 发出，id 由调用传入（`useSendMessage` 的 `variables.conversationId`），hook 级的 `onSuccess`/`onError` 照样用它。运行中（`working`、`outbox`、`live` 在跑）不能再发：输入框的回车、`handleSend` 都挡掉。
- 发送的响应可能晚于事件流：SSE 上 `run_started → run_finished → 重取消息 → settle` 全走完，那个 POST 的响应才回来，它（以及重连时拍得更早的 snapshot）仍写着 `running`。store 的 `finished`（有界，最近 `FINISHED_RUNS_KEPT` 个 run id，`rememberFinished`）记住已结束的 run：`run_finished` 批次、非 running 的 snapshot、`settle` 都会登记；`startRun`（发送、重新生成、改写、回答 ask_user 都走它）、`applySnapshot`、`applyBatch` 里的 `run_started` 遇到登记过的 run 一律不写回 `live`（`startRun` 仍清掉 `outbox`/`replacing`）。run 只前进不后退；新增任何由响应或旧快照驱动、会写 `live` 的路径都要过这道检查。`resetChatStore` 一并清空。
- 附件只在发送响应里有：`run_started` 事件里的那份提问不带附件（链接按读者签名，所有订阅者共用的事件给不了）。两种先后都要保住响应那份的附件：响应先到，`applyBatch` 处理 `run_started` 时保留已有的附件；事件先到，`startRun` 用 `withSentInput`（`chat-core/src/run-state.ts`）把响应的附件放到已有的提问上。少了后一种，图片在发出后消失、直到回答结束重取消息才出现（v1.1.6 及之前）。其他标签页只收到事件，回答期间看不到图，结束后重取即正常。
- 输入框的「+」是一个菜单（`AttachMenu`，照 OpenWebUI 的 `InputMenu`）：上传文件、截图（`getDisplayMedia` 取一帧，手机上没有）、从文件库添加（取签名链接再上传一份新的）、引用其他对话。后两项不弹出第二个菜单，而是把菜单本身换成列表（返回行、搜索框、可滚动的列表，滚到底再取下一页），宽度不变（220px，比 OpenWebUI 的 280px 窄），列表从右侧滑入、返回从左侧滑回；搜索框接住打字，↓ 进入列表，空框按退格返回。文件的来源都先变成一个 File 再走 `useAttachments`，所以模型能收什么、大小上限、存储上限和发送时的审核都照旧。引用对话不是文件：输入框记下 `{conversationId, title}`（`ConversationChip`：照 OpenWebUI 的小号 FileItem，一行的胶囊，自绘的双气泡图标、标题、末尾「对话」，和文件同排换行），发送时作为 `conversationIds` 交给服务器，失败时和文件一样回到输入框；发出的消息带 `references`，显示成同样的胶囊，点一下打开那个对话。服务端怎么读见 [对话与转录](conversations.md#引用其他对话)。
- 文本、Word、Excel、PDF 附件点开在侧边面板里读（`FilePanel`、`stores/file-panel-store.ts`），和写作块、子智能体面板互斥；文件库里点开则铺满整个窗口（`full`，位置记作 `LIBRARY`，离开文件库就关）。Word 用 `docx-preview` 按原件排版（文件没写页面设置时按 A4、Word 默认页边距），整页缩放到面板宽，画在一个不许跑脚本的沙箱 iframe 里（`sandbox` 不含 `allow-scripts`，样式也关在里面），不渲染内嵌 HTML（`renderAltChunks: false`），链接只留 http(s)、mailto 和文内锚点、在新标签打开；PDF 用 `pdf.js` 逐页画（接近视口才画，`cmaps`/`wasm` 由 `vite.config.ts` 的 `pdfjsAssets` 拷进构建、放在 `/pdfjs/`）；这两个库只在打开对应文件时按需加载。Excel 和 CSV/TSV 画成带工作表标签、列字母、行号的表格（`SheetView`，`chat-core/src/workbook.ts`，Excel 用服务器读出的文字），只画看得见的行（固定行高、列宽按前 500 行一次算好，列数按全部行），拖动面板宽度时 PDF 和 Word 先按比例缩放、停下后再重画；Markdown 文件按聊天回答的样子渲染（`Markdown`）；复制按钮复制文字（文档是发给模型的那份），下载是原件。消息和输入框里的文件卡片是浅灰底加彩色折角角标（`FileBadge`，写扩展名），库的网格卡片显示文件开头的内容预览（`FilePagePreview`，`GET /files/{id}/text?preview=true`）。其余：内容直接 `fetch` 附件的签名链接（桶的 CORS 要放行 `GET`，见部署文档），不进查询持久化。文件图标按类型着色（`FileIcon`，分类在 `chat-core/src/file-look.ts`，文本文件按扩展名分）。

## 对话标题的显示

- 新对话页（`Welcome`、`WelcomeStarters`，样式在 `chat-layout.css` 的 `.n-main.is-welcome`）：像 Claude 一样，我们的水波标志加一句按时段的问候（`chat-core/src/greeting.ts`：早上、中午、下午、晚上、深夜各有几句，平实的、带用户名的、俏皮的都有，周一早上、周五、周末白天另有几句，元旦、情人节、圣诞等固定日期的节日当天只说节日的话；句子直接读语言文件的 `chat.welcome.lines`、`chat.welcome.holidays`，加一句只改语言文件，带不带名字看英文原文里有没有 `{{name}}`；随机抽一句，不和上次显示的重复（记在 localStorage），没有名字时只抽不带名字的；衬线字，打开页面时定下不再变）；宽屏上问候和输入框一起在页面略偏上的正中，输入框比对话页窄（52rem），建议是输入框下面一排胶囊，每个带一个专门画的线条图标（`StarterIcon`，不用图标库，点睛的一笔按建议上色、深色主题浅一档），点了只填进输入框不发送；有了消息就回到普通布局，输入框在底部：离开欢迎页的那一帧，`ChatView` 用记下的上一次位置把输入框挪回原处再放开（`element.animate` 的 FLIP，`DOCK_GLIDE_MS`），宽度由 `.n-column` 的 `max-width` 过渡跟上，减少动态效果时都不做。手机上输入框始终在底部，建议横排在它上方、可以左右滑。临时聊天显示幽灵图标和说明，不显示问候和建议。输入框仍然只有一个实例，欢迎页只是换布局。
- 对话标题：服务端把没起名的对话叫 `New chat`（英文，不随语言），显示时一律经 `chat-core/src/titles.ts` 的 `displayTitle`/`useDisplayTitle`（侧栏、搜索、对话页顶、删除确认），它把空标题和 `New chat` 换成 `chat.untitled`；重命名框也以显示出来的名字起头。别直接渲染 `conversation.title`。

## 流式文字的显现与滚动

- 流式文字的显现（`components/chat/arrival-fade.ts` 的 `useArrivalFade`，`MarkdownText.tsx` 的 `Markdown`/`PlainReveal` 用它）：文字**一到就显示，不控节奏**，同一次渲染新增的字算一批，整批在 0.6 秒内一起淡入（`ARRIVAL_FADE_MS`，ease-in-out；什么算一批、浮层怎么淡，在 `chat-core/src/arrival.ts`，App 也用，见 [mobile.md](../mobile.md)），观感取自 flowtoken 演示页（diff 模式、0.6s fadeIn）。实现不碰 React 渲染出的文字：`.n-markdown` 里和 `.markdown-body` 并列一个 `.n-arrival` 层，每次渲染后按 `.markdown-body` 的 `textContent` 长度认出新增的字（Markdown 变长时前面的字位置不变，新字就是最后那些），只从末尾往回走到最早一批的起点，对每批每个文本节点取 `Range.getClientRects()`，每行盖一块该文本所在表面颜色（`surfaceOf`，代码块、表格各用自己的底色）的浮层，rAF 把透明度从 1 降到 0：在纯色表面上这就等于文字自己淡入。元素数只取决于最近 0.6 秒到了几批几行（实测流式时 5~9 个，结束后为 0），和回答多长无关。只有 `text` 变了的那次渲染才算一批：图表、公式在源码之后才画出来、代码高亮这些也会让 `textContent` 变长，悄悄算进已显示的长度，不淡入；图表和公式里的字也从不盖浮层（v1.3.1 把滑动时才画完的图表整张当成一批，几百个标签逐个量位置、中间穿插写入，每次都触发整页样式重算，页面卡了好几秒）。浮层是先把所有位置量完再统一写入；一批超过 4000 字（`MAX_BATCH_CHARS`，中转站一次推来整段、打开已写完的回答）直接显示。挂载时已有的文字直接显示，`fresh`（读者看着开始的零件，`LiveRun.fresh`/`freshFrom` 记下）则首批也淡入；文字变短（被替换）、标签页隐藏、`prefers-reduced-motion` 时不淡入。v1.1.0 之后到这次改动之前，网页用 `useReveal` 显示已收到文字的前缀、逐帧多显示一点（节奏是当时的 `reveal.ts`，App 起初也照搬，后来同样换成整批淡入，`reveal.ts` 已删），末尾盖一段 2.5em 的渐变遮罩（`placeTail`）；对比了 flowtoken、lobehub/streamdown（逐字 180ms 淡入、强制控节奏）、smooth-stream-text（逐词模糊、带节奏）后改成现在这样，lobe 的 `<Streamdown>` 关不掉节奏，做不出整批淡入，所以没换渲染器。**不要给每个字或词建元素**：v1.1.0 用 Streamdown 的 `animated` 给每个字包一个带淡入动画的 span，长思维链展开时有上万个节点和动画，主线程占用随时间涨到一个核的 90%，线上弱电脑直接卡死；它按空格分词时中文整段是一个 span，同一句里新到的字根本不淡入。flowtoken 本身也是每批一个 span、永不回收，同样会越写越多。回复的零件在「流式 partial → 存好的回复 → 交给消息列表」之间是**同一个元素**（`AssistantTurn` 里按位置起 key：第几个回复 `r<n>`、第几个零件，不按消息 id），最后一批才能淡完、不会重新挂载——别改回去。思维链折叠时内容不渲染（Radix Collapsible）。性能回归用无头 Chromium 测（`Performance.getMetrics` 的 `TaskDuration`），浏览器面板在后台时 rAF 被节流，测不准。
- 脚本触发的滚动（`scrollTo`/`scrollBy` 的 `behavior`）用 `lib/motion.ts` 的 `smoothScrollBehavior()`，CSS 的 reduced-motion 管不到它。工具结果里来的链接（`ToolActivity`）只允许 `isHttpUrl`（`chat-core/src/urls.ts`）通过，和 GenUI 的链接一样。

## 表格与后台

- 表格是服务端分页/筛选；`getRowId` 用记录 id；列译名同时放进 `meta.title`。
- 游标分页的表格（使用日志）用 `DataTableCursorPagination`：游标只会往前，往回就是回到走过的游标，所以组件里留着已访问的游标栈，换筛选或每页行数就清空重来。
- 管理后台的进度条挂在根（`AdminProgress`），只在目标路径在 `/admin` 下时才渲染——admin 布局在第一次从聊天进入时还没挂载，挂在布局里就晚了；聊天侧用自己就地的加载提示，不要给它加条。

## 侧边栏、删除对话与事件

- 侧边栏标记正在回答的对话靠 `GET /conversations/running`（`useRunningConversations`）：有在跑的才每 3 秒轮询，`run_started` 时由事件流让它失效；某个 id 消失时刷新列表（标题、顺序可能变了）。当前打开的对话不标，它自己的页面已经在显示。
- 置顶对话：`conversations.pinned_at`（NULL 为未置顶），`PATCH /conversations/{id}` 的 `pinned` 置顶或取消，不动 `updated_at`（置顶不算活动），临时聊天不能置顶（400 `temporary_not_pinnable`）。侧栏最上面的「置顶」分组来自单独的 `GET /conversations?pinned=true`（最新置顶在前，一页最多 200，不分页，因为置顶的可能很旧），其余对话照旧按时间分组、去掉已置顶的；列表本身仍包含置顶对话。前端键 `queryKeys.conversations.pinned()` 放在 `list()` 下面，凡是刷新列表的地方都会一起刷新、一起持久化；置顶/取消时先改本地缓存再刷新，行在菜单关上时就挪过去。菜单项在侧栏行的「…」里（`SidebarConversation`）。
- 删除对话：服务端在删除提交后往该对话的事件流发 `conversation_deleted`（nil run id、version 0；全部删除时每个对话各一条），正在看它的页面（任何标签页、任何设备）收到就停止跟随并回到首页。同一浏览器的其他标签页靠 `lib/conversation-sync.ts` 的 BroadcastChannel（`announceDeleted` / `followDeletions`）立刻从侧栏和搜索里去掉它。新增删除对话的路径要两样都做。
- 收到 `transcript_changed` 时（`chat-core/src/conversation-follow.ts`，网页和 App 共用）先按事件的 `from`（或本页自己的 `replacing`）截缓存，赶在 `run_started` 结束 `replacing` 之前，再用 `refreshNewestMessages` 重取最新一页，不要 `invalidateQueries` 消息键。`spliceNewest`（`chat-core/src/transcript-pages.ts`，有测试）在新一页的第一行不在已持有的行里时整页替换，所以行被删光也对。重新生成时其他标签页没有 `from`，靠 `buildTurns` 里「run 的输入已在列表」那条藏旧回答。

## 压缩上下文

- 压缩上下文：摘要在查询 `queryKeys.conversations.compaction(id)`（`hooks/use-compaction.ts`，不落本地缓存），由事件流维护：`compaction_finished`（带摘要）、`compaction_changed` 直接写进缓存，`transcript_changed` 和 run 结束时重新读。原消息**照常全部显示**，`buildTurns(…, summary)` 只在摘要终点那条消息之后标一个 `after`（整个回复的末尾）或回复内部的 `inside`（run 还在继续、或终点在一轮工具的中间时，分隔线在那条回复的零件之间，不拆回复），`CompactionDivider` 画成细线「以上内容已压缩为摘要」，点开是用聊天同一个 `Markdown` 渲染的摘要和「撤销压缩」（run 在跑时不给）。运行中压缩的状态行是 `CompactingLine`（`ThinkingOrb` + 「正在压缩上下文…」），取代「正在思考…」。上下文圆环（`ContextMeter`）点开只有数字和备注，没有按钮——压缩只有 run 自己会做，用户不能手动压；圆环读的是上一次请求的用量，run 压完后服务端给的是估算（`estimated`），到下一次请求才报真数。因上下文过长失败的回答（服务端已经强制压缩并重试过一次，见 conversations.md），错误提示不带按钮，只说压缩过也装不下，建议新开对话或换窗口更大的模型。`hooks/use-compaction.ts` 只剩读取（`useCompaction`）和撤销（`useUndoCompaction`）。所有字符串走 `t()`。

## 表单、i18n、动效与设置对话框

- 后台表单用 react-hook-form + zod，schema 建在组件内部（文案要翻译）；用 `useWatch` 而不是 `form.watch()`。登录注册页是手写校验。
- 所有面向用户的字符串都走 `t()`（`zh`、`en`，键有类型约束）；服务端 `message` 不直接渲染。
- 动效：全局遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`，用透明度脉动代替位移。
- 通知（sonner，`components/Toaster.tsx`）在所有弹窗之上（z-index 999999999），但 Radix 模态框会把 `<body>` 设成 `pointer-events: none`、并在外部按下时关闭：`toast.css` 给 toast 恢复 `pointer-events`，`lib/toast-layer.ts` 在捕获阶段取消发自 toast 的 `dismissableLayer.*Outside` 事件，点通知里的按钮不会关掉下面的弹窗。
- 设置对话框（通用/个性化/账户/数据）是 `?settings=<tab>` 搜索参数（`lib/settings.ts`），挂在 `_authed` 布局上，渲染进 `.chat-ui` 里；样式照搬 Open WebUI 经典设置弹窗，在 `styles/settings.css`。记忆列表是它里面的第二层弹窗（`MemoryManager`），新增和编辑一条记忆都在第三层弹窗里（`MemoryEditor`，照 Open WebUI 的 Add/Edit Memory：多行文本框、字数、取消/保存，⌘/Ctrl+Enter 保存，Escape 只关最上层）；其中「最近由模型更改」默认折起，点标题展开（每条最多两行）。回复里的记忆提示（`MemoryChip`）只占一行，放不下时省略号截断，点文字展开全文、再点收起；一条记忆服务端上限 200 字（`memory.MaxContent`），手机上会有好几行。记忆相关查询键在 `queryKeys.memory`，不写入本地缓存（`query-persist` 只存白名单），临时对话的转录同样不落本地缓存。

## 较早消息的跳过渲染（`styles/chat-layout.css`）

较早的消息用 `content-visibility: auto` 跳过屏幕外的渲染（WebKit 上不用，见下），最新的一问一答（最后两条）始终绘制。**`contain-intrinsic-size: auto 200px` 加在所有消息上**，不只加在被跳过的那些：浏览器只在元素带着 `contain-intrinsic-size: auto` 时记录它的真实高度，而发新消息的那一刻，原来的最后两条才第一次被套上 `content-visibility`——没有记录过高度，就按 200px 占位排一帧，对话骤然变矮、滚动位置被拉回上一条回答，再跳回来（v1.1.1 及之前每次发送都会闪一下）。Open WebUI 是所有消息一直带着这两个属性（`Message.svelte`，并在 WebKit 上关掉，因为它有消息变空白的绘制 bug）。改这段 CSS 后用无头 Chromium 逐帧记录发送前后滚动容器的 `scrollHeight`，不应出现骤降。

WebKit（Safari，以及 iPhone 上的所有浏览器）照 Open WebUI 整个跳过这两条：它的 `content-visibility` 有让屏幕上的消息变空白的绘制 bug（open-webui#26712）。`main.tsx` 用 `lib/webkit.ts` 的 `isWebKit`（同 Open WebUI 的判断：`navigator.vendor` 认出 iOS 的 WebView 和添加到主屏幕的网页，UA 认出其余）在 `<html>` 上设 `data-webkit`，CSS 用 `:root:not([data-webkit])` 限定。

## Mermaid 图表（`components/chat/MermaidDiagram.tsx`）

`mermaid` 代码块画成图，和 `html` 块的预览一样是 Streamdown 的自定义 renderer（`markdown-components.ts`）。不用 `@streamdown/mermaid`：它的外框是这个构建不生成的 Tailwind 类和聊天里没有的 shadcn 令牌，画出来是黑边框。Mermaid 很大，第一次遇到图表才动态加载；全局配置，所以渲染排队一个一个来。`securityLevel: 'strict'`（不执行脚本、标签里不放 HTML、输出经它净化），浅色用 `neutral`、深色用 `dark`，主题切换时重画（`hooks/use-dark.ts`）。还在输出、或语法错误时显示源码（错误时下面一行说明），画好后可在图和代码间切换。系统提示词只告诉模型这种代码块会画成图，用不用由它判断。

`mermaid.render` 必须传第三个参数 `drawingStage()`（一个 `position: fixed`、`visibility: hidden` 的容器）：不传时 Mermaid 在 `<body>` 末尾建临时节点排版，跨好几个 await 才删；`html`/`body`/`#root` 一屏高又不裁切，整页因此多出一张图的高度，窗口会冒出一条很长的滚动条（v1.4.0，展开含图表的思考时最明显）。

## 智能 UI 的空块

模型偶尔开了一个 `openchat-ui` 块又马上关上（多半是要写表格，而表格规定写在块外），以前显示成「这个界面无法显示」加一个空灰框；`splitSegments` 现在丢掉已结束的空块，还在输出的保留占位。

## 智能 UI 的图片（`api/pictures.go`、`images.go`，前端 `genui/found-image.ts`）

组件里的图片（Image、Gallery、Media、Card/Step/Article 的 `image`）只有一个字段，值是网址或描述，前端按是否 `http(s)://` 开头分流：

- **网址**：走对话的图片代理（`/conversations/{id}/images?url=`），只放行本对话工具结果里出现过的地址（`ToolResultMentions`；`web_fetch` 把页面图片列在 "Images on this page" 下）。
- **描述**（图片搜索开着时）：`GET /images/search?q=` 按后台排的顺序问各服务（Wikimedia Commons 默认、Openverse、Pixabay、Bing 图片，`internal/imagesearch`；Bing 是网页自己用的非公开接口 `/images/async`，没有 Key、图片版权归原站，结果页里一条结果都读不出来时算不可用而不是没找到，跳过带水印的图库站）找第一张合适的图：前一个搜不了（出错、超时、被限流、缺 Key）或没找到就问下一个，最近一分钟出过错的排到最后（`imagesearch.Fallback`）；502–504 先在同一个服务上隔一会儿重试两次。下载进文件存储 `pictures/`（Pixabay 条款要求不直接外链），`pictures` 表按 (provider, provider_id) 存一份，`image_searches` 记「描述 → 图」，同一描述永远同一张；没找到的记一天，服务不可用（503）不记，前端隔 4、12、30 秒再取，期间保留图框。字节由 `/pictures/{id}` 从存储读出，只给搜到过的 id。署名（来源 · 许可）悬停时浮在图上，点开大图带来源页。

模式写在提示词里：`genui.Catalog.Prompt(imageSearch)` 在组件列表后接目录里的 `pictures.search` 或 `pictures.sources` 一段，由 `imageSearchOn`（开关开着且服务能用）决定；关着时模型只能用来源里的网址，和以前一样。后台「图片搜索」页（`/admin/image-search`）管开关、服务及其顺序、Pixabay Key 和试搜（全部按顺序，或只试一个）。每用户搜索有每分钟上限，各服务自己的速率份额在 `imagesearch.limiter`，被服务拒绝时按它给的时间停搜。

## 智能 UI 的「选项」（`components/chat/genui/choices.tsx`）

`Choices`/`Choice` 组件：一个问题加 2~4 个选项，每个选项有给读者看的短标签（`label`）和选中后替读者发出的完整消息（`message`）。点按钮走 `ChatView` 的 `handlePick`，和输入框发送同一条路（同样的忙碌检查、满额检查、`post` + `useSendMessage`，服务端照常审核），不带输入框里的文件；没被接受时消息放回输入框。只有最后一条回答的选项能发（`ChoicesContext` 只给它 `pick`），有回答在输出、有消息在路上、或末尾是要改写的被拒消息时没有 `pick`，按钮禁用。每条回答都会拿到它后面发出的那条消息（`next`，发送途中就是 outbox 里的那条）：等于某个选项的 `message` 就标出它，卡片只读；不等于则只读、不标。状态规则在 `chat-core/src/genui/choices.ts`（有测试）。组件说明里只说它能做什么、用不用由模型判断，不写「什么时候必须用」。

## 手机：禁止缩放与安全区

- **禁止缩放**，照 Open WebUI：`index.html` 的 viewport 带 `maximum-scale=1, user-scalable=no`（`maximum-scale` 同时让 iOS 聚焦字号小于 16px 的输入框时不再自动放大）。iOS 10 起 Safari 双指缩放不理会这两项，靠 `lib/no-zoom.ts` 取消它的 `gesture*` 事件；`globals.css` 给 `html` 设 `touch-action: pan-x pan-y`（向下逐层取交集，所有元素都只能平移），也去掉双击放大。
- `interactive-widget=resizes-content`：Android 弹出键盘时缩小页面，输入框留在键盘上方（iOS 不支持，键盘照旧盖在页面上、由浏览器滚动可视区）。
- **安全区**（`viewport-fit=cover` 让页面铺到刘海和圆角下面）：`.chat-ui` 根元素让开顶部和左右；底部由输入框（`.n-composer-dock`）和侧栏（`.n-sidebar-panel`）各自让开，背景才能铺到屏幕边缘。手机抽屉是另一个 `.chat-ui`，不吃根元素的 padding，自己在 `.n-sidebar-panel` 上让开顶部和左边。Chromium 的设备模拟里 `env(safe-area-inset-*)` 都是 0，要在真机或 iOS 模拟器上看。

## 最后一条消息下面的留白

照 Open WebUI（`Messages.svelte` 的 `pb-18`，输入框有附件时再加 `pb-6`）：对话列 `.n-column.n-chat-fade` 底部留 72px，输入框里有文件时 96px（`has-files`），用 padding 而不是加一个空白子元素——`content-visibility` 那条规则按列的最后两个子元素认定「最新的一问一答」，多一个子元素就会把它们挤出去。两个类名一起写，才压得过 `.n-column` 的 `padding` 简写（窄屏的媒体查询里也有一条）。

## 贴底跟随（`ChatView` 的 `parked`）

- 打开一个对话（包括从别的对话切回正在输出的那个）停在最新输出处，并继续跟随，和 Open WebUI 一样（它在 `loadChat` 里设 `autoScroll = true` 并滚到底）。
- 跟随靠对话列的 `ResizeObserver`：`parked` 时内容一变高就贴底。内容的高度不只在新 token 到达时变（公式、图表、代码高亮画出来，折叠区展开），只按 token 跟随会落后。
- 只有**用户往上滚**才离开底部；到达底部 `BOTTOM_SLACK` 以内重新贴底。判断「往上滚」看用户的动作而不是滚动位置：滚轮向上、触屏手指下拖、方向键上 / PageUp / Home（焦点不在输入框时）、按在滚动条上，任何一个立即停止跟随。只看位置有两个坑：「回到底部」的平滑滚动途中离底部很远，内容还在长，一按按钮就断了跟随；而回复每帧都在长、跟随每帧把视图往下推，触控板慢慢上滚（每次 1px 以内）永远追不上，退不出跟随。位置判断（相对跟随以来到过的最低点）只留作兜底。


「回到底部」按钮只看位置：离开贴底的手势（滚轮向上、触摸下拉、方向键、按滚动条）只在视图还能往上走（`scrollTop > 0`）时才松开，并在下一帧按实际位置决定——还在底部 24px 内就重新贴底、不显示按钮。以前手势一发生就显示，已经在底部、对话太短不能滚、按住滚动条没拖动时都会冒出按钮，并停止跟随。方向键只在焦点在页面本身或对话区里、且不在按钮/输入框/菜单等带 `role` 的元素上时才算（在模型选择器里按方向键也会弹出按钮）。
## 首条消息的 outbox（空键）

新对话第一条消息在建好对话之前挂在 outbox 的空键 `''` 上，所有新对话页共用这个键。所以撤掉它时要带上自己发的那一条（`unpost('', item)`：只有空键上还是这一条才撤），否则已经离开的旧页面在后台完成时，会把新页面刚发的气泡撤掉。页面重来（`ChatPage` 的 `epoch` 变化）时在绘制前清掉空键，新页面不继承旧页面还在路上的消息。等待建对话期间点「新对话」也算页面超前于地址（`startOver(view, starting)`，`starting` 即空键上有消息），页面重来，那条消息在后台照常发完。

## 消息下面的操作图标（`components/chat/ActionIcons.tsx`）

复制、编辑、重新生成和复制成功后的对勾，照 OpenWebUI 用 Iconoir 的线条图标（MIT），不用 lucide：16px、描边 1.5、小圆角（`.n-action-icon`，`styles/chat-theme.css`），和旁边文字一个分量，不像工具栏。悬停的底是圆角方块而不是圆（`.n-actions .n-icon-btn::before`，`border-radius: 8px`）。新增消息下的操作要在这里画图标，别混进 lucide。

## 重试提示

回复照 ChatGPT 分成「过程」和「答案」。答案之前的所有东西（思考、模型路上说的话、工具调用）是一个折叠块，答案在它下面。哪一段算过程由 chat-core 的 `reply-process.ts`（`processSpan`）决定：从第一个思考、文字或调用开始，到最后一个工具调用和紧跟它的思考为止；没有调用时，就是第一段文字之前的思考。网页 `MessageItem` 和 App `Reply.tsx` 都调它，别再各写一份。分组只在前端做，服务端照常按顺序存零件；成熟的开源项目（Open WebUI、LobeChat、LibreChat、Cherry Studio）也都是这样。

- **折叠块的标题**：画在网页 `ToolActivity`、App `ActivityLine`。完成后只写「已思考 N 秒」（N 是思考零件 `durationMs` 之和，没有计时就写「已思考」），搜索次数、来源图标、失败数都不放在这一行。整段过程没有思考时，才写「搜索了 N 次」并带图标。工作时写当前在做的事。
- **展开后**：内容和答案齐平，左边没有竖线，因为竖线会让过渡句看着像思维链。
  - 模型路上说的话照原样显示，和答案一样大、一样颜色，也和答案一样经 `AnswerText`（`components/chat/AnswerText.tsx`）画：里面的 `openchat-ui` 块照样画成组件，不能用普通 `Markdown`，否则第一次调用工具前写的智能 UI 收进过程后会变回源码。
  - 过渡句之间的每段思考和工具调用各收成一行，嵌套的 `ToolActivity`/`ActivityLine` 带 `nested`，比如「已思考 6 秒，搜索了 3 次 ›」，点开才是思考原文和每一步。分段由 chat-core 的 `activitySegments` 决定。
  - 模型一句话没说时只有一段，就直接平铺思考和步骤，不再多一行重复外层的标题。
  - 思考淡一号、小一号。
- **开合**：模型工作时（run 在跑、过程之后还没有内容）自动展开，答案一开始就自动收起；翻看旧对话时默认收起。里面的分段只有最后一段跟着工作状态开合。
- **思考的窗口**：工作时思考只露最后几行，网页 `Thought` 的 `is-peek` 最高 240px，App 是 220；窗口停在最新一行，顶上渐隐，不能单独滚动（网页嵌套滚动框会抢走页面的滚轮）。完成后长的思考（超过 240px 再多 48px 以上，`data-long`）是一个 240px 高、能自己滚动的框，上下有更多内容的一边渐隐（`data-cut`/`data-more`），滚到头时滚轮交还页面；短的直接全文。工作中的窗口不能滚，因为每个 token 都把它拉回末尾。
- **不吸顶**：展开后标题跟着内容一起滚走，不吸在顶部。试过吸顶（外层、内层叠着吸，吸住时顶栏变实色），快速滑动、段尾松开、内外层交接时都会出现重叠或露底，已整个去掉（连同 `useStuck`），别再加回来。
  - 展开区只在竖直方向裁切（`overflow-x: visible; overflow-y: clip`，为了展开时高度的动画），横向也裁的话，会切掉标题悬停时伸出去的灰底；上面的空白用 `padding` 留在裁切范围内，不用 margin。
  - `.is-open`、`.is-active` 的选择器只作用于自己的标题（`> .n-activity-head`），否则外层展开会让内层收起的标题箭头也朝下。
- **过程之外的思考**：答案文字之后又出现的思考，自己成一个只含它的折叠块。

模型请求失败、正在自动重试时（`live.retry`，来自事件 `retry_scheduled`），重试说在「模型在干活」的那一行里，不在上面另堆一条提示：回复还没有工具活动行时是思考行（`MessageItem` 给 `ThinkingLine` 传 `label`，「请求失败，正在重试…」，`chat.retrying`），有了活动行就由活动行的 `retrying` 状态说。

## 对话导航（`components/chat/TurnNavigator.tsx`）

转录右边、竖直居中的一排短横线，一条代表用户说的一句话，正在读的那条更深；平时都是短线，鼠标靠近（左边留 32px 的感应区）才拉长，正在读的那条最长（问题越过顶部下方 96px 那条线就算在读，滚到底时算最后一条）。悬停或聚焦时左边出卡片：问题一行、回答开头三行；点击滚到那条问题。条目由 chat-core 的 `turnOutline`（`outline.ts`，引用段落的请求只取文字、只有文件时显示文件名，回答经 `plainText` 粗略去掉 Markdown）从已画出的 turns 得出；用户消息带 `data-turn`（`UserMessage` 的 `anchor`）供定位。少于两条、右侧空白不足 88px（连感应区都放得下，不压正文）、旁边开着面板、追踪视图时不显示。Open WebUI 没有这个，参照的是 ChatGPT（它放左边，我们放右边）。

## 登录页预取（`lib/warm-app.ts`）

登录、注册页的路由代码和已登录应用的是分开打包的，原来要等密码验证通过才开始下载，服务器远时这就是按下按钮到看见聊天之间的大半等待。`AuthShell` 在页面空闲时（`requestIdleCallback`，没有就 300 ms 后）调 `warmSignedInApp(router)`，只加载 `/_authed`、聊天布局和对话页这几个路由的代码（`router.loadRouteChunk`）：不跑任何守卫或 loader，所以没有会话时不会向服务器问任何东西。文件拿到后缓存，之后基本零成本。
