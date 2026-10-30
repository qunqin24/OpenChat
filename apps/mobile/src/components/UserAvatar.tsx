import { useState } from 'react'
import { Image, StyleSheet, Text, View } from 'react-native'

import { avatarColor, avatarInitial } from '@openchat/chat-core/avatar'

import { onServer } from '@/lib/upload'

/**
 * An account's picture (the web's UserAvatar): the uploaded one when there is
 * one - a signed URL of the server's own, so no session goes with it - else
 * the first letter of the name on the account's colour. The letter is also
 * what stays when the picture cannot be had, so the avatar is never broken.
 */
export function UserAvatar({ id, name, email, src, size = 32 }: { id?: string; name?: string; email?: string; src?: string | null; size?: number }) {
  // The address that failed, not a flag: a new picture gets its own try.
  const [failed, setFailed] = useState<string>()
  const picture = src && src !== failed ? src : undefined
  return (
    <View
      style={[styles.round, { width: size, height: size, borderRadius: size / 2, backgroundColor: avatarColor(id || email || name || '') }]}
      accessible={false}
    >
      {picture ? (
        <Image source={{ uri: onServer(picture) }} style={StyleSheet.absoluteFill} onError={() => setFailed(picture)} />
      ) : (
        <Text style={[styles.letter, { fontSize: size * 0.42 }]}>{avatarInitial(name, email)}</Text>
      )}
    </View>
  )
}

const styles = StyleSheet.create({
  round: { overflow: 'hidden', alignItems: 'center', justifyContent: 'center' },
  letter: { color: '#ffffff', fontWeight: '500' },
})
