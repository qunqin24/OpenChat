import { useState } from 'react'

import { cn } from '@/lib/utils'

/**
 * Brand marks for the model catalogue.
 *
 * The artwork comes from `@lobehub/icons-static-svg` (MIT). Only the files
 * named below are vendored into `public/model-icons/`, by
 * `scripts/sync-model-icons.mjs` — the upstream package ships 906 of them and
 * bundling the lot to show a few dozen would be absurd. That script reads the
 * three maps in this file, so this file is the single list of what gets copied:
 * add a mapping, re-run `pnpm sync:model-icons`.
 *
 * They are served from our own origin rather than a CDN, for the same reason
 * the fonts are self-hosted: rendering a page should not tell a third party who
 * is looking at it.
 *
 * `@lobehub/icons`, the React package, is deliberately NOT used — it has peer
 * dependencies on `@lobehub/ui` and `antd`, and a third UI framework in a repo
 * that already keeps two design systems apart by hand is not a trade worth
 * making.
 */

/**
 * models.dev provider id -> icon file name.
 *
 * Upstream carries 213 providers and grows; most will never have a mark here,
 * which is what the placeholder is for. A few keys are brands that do not have
 * a provider id today (`baidu`, `yi`, …) but plausibly will — they cost one
 * line each and keep the vendored set matching the brands this catalogue is
 * expected to show.
 */
const PROVIDER_ICONS: Record<string, string> = {
  openai: 'openai',
  anthropic: 'anthropic',
  azure: 'azure-color',
  'azure-cognitive-services': 'azure-color',
  'amazon-bedrock': 'bedrock-color',
  google: 'gemini-color',
  'google-vertex': 'vertexai-color',
  'google-vertex-anthropic': 'vertexai-color',
  deepseek: 'deepseek-color',
  alibaba: 'qwen-color',
  'alibaba-cn': 'qwen-color',
  'alibaba-coding-plan': 'qwen-color',
  'alibaba-coding-plan-cn': 'qwen-color',
  'alibaba-token-plan': 'qwen-color',
  'alibaba-token-plan-cn': 'qwen-color',
  moonshotai: 'moonshot',
  'moonshotai-cn': 'moonshot',
  'kimi-for-coding': 'kimi-color',
  zhipuai: 'zhipu-color',
  'zhipuai-coding-plan': 'zhipu-color',
  zai: 'zai',
  'zai-coding-plan': 'zai',
  minimax: 'minimax-color',
  'minimax-cn': 'minimax-color',
  'minimax-coding-plan': 'minimax-color',
  'minimax-cn-coding-plan': 'minimax-color',
  mistral: 'mistral-color',
  meta: 'meta-color',
  llama: 'meta-color',
  ollama: 'ollama',
  'ollama-cloud': 'ollama',
  openrouter: 'openrouter-color',
  xai: 'xai',
  grok: 'grok',
  groq: 'groq',
  togetherai: 'together-color',
  'fireworks-ai': 'fireworks-color',
  siliconflow: 'siliconcloud-color',
  'siliconflow-cn': 'siliconcloud-color',
  volcengine: 'volcengine-color',
  'volcengine-coding-plan': 'volcengine-color',
  bytedance: 'bytedance-color',
  doubao: 'doubao-color',
  baidu: 'baidu-color',
  tencent: 'tencent-color',
  'tencent-coding-plan': 'hunyuan-color',
  'tencent-token-plan': 'hunyuan-color',
  'tencent-tokenhub': 'hunyuan-color',
  hunyuan: 'hunyuan-color',
  spark: 'spark-color',
  yi: 'yi-color',
  stepfun: 'stepfun-color',
  'stepfun-ai': 'stepfun-color',
  'stepfun-ai-step-plan': 'stepfun-color',
  'stepfun-step-plan': 'stepfun-color',
  baichuan: 'baichuan-color',
  cohere: 'cohere-color',
  perplexity: 'perplexity-color',
  'perplexity-agent': 'perplexity-color',
  cerebras: 'cerebras-color',
  'cloudflare-workers-ai': 'workersai-color',
  'cloudflare-ai-gateway': 'cloudflare-color',
  deepinfra: 'deepinfra-color',
  huggingface: 'huggingface-color',
  nvidia: 'nvidia-color',
  'novita-ai': 'novita-color',
  nebius: 'nebius',
  upstage: 'upstage-color',
  vercel: 'vercel',
  v0: 'v0',
  venice: 'venice-color',
  'snowflake-cortex': 'snowflake-color',
  modelscope: 'modelscope-color',
  inception: 'inception',
  lmstudio: 'lmstudio',
  'github-copilot': 'copilot-color',
  poe: 'poe-color',
  'qiniu-ai': 'qiniu-color',
  sensenova: 'sensenova-color',
  longcat: 'longcat-color',
  morph: 'morph-color',
  baseten: 'baseten',
  friendli: 'friendli',
  crusoe: 'crusoe-color',
  databricks: 'dbrx-color',
  watsonx: 'ibm',
  opencode: 'opencode',
  zenmux: 'zenmux',
  aihubmix: 'aihubmix-color',
  '302ai': 'ai302-color',
  submodel: 'submodel-color',
  inference: 'inference',
  nova: 'nova-color',
  xiaomi: 'xiaomimimo',
  'xiaomi-token-plan-ams': 'xiaomimimo',
  'xiaomi-token-plan-cn': 'xiaomimimo',
  'xiaomi-token-plan-sgp': 'xiaomimimo',
}

