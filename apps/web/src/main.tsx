import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode, useEffect } from 'react'
import { createRoot } from 'react-dom/client'

import '@/lib/i18n'
import { queryClient } from '@/lib/query'
import { persistQueryCache, restoreQueryCache } from '@/lib/query-persist'
import { router } from '@/router'
import { applyTheme, useUIStore } from '@/stores/ui-store'

import 'streamdown/styles.css'
import '@/styles/globals.css'

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

const container = document.getElementById('root')
if (!container) throw new Error('#root element is missing from index.html')

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
