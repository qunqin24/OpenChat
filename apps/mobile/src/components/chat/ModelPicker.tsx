import type { ChatModel } from '@openchat/api-client'
import { Check, ChevronDown, ChevronUp } from 'lucide-react-native'
import { useEffect, useRef, useState, type ComponentRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal, Pressable, ScrollView, StyleSheet, Text, useColorScheme, useWindowDimensions, View } from 'react-native'
import Animated, { Easing, useAnimatedStyle, useReducedMotion, useSharedValue, withTiming } from 'react-native-reanimated'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { HeaderPill, PILL } from '@/components/HeaderPill'
import { ModelIcon, preloadModelIcons } from '@/components/ModelIcon'
import { useColors } from '@/theme'

/** Where the pill is on screen, for the menu to grow out of it. */
interface Anchor {
  x: number
  y: number
  width: number
  height: number
}

const MENU_WIDTH = 286
const ROW = 46

/**
 * The model the next run answers with, as the chat's heading: its mark and
 * name in a raised pill beside the drawer button (HeaderPill). Kept in the
 * header rather than the composer, as chat apps on phones do, so the row under
 * the message box holds only what changes from message to message.
 *
 * Tapped, the list grows out of the pill as a menu (as ChatGPT's app has it),
 * not up from the foot: the choice is made where the thumb already is, the
 * page washed pale behind it and the pill drawn again over the wash, so it
 * reads as the menu's own heading. A mark and a name a row; a model at
 * capacity stays in the list, dimmed and marked full (`atCapacity`).
 */
export function ModelPicker({ models, modelId, onPick }: { models: ChatModel[]; modelId: string; onPick: (id: string) => void }) {
  const { t } = useTranslation()
  const colors = useColors()
  const [anchor, setAnchor] = useState<Anchor | null>(null)
  const pill = useRef<ComponentRef<typeof View>>(null)
  const model = models.find((m) => m.id === modelId)
  useEffect(() => preloadModelIcons(models), [models])

  const label = (open: boolean) => (
    <>
      {model ? <ModelIcon providerId={model.providerId} family={model.family} name={model.name} size={18} /> : null}
      <Text style={[styles.name, { color: colors.text }]} numberOfLines={1}>
        {model?.name ?? (modelId || t('chat.model.pick'))}
      </Text>
      {open ? <ChevronUp size={15} color={colors.text3} /> : <ChevronDown size={15} color={colors.text3} />}
    </>
  )

  return (
    <>
      {/* Measured whole, edge and all, so the pill drawn over the wash lands exactly on it. */}
      <View ref={pill} collapsable={false} style={styles.shrink}>
        <HeaderPill>
          <Pressable
            onPress={() => pill.current?.measureInWindow((x, y, width, height) => setAnchor({ x, y, width, height }))}
            accessibilityRole="button"
            accessibilityLabel={t('chat.model.pick')}
            style={({ pressed }) => [styles.pill, pressed && { backgroundColor: colors.field }]}
          >
            {label(false)}
          </Pressable>
        </HeaderPill>
      </View>
      {anchor ? (
        <ModelMenu
          anchor={anchor}
          models={models}
          modelId={modelId}
          heading={label(true)}
          onClose={() => setAnchor(null)}
          onPick={(id) => {
            onPick(id)
            setAnchor(null)
          }}
        />
      ) : null}
    </>
  )
}

