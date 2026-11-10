package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// The cases (docs/design/chat-history-plan.md, section 5): each plants a few
// earlier conversations of a fresh person, among the same forty-odd ordinary
// ones (fillers), and asks one question in a new conversation. What is
// planted is drawn at random for every trial, so an answer cannot be right by
// luck or by what a model remembers from before.
//
// The fillers are newer than everything a case plants except for "pickup":
// more than the 40 conversations the list of recent ones names, so what the
// other cases ask about is in the history and nowhere in the prompt.

type turn struct {
	Role    string // user or assistant
	Content string
}

type plantedConv struct {
	Title string
	At    time.Time // its first message; the next ones a minute apart
	Turns []turn
}

type scenario struct {
	Convs    []plantedConv
	Question string
	Expect   expect
}

type evalCase struct {
	Name string
	// What it measures, for the report.
	About string
	build func(r *rand.Rand, now time.Time) scenario
}

var allCases = []evalCase{
	{Name: "pickup", About: "a new chat picks up yesterday's topic (the list)", build: pickup},
	{Name: "recall", About: "an exact code from an answer two months ago (the tool)", build: recall},
	{Name: "latest", About: "the last of a decision changed twice (the tool, time)", build: latest},
	{Name: "combine", About: "facts from two conversations (the tool, several searches)", build: combine},
	{Name: "restraint", About: "a question about nothing earlier leaves the history alone", build: restraint},
}

func pick[T any](r *rand.Rand, from []T) T { return from[r.IntN(len(from))] }

// pickTwo is two different ones.
func pickTwo[T any](r *rand.Rand, from []T) (T, T) {
	i := r.IntN(len(from))
	j := (i + 1 + r.IntN(len(from)-1)) % len(from)
	return from[i], from[j]
}

func daysAgo(now time.Time, days int, hour int) time.Time {
	d := now.AddDate(0, 0, -days)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, 10, 0, 0, d.Location())
}

var chineseNumber = []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九", "十", "十一", "十二"}

// pickup: yesterday's trip plan, continued without saying where. Knowing which
// plan shows in naming the city, or in naming its province (a museum is often
// the province's: 陕西历史博物馆 for Xi'an, 云南省博物馆 for Kunming).
func pickup(r *rand.Rand, now time.Time) scenario {
	type place struct{ city, province string }
	where := pick(r, []place{
		{"西安", "陕西"}, {"成都", "四川"}, {"厦门", "福建"}, {"青岛", "山东"}, {"长沙", "湖南"},
		{"昆明", "云南"}, {"南京", "江苏"}, {"大理", "云南"}, {"桂林", "广西"}, {"洛阳", "河南"},
	})
	city := where.city
	budget := 1500 + 100*r.IntN(30)
	return scenario{
		Convs: []plantedConv{{
			Title: city + "三日游",
			At:    daysAgo(now, 1, 21),
			Turns: []turn{
				{"user", fmt.Sprintf("帮我规划一个%s三天两晚的行程，预算 %d 元左右，我比较喜欢吃。", city, budget)},
				{"assistant", fmt.Sprintf("好的，这是%s三天两晚的安排：第一天上午到达、入住，下午逛老城区，晚上去夜市；第二天市区经典景点；第三天上午自由活动后返程。", city)},
				{"user", "第一天的安排可以再轻松一点吗？我下午才到。"},
				{"assistant", "可以：第一天只安排入住和晚上的夜市，老城区挪到第三天上午。"},
			},
		}},
		Question: "接着昨天那个旅行计划，第二天我想加一个博物馆，帮我改一下。",
		Expect:   expect{Groups: [][]string{{city, where.province}}, Tool: toolEither},
	}
}

// recall: a password the assistant made up, two months ago.
func recall(r *rand.Rand, now time.Time) scenario {
	words := []string{"Maple", "Otter", "Comet", "Pebble", "Tulip", "Falcon", "Cedar", "Harbor", "Willow", "Ember", "Lantern", "Quartz"}
	first, second := pickTwo(r, words)
	code := fmt.Sprintf("%s-%04d-%s", first, 1000+r.IntN(9000), second)
	return scenario{
		Convs: []plantedConv{{
			Title: "Wi-Fi 访客密码",
			At:    daysAgo(now, 60, 20),
			Turns: []turn{
				{"user", "帮我给家里的 Wi-Fi 设一个访客密码，要好记但不容易被猜到。"},
				{"assistant", "可以用这个：" + code + "\n\n它由两个英文单词和中间四位数字组成，大小写混合，比生日、电话号码难猜得多，读出来也好记。"},
				{"user", "好，就用这个。"},
			},
		}},
		Question: "之前你帮我想的那个 Wi-Fi 访客密码是什么来着？",
		Expect:   expect{Groups: [][]string{{code}}, CaseSensitive: true, Tool: toolMust},
	}
}

