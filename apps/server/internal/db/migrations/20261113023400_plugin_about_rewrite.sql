-- +goose Up
-- +goose StatementBegin
-- The built-in plugins' descriptions say what each one does for the person,
-- and nothing about how it works, where its data comes from, what accounts or
-- keys it needs, or what an administrator should do: the install dialog and
-- its connect or token step show what a person has to do. A server keeps the
-- text it wrote at first start, so it gets the new one - where it still has a
-- text that shipped (or the one 20261113021529 left), so an administrator's
-- own words stay.
UPDATE plugins SET about = '搜索和阅读仓库里的代码，查看、创建和评论 issue 与 pull request，查看 Actions 运行和发布。'
WHERE builtin = 'github'
  AND about IN ('GitHub 官方的 MCP 服务：搜索和阅读仓库里的代码，查看、创建和评论 issue 与 pull request，查看 Actions 运行和发布。每个人用自己的 GitHub 账号授权，只能做这个账号本来就能做的事。');
UPDATE plugins SET about = '在工作区里搜索和阅读页面与数据库，新建和修改页面，查询和整理数据库，读写评论。'
WHERE builtin = 'notion'
  AND about IN ('Notion 官方的 MCP 服务：在工作区里搜索和阅读页面与数据库，新建和修改页面，查询和整理数据库，读写评论。每个人用自己的 Notion 账号授权、选择授权哪个工作区，只能看到和改动这个账号本来就能访问的内容。');
UPDATE plugins SET about = '按关键词、作者、分类和日期搜索 arXiv 上的论文，看作者、摘要和链接，读全文。'
WHERE builtin = 'arxiv'
  AND about IN ('按关键词、作者、分类和日期搜索 arXiv 上的论文，查看论文的作者、摘要、分类和链接，并读全文（优先读 arXiv 生成的 HTML 版，没有时读 PDF）。不需要账号；按 arXiv 的要求，整个服务器每 3 秒最多向 arXiv 发一个请求。', '按关键词、作者、分类和日期搜索 arXiv 上的论文，查看论文的作者、摘要、分类和链接，并读全文（优先读 arXiv 生成的 HTML 版，没有时读 PDF）。按 arXiv 的要求，整个服务器每 3 秒最多向 arXiv 发一个请求。');
UPDATE plugins SET about = '收录 2 亿多篇学术作品：按关键词、年份、类型搜索论文和书，按引用数或时间排序，查一篇论文被谁引用、引用了谁，查学者的论文、引用数和 h 指数。'
WHERE builtin = 'openalex'
  AND about IN ('OpenAlex 是收录 2 亿多篇学术作品的开放目录：按关键词、年份、类型搜索论文和书，按引用数或时间排序，查一篇论文被谁引用、引用了谁，查学者的论文、引用数和 h 指数。OpenAlex 按每日额度计费：不填 API Key 时和同一 IP 的所有人共用很小的免费额度，建议管理员在 openalex.org/settings/api 免费申请一个，填在「请求头」的值里。', 'OpenAlex 是收录 2 亿多篇学术作品的开放目录：按关键词、年份、类型搜索论文和书，按引用数或时间排序，查一篇论文被谁引用、引用了谁，查学者的论文、引用数和 h 指数。和 arXiv 插件互补：arXiv 有预印本全文，OpenAlex 有引用关系。OpenAlex 按每日额度计费：不填 API Key 时和同一 IP 的所有人共用很小的免费额度，建议管理员在 openalex.org/settings/api 免费申请一个，填在「请求头」的值里。');
UPDATE plugins SET about = '查任意城市的实时天气和未来 16 天预报（温度、体感、降水、风、紫外线、日出日落），也能查 1940 年以来的历史天气。'
WHERE builtin = 'weather'
  AND about IN ('查任意城市的实时天气和未来 16 天预报（温度、体感、降水和概率、风、紫外线、日出日落），也能查 1940 年以来的历史天气。数据来自 Open-Meteo，不需要账号；Open-Meteo 对非商业用途免费，商业使用需要购买它的套餐。', '查任意城市的实时天气和未来 16 天预报（温度、体感、降水和概率、风、紫外线、日出日落），也能查 1940 年以来的历史天气。数据来自 Open-Meteo；Open-Meteo 对非商业用途免费，商业使用需要购买它的套餐。');
UPDATE plugins SET about = '只在中国大陆（不含港澳台）可用：查附近门店和菜单、算价格，点外送或到店取餐（下单后给出支付链接，由你自己付款）、取消订单，领优惠券、查积分和兑换，查餐品营养和当月活动。'
WHERE builtin = 'mcdonalds'
  AND about IN ('麦当劳中国官方的 MCP 服务，只在中国大陆（不含港澳台）可用：查附近门店和菜单、算价格、下外送或到店取餐的订单（下单后给出支付链接，由你自己付款）、取消订单，领「麦麦省」优惠券、查自己的券和积分、积分兑换和抽奖，查餐品营养和当月活动。每个人用自己的麦当劳账号：在 open.mcd.cn/mcp 用手机号登录、申请 MCP Token，粘贴到这里。');
UPDATE plugins SET about = '只在中国大陆可用：找附近门店，选饮品和温度、甜度等规格，看优惠后的价格，下自提订单（下单后给出付款二维码，由你自己付款），查询和取消订单。'
WHERE builtin = 'luckin'
  AND about IN ('瑞幸咖啡官方的 MCP 服务，只在中国大陆可用：按位置找附近门店，搜索饮品、选温度甜度等规格，看优惠后的价格，下自提订单（下单后给出付款二维码，由你自己付款），查询和取消订单。每个人用自己的瑞幸账号：在 open.lkcoffee.com 登录、复制 Token，粘贴到这里；Token 有有效期，过期后换一个新的。');
UPDATE plugins SET about = '按语言搜索维基百科条目，读全文或只读导言，看条目在其他语言的标题。中文条目可以读简体或繁体。'
WHERE builtin = 'wikipedia'
  AND about IN ('按语言搜索维基百科条目、读全文或只读导言，并给出条目在其他语言的标题。中文条目默认转成简体，也可以读繁体（台湾、香港）。', '按语言搜索维基百科条目、读全文或只读导言，并给出条目在其他语言的标题。中文条目默认转成简体，也可以读繁体（台湾、香港）。不需要账号。');
UPDATE plugins SET about = '搜索 Hacker News 的帖子和评论（按相关度或时间、按天数和分数筛选），看现在的首页，读一个帖子下的讨论。'
WHERE builtin = 'hackernews'
  AND about IN ('搜索 Hacker News 的帖子和评论（按相关度或时间、按天数和分数筛选），看现在的首页，读一个帖子下的讨论。数据来自 Algolia 的 Hacker News 接口。', '搜索 Hacker News 的帖子和评论（按相关度或时间、按天数和分数筛选），看现在的首页，读一个帖子下的讨论。数据来自 Algolia 的 Hacker News 接口，不需要账号。');
-- +goose StatementEnd

-- +goose Down
-- Nothing to undo: the old texts said nothing a person needed.
SELECT 1;
