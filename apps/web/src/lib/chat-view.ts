/**
 * Which conversation the chat page shows, and which "copy" of the page that is.
 *
 * There is one chat page for the empty new-chat screen and for every
 * conversation (components/next/ChatPage.tsx). Opening a different conversation
 * starts the page over, as a new component instance would: nothing typed,
 * scrolled or chosen on the old one may leak into the new. But sending the first
 * message of a new chat is not "opening another conversation": the page makes
 * the conversation itself, shows it at once, and only then does the address
 * follow. The address changing to the conversation the page already shows must
 * leave the page alone - that is the whole point of the one page, and what this
 * file decides.
 *
 * Pure, so the rules are tested without a router or a DOM.
 */
export interface ChatView {
  /** Bumped whenever the page must start over; used as the page's `key`. */
  epoch: number
  /** The conversation the address named when the view last followed it (none: the new-chat screen). */
  route: string | undefined
  /**
   * The conversation the page shows. It is `route`, except between the page
   * making a conversation and the address catching up with it (or the page
   * giving a conversation up), when the page is ahead of the address.
   */
  shown: string | undefined
}

/** The view of a page that was opened at `route`. */
export function openView(route: string | undefined): ChatView {
  return { epoch: 0, route, shown: route }
}

/**
 * The address now names `route`. Unchanged: nothing happens. Naming what the
 * page shows already (the conversation it just made, or the empty screen after
 * it gave one up): the page stays. Anything else is another conversation being
 * opened - from the sidebar, a search, back or forward, a deep link, a deletion
 * somewhere else - and the page starts over.
 */
export function followAddress(view: ChatView, route: string | undefined): ChatView {
  if (route === view.route) return view
  if (route === view.shown) return { ...view, route }
  return { epoch: view.epoch + 1, route, shown: route }
}

/**
 * The empty page made conversation `id` for the message just sent and shows it
 * from now on. Only an empty page does this: one that shows a conversation is
 * not asked.
 */
export function adopt(view: ChatView, id: string): ChatView {
  return view.shown === undefined ? { ...view, shown: id } : view
}

/**
 * The page gives conversation `id` up and is the empty screen again: the
 * conversation was made for one message, and the server refused that before
 * writing anything, so it was removed. A page that shows something else has
 * moved on, and is left as it is.
 */
export function release(view: ChatView, id: string): ChatView {
  return view.shown === id ? { ...view, shown: undefined } : view
}
