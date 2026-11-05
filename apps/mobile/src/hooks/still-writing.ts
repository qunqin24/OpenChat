import { useEffect, useState } from 'react'

import { STILL_WRITING_MS } from '@openchat/chat-core/arrival'

/**
 * Whether a reply that may stall (`active`) has gone STILL_WRITING_MS without
 * growing (the web's useStillWriting): `size` is how much of it there is, and
 * any change starts the wait again.
 */
export function useStillWriting(size: number, active: boolean): boolean {
  const [quietAt, setQuietAt] = useState<number | null>(null)
  useEffect(() => {
    if (!active) return
    const timer = setTimeout(() => setQuietAt(size), STILL_WRITING_MS)
    return () => clearTimeout(timer)
  }, [size, active])
  return active && quietAt === size
}
