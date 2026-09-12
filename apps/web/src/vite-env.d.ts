/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string
  /** "true" shows the TanStack Router/Query devtools buttons in dev. */
  readonly VITE_DEVTOOLS?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
