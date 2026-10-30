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
  /** A chart's series, in order (the web's --n-chart-1…10). */
  chart: ['#4f9bfa', '#6cc57f', '#ee8b57', '#f2c94c', '#a78bfa', '#f472b6', '#2dd4bf', '#94a3b8', '#a3e635', '#fb7185'],
  /** A sub-agent's colour, fixed by its id (chat-core `colorOf`; the web's --n-agent-1…8). */
  agents: ['#3d6df2', '#e5484d', '#12a58a', '#8e4ef0', '#e08a1e', '#d6409f', '#0b93c4', '#6a9a1f'],
  info: '#2563eb',
  success: '#16a34a',
  warning: '#b45309',
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
  chart: ['#60a5fa', '#74d68a', '#f59e6b', '#f5d061', '#b69cff', '#f78bc6', '#3ee0c9', '#a8b4c4', '#b5ec4f', '#fd8a9b'],
  agents: ['#6b8ff7', '#f06a6e', '#2cc4a5', '#a77bf5', '#f0a443', '#e56bb7', '#39b2e0', '#8fbf3f'],
  info: '#60a5fa',
  success: '#4ade80',
  warning: '#fbbf24',
}
