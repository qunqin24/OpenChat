# 手机 App（`apps/mobile`）

Expo（React Native）写的原生 App，先做 Android，以后 iOS 用同一套代码。目前有登录（密码、通行密钥）、注册、待审批和停用页，以及聊天核心、附件、写作块、定时任务和子智能体：对话列表（抽屉）、聊天页、事件流、发送/停止/重新回答/改写、被拒消息的修改、模型的提问、Markdown、GenUI 界面块、HTML 预览、时间间隔和撤销压缩。设置与记忆、发布和更新陆续加入。

## 结构

- `app.config.ts`：构建期设置，全部来自环境变量，fork 不用改文件：
  - `APP_VERSION`：版本号（tag 去掉 `v`），`versionCode` 由它算出、始终递增（正式版排在它的 rc 之后）。
  - `APP_VARIANT=development`：开发版，包名加 `.dev`，允许明文 HTTP（连模拟器宿主机的 `http://10.0.2.2:8080`）。
  - `APP_ID`：Android 包名，默认 `org.qunqin.openchat`。
  - `EXPO_PUBLIC_SERVER_URL`、`EXPO_PUBLIC_RELEASES_REPO`：连哪台服务器、从哪个仓库的 Release 检查更新。
- `src/app/`：Expo Router 页面。根布局按会话状态用 `Stack.Protected` 决定哪些页面存在：已登录是 `(app)`，未登录是 `login`/`register`，被停用是 `disabled`。登录、登出只改会话，不自己导航。
- `src/lib/`：
  - `token.ts`：会话 token 存在 SecureStore（Android Keystore），内存里留一份给请求中间件。
  - `api.ts`：`@openchat/api-client`，中间件加 `Authorization: Bearer` 和 `Accept-Language`；带 token 的请求遇到 401 就当会话已失效。
  - `session.ts`：会话查询（`GET /auth/session`，顺带续期）、登录、注册、通行密钥、登出。登录类请求带 `X-OpenChat-Client: app`，服务器才会在响应体里给 token（见 [认证](design/auth.md) 的「App 的会话」）。换人时 `queryClient.clear()`。
  - `i18n.ts`：用 chat-core 的同一份 `locales`，App 专有的文字放在 `mobile.*` 下；语言跟随系统。
- `src/theme/tokens.ts`：从网页版 `chat-theme.css`、`auth.css` 移植的颜色，跟随系统深浅色。
- 共用逻辑在 `packages/chat-core`（不依赖 React 和 DOM），网页版和 App 都从那里引用，不复制。聊天这块共用的有：事件跟随循环（`conversation-follow.ts`）、发送的缓存动作和错误文案（`send.ts`）、工具步骤的文案（`tool-activity.ts`）、图表的几何（`genui/chart-geometry.ts`）、界面块的解析（`genui/parse.ts`、`resolve.ts`、`choices.ts`）、页面规则（`chat-view.ts`）、转录（`transcript.ts`、`run-state.ts`）、流式文字的整批淡入（`arrival.ts`）。
- 聊天页（`src/app/(app)/index.tsx` + `components/chat/ChatView.tsx`）：只有一个，地址是 `?id=`（对话）和 `?n=`（要新对话），按 `chat-view.ts` 决定重开还是保留；转录是倒置的 `FlatList`，停在底部时自然跟随，往上翻时才用 `maintainVisibleContentPosition` 稳住位置。
- 事件流（`hooks/events.ts`）用 `expo/fetch`（RN 自带的 fetch 不能边收边读），带 Bearer 和 `Accept-Encoding: identity`；App 退到后台就断开，回到前台重连（从新快照开始）。
- Markdown（`components/chat/Markdown.tsx`）：`marked` 的词法分析出块，每块按源文本 memo，流式时只有最后一块重画；文字和网页一样一到就显示、每批整体淡入（`ArrivalFade.tsx` 的 `FadingText`，批次和曲线是 chat-core 的 `arrival.ts`）：每个文字块用 `onTextLayout` 拿到排好的行，在本批所在的行上盖页面底色的浮层，0.6 秒内从不透明变透明；RN 只给行的位置和内容、不给单个字的位置，所以一行里本批开始或结束的地方按字数比例估算。第一次渲染之后才出现的块是新到的，整块淡入，代码块、表格这些不是文字的块用一次整体淡入（`FadeIn`）。不给每个字建元素，浮层只有最近 0.6 秒到的那几行。减少动态效果、App 在后台时不淡入。

