import { useColorScheme } from 'react-native'

import { dark, light, type Colors } from './tokens'

export type { Colors }

/** The colours for the system's light or dark mode. */
export function useColors(): Colors {
  return useColorScheme() === 'dark' ? dark : light
}
