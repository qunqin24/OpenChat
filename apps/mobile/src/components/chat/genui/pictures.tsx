import type { FoundImage } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'
import { X } from 'lucide-react-native'
import { createContext, useContext, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Image, Linking, Modal, Pressable, StyleSheet, Text, View, type ImageStyle, type StyleProp } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { isHttpUrl } from '@openchat/chat-core/urls'

import { serverUrl } from '@/config'
import { currentToken } from '@/lib/token'

// Pictures in an answer's interface blocks, as the web's genui/found-image.ts
// and image-source.ts: an address goes through the server (for this
// conversation only); a description is found by the server's image search.

const API = `${serverUrl}/api/v1`

/** Turns a picture's address into the server's proxy for it; none outside a conversation. */
export const ImageSource = createContext<(src: string) => string | null>(() => null)

export function proxiedImages(conversationId: string) {
  return (src: string) =>
    isHttpUrl(src) ? `${API}/conversations/${encodeURIComponent(conversationId)}/images?url=${encodeURIComponent(src)}` : null
}

export interface ShownPicture {
  uri: string
  found: FoundImage | null
}

/** Retries for a search the server could not do yet (503), spaced out. */
const SEARCH_RETRIES = [4_000, 12_000, 30_000]

class SearchError extends Error {
  constructor(readonly status: number) {
    super(`image search ${status}`)
  }
}

/** The picture for `image` - an address, or words describing one - and whether looking is over. */
export function usePicture(image: string): { picture: ShownPicture | null; settled: boolean } {
  const resolve = useContext(ImageSource)
  const value = image.trim()
  const address = /^https?:\/\//i.test(value)
  const searchable = !address && value !== '' && !/^[a-z][a-z0-9+.-]*:/i.test(value)
  const found = useQuery({
    queryKey: ['found-image', value.toLowerCase()],
    queryFn: async ({ signal }) => {
      const response = await fetch(`${API}/images/search?q=${encodeURIComponent(value)}`, { signal, headers: authHeaders() })
      if (response.status === 404) return null
      if (!response.ok) throw new SearchError(response.status)
      return (await response.json()) as FoundImage
    },
    enabled: searchable,
    staleTime: Infinity,
    retry: (failures, error) => failures < SEARCH_RETRIES.length && !(error instanceof SearchError && error.status !== 503),
    retryDelay: (failures) => SEARCH_RETRIES[Math.min(failures, SEARCH_RETRIES.length) - 1] ?? 0,
  })
  if (address) {
    const uri = resolve(value)
    return { picture: uri ? { uri, found: null } : null, settled: true }
  }
  const image_ = found.data ?? null
  return {
    picture: image_ ? { uri: `${API}/pictures/${encodeURIComponent(image_.id)}`, found: image_ } : null,
    settled: !searchable || found.isSuccess || found.isError,
  }
}

const authHeaders = (): Record<string, string> => {
  const token = currentToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

/** Who made a found picture and under what licence. */
export const credit = (image: FoundImage | null) => (image ? [image.creator, image.license].filter(Boolean).join(' · ') : '')

/** A picture from the server, carrying the session (the proxy and found pictures want it). */
export function Picture({ picture, style, onMissing, contain }: { picture: ShownPicture; style?: StyleProp<ImageStyle>; onMissing?: () => void; contain?: boolean }) {
  return (
    <Image
      source={{ uri: picture.uri, headers: authHeaders() }}
      style={style}
      resizeMode={contain ? 'contain' : 'cover'}
      onError={onMissing}
      accessibilityIgnoresInvertColors
    />
  )
}

/** A picture full screen, its credit under it and its source page a tap away. */
export function useViewer() {
  const [open, setOpen] = useState<{ picture: ShownPicture; title: string; link?: string } | null>(null)
  const viewer = open ? <Viewer {...open} onClose={() => setOpen(null)} /> : null
  return { viewer, show: (picture: ShownPicture, title: string, link?: string) => setOpen({ picture, title, link }) }
}

function Viewer({ picture, title, link, onClose }: { picture: ShownPicture; title: string; link?: string; onClose: () => void }) {
  const { t } = useTranslation()
  const insets = useSafeAreaInsets()
  const page = picture.found?.landingUrl ?? (link && isHttpUrl(link) ? link : undefined)
  const line = [title, credit(picture.found)].filter(Boolean).join(' · ')
  return (
    <Modal visible transparent animationType="fade" onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <View style={[styles.viewer, { paddingTop: insets.top, paddingBottom: insets.bottom + 12 }]}>
        <Pressable style={styles.close} onPress={onClose} hitSlop={12} accessibilityLabel={t('chat.preview.close')}>
          <X size={24} color="#fff" />
        </Pressable>
        <Picture picture={picture} style={styles.full} contain />
        {line ? (
          <Text style={styles.line} numberOfLines={2} onPress={page ? () => void Linking.openURL(page) : undefined}>
            {line}
          </Text>
        ) : null}
      </View>
    </Modal>
  )
}

const styles = StyleSheet.create({
  viewer: { flex: 1, backgroundColor: '#000' },
  close: { alignSelf: 'flex-end', padding: 16 },
  full: { flex: 1 },
  line: { color: '#ddd', fontSize: 13, textAlign: 'center', paddingHorizontal: 20, paddingTop: 10, textDecorationLine: 'underline' },
})