// latest: the company party, moved once in time and once in place.
func latest(r *rand.Rand, now time.Time) scenario {
	type place struct{ name, short string }
	places := []place{
		{"滨江国际会议中心", "滨江国际"}, {"西湖国宾馆", "西湖国宾馆"}, {"云栖小镇会展中心", "云栖小镇"},
		{"良渚文化艺术中心", "良渚文化"}, {"湘湖逍遥庄园", "湘湖"}, {"千岛湖开元度假村", "千岛湖"},
	}
	p1, p2 := pickTwo(r, places)
	day1 := 5 + r.IntN(10)
	day2 := day1 + 3 + r.IntN(10)
	d1, d2 := fmt.Sprintf("12月%d日", day1), fmt.Sprintf("12月%d日", day2)
	return scenario{
		Convs: []plantedConv{
			{
				Title: "年会主题",
				At:    daysAgo(now, 50, 14),
				Turns: []turn{
					{"user", fmt.Sprintf("公司年会定在 %s，地点在%s，帮我想几个主题。", d1, p1.name)},
					{"assistant", "几个方向：「乘风破浪」回顾这一年，「星光之夜」偏晚会风格，「同行」强调团队。"},
				},
			},
			{
				Title: "年会通知",
				At:    daysAgo(now, 42, 10),
				Turns: []turn{
					{"user", fmt.Sprintf("年会改期了，改到 %s，地点还是%s。帮我改一下通知。", d2, p1.name)},
					{"assistant", fmt.Sprintf("通知已改：公司年会将于 %s 在%s举行，请大家提前安排好时间。", d2, p1.name)},
				},
			},
			{
				Title: "年会场地",
				At:    daysAgo(now, 35, 16),
				Turns: []turn{
					{"user", fmt.Sprintf("年会地点又换了，改在%s，时间还是 %s。", p2.name, d2)},
					{"assistant", fmt.Sprintf("收到，更新后的安排：%s，%s。", d2, p2.name)},
				},
			},
		},
		Question: "我们公司年会最后定在哪天、在哪里办？",
		Expect: expect{
			Groups: [][]string{{d2, fmt.Sprintf("12月%d号", day2), fmt.Sprintf("十二月%d日", day2)}, {p2.name, p2.short}},
			Tool:   toolMust,
		},
	}
}

// combine: the teacher in one conversation, the exam in another, a month apart.
func combine(r *rand.Rand, now time.Time) scenario {
	instrument := pick(r, []string{"大提琴", "古筝", "长笛", "萨克斯", "小号", "琵琶", "单簧管"})
	surname := pick(r, []string{"庞", "闫", "喻", "祁", "薛", "鲍", "栾", "隋"})
	month := 3 + r.IntN(4)
	level := 4 + r.IntN(6)
	return scenario{
		Convs: []plantedConv{
			{
				Title: "学" + instrument,
				At:    daysAgo(now, 65, 19),
				Turns: []turn{
					{"user", fmt.Sprintf("我最近开始学%s了，老师姓%s，每周六上午上课。", instrument, surname)},
					{"assistant", fmt.Sprintf("很好的开始！刚学%s，每天保持二三十分钟的练习，比周末一次练很久更有用。", instrument)},
				},
			},
			{
				Title: "考级准备",
				At:    daysAgo(now, 33, 21),
				Turns: []turn{
					{"user", fmt.Sprintf("%s考级定在明年%d月，我报的是%d级，有点紧张。", instrument, month, level)},
					{"assistant", "还有几个月，时间够用。先把考试曲目分段练熟，最后一个月再完整地过几遍、录下来听。"},
				},
			},
		},
		Question: "我学的那个乐器，老师姓什么？考级是几月、考几级？",
		Expect: expect{
			Groups: [][]string{
				{"姓" + surname, surname + "老师"},
				{fmt.Sprintf("%d月", month), chineseNumber[month] + "月"},
				{fmt.Sprintf("%d级", level), chineseNumber[level] + "级"},
			},
			Tool: toolMust,
		},
	}
}

