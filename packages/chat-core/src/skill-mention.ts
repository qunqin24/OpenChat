/**
 * Skills the person picked for a message with / in the composer
 * (docs/design/skills-plan.md). Like a quoted passage (document-selection.ts),
 * the choice travels inside the message's text: a line `<skill name="…"/>`
 * per skill at its start, which the model's instructions say to open with the
 * skill tool before anything else (api/agent_setup.go, skillsPrompt), and which
 * the page draws as chips. Nothing new on the server; the transcript, export
 * and edit of the message carry it as text.
 */

const TAG = /^<skill name="([a-z0-9]+(?:-[a-z0-9]+)*)"\/>\n/

/**
 * A message that asks for a deep research ("+" → 深度研究) begins with this line,
 * before any skill (docs/design/deep-research-plan.md): the server drafts a
 * plan for it first (api/research.go).
 */
export const RESEARCH_LINE = '<deep-research/>'

/** The message: the deep research line if asked for, a line per picked skill, then the rest. */
export function skillMessage(names: readonly string[], rest: string, research = false): string {
  return (research ? `${RESEARCH_LINE}\n` : '') + [...new Set(names)].map((name) => `<skill name="${name}"/>\n`).join('') + rest
}

/** Whether a message asks for a deep research, the skills it picked, in order, and the rest of it. */
export function parseSkills(content: string): { names: string[]; rest: string; research: boolean } {
  const names: string[] = []
  let rest = content
  const research = rest.startsWith(`${RESEARCH_LINE}\n`) || rest === RESEARCH_LINE
  if (research) rest = rest.slice(RESEARCH_LINE.length + 1)
  for (let match = TAG.exec(rest); match; match = TAG.exec(rest)) {
    names.push(match[1]!)
    rest = rest.slice(match[0].length)
  }
  return { names, rest, research }
}

/**
 * The `/…` being typed at the caret, for the composer's list of skills: the
 * text after the / where it starts the box or follows a space, up to the
 * caret, and where the / is. Null when the caret is not in one - a path or a
 * link (a/b, https://x) is not.
 */
export function mentionAt(text: string, caret: number): { query: string; start: number } | null {
  const before = text.slice(0, caret)
  const match = /(^|\s)\/([^\s/]*)$/.exec(before)
  if (!match) return null
  return { query: match[2]!.toLocaleLowerCase(), start: caret - match[2]!.length - 1 }
}