## 本地运行

需要 Android Studio（带 JDK 和 SDK）和一个模拟器（API 28 以上）。

```bash
export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home" ANDROID_HOME=~/Library/Android/sdk
cd apps/mobile
APP_VARIANT=development EXPO_PUBLIC_SERVER_URL=http://10.0.2.2:8080 npx expo prebuild -p android --clean
APP_VARIANT=development EXPO_PUBLIC_SERVER_URL=http://10.0.2.2:8080 npx expo run:android
```

`android/` 是生成的，不进版本库；改了 `app.config.ts` 或原生依赖要重新 `prebuild`。只改 JS 时 Metro 热更新即可。

检查：`pnpm --filter @openchat/mobile typecheck`、`pnpm --filter @openchat/mobile lint`。

## 通行密钥

走 Android Credential Manager（`react-native-passkey`，最低 API 28）。要在 App 里用某个站点的通行密钥：

1. 后台「身份验证」→ 通行密钥里把依赖方 ID 固定为站点域名，并在「Android 应用」里登记包名和签名证书的 SHA-256 指纹（正式版、开发版各一行）。
2. 服务器据此提供 `/.well-known/assetlinks.json`，并接受 `android:apk-key-hash:` 来源的断言。

只能对 https 的正式域名测试；模拟器要登录 Google 账号，或者用真机。

## GenUI 与预览

- 界面块（`components/chat/genui/GenUIBlock.tsx`）把目录里的 31 个组件都画成原生控件，和网页的 `genui/GenUIBlock.tsx` 一一对应；手机上 Grid 最多两列、Row 横向滚动。图表（`genui/Chart.tsx`）用 `react-native-svg`，刻度、路径和扇形与网页共用 `chat-core/src/genui/chart-geometry.ts`，点一下柱、点或扇形在下方显示数值。
- 图片：地址经服务器代理（`/conversations/{id}/images`），描述经 `/images/search` 找图，都带 Bearer；点开全屏查看。
- Choices 只有最后一条回答能发，发出去就是下一条消息（`ChatView` 的 `handlePick`），之后卡片标出选的是哪个。
- HTML 代码块可以「预览」（`PagePreview.tsx`）：独立 WebView，允许脚本，但页面不能跳转——网页链接交给系统浏览器，其他一律拒绝；没写 viewport 的页面补上手机宽度。
- Mermaid 代码块点「图表」全屏画出来（`MermaidPreview.tsx`），设置同网页（`securityLevel: 'strict'`，深色用 `dark`、浅色用 `neutral`），页面允许放大（`maximum-scale=6`）；画不出来就提示并留着源码。Mermaid 打进 App 而不是从 CDN 加载（不连第三方）：`mermaid` 的版本和网页锁成同一个，安装依赖时 `scripts/vendor-mermaid.mjs`（`postinstall`）把 `mermaid.min.js` 写成 `src/vendor/mermaid.gen.js`（不进版本库，约 3.5 MB），预览时内联进页面的 `<script>`——改用「页面加载前注入」时这么大的脚本让 WebView 渲染进程直接崩了。脚本里如果出现 `<script`/`</script` 就无法内联，生成时会直接报错。

## 附件与分享进 App

