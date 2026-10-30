# 联网：搜索与读网页

模型的两个联网工具在 `apps/server/internal/tools`，只有对话开着联网时才提供（`UsesWeb`）。后台「联网搜索」设置在 `api/web_search.go`。规则摘要见 [AGENTS.md](../../AGENTS.md)「联网」一节。

## `web_search`（`websearch.go`、`freesearch.go`）

- 后端是 `Searcher` 接口：Tavily（要 key），或免 key 的 Exa / Parallel / Firecrawl / Keenable 轮流用（`FreeSearch`，某家限流或出错就换下一家，四家都失败才算失败）。
- 结果给模型的是标题、链接和摘要；后台的「压缩」设置决定给摘要、截断的正文还是整篇正文（上限 `fetchMaxChars`）。屏蔽站点在后端能排除的就让后端排除，回来的结果再过一遍 `Blocklist`。
- **时间范围与限定网站**：模型可以给 `recency`（day / week / month / year）和 `sites`（最多 5 个域名，含子域名；写成网址也认）。先让后端尽量按它过滤：Tavily 用 `time_range` 和 `include_domains`；Exa、Parallel 把要求写进 `objective`（`inWords`）；Parallel、Firecrawl、Keenable 在查询里加 `site:`（`withSites`），Firecrawl 另加 Google 的 `tbs=qdr:*`。回来的结果在 `Run` 里再按同样的条件过一遍：不在限定网站上的去掉，有日期且早于时间范围的去掉，没有日期的留下并标「Published: unknown」。限定网站时向后端多要一倍结果，免得过滤后不够。
- **发布日期**：Tavily 一律要 `include_published_date`，Exa、Parallel 的结果自带；统一成 `YYYY-MM-DD`（`publishedDay`）后给模型，也放进 details。
- **提示词**（`api/agent_setup.go` 的 `webGuide`，子 Agent 同用）：什么时候该搜、什么时候不用搜；多个方面在同一轮里并行搜；查询写成网页的说法，技术和国际话题用英文搜；时效性内容用 `recency` 或在查询里带年份，官方文档用 `sites`；优先一手来源、看日期；重要事实两个来源印证，来源矛盾就说出来；搜不到就换说法，不拿记忆冒充搜到的结果。快/深两种搜索方式现阶段不做（只有 Tavily 有这个区分，管理员在后台统一设）。

## `web_fetch`（`webfetch.go`、`webpage.go`）

- **安全**：地址来自模型输出，所以每一次连接（包括每一跳重定向）都在拨号时检查目标地址是不是公网（`GuardedClient`），不读环境里的代理；屏蔽站点按请求的地址和重定向后的地址各查一次。
- **网页读成正文 Markdown**：先去掉不含正文的元素（脚本、样式、`nav`、`footer`、`select` 等；不去 `form`，有些站把整页包在里面），再用 readability（`codeberg.org/readeck/go-readability`，Firefox 阅读模式的移植）找正文，转成 Markdown（`html-to-markdown`，含表格）。正文太短、明显不是整页的主体时（少于 500 字且不到全页一半）退回整页。图片从正文里去掉、在结果末尾单独列（`htmlImages`，图片代理只放行列出过的地址）；链接的 title、脚注标记和页内锚点链接去掉，非 ASCII 的转义还原成文字（`readableAddress`，只还原 0x80 以上的字节，不会冒出空格、括号、`?`）。readability 认出发布日期时带 `Published:` 行。
- **PDF**：响应是 `application/pdf`，或是 `octet-stream` 但开头是 `%PDF-`，就交给 `doctext`（和附件同一套 pdftotext，带 `[Page n]`），上限 20 MB；扫描件、太长、服务器没装 pdftotext 都明确告诉模型换来源。
- **分段读与查找**：一次最多 `fetchMaxChars`（2 万字）。结果末尾写明读到哪、下一段的 `offset`；`offset` 按字符（rune）计，起点往回对齐到行首、终点往前对齐到行尾。`find` 不按顺序读，而是返回从 `offset` 起含这个词（不分大小写）的段落，最多 8 段，每段注明起点 offset，模型可以从那里接着读。挨得近的命中合进同一段，但一段最长 2000 字（`findPassageMax`）、所有段加起来不超过一次读取的 2 万字；超出的命中只计数，结果里给出下一次查找的 `offset`——常见词不会把整篇长文一次带进来。
- **短期缓存**：同一地址读过的页面在进程内留 10 分钟、最多 32 个（`recentPages`），接着读、查找不再重新抓取，offset 前后一致。网页一律不带 cookie 和凭据，所以缓存不分人。
- 前端（文案在 `chat-core/src/tool-activity.ts` 的 `stepLabel`/`activityHead`，网页的 `ToolActivity.tsx` 和 App 共用）：搜索步骤在查询后面写上时间范围和限定网站（「go 发布 · 最近一周 · go.dev」）；查找显示「在 某站 中查找「词」」，接着读显示「接着读 某站」；收起时的「读了 N 个网页」按不同地址计。
