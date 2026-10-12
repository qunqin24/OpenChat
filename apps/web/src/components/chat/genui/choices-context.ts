import { createContext } from 'react'

/**
 * What a Choices block in a reply can do, from the page around it (ChatView):
 * `pick` sends a choice's message as the reader's next message, and is there
 * only on the last answer while nothing else is going on; `next` is the message
 * the reader sent after this reply, if any, to show which choice it was.
 */
export interface ChoicesActions {
  pick?: (message: string) => void
  next?: string
}

export const ChoicesContext = createContext<ChoicesActions>({})