- 「+」菜单（`AttachMenu.tsx`）是一张四周留空、四角都圆的浮动卡片（`Sheet` 的 `floating`），版式参考 Kimi：上面一排方块是拍照、照片、本地文件，模型看不了图片时前两个变灰；下面两行带说明，是文件库和引用其他对话，点进去在同一张卡片里换成列表。照片走系统的照片选择器（不要媒体权限），拍照调系统相机（不申请相机权限），文件走系统文件选择器；文件库的文件下载后重新上传（和网页一样），一条消息最多引用 5 个对话。
- 回答里的表格（`Markdown.tsx` 的 `Table`）和代码块用同一套外框：顶上一条标题栏，写着「表格 · N 列 · M 行」，右边是带文字的「复制」按钮，点了变成「已复制」，复制的是这张表的 Markdown 原文。单元格不能跨格选中，这是把表格拿走的办法。列宽先按内容估算（72 到 240）：放得下时按比例撑满外框；放不下时每列最多占外框的 42%，好让下一列露出一截。右边还有内容时，边缘有一道渐隐。
- 回答的过程（`Activity.tsx` 的 `ActivityLine`）和网页一样照 ChatGPT 排：答案之前是一个「已思考 N 秒」折叠块，规则见 [聊天页与前端约定](design/frontend-chat.md)。
  - 展开后没有竖线：模型路上说的话照原样显示；过渡句之间的思考和步骤各收成一行，点开才是细节。
  - 模型工作时，思考只露最后几行（最高 220），顶上用 SVG 渐隐，不放能单独滚动的框，否则会抢走对话列表的滑动。
  - 只有搜索、没有思考时，标题写「搜索了 N 次」，后面叠着来源网站的图标（经服务端 `/favicons/{domain}` 取，没有图标时显示站名首字母）。
  - 搜索步骤写「搜索到 N 个网页」，叠着该次的网站图标（同一网站只出现一次），下一行是搜索词；点开是结果列表（站点、日期、标题、两行摘要），点一条打开网页。读网页的步骤写「已浏览 某站」。
  - 摘要先经 chat-core 的 `plainSnippet` 去掉 Markdown 链接、图片和裸网址再显示。