/**
 * Vendor brand -> icon file name, for callers that know neither a models.dev
 * provider id nor a model family.
 *
 * The channel form is the one that does: what an operator picks there is a
 * vendor, and `channelTypeDef.IconID` on the server names the brand it should
 * be drawn with — `anthropic` the channel type is drawn with the Claude mark,
 * `dashscope` with Qwen's. Those names are brands, not provider ids, so they
 * cannot go through `PROVIDER_ICONS`: `gemini`, `zhipu`, `moonshot` and
 * `siliconcloud` are all spelled differently by models.dev (`google`,
 * `zhipuai`, `moonshotai`, `siliconflow`), and looking them up there is how
 * half the vendor list ended up as bare letters.
 *
 * Kept here rather than as a file name on the server so that this file stays
 * the single list of what gets vendored, and so that which brands have colour
 * artwork is answered in the same place for every caller.
 */
const BRAND_ICONS: Record<string, string> = {
  openai: 'openai',
  claude: 'claude-color',
  gemini: 'gemini-color',
  openrouter: 'openrouter-color',
  deepseek: 'deepseek-color',
  siliconcloud: 'siliconcloud-color',
  moonshot: 'moonshot',
  zhipu: 'zhipu-color',
  qwen: 'qwen-color',
  volcengine: 'volcengine-color',
  minimax: 'minimax-color',
  grok: 'grok',
  groq: 'groq',
  ollama: 'ollama',
  mistral: 'mistral-color',
  stepfun: 'stepfun-color',
  hunyuan: 'hunyuan-color',
  wenxin: 'wenxin-color',
  mimo: 'xiaomimimo',
  together: 'together-color',
  fireworks: 'fireworks-color',
  nvidia: 'nvidia-color',
  modelscope: 'modelscope-color',
  cerebras: 'cerebras-color',
  lmstudio: 'lmstudio',
}

/**
 * Model family root -> icon file name, consulted only when the provider has no
 * mark of its own. Resellers are the majority of the catalogue: without this a
 * card for Qwen-on-Nebius would be a bare letter, and the model's own brand is
 * the more useful thing to show once the provider's is unknown.
 */
const FAMILY_ICONS: Record<string, string> = {
  claude: 'claude-color',
  gemini: 'gemini-color',
  gemma: 'gemma-color',
  gpt: 'openai',
  o: 'openai',
  codex: 'openai',
  qwen: 'qwen-color',
  glm: 'chatglm-color',
  kimi: 'kimi-color',
  minimax: 'minimax-color',
  llama: 'meta-color',
  deepseek: 'deepseek-color',
  grok: 'grok',
  mistral: 'mistral-color',
  ministral: 'mistral-color',
  devstral: 'mistral-color',
  magistral: 'mistral-color',
  codestral: 'mistral-color',
  pixtral: 'mistral-color',
  nemotron: 'nvidia-color',
  seed: 'doubao-color',
  doubao: 'doubao-color',
  mimo: 'xiaomimimo',
  ling: 'antgroup-color',
  command: 'cohere-color',
  phi: 'microsoft-color',
  nova: 'nova-color',
  flux: 'flux',
  hunyuan: 'hunyuan-color',
  hy: 'hunyuan-color',
  step: 'stepfun-color',
  yi: 'yi-color',
  baichuan: 'baichuan-color',
  internlm: 'internlm-color',
  spark: 'spark-color',
  ernie: 'wenxin-color',
  sonar: 'perplexity-color',
}

