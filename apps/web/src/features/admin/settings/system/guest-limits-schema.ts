import { z } from 'zod'

/** The most the limits can be set to; the server's (internal/api/guest.go) checks them too. */
export const MAX_HOURLY = 100_000
export const MAX_DAILY = 10_000_000
/** The most a run limit can be set to (internal/api/run_limits.go's maxRunLimit). */
export const MAX_RUNS = 1000

/** The words of the messages, translated by the form that builds the schema. */
export interface LimitMessages {
  required: string
  whole: string
  range: (max: number) => string
}

/**
 * A limit field: a whole number from 0 up to `max`, where 0 is the explicit
 * "no limit". An empty field is *not* 0: `z.coerce.number()` reads "" as 0,
 * which would turn a cleared box into unlimited access - the opposite of what
 * leaving it blank looks like.
 */
export function limitField(max: number, messages: LimitMessages) {
  return z
    .union([z.string(), z.number()])
    .refine((value) => String(value).trim() !== '', { error: messages.required })
    .transform((value) => Number(String(value).trim()))
    .pipe(
      z
        .number({ error: messages.whole })
        .int(messages.whole)
        .min(0, messages.range(max))
        .max(max, messages.range(max)),
    )
}

export function guestLimitsSchema(messages: LimitMessages) {
  return z.object({
    guestHourlyLimit: limitField(MAX_HOURLY, messages),
    guestDailyLimit: limitField(MAX_DAILY, messages),
    guestMaxConcurrentRuns: limitField(MAX_RUNS, messages),
  })
}
