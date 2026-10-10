# models.dev：模型能力目录与 V1 接入策略

> 研究日期：2026-09-13；本篇是讨论阶段的设计记录，**尚未接入 OpenChat 应用**。
> 官方仓库：`anomalyco/models.dev`（旧地址 `sst/models.dev` 重定向至此），MIT。
> 固定源码快照：`2671cfe1743e05e420da45c143b0ec96ae61a8f1`（`dev`，2026-09-13）。
> 方法：官方源码和公开 JSON 接口的只读核对，没有调用模型或使用用户密钥。

## 1. 已采纳的方向

**models.dev 作为 OpenChat 模型能力目录的默认上游数据源。** 新模型的常见能力信息
通过目录刷新获取，减少逐模型手写参数支持表和发布新版本的需要。

它解决的是模型元数据维护。协议转换、连接可用性、产品能力与用户权限仍由 OpenChat 管理。
数据可更新，代码通过稳定接口和显式装配扩展，不做插件系统。

## 2. 公开接口与数据基线

本次只读请求确认以下接口可访问：

| 接口 | 内容 | V1 用法建议 |
| --- | --- | --- |
| [api.json](https://models.dev/api.json) | 按 provider 分组的模型目录，含渠道相关信息和价格 | 作为主要导入源 |
| [models.json](https://models.dev/models.json) | provider-agnostic 模型元数据 | 需要规范模型视图时再使用 |
| [catalog.json](https://models.dev/catalog.json) | models/providers 合并目录 | 不必与 api.json 重复下载 |
| `https://models.dev/logos/{provider}.svg` | 官方文档描述的供应商 logo | 独立展示资源，不影响模型可用性 |

`api.json` 查询时约 4.6MB；数量、价格和内容会随服务更新，不应把观察值当作协议常量。
本次响应有 ETag，并要求缓存重验证；实现应处理 304、超时及服务失败。

**基线区别**：GitHub 源码 commit 可以固定；在线 JSON 是动态数据，不保证与该 commit
在同一瞬间发布。导入时记录获取时间、ETag 和内容摘要，才能追查实际使用的快照。

仓库 MIT 与模型自身的开放权重许可证是两件事；模型条目中的 `license` 不代表拥有
该模型的 API 调用权限，也不代替目录代码/数据的许可核对。

## 3. api.json 的结构

以下省略无关字段，值仅为形状示意，不是对某个线上模型的承诺：

```json
{
  "provider-id": {
    "id": "provider-id",
    "name": "Provider name",
    "env": ["PROVIDER_API_KEY"],
    "npm": "@ai-sdk/openai-compatible",
    "api": "https://api.example.test/v1",
    "models": {
      "upstream-model-id": {
        "id": "upstream-model-id",
        "name": "Display name",
        "reasoning": true,
        "reasoning_options": [{ "type": "effort", "values": ["low", "high"] }],
        "tool_call": true,
        "temperature": true,
        "modalities": { "input": ["text", "image"], "output": ["text"] },
        "limit": { "context": 128000, "output": 8192 }
      }
    }
  }
}
```

目录查询键应是 **`(catalogProviderId, catalogModelId)`**，不能只按显示名称归并。
同名模型在不同渠道的价格、上限、协议和开放情况可能不同。

## 4. 可自动导入的能力与参数信息

主要字段来自固定版本的 [`packages/core/src/schema.ts`][schema]：

| 字段 | 产品用途 | 边界 |
| --- | --- | --- |
| `reasoning` | 模型是否有 reasoning 能力 | 不等于本次请求一定开启，也不等于一定返回可展示文本 |
| `reasoning_options` | 开关、档位或预算控制 | 不直接给出每种协议的 HTTP 字段名 |
| `tool_call` | 函数/工具调用能力 | 不授予模型执行工具的权限 |
| `attachment`、`modalities` | 文件与输入输出模态提示 | 仍要与适配器编码能力、产品功能取交集 |
| `temperature` | 是否提供温度控制 | 不是完整参数范围与组合约束说明 |
| `structured_output` | 结构化输出能力提示 | 具体格式与请求方式由协议适配器决定 |
| `limit.context/input/output` | 预算与上限校验 | 上限不是推荐默认值，不应把每次请求开到最大 |
| `cost` | 成本估算与展示 | 渠道价格、阶梯和 token 口径需明确，实际账单以供应商为准 |
| `knowledge/release_date/last_updated` | 模型信息展示 | 可缺失；知识截止不意味着具备联网能力 |
| `status/experimental/open_weights/family` | 目录筛选和展示 | 不是当前账号的可用性探测 |

### reasoning_options

本次核对的 schema 支持：

- `toggle`：开关。
- `effort`：每个模型给出自己的 values 子集。
- `budget_tokens`：可含 min/max。

schema 的 effort 候选包括 none、minimal、low、medium、high、xhigh、max、default
及允许的空值情形；**UI 应使用当前模型的子集，不能把总枚举全部展示为可用档位**。

适配器仍要将选择映射成 `reasoning_effort`、`reasoning.effort`、thinking budget 等
具体表达；是否显式关闭默认 thinking 也属于协议语义。目录不是完整的厂商请求 schema。

## 5. 缺失字段与渠道映射

本次数据中有模型缺少 cost、temperature、structured_output 或 knowledge。
**缺失表示未知，不能一律转成 false、0 或“免费”。**

建议内部支持 supported / unsupported / unknown，并保留信息来源：目录、管理员覆盖或保守默认。
Go 中可用指针字段或明确枚举避免丢失区别。

供应商元信息中的 `npm` 是 AI SDK 包名，`api` 是端点提示，部分值可含环境变量占位符。
模型还可能覆盖 provider 的 api/npm/shape/body/headers。对我们而言这些都是数据提示：

- 不按 `npm` 安装或加载代码。
- 不让目录刷新自动修改现有连接的 Base URL、凭据和请求头。
- 受控映射可帮助初始化配置；实际 WireAPI 仍由内置适配器与连接配置确定。

源数据的 `base_model` 关系在 `api.json` 生成时已解析，不能假定在线 JSON 仍提供完整
别名关系。企业代理、自定义模型名或目录尚未收录的模型需要显式 mapping/override。

## 6. 推荐的 V1 数据链路

```text
models.dev/api.json
  → 服务端获取与校验
  → 版本化快照 + last-good 缓存
  → 目录模型元数据
  → 本地连接/模型 override
  → 与适配器支持、产品权限取交集
  → EffectiveModel：前端控件、请求校验、上下文预算
```

推荐职责：

- **Catalog Source**：获取公共数据，支持条件请求和有上限重试。
- **Catalog Snapshot Store**：保存原始数据、归一化结果、来源和更新时间。
- **Capability Resolver**：纯函数，合并覆盖，生成有效能力与诊断原因。
- **Connection / Model 配置**：明确目录绑定、上游模型 ID、已开放模型和管理员覆盖。

它们可以属于 `internal/models` 的内部模块，由现有 Go 服务显式装配。
聊天请求读取本地快照，不在每次发送时访问 models.dev。

### 覆盖与硬约束

元数据合并优先级建议为：模型级覆盖 → 连接级覆盖 → models.dev 默认值 → 保守的协议默认值。
false/0 等显式值必须与“没有覆盖”区分，覆盖也应能撤销回继承状态。

随后再应用硬约束：

```text
有效功能 = 合并后的模型能力 ∩ 适配器已实现能力 ∩ 产品与用户允许的能力
```

例如目录标记图片输入为 true，但尚未实现图片编码或附件功能，UI 仍不能开放上传。
目录标记 tool_call 为 true，也不会把写工具或未注册工具加入请求。

## 7. 刷新、失败与新模型上线

建议默认后台每 6 小时刷新一次，并允许管理员手动刷新。这是 OpenChat 拟议策略，
不是 models.dev 对数据时效的承诺。

1. 启动优先用 last-good 快照；可随应用提供固定的初始快照以支持首次离线启动。
2. 有 ETag 时使用 If-None-Match，304 保留当前数据。
3. 新数据先校验、归一化，再原子发布；限制下载体积和解析资源。
4. 网络、解析或校验失败保留旧快照，并显示更新时间/stale 状态。
5. 一次运行固定解析后的模型配置，避免目录刷新在半段响应中改变上限或参数规则。
6. 新目录项可成为候选模型；是否向用户开放仍由产品配置决定。

上游有每小时同步任务，但部分供应商自动补新模型，部分只跟踪已有条目并需要人工补充。
创建同步 PR 也不等于网站已发布更新，因此新模型不保证发布当天立即完整收录。

目录未命中的模型仍可显式添加，常规文本能力按已知协议处理；未知 reasoning 选项、
模态和上限不能猜测为已支持，应允许补齐并给出可解释的状态。

## 8. 拟议验收清单

- 有效快照、304、超时、错误 JSON、新增未知字段、异常体积、旧缓存保留。
- 同 modelId 不同 provider 不被错误合并，自定义代理 mapping 能定位正确条目。
- true / false / unknown、显式覆盖和撤销覆盖的行为正确。
- reasoning toggle/effort/budget 控件与实际请求参数匹配。
- 目录宣称支持而适配器/产品未实现时，后端仍拒绝不支持请求。
- 价格缺失不显示为零，目录价格与真实 usage 口径不重复计算。
- 刷新不会修改凭据/连接端点，也不会改变正在运行请求的配置。
- 新模型通过数据更新获得已实现协议内的能力；全新协议仍需适配器和测试。

本篇只核对公开数据和源码，没有执行这些未来的 OpenChat 验收用例。

## 来源与相关文档

- [固定仓库快照][repo]、[Schema][schema]、[同步入口][sync]、[同步工作流][workflow]、[README][readme]。
- 在线数据：[api.json](https://models.dev/api.json)、[models.json](https://models.dev/models.json)、[catalog.json](https://models.dev/catalog.json)。
- 本地：[研究索引](README.md)、[Pi AI](pi-ai.md)、[Cherry Studio](cherry-studio.md)。

[repo]: https://github.com/anomalyco/models.dev/tree/2671cfe1743e05e420da45c143b0ec96ae61a8f1
[schema]: https://github.com/anomalyco/models.dev/blob/2671cfe1743e05e420da45c143b0ec96ae61a8f1/packages/core/src/schema.ts
[sync]: https://github.com/anomalyco/models.dev/blob/2671cfe1743e05e420da45c143b0ec96ae61a8f1/packages/core/src/sync/index.ts
[workflow]: https://github.com/anomalyco/models.dev/blob/2671cfe1743e05e420da45c143b0ec96ae61a8f1/.github/workflows/sync-models.yml
[readme]: https://github.com/anomalyco/models.dev/blob/2671cfe1743e05e420da45c143b0ec96ae61a8f1/README.md
