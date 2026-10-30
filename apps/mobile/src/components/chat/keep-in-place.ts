import { createContext, useContext, useRef } from 'react'
import type { LayoutChangeEvent } from 'react-native'

/**
 * Scrolls the transcript by a change in height, so what the reader tapped
 * stays where it was. The transcript is an inverted list held at its foot: a
 * part of an answer that grows when opened - the steps, the thinking, the rest
 * of a code block - would otherwise push everything above it up, the tapped
 * line out of view with it. ChatView provides it; elsewhere it does nothing.
 */
export const KeepInPlace = createContext<(delta: number) => void>(() => undefined)

/**
 * For a part that opens and closes: lay `onLayout` on it and call `expect()`
 * as it is toggled, and the change in its height that follows is given back to
 * the transcript. Only a toggle's change counts - growth while an answer is
 * written is the transcript's own to follow.
 */
export function useKeepInPlace() {
  const keep = useContext(KeepInPlace)
  const height = useRef(0)
  const expecting = useRef(false)
  const onLayout = (event: LayoutChangeEvent) => {
    const next = event.nativeEvent.layout.height
    if (expecting.current && height.current > 0) keep(next - height.current)
    expecting.current = false
    height.current = next
  }
  const expect = () => {
    expecting.current = true
  }
  return { onLayout, expect }
}
