import type { components } from '@openchat/api-client'
import { z } from 'zod'

type Schemas = components['schemas']

export type ModerationCategory = Schemas['ModerationCategory']
export type ModerationMode = Schemas['ModerationMode']
export type ModerationOnError = Schemas['ModerationOnError']

/** OpenAI's categories, in the order the server lists them. */
export const MODERATION_CATEGORIES = [
  'sexual',
  'sexual/minors',
  'harassment',
  'harassment/threatening',
  'hate',
  'hate/threatening',
  'illicit',
  'illicit/violent',
  'self-harm',
  'self-harm/intent',
  'self-harm/instructions',
  'violence',
  'violence/graphic',
] as const satisfies readonly ModerationCategory[]

/** What blocks until an administrator chooses (internal/api/moderation_settings.go). */
export const DEFAULT_BLOCKED_CATEGORIES: readonly ModerationCategory[] = [
  'sexual/minors',
  'harassment/threatening',
  'hate/threatening',
  'illicit/violent',
  'self-harm/instructions',
  'violence/graphic',
]

/** The ranges the server enforces too (400 `invalid_moderation_*`). */
export const MAX_STRIKES = 1000
export const MAX_WINDOW_HOURS = 24 * 365

/** `sexual/minors` as it is spelled in a translation key. */
export const categorySlug = (category: ModerationCategory) => category.replace('/', '-')

/** The words of the messages, translated by the form that builds the schema. */
export interface ModerationMessages {
  required: string
  whole: string
  range: (min: number, max: number) => string
  score: string
  url: string
  keyRequired: string
  modelRequired: string
}

/**
 * A whole number field in [min, max]. An empty box is an error and not a 0:
 * `z.coerce.number()` reads "" as 0, which for the strike count would turn a
 * cleared field into "never sanction".
 */
function wholeField(min: number, max: number, messages: ModerationMessages) {
  return z
    .union([z.string(), z.number()])
    .refine((value) => String(value).trim() !== '', { error: messages.required })
    .transform((value) => Number(String(value).trim()))
    .pipe(
      z
        .number({ error: messages.whole })
        .int(messages.whole)
        .min(min, messages.range(min, max))
        .max(max, messages.range(min, max)),
    )
}

/** The minimum score: 0 to 1, where 0 is "use what the service flagged". */
function scoreField(messages: ModerationMessages) {
  return z
    .union([z.string(), z.number()])
    .refine((value) => String(value).trim() !== '', { error: messages.required })
    .transform((value) => Number(String(value).trim()))
    .pipe(z.number({ error: messages.score }).min(0, messages.score).max(1, messages.score))
}

function isHttpUrl(value: string): boolean {
  try {
    const { protocol } = new URL(value)
    return protocol === 'http:' || protocol === 'https:'
  } catch {
    return false
  }
}

/**
 * The moderation form. `keyStored` is whether the server holds an OpenAI key
 * (it never says what it is), and `clearingKey` that this save would remove it:
 * the mode that needs a key is then not allowed without one typed.
 */
export function moderationSchema(messages: ModerationMessages, context: { keyStored: boolean; clearingKey: boolean }) {
  return z
    .object({
      mode: z.enum(['off', 'openai', 'model']),
      openaiApiKey: z.string(),
      openaiBaseUrl: z
        .string()
        .trim()
        .refine((value) => value === '' || isHttpUrl(value), { error: messages.url }),
      model: z.string(),
      categories: z.array(z.enum(MODERATION_CATEGORIES)),
      minScore: scoreField(messages),
      exemptAdmins: z.boolean(),
      onError: z.enum(['allow', 'block']),
      strikes: wholeField(0, MAX_STRIKES, messages),
      strikeWindowHours: wholeField(1, MAX_WINDOW_HOURS, messages),
    })
    .superRefine((values, ctx) => {
      const hasKey = values.openaiApiKey.trim() !== '' || (context.keyStored && !context.clearingKey)
      if (values.mode === 'openai' && !hasKey) {
        ctx.addIssue({ code: 'custom', path: ['openaiApiKey'], message: messages.keyRequired })
      }
      if (values.mode === 'model' && values.model.trim() === '') {
        ctx.addIssue({ code: 'custom', path: ['model'], message: messages.modelRequired })
      }
    })
}
