import { router } from 'expo-router'
import { useDrawerProgress } from '@react-navigation/drawer'
import { Drawer } from 'expo-router/drawer'
import type { ReactNode } from 'react'
import { StyleSheet } from 'react-native'
import Animated, { interpolate, useAnimatedStyle } from 'react-native-reanimated'

import { ConversationDrawer } from '@/components/chat/ConversationDrawer'
import { ShareInbox } from '@/components/ShareInbox'
import { useShownConversation } from '@/lib/shown'
import { useColors } from '@/theme'

/**
 * The signed-in app: the one chat screen, with the conversation list in a
 * drawer from the left. Opening a conversation or a new chat only changes the
 * screen's address; the screen decides what that means (chat-core/chat-view.ts).
 */
export default function AppLayout() {
  const colors = useColors()
  const shown = useShownConversation((s) => s.id)
  return (
    <>
    <ShareInbox />
    <Drawer
      screenOptions={{
        headerShown: false,
        // The chat is pushed aside, not covered, so its edge stays in view.
        drawerType: 'slide',
        swipeEdgeWidth: 48,
        drawerStyle: { width: '84%', backgroundColor: colors.panel },
        overlayColor: 'transparent',
        sceneStyle: { backgroundColor: colors.panel },
      }}
      screenLayout={({ children }) => <SceneCard>{children}</SceneCard>}
      drawerContent={({ navigation }) => (
        <ConversationDrawer
          current={shown}
          // By path: the drawer may be over the tasks screen, whose params these are not.
          onOpen={(id) => {
            router.navigate({ pathname: '/', params: { id } })
            navigation.closeDrawer()
          }}
          onNewChat={() => {
            router.navigate({ pathname: '/', params: { n: String(Date.now()) } })
            navigation.closeDrawer()
          }}
          onTasks={() => {
            router.navigate('/tasks')
            navigation.closeDrawer()
          }}
          onSettings={() => {
            router.navigate('/settings')
            navigation.closeDrawer()
          }}
        />
      )}
    >
      <Drawer.Screen name="index" />
      <Drawer.Screen name="tasks" />
      <Drawer.Screen name="settings" />
    </Drawer>
    </>
  )
}

/**
 * The screen beside the open drawer, as a card: it draws in a little, rounds
 * its corners, casts a soft shadow onto the drawer and pales, so it reads as
 * set aside rather than cut off at the drawer's edge.
 */
function SceneCard({ children }: { children: ReactNode }) {
  const colors = useColors()
  const progress = useDrawerProgress()
  const card = useAnimatedStyle(() => ({
    borderRadius: interpolate(progress.value, [0, 1], [0, 32]),
    transform: [{ scale: interpolate(progress.value, [0, 1], [1, 0.94]) }],
  }))
  const pale = useAnimatedStyle(() => ({ opacity: interpolate(progress.value, [0, 1], [0, 0.45]) }))
  return (
    <Animated.View style={[styles.card, { backgroundColor: colors.bg }, card]}>
      {children}
      <Animated.View pointerEvents="none" style={[StyleSheet.absoluteFill, { backgroundColor: colors.bg }, pale]} />
    </Animated.View>
  )
}

const styles = StyleSheet.create({
  card: { flex: 1, overflow: 'hidden', boxShadow: '0 0 20px rgba(0, 0, 0, 0.02)' },
})
