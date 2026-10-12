# 前端：聊天页与数据约定

聊天页（一个页面、一个实例）、首条消息流程、流式显现、转录分页与交接、发送与 outbox、已结束的 run、事件流、会话失效、压缩上下文的前端，以及通用的前端约定。规则摘要见 [AGENTS.md](../../AGENTS.md)；下文为完整设计说明（原 AGENTS.md「前端约定」，未删减）。通用的前端手册见 [../frontend.md](../frontend.md)。

> 文中「后端约定」「前端约定」「游客模式」「内容审核」等指原 AGENTS.md 的章节，现按主题分在本目录各文件，索引见 [README.md](README.md)。

## 数据、路由与会话

- 服务端数据用 TanStack Query（键在 `lib/query-keys.ts`、`features/admin/use-admin.ts`）；流式中间态和 UI 状态用 Zustand。流结束后让查询失效，重新拉取。查询键含筛选条件时加 `placeholderData: (previous) => previous`。
- 路由守卫挂在无路径布局的 `beforeLoad`：`_authed.tsx` 要求登录，`admin.tsx` 要求管理员。会话用 `currentSession(queryClient)`（`features/auth/use-auth.ts`）取：5 分钟内查过的直接用，更老的重新问服务端（别用 `ensureQueryData`，它不管多旧都返回缓存，被停用的账号会一直半死不活）；重新问失败（断网）时退回上次的答案。
- 会话在页面开着时失效（被停用、改了密码）靠 401 发现：`lib/query.ts` 的 `QueryCache`/`MutationCache` 把每个失败交给 `setErrorListener` 登记的监听者（`main.tsx` 里是 `leaveIfSessionExpired`，`lib/session-expired.ts`），不走缓存的调用（事件流）自己调 `reportError`。账号模式下的 401（`invalid_credentials` 除外——那是登录表单自己的错，而且认证接口抛的是 `AuthFieldError`）且页面此前认为已登录，就清缓存、清 chat store、清本地存的缓存，`replace` 到 `/login?redirect=…`；已在 `/login`、`/register` 不动，游客模式不管。事件流收到 401 就停止重连（重连永远是 401）。新写的不经查询缓存的网络调用，遇到 401 也要 `reportError`。
- 换人要清干净：登录、注册成功和退出都 `queryClient.clear()` + `resetChatStore()` + `forgetQueryCache()`；`adoptQueryCache` 发现缓存属于别人（本页内存里的或上次存的）时，除会话外的查询全部移除（记忆、个性化、后台的也算）并重置 chat store。

## 一个聊天页

- **一个聊天页**（`components/next/ChatPage.tsx`，规则在 `lib/chat-view.ts`，有测试）：空白新对话页（`/`、`/guest`）和每个对话（`/chat/$id`、`/guest/chat/$id`）是同一个 `NextChat` 实例，像 Open WebUI 的同一个 `Chat.svelte`。路由是无路径布局 `_authed._chat.tsx` / `guest._chat.tsx`，它渲染 `ChatPage` 而**不渲染 `<Outlet />`**；子路由（`index`、`chat.$conversationId`）没有组件，只是地址，`chat.$conversationId` 的 loader 照旧预取（悬停预加载 `defaultPreload: 'intent'`、`enterPath` 对游客模式的规则都没变）。`ChatPage` 持有 `ChatView`（`epoch`、`route`、`shown`），`key={epoch}`：地址变到**页面没在显示的**对话（侧栏、搜索、前进后退、深链、别处删除后回首页）就 `epoch+1`，页面整个重来，和以前的 `key={conversationId}` 一样；地址变到页面**已经显示的**对话（首条消息自己建的那个）不动页面——这是整个重构的目的。首条消息的流程在 `NextChat.handleSend`：先 `post('', item)` 立刻显示气泡，`createConversation`（`onSuccess` 把详情和一页空转录塞进缓存，所以新页面没有 loading 也没有 `temporary` 样式闪烁），`post(id, item)`，`onCreated(id)`（页面从此显示这个对话，地址还在 `/`），`sendMessage.mutateAsync`，**服务器答复之后**才 `navigate({ to: paths.chat, replace: true })`（`replace`：空白页只是草稿，不是回得去的地方，和 Open WebUI 的 `history.replaceState` 一样）。所以失败路径永远在 `/` 上：没写任何东西（`startWroteNothing`）就 `deleteConversation` + `onRemoved(id)`（页面回到空白页，文字和文件原样在输入框里）；被审核拒绝等则 navigate 到对话。`outbox['']` 只在建对话那一小段用，页面显示对话后由 effect 放掉（先放会让空白页多画一帧欢迎页）。页面卸载后（用户在等待中点了别的对话）这次发送在后台完成，`alive` ref 挡住一切对页面的操作，不会把用户拉回去；空对话照样删。新增在首条消息路径上写页面状态的代码要过 `alive`。**等待期间点「新对话」**：地址还是 `/`，而路由器对「去当前地址」什么都不做（同址只重新 load，不留痕），页面无从得知——所以每个「新对话」入口（侧栏的链接和 ⌘⇧O、标志、搜索面板）都带 `state: newChatState`，在 history state 里放一个每次不同的 `newChat` 标记；`ChatPage` 看到**新出现的**标记、且地址是 `/` 时 `startOver`（页面比地址超前就重来成空白页，本来就是空白页则不动，草稿保留），重来后旧页面卸载，`alive` 挡住它的后续跳转，那条消息在后台照常发完。只认「出现」不认「消失」：跳到 `/chat/<id>` 时标记没了，而路由器先更新 location 再更新 params，那一帧里路由还读作 `/`，当成请求会把刚建的对话页重来一遍。新增「新对话」入口要带 `newChatState`。没有 `pendingPrompt`/`returnedDraft`/`handOff`/`adopt`/`is-carried`——它们是「两个页面之间交接」的产物，现在没有交接。

