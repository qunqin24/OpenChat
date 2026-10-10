import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { StrictMode, useEffect } from 'react'
import { createRoot } from 'react-dom/client'

import '@/lib/i18n'
import { queryClient } from '@/lib/query'
import { router } from '@/router'
import { applyTheme, useUIStore } from '@/stores/ui-store'

import 'katex/dist/katex.min.css'
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

const container = document.getElementById('root')
if (!container) throw new Error('#root element is missing from index.html')

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