- 展开或收起回答里的某一块（思考、活动、长代码）时，列表按这块高度的变化跟着滚动，点的那一行留在手指下面（`keep-in-place.ts`）。对话列表是倒序的、贴着底部，不这样做的话，展开的内容会把上面的东西整个顶出屏幕。要等列表重新排好版再滚，所以放在下一帧。
- 侧栏（`ConversationDrawer.tsx`）照 Kimi 的布局：浅灰底；顶上是头像和名字（点开设置）和新对话圆钮；中间是对话列表，字大、无分隔线，当前对话一块圆角浅灰底；底部是搜索胶囊和定时任务圆钮，键盘弹出时整个侧栏跟着抬高（`KeyboardAvoidingView`）。打开时聊天页被推开而不是被盖住（`drawerType: 'slide'`），并按侧栏的打开程度（`useDrawerProgress`，经 Drawer 的 `screenLayout` 包在每个页面外）缩小到 94%、圆角 32、边缘只有一圈几乎看不出的阴影（`rgba(0,0,0,0.02)`，再深就成了一条灰带）、内容变淡。Kimi 在对话标题下显示附件缩略图，我们的对话列表接口不返回附件，暂时没有。
- 模型在聊天页的顶栏选（`ModelPicker.tsx`，菜单按钮旁边的模型名），不在输入框下面；顶栏因此不再显示对话标题，标题在侧栏里看。
- 顶栏是几颗浮起的按钮（`HeaderPill.tsx`：白底、细边框、轻阴影，44 高）：左边菜单圆钮和模型名，右边子智能体、写作和新对话共用一颗胶囊。对话列表也延伸到顶栏底下（`marginTop` 为负的顶栏高度），顶栏下面铺一层 85% 不透明的页面底色，正文从底下经过时只隐约可见，不会和状态栏、按钮叠在一起；这层底色在按钮下方再延伸 32，渐变到全透明，不留一条边。SVG 渐变用明确的宽高，写 `100%` 时高度按错了参照，遮罩会提前变淡。
- 输入框（`Composer.tsx`）是白底、细边框、轻阴影（`boxShadow`）的胶囊，「+」和发送都是 44 的浅灰圆钮，有内容可发或正在停止时发送钮才变成墨色。平时是一行：「+」、提示文字、发送。聚焦、有文字、附件栏有东西或正在改写被拒绝的消息时展开成两行，第二行左边是「+」，右边是思考强度（灯泡）和发送，三个都是 44 的圆钮。展开前后 `TextInput` 在组件树里的位置不变，所以点一下展开不会丢焦点；Android 按返回收起键盘时焦点还在，所以在 `keyboardDidHide` 时主动 `blur`，让它收回一行。键盘弹出时页面已经整体抬高了键盘的高度，输入框底下给手势条留的空间要跟着键盘动画收掉（`useReanimatedKeyboardAnimation`），否则输入框和键盘之间会空出一大截。平时输入框底下离手势条再多留 10。
- 输入框浮在对话上：消息列表向下延伸到输入框底下（`marginBottom` 为负的输入框高度），输入框四周和下方看到的是对话本身，不是一条白底栏；列表末端（倒序列表的 `paddingTop`）和回到底部的按钮按输入框的实测高度让开，最后一条消息仍完整停在输入框上方。输入框外层 `pointerEvents="box-none"`，空白处的触摸交给下面的列表。
- `BlurTargetView`（思考强度滑块要模糊的范围）里只放一个固定的 `View`（`collapsable={false}`）：expo-blur 把它的子 View 转交给自己内部的一个 View，直接子 View 增减或被 React Native 拍平时两边的序号会对不上，Android 上直接崩溃（`addViewAt: failed to insert view`）。
- 联网开关只在「+」菜单里（带开关的一行，拨动时菜单不关），输入框里不再显示。开关用 `Toggle.tsx`（App 的黑白色，不用 Android 默认的青色）。
- 上传（`lib/upload.ts`、`hooks/attachments.ts`）照网页：先 `POST /files` 拿签名地址，再把文件从磁盘 `PUT` 过去（`expo-file-system` 的上传任务，有进度），Content-Type 必须和签名时一致；本地存储给的是相对路径，按服务器地址补全，S3 原样用，都不带登录凭证。挑选、限额、拒绝原因用 chat-core 的 `attachments.ts`。手机照片常是 HEIC 或超过 5 MB，发之前用 `expo-image-manipulator` 转成 JPEG、长边不超过 2560。
- 发送和网页一样先 `take` 出输入框，失败 `restore`、成功 `release`；首条消息被拒且服务器说某些上传不能再用的，按 `returnableFiles` 只还回能用的。
- 消息里的附件（`MessageAttachments.tsx`）：图片缩略图点开全屏；文本文件直接读文件内容，Word/Excel/PDF 读 `GET /files/{id}/text`，都在底部弹层里显示；其他文件下载后交给系统「用其他应用打开」（`expo-sharing`）；已删除的文件显示为灰色；引用的对话点一下就打开。
- 分享进 App（`expo-share-intent`，`ShareInbox.tsx`）：接收 `SEND`/`SEND_MULTIPLE` 的文字、图片和任意文件，打开一个新对话，文字放进输入框，文件进附件栏（等模型列表到了再判断能不能收）。没登录时分享留在 provider 里，登录后再处理；冷启动和 App 已经开着都走这条路。iOS 的分享扩展等做 iOS 时再加（`disableIOS`）。
- `expo-share-intent` 打了补丁（`patches/expo-share-intent@9.0.0.patch`）：分享来的文件读不出来（游标为空、没有读权限）时原版直接让 App 崩溃，补丁改成跳过这个文件并报错。升级这个库时要看补丁还需不需要。

## 写作块、定时任务、子智能体