function ModelMenu({
  anchor,
  models,
  modelId,
  heading,
  onClose,
  onPick,
}: {
  anchor: Anchor
  models: ChatModel[]
  modelId: string
  heading: ReactNode
  onClose: () => void
  onPick: (id: string) => void
}) {
  const { t } = useTranslation()
  const colors = useColors()
  const dark = useColorScheme() === 'dark'
  const window = useWindowDimensions()
  const insets = useSafeAreaInsets()
  const reduced = useReducedMotion()
  // Grows from the pill: from a little smaller and clear, quickly, eased out.
  const shown = useSharedValue(reduced ? 1 : 0)
  useEffect(() => {
    shown.value = withTiming(1, { duration: 180, easing: Easing.out(Easing.cubic) })
  }, [shown])
  const grow = useAnimatedStyle(() => ({ opacity: shown.value, transform: [{ scale: 0.92 + 0.08 * shown.value }] }))
  const wash = useAnimatedStyle(() => ({ opacity: shown.value }))

  const top = anchor.y + anchor.height + 8
  const left = Math.min(anchor.x, window.width - MENU_WIDTH - 12)
  const room = window.height - top - insets.bottom - 16

  return (
    <Modal visible transparent animationType="none" onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <Animated.View style={[StyleSheet.absoluteFill, { backgroundColor: dark ? 'rgba(0,0,0,0.5)' : 'rgba(255,255,255,0.6)' }, wash]}>
        <Pressable style={StyleSheet.absoluteFill} onPress={onClose} accessibilityLabel={t('chat.conversation.cancel')} />
      </Animated.View>
      {/* The pill again, over the wash: the menu's heading, and a tap on it closes the menu. */}
      <View style={[styles.anchor, { top: anchor.y, left: anchor.x, width: anchor.width, height: anchor.height }]}>
        <HeaderPill style={styles.fill}>
          <Pressable onPress={onClose} accessibilityRole="button" accessibilityLabel={t('chat.model.pick')} style={styles.pill}>
            {heading}
          </Pressable>
        </HeaderPill>
      </View>
      <Animated.View
        style={[
          styles.menu,
          { top, left, maxHeight: room, backgroundColor: colors.card, borderColor: colors.line, transformOrigin: `${Math.max(24, anchor.x - left + 40)}px 0px` },
          grow,
        ]}
      >
        <ScrollView bounces={false} contentContainerStyle={styles.list}>
          {models.length === 0 ? <Text style={[styles.empty, { color: colors.text3 }]}>{t('chat.model.none')}</Text> : null}
          {models.map((m) => {
            const selected = m.id === modelId
            return (
              <Pressable
                key={m.id}
                disabled={m.atCapacity}
                onPress={() => onPick(m.id)}
                accessibilityRole="button"
                accessibilityState={{ selected, disabled: m.atCapacity }}
                accessibilityHint={m.atCapacity ? t('chat.model.at-capacity') : undefined}
                style={({ pressed }) => [
                  styles.row,
                  (selected || pressed) && { backgroundColor: colors.bubble },
                  m.atCapacity && styles.full,
                ]}
              >
                <ModelIcon providerId={m.providerId} family={m.family} name={m.name} size={20} />
                <Text style={[styles.rowName, selected && styles.selected, { color: colors.text }]} numberOfLines={1}>
                  {m.name}
                </Text>
                {m.atCapacity ? (
                  <Text style={[styles.tag, { color: colors.text2, backgroundColor: colors.bubble }]}>{t('chat.model.full')}</Text>
                ) : selected ? (
                  <Check size={18} color={colors.text} />
                ) : null}
              </Pressable>
            )
          })}
        </ScrollView>
      </Animated.View>
    </Modal>
  )
}

const styles = StyleSheet.create({
  shrink: { flexShrink: 1 },
  fill: { flex: 1 },
  pill: { flexDirection: 'row', alignItems: 'center', gap: 6, flexShrink: 1, height: PILL - 2, paddingHorizontal: 16, borderRadius: PILL / 2 },
  name: { fontSize: 16, fontWeight: '500', flexShrink: 1 },
  anchor: { position: 'absolute' },
  menu: {
    position: 'absolute',
    width: MENU_WIDTH,
    borderRadius: 24,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
    boxShadow: '0 18px 48px rgba(0, 0, 0, 0.18)',
  },
  list: { padding: 6 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 12, height: ROW, paddingHorizontal: 12, borderRadius: 16 },
  rowName: { flex: 1, fontSize: 15.5 },
  selected: { fontWeight: '600' },
  full: { opacity: 0.45 },
  tag: { fontSize: 11, paddingHorizontal: 7, paddingVertical: 2, borderRadius: 8, overflow: 'hidden' },
  empty: { fontSize: 14, padding: 14 },
})
