import { Keyboard } from 'react-native'

/**
 * A modal's back button: with the keyboard up it only lowers the keyboard, as
 * everywhere else on Android - otherwise a form would be closed, and what was
 * typed lost, by the press meant to put the keyboard away.
 */
export function lowerKeyboardOr(close: () => void) {
  if (Keyboard.isVisible()) Keyboard.dismiss()
  else close()
}
