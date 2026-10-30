import en from '../locales/en/common.json'

/**
 * The line a new chat greets the person with (Welcome). Each part of the day
 * has several, some plain, some with the person's name, some playful; Mondays,
 * Fridays and weekends add their own, and on a holiday only its own lines are
 * said. One is drawn at random, never the one shown last time, so opening new
 * chats does not read the same line over and over.
 *
 * The lines are read from the locale file (`chat.welcome.lines.<group>`,
 * `chat.welcome.holidays.<MM-DD>`): adding one there is enough. Whether a line
 * says the name is read from its English text; the locale test keeps the
 * Chinese one's placeholders the same.
 */
export type DayPeriod = 'morning' | 'noon' | 'afternoon' | 'evening' | 'night'

export function dayPeriod(hour: number): DayPeriod {
  if (hour >= 5 && hour < 11) return 'morning'
  if (hour >= 11 && hour < 13) return 'noon'
  if (hour >= 13 && hour < 18) return 'afternoon'
  if (hour >= 18 && hour < 22) return 'evening'
  return 'night'
}

type Lines = typeof en.chat.welcome.lines
type Holidays = typeof en.chat.welcome.holidays
type Group = keyof Lines

export type GreetingKey =
  | { [G in Group]: `chat.welcome.lines.${G}.${keyof Lines[G] & string}` }[Group]
  | { [D in keyof Holidays]: `chat.welcome.holidays.${D}.${keyof Holidays[D] & string}` }[keyof Holidays]

/** A greeting's key, and whether it says the name. */
export interface GreetingLine {
  key: GreetingKey
  named: boolean
}

function linesOf(prefix: string, texts: Record<string, string>): GreetingLine[] {
  return Object.entries(texts).map(([id, text]) => ({
    key: `${prefix}.${id}` as GreetingKey,
    named: text.includes('{{name}}'),
  }))
}

const group = (name: Group) => linesOf(`chat.welcome.lines.${name}`, en.chat.welcome.lines[name])

/** The groups whose lines fit this moment. The small hours belong to the night, whatever the day. */
function groupsFor(period: DayPeriod, day: number): Group[] {
  const groups: Group[] = [period]
  const daytime = period !== 'evening' && period !== 'night'
  if (day === 1 && period === 'morning') groups.push('monday')
  if (day === 5 && daytime) groups.push('friday')
  if (day === 5 && period === 'evening') groups.push('friday-night')
  if ((day === 0 || day === 6) && period !== 'night') groups.push('weekend')
  return groups
}

/** The lines that fit this moment; without a name, only those that do not use one. */
export function greetingLines(now: Date, hasName: boolean): GreetingLine[] {
  const usable = (lines: GreetingLine[]) => (hasName ? lines : lines.filter((line) => !line.named))
  const date = `${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const holidays: Record<string, Record<string, string>> = en.chat.welcome.holidays
  const holiday = holidays[date]
  const festive = holiday ? usable(linesOf(`chat.welcome.holidays.${date}`, holiday)) : []
  if (festive.length > 0) return festive
  return usable(groupsFor(dayPeriod(now.getHours()), now.getDay()).flatMap(group))
}

/** One of the lines at random, other than the last one shown where there is a choice. */
export function pickGreeting(lines: GreetingLine[], last: string | null, random = Math.random): GreetingLine {
  const fresh = lines.length > 1 ? lines.filter((line) => line.key !== last) : lines
  // greetingLines always has a line without the name; the plain one is only
  // for a caller that passed none.
  return fresh[Math.floor(random() * fresh.length)] ?? lines[0] ?? { key: 'chat.welcome.lines.morning.start', named: false }
}
