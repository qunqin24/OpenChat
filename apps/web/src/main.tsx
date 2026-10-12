import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode, useEffect } from 'react'
import { createRoot } from 'react-dom/client'
import { toast } from 'sonner'

import i18n from '@/lib/i18n'
import { onBuildChange } from '@/lib/build-check'
import { queryClient, setErrorListener } from '@/lib/query'
import { persistQueryCache, restoreQueryCache } from '@/lib/query-persist'
import { preventZoom } from '@/lib/no-zoom'
import { leaveIfSessionExpired } from '@/lib/session-expired'
import { isWebKit } from '@/lib/webkit'
import { router } from '@/router'
import { applyTheme, useUIStore } from '@/stores/ui-store'

import 'streamdown/styles.css'
import '@/styles/globals.css'
import { followDeletions } from '@/lib/conversation-sync'

function App() {
  const theme = useUIStore((s) => s.theme)
  useEffect(() => applyTheme(theme), [theme])

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

// Last visit's conversation list and recent chats, shown while they refresh.
restoreQueryCache(queryClient)
persistQueryCache(queryClient)
// A session that ended is noticed by whichever call finds out first.
setErrorListener((error) => void leaveIfSessionExpired(error, router))
// The server now serves a newer front end than this page was loaded from: say
// so, and let the reader reload when it suits them (lib/build-check.ts). It
// stays until dismissed or acted on.
onBuildChange(() =>
  toast.info(i18n.t('app.updated.message'), {
    id: 'build-updated',
    duration: Infinity,
    action: { label: i18n.t('app.updated.reload'), onClick: () => window.location.reload() },
  }),
)
followDeletions(queryClient)
preventZoom()
if (isWebKit()) document.documentElement.dataset.webkit = ''

const container = document.getElementById('root')
if (!container) throw new Error('#root element is missing from index.html')

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
