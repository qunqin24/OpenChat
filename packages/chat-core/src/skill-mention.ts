/**
 * Skills the person picked for a message with `$` in the composer
 * (docs/design/skills-plan.md). Like a quoted passage (document-selection.ts),
 * the choice travels inside the message's text: a line `<skill name="…"/>`
 * per skill at its start, which the model's instructions say to open with the
 * skill tool before anything else (api/agent_setup.go, skillsPrompt), and which
 * the page draws as chips. Nothing new on the server; the transcript, export
 * and edit of the message carry it as text.
 */

const TAG = /^<skill name="([a-z0-9]+(?:-[a-z0-9]+)*)"\/>\n/

/** The message: a line per picked skill, then the rest. */
export function skillMessage(names: readonly string[], rest: string): string {
  return [...new Set(names)].map((name) => `<skill name="${name}"/>\n`).join('') + rest
}

/** The skills a message picked, in order, and the rest of it. */
export function parseSkills(content: string): { names: string[]; rest: string } {
  const names: string[] = []
  let rest = content
  for (let match = TAG.exec(rest); match; match = TAG.exec(rest)) {
    names.push(match[1]!)
    rest = rest.slice(match[0].length)
  }
  return { names, rest }
}

/**
 * The `$name` being typed at the caret, for the picker: the text after a `$`
 * that starts the box or follows a space, up to the caret, and where the `$`
 * is. Null when the caret is not in one.
 */
export function mentionAt(text: string, caret: number): { query: string; start: number } | null {
  const before = text.slice(0, caret)
  const match = /(^|\s)\$([a-z0-9-]*)$/i.exec(before)
  if (!match) return null
  return { query: match[2]!.toLowerCase(), start: caret - match[2]!.length - 1 }
}
