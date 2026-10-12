/**
 * The state of a Choices block (components/chat/genui/choices.tsx), from the
 * messages of its choices and what the page says about its reply.
 *
 * - `taken`: the choice whose message is the one sent after the reply, if any.
 *   While a pick is on its way it is the page's outgoing message, so it is
 *   marked at once; if it is not taken, it goes back to the composer, there is
 *   no next message any more, and the card is open again.
 * - `open`: nothing has been sent after the reply, and the block is finished:
 *   the card offers its button.
 * - `live`: open, and the page lets this reply send (the last answer, nothing
 *   else going on).
 */
export function choicesState(
  messages: string[],
  { complete, next, canPick }: { complete: boolean; next?: string; canPick: boolean },
): { taken: number | null; open: boolean; live: boolean } {
  const index = next === undefined ? -1 : messages.findIndex((message) => message.trim() === next.trim())
  const open = complete && next === undefined
  return { taken: index >= 0 ? index : null, open, live: open && canPick }
}
