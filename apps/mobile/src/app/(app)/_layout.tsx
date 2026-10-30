import { router } from 'expo-router'
import { Drawer } from 'expo-router/drawer'

import { ConversationDrawer } from '@/components/chat/ConversationDrawer'
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
    <Drawer
      screenOptions={{
        headerShown: false,
        drawerType: 'front',
        swipeEdgeWidth: 48,
        drawerStyle: { width: '84%', backgroundColor: colors.bg },
        overlayColor: colors.overlay,
        sceneStyle: { backgroundColor: colors.bg },
      }}
      drawerContent={({ navigation }) => (
        <ConversationDrawer
          current={shown}
          onOpen={(id) => {
            router.setParams({ id, n: undefined })
            navigation.closeDrawer()
          }}
          onNewChat={() => {
            router.setParams({ id: undefined, n: String(Date.now()) })
            navigation.closeDrawer()
          }}
        />
      )}
    >
      <Drawer.Screen name="index" />
    </Drawer>
  )
}