## 改写与重新生成的显示、转录分页与交接

- 没有版本翻页器。改写和重新生成从点击起就把要删的行藏起来（store 的 `replacing`：`{from}` 是被改的提问及其后，`{after}` 是最后一条提问之后；`buildTurns` 用它），服务端接受后用 `truncateTranscript` 把这些行从缓存里去掉，再让 `replacing` 结束；run 的输入已在列表里（重新生成）时 `buildTurns` 也会藏掉它后面不属于这个 run 的行。
- 对话记录从尾部分页：`GET .../messages` 首页是最新的一页（页内仍是旧→新），`before` 取更早的页；`hooks/use-messages.ts` 是 `useInfiniteQuery`（`pages[0]` 最新），`NextChat` 只渲染最后 20 个 turn，滚到顶部再多画 20 个，画到已加载的最旧处才请求更早一页。run 结束只用 `refreshNewestMessages` 重取最新一页并拼接（交接逻辑在 `lib/hand-over.ts`，有测试：只有刷新**确认成功**——`refreshConfirmed`，兜底的 `refetchQueries` 也要 `throwOnError`——之后才 `settle` 本地的 live 副本；失败时答案留在屏幕上，隔几秒重试，之后任何一次成功的刷新、重连后的 snapshot 都会放手），不要对该键 `invalidateQueries`（会按游标重下所有已展开的页）。

## 发送、outbox 与已结束的 run

- 发送：`outbox` 里的气泡由 `useSendMessage` 的 hook 级 `onError` 撤掉（调用级的 `mutate(…, { onError })` 在组件卸载后不执行，发完立刻切对话就会留下永远等回复的气泡）；新对话的第一条消息也一样：它用 `sendMessage.mutateAsync({ conversationId: 刚建的 id, … })` 发出，id 由调用传入（`useSendMessage` 的 `variables.conversationId`），hook 级的 `onSuccess`/`onError` 照样用它。运行中（`working`、`outbox`、`live` 在跑）不能再发：输入框的回车、`handleSend` 都挡掉，游客的 `runner.start` 对同一对话已有未结束的 run 直接 409 `conversation_busy`，不排队。
- 发送的响应可能晚于事件流：SSE 上 `run_started → run_finished → 重取消息 → settle` 全走完，那个 POST 的响应才回来，它（以及重连时拍得更早的 snapshot）仍写着 `running`。store 的 `finished`（有界，最近 `FINISHED_RUNS_KEPT` 个 run id，`rememberFinished`）记住已结束的 run：`run_finished` 批次、非 running 的 snapshot、`settle` 都会登记；`startRun`（发送、重新生成、改写、回答 ask_user 都走它，游客的响应也一样）、`applySnapshot`、`applyBatch` 里的 `run_started` 遇到登记过的 run 一律不写回 `live`（`startRun` 仍清掉 `outbox`/`replacing`）。run 只前进不后退；新增任何由响应或旧快照驱动、会写 `live` 的路径都要过这道检查。`resetChatStore` 一并清空。

## 对话标题的显示

