import { Stack } from 'expo-router'

import { useColors } from '@/theme'

/** Settings: a list of pages, each pushed over it and backed out of. */
export default function SettingsLayout() {
  const colors = useColors()
  return <Stack screenOptions={{ headerShown: false, animation: 'slide_from_right', contentStyle: { backgroundColor: colors.bg } }} />
}
