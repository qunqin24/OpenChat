import { createContext } from 'react'

import { isGuestMode } from '@/lib/guest/mode'

/**
 * Where a picture in an answer is loaded from. In a conversation it is the
 * server's image proxy, which serves only addresses the conversation's own
 * sources listed; the address the model wrote is never loaded directly. With
 * no provider — or for an address the provider will not take — there is no
 * picture.
 */
export const ImageSource = createContext<(src: string) => string | null>(() => null)

/**
 * Pictures for one conversation, through the server's image proxy. A guest's
 * conversation is not on the server to check an address against, so theirs go
 * through the proxy that needs none.
 */
export function proxiedImages(apiBase: string, conversationId: string) {
  return (src: string) => {
    if (!/^https?:\/\//.test(src)) return null
    return isGuestMode()
      ? `${apiBase}/guest/images?url=${encodeURIComponent(src)}`
      : `${apiBase}/conversations/${encodeURIComponent(conversationId)}/images?url=${encodeURIComponent(src)}`
  }
}