- 对话标题：服务端和游客仓库都把没起名的对话叫 `New chat`（英文，不随语言），显示时一律经 `lib/titles.ts` 的 `displayTitle`/`useDisplayTitle`（侧栏、搜索、对话页顶、删除确认），它把空标题和 `New chat` 换成 `chat.untitled`；重命名框也以显示出来的名字起头。别直接渲染 `conversation.title`。

## 流式文字的显现与滚动

- 流式文字的显现（`components/next/MarkdownText.tsx` 的 `useReveal`，节奏是纯函数 `lib/reveal.ts`，有 vitest）：答案是一批一批到的（服务端每 150ms 提交一次，写库慢、反代或中转站攒着、标签页从后台回来时一批会很大），所以页面显示的是已收到文字的**前缀**，每帧多显示一点，速度跟着积压走（约 0.4 秒追上，最慢每秒 40 字，速度平滑变化），和 LobeChat 的 `createSmoothMessage` 同一个思路；渲染最多每 32ms 一次（每次都要把显示的文字解析成 Markdown，越长越贵）。挂载时已有的文字直接显示（中途打开、刷新不会重打一遍），不是延续的文字（被替换、变短）直接显示，标签页隐藏或 `prefers-reduced-motion` 时直接显示。**不要给每个字建元素**：v1.1.0 用 Streamdown 的 `animated` 给每个字包一个带淡入动画的 span，长思维链展开时有上万个节点和动画，主线程占用随时间涨到一个核的 90%，线上弱电脑直接卡死。流结束后仍会把排队的字放完，这要求同一段文字在「流式 partial → 存好的回复 → 交给消息列表」之间是**同一个元素**，所以 `AssistantTurn` 里回复的零件按位置（第几个回复 `r<n>`、第几个零件）起 key，不按消息 id——别改回去。思维链折叠时内容不渲染（Radix Collapsible），开销和展开时一样低。性能回归用无头 Chromium 测（`Performance.getMetrics` 的 `TaskDuration`），浏览器面板在后台时 rAF 被节流，测不准。
- 脚本触发的滚动（`scrollTo`/`scrollBy` 的 `behavior`）用 `lib/motion.ts` 的 `smoothScrollBehavior()`，CSS 的 reduced-motion 管不到它。工具结果里来的链接（`ToolActivity`）只允许 `isHttpUrl`（`lib/urls.ts`）通过，和 GenUI 的链接一样。

## 表格与后台

- 表格是服务端分页/筛选；`getRowId` 用记录 id；列译名同时放进 `meta.title`。
- 游标分页的表格（使用日志）用 `DataTableCursorPagination`：游标只会往前，往回就是回到走过的游标，所以组件里留着已访问的游标栈，换筛选或每页行数就清空重来。
- 管理后台的进度条挂在根（`AdminProgress`），只在目标路径在 `/admin` 下时才渲染——admin 布局在第一次从聊天进入时还没挂载，挂在布局里就晚了；聊天侧用自己就地的加载提示，不要给它加条。

## 侧边栏、删除对话与事件

- 侧边栏标记正在回答的对话靠 `GET /conversations/running`（`useRunningConversations`）：有在跑的才每 3 秒轮询，`run_started` 时由事件流让它失效；某个 id 消失时刷新列表（标题、顺序可能变了）。当前打开的对话不标，它自己的页面已经在显示。
- 删除对话：服务端在删除提交后往该对话的事件流发 `conversation_deleted`（nil run id、version 0；全部删除时每个对话各一条），正在看它的页面（任何标签页、任何设备）收到就停止跟随并回到首页；游客由 `hub.announceDeleted` 在本页和其他标签页发同样的事件。同一浏览器的其他标签页靠 `lib/conversation-sync.ts` 的 BroadcastChannel（`announceDeleted` / `followDeletions`）立刻从侧栏和搜索里去掉它。新增删除对话的路径要两样都做。
- 收到 `transcript_changed` 时（`use-conversation-events.ts`）先按事件的 `from`（或本页自己的 `replacing`）截缓存，赶在 `run_started` 结束 `replacing` 之前，再用 `refreshNewestMessages` 重取最新一页，不要 `invalidateQueries` 消息键。`spliceNewest`（`lib/transcript-pages.ts`，有测试）在新一页的第一行不在已持有的行里时整页替换，所以行被删光也对。重新生成时其他标签页没有 `from`，靠 `buildTurns` 里「run 的输入已在列表」那条藏旧回答。

## 压缩上下文

