// The chat interface's colours (apps/web/src/styles/chat-theme.css and, for
// the sign-in fields, auth.css), so the app reads as the same product.

export const light = {
  bg: '#ffffff',
  panel: '#f4f4f4',
  card: '#ffffff',
  text: '#1a1a1a',
  text2: '#6f6f6f',
  text3: '#9a9a9a',
  line: 'rgba(0,0,0,0.08)',
  accent: '#000000',
  accentFg: '#ffffff',
  field: '#f3f3f3',
  fieldFocus: 'rgba(26,26,26,0.22)',
  errInk: '#c0262d',
  errFill: 'rgba(239,68,68,0.08)',
  wait: '#e0a100',
  /** The user's bubble, a code block, a card: a step off the page. */
  bubble: '#f4f4f4',
  code: '#f6f6f6',
  link: '#1a1a1a',
  overlay: 'rgba(0,0,0,0.35)',
}

export type Colors = typeof light

export const dark: Colors = {
  bg: '#161616',
  panel: '#262626',
  card: '#1e1e1e',
  text: '#f5f5f5',
  text2: '#a8a8a8',
  text3: '#7a7a7a',
  line: 'rgba(255,255,255,0.09)',
  accent: '#f5f5f5',
  accentFg: '#000000',
  field: '#222222',
  fieldFocus: 'rgba(245,245,245,0.22)',
  errInk: '#ff8a80',
  errFill: 'rgba(239,68,68,0.12)',
  wait: '#e0a100',
  bubble: '#262626',
  code: '#1e1e1e',
  link: '#f5f5f5',
  overlay: 'rgba(0,0,0,0.55)',
}
