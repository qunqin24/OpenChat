import { useEffect, useState } from 'react'
import { StyleSheet, Text, useColorScheme, View } from 'react-native'
import { SvgXml } from 'react-native-svg'

import { isInkIcon, isMonochrome, resolveIcon } from '@openchat/chat-core/model-icons'

import { serverUrl } from '@/config'
import { useColors } from '@/theme'

/**
 * Marks fetched, by file: each is a few hundred bytes, asked for once while
 * the app runs, so a menu of models shows them at once the next time it opens.
 * Null where there is no such file.
 */
const marks = new Map<string, Promise<string | null>>()

function fetchMark(file: string): Promise<string | null> {
  let mark = marks.get(file)
  if (!mark) {
    mark = fetch(`${serverUrl}/model-icons/${file}.svg`)
      .then((response) => (response.ok ? response.text() : null))
      // A title and a stylesheet are for browsers: the ink an ink mark pins
      // there is given as `color` instead, from the theme.
      .then((svg) => svg?.replace(/<title>[\s\S]*?<\/title>|<style>[\s\S]*?<\/style>/g, '') ?? null)
      .catch(() => null)
    marks.set(file, mark)
  }
  return mark
}

/** Fetches the marks of these models ahead of time, so the model menu opens with them drawn. */
export function preloadModelIcons(models: { providerId?: string | null; family?: string | null }[]) {
  for (const model of models) {
    const icon = resolveIcon(undefined, model.providerId || undefined, model.family || undefined, 'model')
    if (icon) void fetchMark(icon)
  }
}

/**
 * A model's brand mark (the web's ModelIcon, which mark from chat-core's
 * `model-icons.ts`), drawn from the server's copy of the web's files; or,
 * where there is none, the name's first letter on a quiet tile. Flat marks
 * and ink marks are painted with the theme's text colour, as the web inverts
 * or swaps them in its dark theme.
 */
export function ModelIcon({ providerId, family, name, size = 20 }: { providerId?: string; family?: string; name?: string; size?: number }) {
  const colors = useColors()
  const scheme = useColorScheme()
  const icon = resolveIcon(undefined, providerId || undefined, family || undefined, 'model')
  const [drawn, setDrawn] = useState<{ icon: string; xml: string | null } | null>(null)
  useEffect(() => {
    if (!icon) return
    let current = true
    void fetchMark(isInkIcon(icon) && scheme === 'dark' ? `${icon}.dark` : icon).then((xml) => {
      if (current) setDrawn({ icon, xml })
    })
    return () => {
      current = false
    }
  }, [icon, scheme])

  const xml = drawn && drawn.icon === icon ? drawn.xml : undefined
  if (icon && xml) {
    return <SvgXml xml={xml} width={size} height={size} color={isMonochrome(icon) || isInkIcon(icon) ? colors.text : undefined} />
  }
  // Still coming: the room it takes, so the row does not shift when it arrives.
  if (icon && xml === undefined) return <View style={{ width: size, height: size }} />
  return (
    <View style={[styles.letter, { width: size, height: size, borderRadius: size / 4, backgroundColor: colors.bubble }]}>
      <Text style={[styles.letterText, { color: colors.text2, fontSize: size * 0.5 }]}>{(name ?? '?').trim().charAt(0).toUpperCase()}</Text>
    </View>
  )
}

const styles = StyleSheet.create({
  letter: { alignItems: 'center', justifyContent: 'center' },
  letterText: { fontWeight: '600' },
})