- 写作块（`DocumentCard.tsx`、`DocumentScreen.tsx`、`hooks/documents.ts`）：回答里的卡片显示这次写的或改的段落（`changedPassages`），点开全屏。最新版本可以阅读，也可以「编辑」——编辑的是它的 Markdown 原文，不是网页那样的所见即所得（Tiptap 在手机上的选区、键盘都不稳，格式和保存规则不变）。停止打字 1.2 秒保存为新版本，带 `baseVersion`；一次只存一个，模型在这期间写了新版本就 409，提示后载入模型的版本。没有未保存的修改时，显示的就是最新版本，模型写的新版本自己出现。左右箭头翻版本，旧版本只读、可「恢复这个版本」（以它的内容存一个新版本）。编辑时选中一段可以「提出修改请求」：经 `passageFor` 确认这段在文中只出现一次，放进输入框上方的引用卡片，发送时用 `quoteMessage` 包成 `<document-selection>`。对话标题栏有本对话的写作列表。
- 定时任务（`app/(app)/tasks.tsx`、`components/tasks/TaskEditor.tsx`、`hooks/tasks.ts`）：抽屉里进入，列表、空状态的灵感、新建/编辑（日期和时刻用系统选择器 `@react-native-community/datetimepicker`）、立即运行、暂停/恢复、打开对话、删除；表单逻辑和时间描述用 chat-core 的 `tasks.ts`。回答里模型安排的任务是一行，点一下去任务页。后台关掉定时任务时入口不显示。
- 子智能体（`SubagentLines.tsx`、`SubagentPanel.tsx`、`SubagentMark.tsx`、`hooks/subagents.ts`）：回答里派出子智能体是一行（花朵标记的颜色按 id 固定、工作时转动），文字随进度变化，前台等待时可「转到后台」；空闲时送达的报告是一行一个；标题栏按钮和这些行都打开全屏面板：列表（运行中/已完成，可停止、继续）和某个子智能体只读的全过程（复用聊天的回复组件）。文字和进度判断用 chat-core 的 `subagents.ts`。
- 弹出的表单（任务、写作）里，键盘开着时按返回只收起键盘（`lib/back.ts` 的 `lowerKeyboardOr`），不会把表单连同没保存的内容一起关掉。

- 弹出的整页表单用 `react-native-keyboard-controller` 的 `KeyboardAvoidingView behavior="padding"` 时，它会用键盘高度覆盖 `paddingBottom`，所以底部安全区要放在里面的底栏上，不能写在它自己的 style 里，否则按钮会压在手势条上。

## 思考强度滑块

输入框展开后，发送钮左边的灯泡圆钮（和发送钮一样大）打开 `EffortSlider.tsx`：一条胶囊浮在聊天页下半部分的模糊上，档位是「自动」加上当前模型支持的级别。墨色从左边涨到旋钮，拖动或点击会吸附到最近的一档，上方显示档名，每换一档轻震一下（`expo-haptics`），松手就生效。点其他地方或按返回键关闭；系统开了减弱动态效果时，吸附和淡入都改成直接切换。

- 键盘不收起：滑块挂在聊天页之外、盖满整个屏幕，按 `useReanimatedKeyboardAnimation` 的键盘高度把模糊区域和胶囊抬到键盘上方，胶囊盖在输入框上（输入框和页面一起被模糊）。键盘是系统的独立窗口，模糊不了，这一点和 iOS 不同。
- 模糊用 `expo-blur`。Android 上模糊只能看到同一个窗口里的内容，所以聊天页包在 `BlurTargetView` 里，滑块是盖在上面的普通 View，不能用 `Modal`。只模糊屏幕下半部分：一层模糊被渐变遮罩（`@react-native-masked-view/masked-view`）切成从上往下逐渐变强，上面再盖一层同样走向的页面底色。不用遮罩、按位置分几层模糊时，每层的起点都是一条看得见的线；Android 的模糊输出又偏灰、带颗粒，没有那层底色时空白页上会出现灰带。Android 12 以下没有真模糊，只剩那层底色。
- 关闭时模糊的强度随淡出一起降到 0（`useAnimatedProps`）：Android 的模糊不理会外层 View 的透明度，只淡外层会让模糊灰着停到最后一帧。胶囊和旋钮的阴影用 `boxShadow`，不用 `elevation`（elevation 的阴影不随透明度淡出，会留下一团灰）；胶囊比背景淡得快一点、略微缩小，免得白底先消失、只剩旋钮的黑圈飘着。
- 拖动用 gesture-handler 3 的 `usePanGesture`，旋钮由 reanimated 在 UI 线程上移动，只有换档时才回到 JS（更新档名、轻震）。

