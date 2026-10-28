/**
 * The part of the day a new chat greets the person by (Welcome): its words are
 * `chat.welcome.hello.<period>`. Late at night it is not "good evening" but a
 * nudge to rest.
 */
export type DayPeriod = 'morning' | 'noon' | 'afternoon' | 'evening' | 'night'

export function dayPeriod(hour: number): DayPeriod {
  if (hour >= 5 && hour < 11) return 'morning'
  if (hour >= 11 && hour < 13) return 'noon'
  if (hour >= 13 && hour < 18) return 'afternoon'
  if (hour >= 18 && hour < 22) return 'evening'
  return 'night'
}
