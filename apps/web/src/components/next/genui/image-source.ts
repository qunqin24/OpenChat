import { createContext } from 'react'

/**
 * Where a picture in an answer is loaded from. In a conversation it is the
 * server's image proxy, which serves only addresses the conversation's own
 * sources listed; the address the model wrote is never loaded directly. With
 * no provider — or for an address the provider will not take — there is no
 * picture.
 */
export const ImageSource = createContext<(src: string) => string | null>(() => null)

/** Pictures for one conversation, through the server's image proxy. */
export function proxiedImages(apiBase: string, conversationId: string) {
  return (src: string) =>
    /^https?:\/\//.test(src)
      ? `${apiBase}/conversations/${encodeURIComponent(conversationId)}/images?url=${encodeURIComponent(src)}`
      : null
}