思考强度的图标是一只灯泡（不带光芒，和 lucide 的灯泡一样占满图标高度、居中），网页和 App 共用同一份线条数据（chat-core 的 `thinking-icon.ts`，两边各有一个 `ThinkingIcon.tsx` 负责画）。代表记忆的大脑图标不变。

## 告诉模型用户在手机上

从 App 发起的 run，系统提示词里多一节「Reading on a phone」（`api/agent_setup.go` 的 `phoneGuide`）：屏幕窄，段落短、少用宽表格、列表不超过两层、代码行尽量短，并且不要提这件事。

- 怎么认出来：只有 App 登录拿到 bearer token（浏览器用 Cookie），`auth.Require` 据此在请求上下文里记 `auth.FromApp`。
- run 在后台执行，请求早已结束，所以 `chooseRun` 把它记进 `runs.from_app`，执行时经 `agent.Scope.FromApp` 交给 `NewSystemPrompt`。
- 服务端自己起的 run（定时任务到点、子智能体报告送达）和子智能体都不加。同一个对话在手机和网页之间换着用时，系统提示词会跟着变，换端那一次提示词缓存失效，之后照常。

## 界面的公共部件

- 页头一律用 `PageHeader`（`HeaderPill.tsx`）：左边一颗返回或菜单圆钮、标题居中、右边至多一颗动作圆钮，和聊天页顶栏同一套白底浮起的按钮。
- 底部弹层（`Sheet`）默认是四周留空、四角都圆的浮动卡片；`floating={false}` 才贴底。
- 开关用自己画的 `Toggle`（App 的黑白色，夜间模式下颜色反过来，拨动时轻震一下），不用系统 `Switch`。
- 空页面用 `EmptyState`（`settings/parts.tsx`）：圆底图标、一句说明、添加第一个的按钮。
- 轻震：发送和停止是轻击，长按对话是中等，滑块换档和开关是选择反馈（`expo-haptics`）。

## 设置与记忆

抽屉底部的头像和名字进入设置（`app/(app)/settings/`，抽屉里的一个 Stack）。首页是分组列表：账户、外观和语言、自定义指令和记忆、数据、关于、退出登录。各页对应网页设置弹窗的各个 Tab，文案共用 `settings.*`。

- 账户（`account.tsx`、`hooks/account.ts`）：
  - 用户名和邮箱有改动时，底部出现「保存」。
  - 头像用系统相册和系统裁剪框裁成正方形，再用 `expo-image-manipulator` 缩到 256 px，以 PNG 格式 `PUT /me/avatar`。只有后台开了头像上传才能换，移除随时可以。
  - 修改密码默认折叠。
  - 通行密钥：后台开了才显示。用 Credential Manager（`Passkey.create`）添加，请求选项同样要先 `withoutNulls`。点一个通行密钥可以重命名或移除。
  - 头像组件 `UserAvatar` 与网页共用 chat-core 的 `avatar.ts`（颜色和首字）。
- 外观和语言（`lib/appearance.ts`）：存在本机，选了立即生效。主题用 `Appearance.setColorScheme` 覆盖系统设置，所以 `useColors`、状态栏和系统对话框都会跟着变。语言选「跟随系统」时取设备的第一种语言。
- 个性化（`personalization.tsx`）：
  - 自定义指令有改动时才出现「保存」；被审核拒绝时，原因显示在输入框下面，已写的文字保留。
  - 记忆开关立即生效。
- 记忆（`memory.tsx`、`hooks/memory.ts`）：
  - 搜索。
  - 「最近由模型更改」默认折叠，每条可以撤销。
  - 点一条记忆打开全屏编辑器修改；垃圾桶删除前会确认；也可以清空全部。
  - 错误文案与网页的 `use-memory-errors` 相同。
