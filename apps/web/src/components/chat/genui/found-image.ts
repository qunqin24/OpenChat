import type { FoundImage } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'
import { useContext } from 'react'

import { API_BASE_URL } from '@/lib/api'
import { ImageSource } from './image-source'

/**
 * A picture an answer shows, as its components give it: an address one of the
 * conversation's sources listed, loaded through the conversation's image
 * proxy (ImageSource), or - with image search on - a description such as
 * "laptop keyboard", for which the server finds a photo, keeps
 * it and serves it (api/pictures.go), so the same words show the same picture
 * on every reload. `found` is that photo, with what its licence asks to be
 * shown. null while it is being found, and for an address no source listed or
 * words that find nothing: the picture is then simply left out.
 */
export interface ShownPicture {
  src: string
  found: FoundImage | null
}

export function usePicture(image: string): { picture: ShownPicture | null; settled: boolean } {
  const resolve = useContext(ImageSource)
  const value = image.trim()
  const address = /^https?:\/\//i.test(value)
  const { image: found, settled } = useFoundImage(address ? '' : value)
  if (address) {
    const src = resolve(value)
    return { picture: src ? { src, found: null } : null, settled: true }
  }
  return { picture: found ? { src: pictureSrc(found), found } : null, settled }
}

/**
 * The photo found for a description. Not in query-keys and never persisted
 * (query-persist keeps only what it names): the server keeps the words'
 * picture, the browser its answer a day.
 */
function useFoundImage(query: string): { image: FoundImage | null; settled: boolean } {
  // Anything else with a scheme (data:, javascript:) is neither an address
  // loaded nor words searched.
  const searchable = query !== '' && !/^[a-z][a-z0-9+.-]*:/i.test(query)
  const found = useQuery({
    queryKey: ['found-image', query.toLowerCase()],
    queryFn: async ({ signal }) => {
      const response = await fetch(`${API_BASE_URL}/images/search?q=${encodeURIComponent(query)}`, { signal, credentials: 'include' })
      if (response.status === 404) return null
      if (!response.ok) throw new SearchError(response.status)
      return (await response.json()) as FoundImage
    },
    enabled: searchable,
    staleTime: Infinity,
    // The service down for a while (503, the server having tried a few times
    // itself) is asked again later, the picture's frame kept meanwhile; words
    // it could not take, or too many asked for, are not.
    retry: (failures, error) => failures < SEARCH_RETRIES.length && !(error instanceof SearchError && error.status !== 503),
    retryDelay: (failures) => SEARCH_RETRIES[Math.min(failures, SEARCH_RETRIES.length) - 1] ?? 0,
  })
  return { image: found.data ?? null, settled: !searchable || found.isSuccess || found.isError }
}

/** The waits, in milliseconds, before asking an unavailable search again. */
const SEARCH_RETRIES = [4_000, 12_000, 30_000]

class SearchError extends Error {
  constructor(readonly status: number) {
    super(`image search ${status}`)
  }
}

function pictureSrc(image: FoundImage): string {
  return `${API_BASE_URL}/pictures/${encodeURIComponent(image.id)}`
}

/** What a found photo's licence asks to be shown: its creator and licence. */
export function credit(image: FoundImage | null): string {
  return image ? [image.creator, image.license].filter(Boolean).join(' · ') : ''
}
