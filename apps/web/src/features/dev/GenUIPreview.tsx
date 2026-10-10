import { useEffect, useState } from 'react'

import { ImageSource } from '@/components/next/genui/image-source'
import { AnswerText } from '@/components/next/MessageItem'

/** A stand-in picture: a gradient, drawn in place, since this page has no sources. */
const art = (from: string, to: string) =>
  `data:image/svg+xml,${encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="500"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient></defs><rect width="800" height="500" fill="url(#g)"/></svg>`,
  )}`

const SAMPLE = `**东京三日游（Tokyo）**的节奏**轻松**，以步行和地铁为主，适合第一次去。先看几个关键数字：

\`\`\`openchat-ui
root = Grid([s1, s2, s3], columns: 3)
s1 = Stat("预计花费", "¥4,800", change: "比上次 -12%", trend: "down", tone: "good")
s2 = Stat("每日步行", "1.2 万步", hint: "建议穿舒适的鞋")
s3 = Stat("景点数", "9")
\`\`\`

### 每天去哪

第一天已经排好，第二天正在调整：

\`\`\`openchat-ui
root = Steps([d1, d2, d3])
d1 = Step("第一天：浅草与上野", detail: "浅草寺、仲见世通、上野公园。", status: "done")
d2 = Step("第二天：涩谷与原宿", detail: "明治神宫、竹下通、**涩谷天空**看夜景。", status: "current")
d3 = Step("第三天：银座与台场")
\`\`\`

第一天的具体时间和花费如下，午餐是当天主要的开销。

| 时间 | 地点 | 花费 |
| --- | --- | ---: |
| 09:00 | 浅草寺 | 0 |
| 12:30 | 仲见世通午餐 | 1500 |
| 15:00 | 上野公园 | 0 |

### 出发前

还有两件事没办完，签证最需要抓紧：

| 事项 | 负责人 | 进度 |
| --- | --- | --- |
| 订机票 | 张伟 | 100% |
| 订酒店 | 李娜 | 75% |
| 办签证 | 王强 | 40% |

\`\`\`openchat-ui
root = Callout("Suica 卡可以坐地铁也能在便利店付款，到机场先办一张。", tone: "info", title: "交通")
\`\`\`

下次想换个玩法，可以从这几个方向挑：

\`\`\`openchat-ui
root = Grid([c1, c2, c3, c4], columns: 2)
c1 = Card(title: "美食之旅", subtitle: "筑地、拉面与居酒屋", icon: "utensils")
c2 = Card(title: "动漫圣地", subtitle: "秋叶原、池袋与吉卜力", icon: "sparkles")
c3 = Card(title: "自然风光", subtitle: "富士山、箱根与温泉", icon: "leaf")
c4 = Card(title: "购物路线", subtitle: "银座、表参道与药妆店", icon: "shopping-cart")
\`\`\`

### 数据与组件

近七天的访问量稳步上升，周日最高：

\`\`\`openchat-ui
root = Chart("line", ["周一", "周二", "周三", "周四", "周五", "周六", "周日"], [v], title: "近七日访问趋势", caption: "演示数据")
v = Series("访问量", [124, 192, 158, 241, 310, 283, 421])
\`\`\`

\`\`\`openchat-ui
root = Grid([d, b], columns: 2)
d = Chart("donut", ["编程", "研究", "写作", "其他"], [s], title: "任务类型分布", unit: " 次")
s = Series("任务", [45, 30, 15, 10])
b = Chart("bar", ["Q1", "Q2", "Q3", "Q4"], [r1, r2], title: "季度营收", unit: " 万")
r1 = Series("2025", [120, 150, 170, 210])
r2 = Series("2026", [140, 185, 220, 260])
\`\`\`

\`\`\`openchat-ui
root = Card([stats, chart], title: "系统监控", subtitle: "实时运行概览 · 模拟界面", icon: "monitor", badge: ok, footer: "最近更新：刚刚")
ok = Badge("正常", tone: "success")
stats = Grid([cpu, mem], columns: 2)
cpu = Stat("CPU 使用率", "24%", change: "-6%", trend: "down", tone: "good")
mem = Stat("内存使用", "3.8 GB")
chart = Chart("area", ["10:00", "10:05", "10:10", "10:15", "10:20", "10:25"], [c, m])
c = Series("CPU", [20, 30, 26, 43, 34, 24])
m = Series("内存", [30, 32, 37, 42, 39, 38])
\`\`\`

\`\`\`openchat-ui
root = Tabs([t1, t2, t3])
t1 = Tab("概览", [Text("展示产品定位、核心功能与使用场景。"), Badge("目前最需要关注", tone: "danger")])
t2 = Tab("性能", [Text("冷启动 **1.2 秒**，内存占用比上一版低 18%。")])
t3 = Tab("安全", [Text("全部数据端到端加密。")])
\`\`\`

\`\`\`openchat-ui
root = Row([a, b2, c2, d2, e])
a = Badge("已完成", tone: "success")
b2 = Badge("进行中", tone: "warning")
c2 = Badge("失败", tone: "danger")
d2 = Badge("测试版", tone: "info")
e = Badge("草稿")
\`\`\`

\`\`\`openchat-ui
root = Checklist([k1, k2, k3], title: "今日开发计划")
k1 = Check("设计主界面", done: true)
k2 = Check("完成接口集成", done: true)
k3 = Check("测试并发布")
\`\`\`

\`\`\`openchat-ui
root = Details("推导过程", [Text("由 $$a + b + c = 12$$ 与 $$a - c = 2$$ 得 $$b = 10 - 2c$$。")])
\`\`\`

注意力机制的核心公式：

$$
\\mathrm{Attention}(Q, K, V) = \\mathrm{softmax}\\left(\\frac{QK^T}{\\sqrt{d_k}}\\right)V
$$

### 图片

\`\`\`openchat-ui
root = Gallery([g1, g2, g3, g4], columns: 2)
g1 = Image("${art('#cbd5e1', '#64748b')}", caption: "清水混凝土住宅", link: "https://www.archdaily.com/")
g2 = Image("${art('#a855f7', '#0ea5e9')}", caption: "赛博朋克街景", link: "https://www.artstation.com/")
g3 = Image("${art('#38bdf8', '#16a34a')}", caption: "阿尔卑斯湖", link: "https://unsplash.com/")
g4 = Image("${art('#fbcfe8', '#a5f3fc')}", caption: "3D 几何", link: "https://dribbble.com/")
\`\`\`

\`\`\`openchat-ui
root = Carousel([k1, k2, k3, k4])
k1 = Card(title: "工作设备", subtitle: "简洁高效的工作空间", image: "${art('#e5e7eb', '#a78bfa')}")
k2 = Card(title: "外设装备", subtitle: "个性化桌面配置", image: "${art('#f97316', '#22d3ee')}")
k3 = Card(title: "未来桌面", subtitle: "沉浸式多任务工作站", image: "${art('#6d28d9', '#ec4899')}")
k4 = Card(title: "极简收纳", subtitle: "线缆与桌面整理", image: "${art('#fde68a', '#f59e0b')}")
\`\`\`

\`\`\`openchat-ui
root = Article("谷歌推出面向工作的 Gemini 智能体", "谷歌云发布面向企业的 Gemini 智能体，可处理任务管理、内容生成和编程。它的目标已经从聊天助手延伸到**能执行实际任务的数字同事**。", link: "https://cloud.google.com/blog", image: "${art('#0f766e', '#22c55e')}", category: "今日头条 · 人工智能", date: "10 月 8 日")
\`\`\`

需要的话，我可以按你的预算再调整。`

