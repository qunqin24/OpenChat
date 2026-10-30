/**
 * Mermaid's browser build, as text, for the diagram preview's WebView
 * (components/chat/MermaidPreview.tsx). The file it reads is written on
 * install by scripts/vendor-mermaid.mjs from the pinned `mermaid` package -
 * the web app's version - and is not checked in. Required only when a diagram
 * is first shown.
 */
export function mermaidSource(): string {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  return require('./mermaid.gen.js') as string
}
