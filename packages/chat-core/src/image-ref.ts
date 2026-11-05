/**
 * Pictures the person picked with 「拿来改」 for a message
 * (docs/design/image-generation-plan.md §10). Like a picked skill
 * (skill-mention.ts) the choice travels in the message's text: a line
 * `<image-ref id="img_…"/>` per picture, after the skills and before the
 * words, which the model's instructions say to change as the message asks
 * (api/image_tool.go, imageGuide). The page draws them as chips; the server
 * leaves them out of what it reads as the person's words (personWords).
 */

const TAG = /^<image-ref id="(img_[0-9a-f]{8})"\/>\n/

/** The pictures' lines, then the rest. */
export function imageRefMessage(handles: readonly string[], rest: string): string {
  return [...new Set(handles)].map((handle) => `<image-ref id="${handle}"/>\n`).join('') + rest
}

/** The pictures a message picked, in order, and the rest of it. */
export function parseImageRefs(content: string): { handles: string[]; rest: string } {
  const handles: string[] = []
  let rest = content
  for (let match = TAG.exec(rest); match; match = TAG.exec(rest)) {
    handles.push(match[1]!)
    rest = rest.slice(match[0].length)
  }
  return { handles, rest }
}