/**
 * Generative UI with no model: a sample answer, streamed in at roughly model
 * speed so the partial states can be looked at, or shown whole.
 */
export function GenUIPreview() {
  const [length, setLength] = useState(0)
  const [playing, setPlaying] = useState(true)

  useEffect(() => {
    if (!playing) return
    const timer = window.setInterval(() => {
      setLength((n) => {
        if (n >= SAMPLE.length) {
          setPlaying(false)
          return n
        }
        return Math.min(SAMPLE.length, n + 12)
      })
    }, 40)
    return () => window.clearInterval(timer)
  }, [playing])

  const streaming = length < SAMPLE.length
  return (
    <div className="prism-next" style={{ minHeight: '100vh', background: 'var(--n-bg)', padding: 32 }}>
      <div style={{ maxWidth: 760, margin: '0 auto' }}>
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="button" onClick={() => { setLength(0); setPlaying(true) }}>
            Replay
          </button>
          <button type="button" onClick={() => { setPlaying(false); setLength(SAMPLE.length) }}>
            Show all
          </button>
        </div>
        {/* No conversation here: the sample's pictures are drawn in place. */}
        <ImageSource.Provider value={(src) => (src.startsWith('data:image/') ? src : null)}>
          <AnswerText text={SAMPLE.slice(0, length)} streaming={streaming} />
        </ImageSource.Provider>
      </div>
    </div>
  )
}
