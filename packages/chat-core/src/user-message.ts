import { parseQuotes, quoteMessage, type DocumentQuote } from './document-selection'
import { imageRefMessage, parseImageRefs } from './image-ref'
import { parseSkills, skillMessage } from './skill-mention'

/**
 * A person's message as it is stored, taken apart. What they picked for it
 * travels in its text, in this order: the deep research line and a line per
 * skill (skill-mention.ts), a line per picture to change (image-ref.ts), the
 * passages it asks about (document-selection.ts), then the words they typed.
 * Showing, copying and editing a message all want the words alone, and an edit
 * has to put the rest back around them - so both apps go through here rather
 * than each peeling the layers it happens to know about.
 */
export interface UserMessage {
  /** Asked as a deep research. */
  research: boolean
  /** The skills picked with /, in order. */
  skills: string[]
  /** The pictures picked to change (`img_…`), in order. */
  pictures: string[]
  /** The passages it asks about; none for an ordinary message. */
  quotes: DocumentQuote[]
  /** What the person typed. */
  words: string
}

export function parseUserMessage(content: string): UserMessage {
  const { names: skills, rest: afterSkills, research } = parseSkills(content)
  const { handles: pictures, rest } = parseImageRefs(afterSkills)
  const quoted = parseQuotes(rest)
  return { research, skills, pictures, quotes: quoted?.quotes ?? [], words: quoted ? quoted.request : rest }
}

/** The message with its words replaced and everything picked for it kept. */
export function rewordUserMessage(message: UserMessage, words: string): string {
  const quoted = message.quotes.length > 0 ? quoteMessage(message.quotes, words) : words
  return skillMessage(message.skills, imageRefMessage(message.pictures, quoted), message.research)
}