- 压缩上下文：摘要在查询 `queryKeys.conversations.compaction(id)`（`hooks/use-compaction.ts`，不落本地缓存），由事件流维护：`compaction_finished`（带摘要）、`compaction_changed` 直接写进缓存，`transcript_changed` 和 run 结束时重新读。原消息**照常全部显示**，`buildTurns(…, summary)` 只在摘要终点那条消息之后标一个 `after`（整个回复的末尾）或回复内部的 `inside`（run 还在继续、或终点在一轮工具的中间时，分隔线在那条回复的零件之间，不拆回复），`CompactionDivider` 画成细线「以上内容已压缩为摘要」，点开是用聊天同一个 `Markdown` 渲染的摘要和「撤销压缩」（run 在跑时不给）。运行中压缩的状态行是 `CompactingLine`（`ThinkingOrb` + 「正在压缩上下文…」），取代「正在思考…」。上下文圆环（`ContextMeter`）点开是同样的数字加「立即压缩」；圆环读的是上一次请求的用量，所以压完要到下一次请求才降。因上下文过长失败的回答，错误提示不再叫人开新对话，而是带「压缩上下文」按钮：先手动压缩再重新生成（只在最后一条回答、且不在忙时给）。所有字符串走 `t()`。

## 表单、i18n、动效与设置对话框

- 后台表单用 react-hook-form + zod，schema 建在组件内部（文案要翻译）；用 `useWatch` 而不是 `form.watch()`。登录注册页是手写校验。
- 所有面向用户的字符串都走 `t()`（`zh`、`en`，键有类型约束）；服务端 `message` 不直接渲染。
- 动效：全局遵守 `prefers-reduced-motion`，加载指示器标 `data-motion="essential"`，用透明度脉动代替位移。
- 设置对话框（通用/个性化/账户/数据）是 `?settings=<tab>` 搜索参数（`lib/settings.ts`），挂在 `_authed` 布局上，渲染进 `.prism-next` 里；样式照搬 Open WebUI 经典设置弹窗，在 `styles/settings.css`。记忆列表是它里面的第二层弹窗（`MemoryManager`）。记忆相关查询键在 `queryKeys.memory`，不写入本地缓存（`query-persist` 只存白名单），临时对话的转录同样不落本地缓存。

## 较早消息的跳过渲染（`styles/chat-layout.css`）

较早的消息用 `content-visibility: auto` 跳过屏幕外的渲染，最新的一问一答（最后两条）始终绘制。**`contain-intrinsic-size: auto 200px` 加在所有消息上**，不只加在被跳过的那些：浏览器只在元素带着 `contain-intrinsic-size: auto` 时记录它的真实高度，而发新消息的那一刻，原来的最后两条才第一次被套上 `content-visibility`——没有记录过高度，就按 200px 占位排一帧，对话骤然变矮、滚动位置被拉回上一条回答，再跳回来（v1.1.1 及之前每次发送都会闪一下）。Open WebUI 是所有消息一直带着这两个属性（`Message.svelte`，并在 WebKit 上关掉，因为它有消息变空白的绘制 bug）。改这段 CSS 后用无头 Chromium 逐帧记录发送前后滚动容器的 `scrollHeight`，不应出现骤降。

## 最后一条消息下面的留白

照 Open WebUI（`Messages.svelte` 的 `pb-18`，输入框有附件时再加 `pb-6`）：对话列 `.n-column.n-chat-fade` 底部留 72px，输入框里有文件时 96px（`has-files`），用 padding 而不是加一个空白子元素——`content-visibility` 那条规则按列的最后两个子元素认定「最新的一问一答」，多一个子元素就会把它们挤出去。两个类名一起写，才压得过 `.n-column` 的 `padding` 简写（窄屏的媒体查询里也有一条）。

## 贴底跟随（`NextChat` 的 `parked`）

- 打开一个对话（包括从别的对话切回正在输出的那个）停在最新输出处，并继续跟随，和 Open WebUI 一样（它在 `loadChat` 里设 `autoScroll = true` 并滚到底）。
- 跟随靠对话列的 `ResizeObserver`：`parked` 时内容一变高就贴底。不能只在新 token 到达时滚——文字是逐帧显现的（`useReveal`），流结束后还在长，只按 token 跟随会一路落后几行，答案最后几行停在屏幕下面。Open WebUI 每个 delta 滚一次就够，因为它的文字只在 delta 时变。
- 只有**用户往上滚**（`scrollTop` 变小）才离开底部；到达底部 `BOTTOM_SLACK` 以内重新贴底。不要用「离底部的距离」判断离开：「回到底部」按钮的平滑滚动在途中离底部很远，内容还在长，那样一按按钮就断了跟随。

