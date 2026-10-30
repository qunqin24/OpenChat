import { X } from 'lucide-react-native'
import { useTranslation } from 'react-i18next'
import { Linking, Modal, Pressable, StyleSheet, Text, View } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'
import { WebView, type WebViewMessageEvent } from 'react-native-webview'

import { isHttpUrl } from '@openchat/chat-core/urls'

import { useColors } from '@/theme'

interface Props {
  title: string
  html: string
  onMessage?: (event: WebViewMessageEvent) => void
  onClose: () => void
}

/**
 * A page drawn from an answer, full screen: an HTML block as the page it
 * describes (the web's HtmlPreview), or a diagram. It runs in a WebView of its
 * own: scripts on, but it never navigates - a web link it opens goes to the
 * system browser, anything else goes nowhere - and it holds nothing of the
 * app's (the session is a bearer token, not a cookie the page could ride on).
 */
export function PagePreview({ title, html, onMessage, onClose }: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  return (
    <Modal visible animationType="slide" onRequestClose={onClose} statusBarTranslucent navigationBarTranslucent>
      <View style={[styles.page, { backgroundColor: colors.bg, paddingTop: insets.top, paddingBottom: insets.bottom }]}>
        <View style={[styles.bar, { borderBottomColor: colors.line }]}>
          <Text style={[styles.title, { color: colors.text }]}>{title}</Text>
          <Pressable hitSlop={12} onPress={onClose} accessibilityLabel={t('chat.preview.close')}>
            <X size={22} color={colors.text} />
          </Pressable>
        </View>
        <WebView
          style={[styles.web, { backgroundColor: colors.bg }]}
          originWhitelist={['*']}
          source={{ html: withViewport(html) }}
          onMessage={onMessage}
          javaScriptEnabled
          domStorageEnabled={false}
          setSupportMultipleWindows={false}
          allowFileAccess={false}
          onShouldStartLoadWithRequest={(request) => {
            // The document itself loads; every navigation after it is refused.
            if (request.url === 'about:blank' || request.url.startsWith('data:')) return true
            if (isHttpUrl(request.url)) void Linking.openURL(request.url)
            return false
          }}
        />
      </View>
    </Modal>
  )
}

/**
 * A page that does not say how wide it is laid out at the phone's width: a
 * WebView otherwise takes a desktop's and shows it shrunk to a corner (the
 * browser's iframe has the width of the panel instead).
 */
function withViewport(html: string): string {
  if (/<meta[^>]+name=["']?viewport/i.test(html)) return html
  return `<meta name="viewport" content="width=device-width, initial-scale=1">${html}`
}

const styles = StyleSheet.create({
  page: { flex: 1 },
  bar: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  title: { fontSize: 16, fontWeight: '500' },
  web: { flex: 1 },
})