/**
 * Upstream families read `claude-opus`, `gemini-flash-lite`, `qwen3.6` — a
 * brand with a variant glued on. Everything from the first hyphen or digit is
 * that variant, so the root is what is left.
 */
function familyRoot(family: string): string {
  const [head = ''] = family.toLowerCase().split('-')
  return head.replace(/[\d.].*$/, '')
}

/**
 * Lobe names the coloured artwork `<brand>-color` and the flat artwork
 * `<brand>`. The flat files are a single path painted with `currentColor`,
 * which inside an `<img>` resolves to black — so those, and only those, get
 * inverted in dark mode.
 */
function isMonochrome(icon: string): boolean {
  return !icon.endsWith('-color')
}

/**
 * Brand first, because a caller that names one has been told exactly which mark
 * to draw; then the provider; then the model's own family, which is the best
 * hint left once the provider is a reseller nobody has a mark for.
 */
function resolveIcon(
  brand?: string,
  providerId?: string,
  family?: string,
  subject: 'vendor' | 'model' = 'vendor',
): string | undefined {
  if (brand && BRAND_ICONS[brand]) return BRAND_ICONS[brand]
  const familyIcon = family ? FAMILY_ICONS[familyRoot(family)] : undefined
  // Standing for a model, the model's own mark wins: Claude Sonnet is drawn
  // with Claude's mark, not with Anthropic's, which says who sells it.
  if (subject === 'model' && familyIcon) return familyIcon
  if (providerId && PROVIDER_ICONS[providerId]) return PROVIDER_ICONS[providerId]
  return familyIcon
}

type ModelIconProps = {
  /**
   * A vendor brand, as `ChannelTypeInfo.iconId` spells it. Wins over the two
   * below: a caller supplying one has been told which mark to draw.
   */
  brand?: string
  /** models.dev provider id, e.g. `anthropic`, `amazon-bedrock`. */
  providerId?: string
  /** models.dev model family; a sharper mark than a reseller's own. */
  family?: string
  /** Falls back to its first character when no mark is known. */
  name?: string
  /**
   * What the mark stands for. `vendor` (the default) prefers the provider's
   * mark; `model` prefers the model family's — an avatar for Claude Sonnet is
   * Claude's mark even when Anthropic, or a reseller, serves it.
   */
  subject?: 'vendor' | 'model'
  className?: string
}

/**
 * A brand mark, or a neutral square with the first letter in it. Most of the
 * catalogue will land on the letter, so it is a designed state rather than an
 * apology: a broken-image glyph must never appear.
 *
 * The styling here leans on neither design system's tokens — it is plain
 * black/white alpha — so the component stays usable from the chat side too.
 */
export function ModelIcon({ brand, providerId, family, name, subject, className }: ModelIconProps) {
  const icon = resolveIcon(brand, providerId, family, subject)
  // A file can go missing if the map moves ahead of the last icon sync; the
  // letter is a better answer than a broken image. Remembering which name
  // failed rather than a bare flag means a different model reusing this
  // component gets its own attempt, with no effect to reset anything.
  const [failedIcon, setFailedIcon] = useState<string>()

  const box = cn(
    'inline-flex size-8 shrink-0 items-center justify-center overflow-hidden rounded-md',
    className,
  )

  if (!icon || icon === failedIcon) {
    const letter = (name ?? providerId ?? brand ?? '').trim().charAt(0).toUpperCase()
    return (
      <span
        className={cn(
          box,
          'bg-black/5 text-xs font-semibold text-black/55 dark:bg-white/10 dark:text-white/65',
        )}
        aria-hidden="true"
      >
        {letter || '?'}
      </span>
    )
  }

  return (
    <img
      src={`/model-icons/${icon}.svg`}
      alt=""
      aria-hidden="true"
      loading="lazy"
      decoding="async"
      width={32}
      height={32}
      onError={() => setFailedIcon(icon)}
      // No inset. There is no background behind the mark to hold it off, and a
      // fixed one ate half of a `size-4` box — which is how the marks in the
      // channel picker came out visibly smaller than the letters beside them.
      // The mark now fills whatever box the caller asked for, which is also
      // what the letter below does, so the two states are the same size.
      className={cn(box, isMonochrome(icon) && 'dark:invert')}
    />
  )
}
