import { QueryClientProvider } from '@tanstack/react-query'
import { ShareIntentProvider } from 'expo-share-intent'
import { isOpenChatError } from '@openchat/api-client'
import { Stack } from 'expo-router'
import * as SplashScreen from 'expo-splash-screen'
import { StatusBar } from 'expo-status-bar'
import { useEffect } from 'react'
import { I18nextProvider } from 'react-i18next'
import { StyleSheet, Text, View } from 'react-native'
import { GestureHandlerRootView } from 'react-native-gesture-handler'
import { KeyboardProvider } from 'react-native-keyboard-controller'
import { SafeAreaProvider } from 'react-native-safe-area-context'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/Button'
import { Toaster } from '@/components/Toaster'
import { UpdatePrompt } from '@/components/UpdatePrompt'
import { setUnauthorizedListener } from '@/lib/api'
// Applies the saved theme and language as soon as they are read back.
import '@/lib/appearance'
import i18n from '@/lib/i18n'
import { queryClient, setErrorListener } from '@/lib/query'
import { sessionEnded, showIfAccountDisabled, useSession } from '@/lib/session'
import { useColors } from '@/theme'

void SplashScreen.preventAutoHideAsync()

// The session ending anywhere signs this device out; an account disabled
// anywhere shows why (the web's main.tsx does the same through its listener).
setUnauthorizedListener(() => void sessionEnded())
setErrorListener((error) => {
  if (isOpenChatError(error) && error.status === 401) void sessionEnded()
  else showIfAccountDisabled(error)
})

export default function RootLayout() {
  return (
    // First, as expo-share-intent asks: it reads the share the app was opened with.
    <ShareIntentProvider>
    <GestureHandlerRootView style={styles.fill}>
      <SafeAreaProvider>
        <KeyboardProvider>
          <QueryClientProvider client={queryClient}>
            <I18nextProvider i18n={i18n}>
              <Screens />
            </I18nextProvider>
          </QueryClientProvider>
        </KeyboardProvider>
      </SafeAreaProvider>
    </GestureHandlerRootView>
    </ShareIntentProvider>
  )
}

/**
 * Which screens exist follows the session: signed in, the app; signed out,
 * the sign-in pages; disabled, the page that says why. A guard turning false
 * takes the router to the first screen that is left, so signing in or out
 * needs no navigation of its own.
 */
function Screens() {
  const colors = useColors()
  const session = useSession()
  const state = session.data?.state

  useEffect(() => {
    if (!session.isPending) void SplashScreen.hideAsync()
  }, [session.isPending])

  if (session.isPending) return null
  if (session.isError) return <Unreachable onRetry={() => void session.refetch()} busy={session.isFetching} />

  return (
    <>
      <StatusBar style="auto" />
      <Stack screenOptions={{ headerShown: false, contentStyle: { backgroundColor: colors.bg } }}>
        <Stack.Protected guard={state === 'signed-in'}>
          <Stack.Screen name="(app)" />
        </Stack.Protected>
        <Stack.Protected guard={state === 'signed-out'}>
          <Stack.Screen name="login" />
          <Stack.Screen name="register" />
        </Stack.Protected>
        <Stack.Protected guard={state === 'disabled'}>
          <Stack.Screen name="disabled" />
        </Stack.Protected>
      </Stack>
      <UpdatePrompt />
      <Toaster />
    </>
  )
}

/** Started with a token but could not ask the server about it: kept, and asked again. */
function Unreachable({ onRetry, busy }: { onRetry: () => void; busy: boolean }) {
  const { t } = useTranslation()
  const colors = useColors()
  return (
    <View style={[styles.fill, styles.center, { backgroundColor: colors.bg }]}>
      <StatusBar style="auto" />
      <Text style={[styles.message, { color: colors.text2 }]}>{t('mobile.network')}</Text>
      <Button label={t('mobile.retry')} onPress={onRetry} busy={busy} kind="secondary" />
    </View>
  )
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  center: { justifyContent: 'center', paddingHorizontal: 32, gap: 20 },
  message: { fontSize: 15, lineHeight: 22, textAlign: 'center' },
})