// restraint: an ordinary question, with the same history behind it.
func restraint(r *rand.Rand, now time.Time) scenario {
	question := pick(r, []string{
		"用两三句话解释一下什么是光合作用。",
		"把「欲速则不达」翻译成英文，并解释一下意思。",
		"一年中白天最长的是哪一天？简单说一下原因。",
	})
	return scenario{Question: question, Expect: expect{Tool: toolNever}}
}

// fillers are the ordinary conversations every trial's person has had in the
// last four weeks: 44 of them, the newest two days ago. None of them touches
// what a case plants.
func fillers(now time.Time) []plantedConv {
	topics := [][3]string{
		{"请假邮件", "帮我写一封请两天事假的邮件，给部门经理。", "好的，邮件如下：王经理您好，因家中有事，我想于本周四、周五请事假两天……"},
		{"读 CSV", "Python 里怎么读一个 CSV 文件？", "最简单的是用 csv 模块，或者 pandas.read_csv，后者能直接得到表格。"},
		{"番茄炒蛋", "番茄炒蛋怎么做比较好吃？", "先把蛋炒到刚凝固盛出，再炒番茄出汁，加少许糖，最后把蛋倒回去翻匀。"},
		{"周报", "帮我把这周做的事整理成周报：修了登录的 bug，写了接口文档。", "本周工作：一、修复登录相关问题；二、完成接口文档编写。下周计划……"},
		{"Excel 去重", "Excel 里怎么删掉重复的行？", "选中数据，点「数据」里的「删除重复项」，勾选要比较的列即可。"},
		{"跑步膝盖疼", "跑步之后膝盖外侧疼是怎么回事？", "常见原因是髂胫束综合征，建议先减量、拉伸，持续疼痛请去看医生。"},
		{"生日祝福", "给同事写一句生日祝福，轻松一点的。", "生日快乐！愿你新的一岁代码零报错，下班准点走。"},
		{"git 撤销提交", "git 怎么撤销最近一次提交但保留改动？", "用 git reset --soft HEAD~1，改动会留在暂存区。"},
		{"英文自我介绍", "帮我写一段一分钟的英文自我介绍，面试用。", "Hello, my name is ... I have three years of experience in ..."},
		{"绿萝发黄", "家里的绿萝叶子发黄怎么办？", "多半是浇水太多或光照不足，等表土干了再浇，放到明亮的散射光处。"},
		{"正则匹配邮箱", "写一个匹配邮箱地址的正则。", "一个够用的写法：^[\\w.+-]+@[\\w-]+(\\.[\\w-]+)+$，严格校验还是要发验证邮件。"},
		{"会议纪要", "把下面的讨论整理成会议纪要：大家同意下周上线，测试再跑一轮。", "会议纪要：一、结论：下周上线；二、待办：测试补充回归一轮。"},
		{"咖啡因", "一天喝几杯咖啡算多？", "对多数成年人，每天咖啡因不超过 400 毫克，大约三四杯普通咖啡。"},
		{"SQL 分组", "SQL 里怎么统计每个部门的人数？", "SELECT department, count(*) FROM employees GROUP BY department;"},
		{"感谢信", "帮我写一封感谢客户长期合作的短信。", "感谢您一直以来的信任与支持，新的一年我们会继续做好服务……"},
		{"睡眠", "晚上总是醒好几次，有什么办法？", "固定作息、睡前少看屏幕、下午之后不喝咖啡；长期如此建议就医。"},
		{"JS 深拷贝", "JavaScript 怎么深拷贝一个对象？", "现在可以直接用 structuredClone(obj)，函数和 DOM 节点除外。"},
		{"租房合同", "租房合同里要注意哪些条款？", "押金退还条件、维修责任、提前退租的约定、水电燃气的结算方式。"},
		{"演讲开场", "帮我想一个年终分享的开场白。", "大家好，今天不讲大道理，只讲这一年我们踩过的三个坑……"},
		{"猫不吃饭", "猫这两天不怎么吃东西正常吗？", "超过一天完全不吃就要警惕，尤其是成年猫，建议尽快带去看兽医。"},
		{"Docker 端口", "Docker 容器的端口怎么映射到本机？", "运行时加 -p 本机端口:容器端口，例如 -p 8080:80。"},
		{"道歉消息", "帮我写一条给朋友的道歉消息，昨天聚会迟到了。", "昨天让你们等了那么久，真的很抱歉，下次我请大家喝奶茶！"},
		{"理财入门", "刚工作，存钱有什么简单的方法？", "工资到账先存一部分，比如 20%，剩下的再花；先攒够三到六个月的生活费。"},
		{"缩写解释", "KPI 和 OKR 有什么区别？", "KPI 衡量结果是否达标，OKR 设定有挑战的目标和可衡量的关键结果。"},
		{"背单词", "怎么背单词记得牢？", "间隔重复最有效：今天学的，明天、三天后、一周后各复习一次。"},
		{"Linux 查端口", "Linux 怎么查看哪个进程占用了 8080 端口？", "用 lsof -i :8080 或 ss -ltnp | grep 8080。"},
		{"早餐", "有什么十分钟能做好的早餐？", "燕麦加牛奶微波两分钟，配一个水煮蛋和一根香蕉。"},
		{"邮件催办", "怎么礼貌地催同事交材料？", "可以这样写：您好，想跟进一下上次说的材料，周三前方便发我吗？谢谢！"},
		{"标点用法", "中文里破折号和连接号有什么区别？", "破折号「——」占两个字的位置，用于解释说明；连接号「–」连接数字范围等。"},
		{"打印机", "打印机连上了但打不出来怎么办？", "先看打印队列有没有卡住的任务，清掉后重启打印机和电脑再试。"},
		{"文案润色", "帮我润色这句：我们的产品很好用，大家都喜欢。", "我们的产品简单顺手，用过的人都说好。"},
		{"时间管理", "事情太多总是拖延怎么办？", "每天早上只列三件最重要的事，先做最难的那件，其他的再说。"},
		{"Go 并发", "Go 里怎么等待一组 goroutine 结束？", "用 sync.WaitGroup：启动前 Add，结束时 Done，最后 Wait。"},
		{"颈椎", "久坐脖子酸有什么缓解办法？", "每小时起来活动一下，屏幕抬到眼睛高度，做几组缓慢的转头和收下巴。"},
		{"写诗", "写一首关于秋天的四句小诗。", "风过银杏黄，云低雁成行。一杯茶未冷，满城桂花香。"},
		{"手机发热", "手机充电时很烫正常吗？", "边充边玩、快充时会热一些；如果不玩也很烫，换原装充电器试试。"},
		{"面试问题", "面试时问「你的缺点是什么」怎么回答？", "说一个真实但不致命的缺点，再讲你正在怎么改进它。"},
		{"表格公式", "Excel 里怎么按条件求和？", "用 SUMIF，例如 =SUMIF(A:A,\"销售部\",B:B)。"},
		{"搬家清单", "搬家前要准备些什么？", "提前一周打包不常用的东西，办好水电燃气过户，贵重物品随身带。"},
		{"英文邮件结尾", "英文邮件结尾除了 Best regards 还能写什么？", "Kind regards、Many thanks、Cheers（熟人之间）都可以。"},
		{"减脂", "晚饭不吃能减肥吗？", "能少一些热量，但容易饿到半夜加餐；不如晚饭少吃主食、多吃蔬菜和蛋白质。"},
		{"幻灯片配色", "做汇报幻灯片用什么配色好？", "一种主色加一种强调色，其余用黑白灰，整套保持一致。"},
		{"快递丢件", "快递显示签收但我没收到怎么办？", "先问问门卫和邻居，再联系快递员；还找不到就向平台投诉申请理赔。"},
		{"Markdown 表格", "Markdown 怎么画表格？", "用竖线分列，第二行用 --- 分隔表头，例如 | 名称 | 数量 |。"},
	}
	out := make([]plantedConv, len(topics))
	for i, t := range topics {
		// The newest two days ago, one a little over every half day before.
		at := now.Add(-48*time.Hour - time.Duration(i)*13*time.Hour)
		out[i] = plantedConv{Title: t[0], At: at, Turns: []turn{{"user", t[1]}, {"assistant", t[2]}}}
	}
	return out
}
