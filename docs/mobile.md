# 手机 App（`apps/mobile`）

Expo（React Native）写的原生 App，先做 Android，以后 iOS 用同一套代码。目前有登录（密码、通行密钥）、注册、待审批和停用页，以及聊天核心：对话列表（抽屉）、聊天页、事件流、发送/停止/重新回答/改写、被拒消息的修改、模型的提问、Markdown。附件、写作块、定时任务、子智能体、设置陆续加入。

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
- 共用逻辑在 `packages/chat-core`（不依赖 React 和 DOM），网页版和 App 都从那里引用，不复制。聊天这块共用的有：事件跟随循环（`conversation-follow.ts`）、发送的缓存动作和错误文案（`send.ts`）、工具步骤的文案（`tool-activity.ts`）、页面规则（`chat-view.ts`）、转录（`transcript.ts`、`run-state.ts`）、逐字显现的节奏（`reveal.ts`）。
- 聊天页（`src/app/(app)/index.tsx` + `components/chat/ChatView.tsx`）：只有一个，地址是 `?id=`（对话）和 `?n=`（要新对话），按 `chat-view.ts` 决定重开还是保留；转录是倒置的 `FlatList`，停在底部时自然跟随，往上翻时才用 `maintainVisibleContentPosition` 稳住位置。
- 事件流（`hooks/events.ts`）用 `expo/fetch`（RN 自带的 fetch 不能边收边读），带 Bearer 和 `Accept-Encoding: identity`；App 退到后台就断开，回到前台重连（从新快照开始）。
- Markdown（`components/chat/Markdown.tsx`）：`marked` 的词法分析出块，每块按源文本 memo，流式时只有最后一块重画；逐字显现用 `hooks/reveal.ts`（节奏是 `chat-core/src/reveal.ts`；网页以前也这样，现在改成整批淡入，见 frontend-chat.md），只显现前缀，不给每个字建元素。帧循环自己读 `performance.now()`（RN 的帧时间戳在后台回来后和它不是一个时钟），追上后若同一次渲染里又来了字要重新启动循环，否则会停在半截。

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