- 数据（`data.tsx`、`hooks/my-data.ts`）：
  - 存储用量条。
  - 导出：先把 `/me/export` 下载到缓存，按服务器给的文件名改名，再交给系统分享面板（存到「文件」、云盘，或发出去）。
  - 删除全部对话前会确认。
  - 手机上没有文件库，所以没有「管理文件」。
- 关于（`about.tsx`）：显示版本号和服务器地址，可以手动检查更新。

开发版的已知现象：在弹出的 Modal 里，用 `adb shell input text` 打出的按键，React Native 会转交给 Activity 处理，连按两次 r 就会重新载入 JS。真实键盘经输入法提交文字，不受影响；正式版没有开发菜单。在模拟器上测试时，避开连续的 r。

## 发布与更新

App 和服务端分开发版。App 用自己的标签：

```bash
git tag android-v1.0.0 && git push origin android-v1.0.0
```

推送 `android-v1.2.3` 或 `android-v1.2.3-rc.1` 后，`.github/workflows/android-release.yml` 会：

1. 取标签里的版本：`APP_VERSION` 是 `1.2.3`，`app.config.ts` 由它算出 `versionCode`。
2. `expo prebuild` 后执行 `./gradlew assembleRelease`，只打 arm64。
3. 创建 Release「Android v1.2.3」，附上 `openchat-android-v1.2.3.apk` 和 `.sha256`。发布说明用 `docs/releases/android-v1.2.3.md`，没有这个文件时，自动生成上一个 App 标签以来的提交列表。带后缀的标签标为预发布。

App 的 Release 一律不标为「最新」（`--latest=false`）：GitHub 的「最新」留给服务端，服务器检查自己的更新只看它（`api/version.go`）。服务端的 `release.yml` 只出镜像，自动生成说明时也从上一个服务端标签算起，不会被夹在中间的 App 标签打乱。App 的版本号和服务端各自独立。

签名：
- APK 用自己的发布密钥签名。config plugin `plugins/with-release-signing.js` 读取 `ANDROID_KEYSTORE_PATH`、`ANDROID_KEYSTORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD`。
- 没设置这些变量时保留 debug 签名，本地构建照常能用，但这样的包不能被正式版覆盖升级。
- 工作流里对应四个 secrets：`ANDROID_KEYSTORE_BASE64`（keystore 的 base64）和另外三个。缺 keystore 时工作流直接失败，不会悄悄发出 debug 签名的包。
- keystore 用下面的命令生成，必须离线备份两份。丢了就再也无法覆盖升级，所有用户只能卸载重装。

  ```bash
  keytool -genkeypair -keystore openchat-release.jks -alias openchat -keyalg RSA -keysize 2048 -validity 10000
  ```
- 构建后工作流会打印签名证书的指纹，要到后台「身份验证 → 通行密钥 → Android 应用」登记。

App 内的更新检查（`src/lib/update.ts`，挑选逻辑在 chat-core 的 `app-releases.ts`）：
- 启动和回到前台时检查，最多每 6 小时一次，用 ETag 缓存，失败不打扰用户。读的是 `releasesRepo` 最近 30 个 Release 的列表，从中找 `android-v*` 标签、带 APK、比装着的版本新的那个，取最新的。
- 版本新旧用 chat-core 的 `semver.ts` 判断，与服务端 `version.go` 是同一套比较。预发布只推给装着预发布的人。开发版（版本号 0.0.0）不自动检查。
- 有新版时由 `UpdatePrompt`（挂在根布局）弹出发布说明，可以「下载并安装」「稍后」或「跳过这个版本」。
- 下载到缓存目录后交给系统安装器（需要 `REQUEST_INSTALL_PACKAGES`）。第一次安装时，系统会引导用户允许 OpenChat 安装应用。
- 设置 → 关于里可以手动检查。

图标和启动图由 `scripts/make-icons.mjs` 用 sharp 生成，来源是 `design/logo-tangent.svg` 和网页的 `public/logo-mark.svg`。改了标志后在 `apps/mobile` 下重新运行它。
